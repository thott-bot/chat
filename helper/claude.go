package main

import (
	"bytes"
	"cmp"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

const claudePrefix = "claude:"

var claudeEfforts = []string{"low", "medium", "high", "xhigh", "max"}

var claudeFileTools = map[string]bool{"Edit": true, "Write": true, "MultiEdit": true, "NotebookEdit": true}

type claudeThread struct {
	ID          string        `json:"id"`
	Title       string        `json:"title"`
	Cwd         string        `json:"cwd"`
	Model       string        `json:"model"`
	Effort      string        `json:"effort"`
	Updated     int64         `json:"updated"`
	ReadAt      int64         `json:"readAt"`
	Origin      string        `json:"origin,omitempty"`
	Started     bool          `json:"started"`
	ContextLeft *int          `json:"contextLeft,omitempty"`
	Turns       []payloadTurn `json:"turns"`

	run *claudeRun
	err string
}

type claudeRun struct {
	agentProcess

	since     int64
	activity  string
	act       activity
	tools     map[string]claudeToolUse
	answer    string
	usage     claudeUsage
	model     string
	approvals []*claudeApproval
	queue     []packet
	stopping  bool
	finished  bool
}

type claudeApproval struct {
	id, kind, text, reason string
	input                  json.RawMessage
}

type claudeToolUse struct {
	name  string
	input claudeInput
}

type claudeUsage struct {
	InputTokens              int64 `json:"input_tokens"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
}

type claudeInput struct {
	Command      string `json:"command"`
	Description  string `json:"description"`
	FilePath     string `json:"file_path"`
	NotebookPath string `json:"notebook_path"`
	Pattern      string `json:"pattern"`
	Query        string `json:"query"`
	URL          string `json:"url"`
}

type claudeMessage struct {
	Type            string  `json:"type"`
	Subtype         string  `json:"subtype"`
	RequestID       string  `json:"request_id"`
	ParentToolUseID *string `json:"parent_tool_use_id"`
	Request         struct {
		Subtype     string          `json:"subtype"`
		ToolName    string          `json:"tool_name"`
		DisplayName string          `json:"display_name"`
		Description string          `json:"description"`
		Input       json.RawMessage `json:"input"`
	} `json:"request"`
	Message struct {
		Model   string `json:"model"`
		Content []struct {
			Type      string          `json:"type"`
			ID        string          `json:"id"`
			Text      string          `json:"text"`
			Name      string          `json:"name"`
			Input     json.RawMessage `json:"input"`
			ToolUseID string          `json:"tool_use_id"`
			IsError   bool            `json:"is_error"`
		} `json:"content"`
		Usage *claudeUsage `json:"usage"`
	} `json:"message"`
	Result     string   `json:"result"`
	IsError    bool     `json:"is_error"`
	Errors     []string `json:"errors"`
	ModelUsage map[string]struct {
		ContextWindow int64 `json:"contextWindow"`
	} `json:"modelUsage"`
}

func claudeModels(codexDefault bool) []payloadModel {
	return []payloadModel{
		{ID: claudePrefix + "opus", Label: "Claude Opus", Default: !codexDefault, Effort: "high", Efforts: claudeEfforts},
		{ID: claudePrefix + "sonnet", Label: "Claude Sonnet", Effort: "high", Efforts: claudeEfforts},
	}
}

func (s *session) superviseClaude(now time.Time) {
	if s.claudeCheck != nil {
		select {
		case <-s.claudeCheck:
			s.claudeCheck = nil
		default:
			return
		}
	}
	if now.Before(s.nextClaude) {
		return
	}
	s.nextClaude = now.Add(30 * time.Second)
	done := make(chan struct{})
	s.claudeCheck = done
	go func() {
		s.setClaudeAvailability(checkClaude(s.ctx, s.findAgent("claude")))
		s.write(time.Now())
		close(done)
	}()
}

type claudeAvailability struct {
	path, configDir     string
	installed, loggedIn bool
}

// The auth command reads credentials with Claude Code's own code. Do not call this on the packet loop. A sign-in
// check must not delay a message or a streamed reply.
func checkClaude(ctx context.Context, path string) claudeAvailability {
	if path == "" {
		return claudeAvailability{}
	}
	found := claudeAvailability{installed: true, configDir: claudeConfigDir()}
	cmd := agentCommand(path, "auth", "status")
	cmd.Env = agentEnv(path)
	cmd.WaitDelay = 2 * time.Second
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = io.Discard
	if err := startAgent(cmd); err != nil {
		return found
	}
	waited := make(chan error, 1)
	go func() { waited <- cmd.Wait() }()
	// `claude auth status` answers in about 0.1 s (observed). After 5 s, stop the wait but not the process. The app
	// never stops an agent.
	var err error
	select {
	case err = <-waited:
	case <-ctx.Done():
		return found
	case <-time.After(5 * time.Second):
		return found
	}
	var status struct {
		LoggedIn        bool   `json:"loggedIn"`
		ConfigDirectory string `json:"configDirectory"`
	}
	if err == nil && json.Unmarshal(out.Bytes(), &status) == nil && status.LoggedIn {
		found.path, found.loggedIn = path, true
		if status.ConfigDirectory != "" {
			found.configDir = status.ConfigDirectory
		}
	}
	return found
}

func (s *session) setClaudeAvailability(found claudeAvailability) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dirty = s.dirty || !s.claudeChecked
	s.claudeChecked = true
	if s.claudeInstalled != found.installed || s.claudePath != found.path || s.claudeConfig != found.configDir {
		logf("Claude Code installed=%t signed-in=%t", found.installed, found.loggedIn)
		s.claudeInstalled, s.claudePath, s.claudeConfig = found.installed, found.path, found.configDir
		s.dirty = true
	}
}

func (s *session) loadClaude(path string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dirty = true
	s.claudeFile = path
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var threads []*claudeThread
	if err := json.Unmarshal(data, &threads); err != nil {
		logf("Couldn't read %s: %v", path, err)
		return
	}
	for _, t := range threads {
		s.claude[t.ID] = t
	}
}

func (s *session) saveClaude() {
	threads := slices.SortedFunc(maps.Values(s.claude), func(a, b *claudeThread) int { return cmp.Compare(b.Updated, a.Updated) })
	for _, t := range threads[min(len(threads), maxThreads):] {
		if t.run == nil {
			delete(s.claude, t.ID)
		}
	}
	threads = threads[:min(len(threads), maxThreads)]
	if s.claudeFile == "" {
		return
	}
	data, err := json.MarshalIndent(threads, "", "  ")
	if err == nil {
		err = writeFileAtomic(s.claudeFile, append(data, '\n'))
	}
	if err != nil {
		logf("Couldn't save Claude threads: %v", err)
	}
}

func (s *session) forClaude(p packet) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if p.Type == "create" {
		return strings.HasPrefix(p.Model, claudePrefix)
	}
	return s.claude[p.Thread] != nil
}

func (s *session) handleClaude(p packet) {
	s.mu.Lock()
	start := s.applyClaude(p)
	s.mu.Unlock()
	if start != nil {
		start()
	}
}

// The caller holds s.mu. If p must start Claude Code, applyClaude returns the start. Run it after you release
// s.mu, because it reads Claude's config and starts the process.
func (s *session) applyClaude(p packet) (start func()) {
	s.dirty = true
	t := s.claude[p.Thread]
	if p.Type != "create" && t == nil {
		return nil
	}
	switch p.Type {
	case "create":
		now := time.Now().Unix()
		title := oneLine(p.Text)
		if title == "" {
			title = "Untitled thread"
		}
		t = &claudeThread{
			ID: newUUID(), Title: trimText(title, 120), Cwd: p.Cwd, Model: p.Model, Effort: p.Effort,
			Updated: now, ReadAt: now, Origin: fmt.Sprintf("%s:%d", p.Session, int64(p.Seq)),
		}
		s.claude[t.ID] = t
		logf("Started Claude session %s in %s for %s", t.ID, t.Cwd, t.Origin)
		start = s.claudeSend(t, p)
	case "reply":
		start = s.claudeSend(t, p)
	case "approve", "deny":
		s.claudeAnswer(t, p.Approval, p.Type == "approve")
	case "stop":
		s.claudeStop(t)
	case "read":
		t.ReadAt = t.Updated
	case "rename":
		title := trimText(oneLine(p.Title), 120)
		if title == "" || title == t.Title {
			return nil
		}
		if err := renameClaudeSession(firstOf(s.claudeConfig, claudeConfigDir()), t.ID, title); err != nil {
			t.err = oneLine("Couldn't rename the conversation: " + err.Error())
			logf("Couldn't rename Claude session %s: %v", t.ID, err)
			return nil
		}
		t.Title = title
		t.err = ""
	default:
		return nil
	}
	s.saveClaude()
	return start
}

func (s *session) claudeSend(t *claudeThread, p packet) func() {
	t.err = ""
	if r := t.run; r != nil && !r.finished {
		r.queue = append(r.queue, p)
		logf("Queued the player's message for Claude session %s", t.ID)
		return nil
	}
	if s.claudePath == "" {
		t.err = claudeSignInNote
		return nil
	}
	t.addUser(p)
	args := []string{"-p", "--input-format", "stream-json", "--output-format", "stream-json", "--verbose",
		"--model", strings.TrimPrefix(t.Model, claudePrefix), "--permission-prompt-tool", "stdio"}
	if t.Effort != "" {
		args = append(args, "--effort", t.Effort)
	}
	if t.Started {
		args = append(args, "--resume", t.ID)
	} else {
		args = append(args, "--session-id", t.ID)
	}
	// Set t.run before the start, so the reader treats the first events and an early exit as the current run.
	r := &claudeRun{tools: map[string]claudeToolUse{}, since: time.Now().Unix()}
	t.run = r
	path, configDir, cwd := s.claudePath, firstOf(s.claudeConfig, claudeConfigDir()), t.Cwd
	return func() {
		names := claudeServerNames(configDir, cwd)
		err := r.start(path, cwd, args, func(line []byte) { s.claudeEvent(t, r, line) }, func() { s.claudeExited(t, r) })
		s.mu.Lock()
		defer s.mu.Unlock()
		s.dirty = true
		if err != nil {
			t.run, t.err = nil, oneLine("Claude couldn't start: "+err.Error())
			logf("Couldn't start Claude for session %s: %v", t.ID, err)
			return
		}
		if t.run == r {
			r.act.serverNames = names
			s.claudeWrite(t, r, p)
		}
	}
}

func (t *claudeThread) addUser(p packet) {
	now := time.Now().Unix()
	t.Updated, t.ReadAt = now, now
	t.addTurns(payloadTurn{Role: "user", Text: p.Text, At: now, Context: true})
}

func (t *claudeThread) addTurns(turns ...payloadTurn) {
	for i := range turns {
		turns[i].Text = trimText(turns[i].Text, maxText)
	}
	t.Turns = append(t.Turns, turns...)
	t.Turns = t.Turns[max(0, len(t.Turns)-maxTurnEntries):]
}

func (s *session) claudeWrite(t *claudeThread, r *claudeRun, p packet) {
	r.since = time.Now().Unix()
	msg := map[string]any{"type": "user", "message": map[string]string{"role": "user", "content": prompt(p)}}
	if err := r.send(msg); err != nil {
		logf("Couldn't send the player's message to Claude session %s: %v", t.ID, err)
		return
	}
	logf("Sent the player's message to Claude session %s", t.ID)
}

func (s *session) claudeAnswer(t *claudeThread, id string, allow bool) {
	r := t.run
	i := -1
	if r != nil {
		i = slices.IndexFunc(r.approvals, func(a *claudeApproval) bool { return a.id == id })
	}
	if i < 0 {
		logf("Approval %s is no longer pending", id)
		return
	}
	a := r.approvals[i]
	r.approvals = slices.Delete(r.approvals, i, i+1)
	decision := map[string]any{"behavior": "deny", "message": "The player denied this from the game."}
	if allow {
		decision = map[string]any{"behavior": "allow", "updatedInput": a.input}
	}
	msg := map[string]any{"type": "control_response", "response": map[string]any{"subtype": "success", "request_id": a.id, "response": decision}}
	if err := r.send(msg); err != nil {
		logf("Couldn't answer approval %s: %v", a.id, err)
		return
	}
	logf("Player chose %s for approval %s in Claude session %s", decision["behavior"], a.id, t.ID)
}

func (s *session) claudeStop(t *claudeThread) {
	r := t.run
	if r == nil || r.finished {
		logf("Nothing is running in Claude session %s", t.ID)
		return
	}
	r.stopping, r.approvals = true, nil
	msg := map[string]any{"type": "control_request", "request_id": "stop-" + newUUID(), "request": map[string]string{"subtype": "interrupt"}}
	if err := r.send(msg); err != nil {
		logf("Couldn't stop Claude session %s: %v", t.ID, err)
		return
	}
	logf("Stopped the turn in Claude session %s", t.ID)
}

func (s *session) claudeExited(t *claudeThread, r *claudeRun) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || t.run != r {
		return
	}
	t.run = nil
	s.dirty = true
	if r.finished {
		return
	}
	reason := r.exitReason()
	logf("Claude stopped in session %s: %s", t.ID, reason)
	t.addTurns(r.entries(time.Now().Unix())...)
	t.err, t.Updated = oneLine("Claude stopped: "+reason), time.Now().Unix()
	s.saveClaude()
}

func (s *session) claudeEvent(t *claudeThread, r *claudeRun, line []byte) {
	var m claudeMessage
	json.Unmarshal(line, &m)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || t.run != r {
		return
	}
	s.dirty = true
	switch m.Type {
	case "system":
		t.Started = t.Started || m.Subtype == "init"
	case "assistant":
		if m.ParentToolUseID != nil {
			return
		}
		t.Started = true
		if m.Message.Usage != nil {
			r.usage, r.model = *m.Message.Usage, m.Message.Model
		}
		for _, c := range m.Message.Content {
			switch c.Type {
			case "text":
				r.answer, r.activity = c.Text, "Writing"
			case "thinking":
				r.activity = "Thinking"
			case "tool_use":
				var in claudeInput
				json.Unmarshal(c.Input, &in)
				r.tools[c.ID] = claudeToolUse{c.Name, in}
				r.activity = describeClaude(c.Name, in, r.act.serverNames)
			}
		}
	case "user":
		for _, c := range m.Message.Content {
			if tool, ok := r.tools[c.ToolUseID]; ok && c.Type == "tool_result" && !c.IsError && m.ParentToolUseID == nil {
				r.act.addClaude(tool.name, tool.input, t.Cwd)
			}
		}
	case "control_request":
		if m.Request.Subtype != "can_use_tool" {
			logf("Declined Claude request %s: the addon can't answer it", m.Request.Subtype)
			r.send(map[string]any{"type": "control_response", "response": map[string]any{"subtype": "error", "request_id": m.RequestID, "error": "thott.bot chat can't answer " + m.Request.Subtype}})
			return
		}
		r.approvals = append(r.approvals, claudeApprovalFor(m, t.Cwd))
		logf("Claude asks the player to approve in session %s: %s", t.ID, m.Request.ToolName)
	case "control_cancel_request":
		r.approvals = slices.DeleteFunc(r.approvals, func(a *claudeApproval) bool { return a.id == m.RequestID })
	case "result":
		s.claudeFinish(t, r, m)
	}
}

func claudeApprovalFor(m claudeMessage, cwd string) *claudeApproval {
	var in claudeInput
	json.Unmarshal(m.Request.Input, &in)
	a := &claudeApproval{id: m.RequestID, input: m.Request.Input, kind: "command",
		text: firstOf(in.Command, m.Request.DisplayName, m.Request.ToolName), reason: m.Request.Description}
	if claudeFileTools[m.Request.ToolName] {
		a.kind, a.text, a.reason = "file", relPath(firstOf(in.FilePath, in.NotebookPath), cwd), ""
	}
	if len(a.input) == 0 {
		a.input = json.RawMessage(`{}`)
	}
	return a
}

func (s *session) claudeFinish(t *claudeThread, r *claudeRun, m claudeMessage) {
	now := time.Now().Unix()
	t.addTurns(r.entries(now)...)
	if window := m.ModelUsage[r.model].ContextWindow; window > 0 {
		used := r.usage.InputTokens + r.usage.CacheCreationInputTokens + r.usage.CacheReadInputTokens
		left := int(math.Round(float64(max(window-used, 0)) / float64(window) * 100))
		t.ContextLeft = &left
	}
	if m.IsError && !r.stopping {
		t.err = oneLine(firstOf(m.Result, strings.Join(m.Errors, "; "), "Claude couldn't finish the turn."))
		logf("Claude reported an error in session %s: %s", t.ID, t.err)
	}
	t.Updated = now
	r.act, r.answer, r.activity, r.approvals, r.stopping = activity{serverNames: r.act.serverNames}, "", "", nil, false
	if len(r.queue) > 0 {
		next := r.queue[0]
		r.queue = r.queue[1:]
		t.addUser(next)
		s.claudeWrite(t, r, next)
	} else {
		r.finished = true
		r.close()
	}
	s.saveClaude()
}

func (r *claudeRun) entries(now int64) []payloadTurn {
	var out []payloadTurn
	if summary := r.act.summary(); summary != "" {
		out = append(out, payloadTurn{Role: "activity", Text: summary, Items: slices.Clone(r.act.details)})
	}
	if r.answer != "" {
		out = append(out, payloadTurn{Role: "assistant", Text: r.answer, At: now})
	}
	return out
}

func (t *claudeThread) payload() payloadThread {
	p := payloadThread{
		ID: t.ID, Agent: "claude", Title: t.Title, State: "idle", Project: filepath.Base(t.Cwd), Cwd: t.Cwd,
		Model: t.Model, Effort: t.Effort, ContextLeft: t.ContextLeft, Updated: t.Updated,
		Unread: t.Updated > t.ReadAt, Origin: t.Origin, Turns: t.Turns,
	}
	if r := t.run; r != nil && !r.finished {
		p.State, p.Since, p.Activity = "active", r.since, r.activity
		p.Turns = append(slices.Clone(t.Turns), r.entries(r.since)...)
		if len(r.approvals) > 0 {
			a := r.approvals[0]
			p.State, p.Approval = "needs_you", &payloadApproval{ID: a.id, Kind: a.kind, Text: a.text, Reason: a.reason}
		}
	}
	if t.err != "" {
		p.State, p.Error = "error", t.err
	}
	return p
}

// The app never stops Claude Code. Claude Code finishes and saves a running turn itself, then exits when its input
// closes.
func (s *session) detachClaude() {
	s.mu.Lock()
	s.closed = true
	var runs []*claudeRun
	for _, t := range s.claude {
		if t.run != nil {
			runs = append(runs, t.run)
		}
	}
	s.mu.Unlock()
	for _, r := range runs {
		r.close()
	}
}

func (a *activity) addClaude(name string, in claudeInput, cwd string) {
	switch name {
	case "Bash":
		a.commands++
		a.detail("Ran " + in.Command)
	case "Read":
		a.reads++
		a.detail("Read " + relPath(in.FilePath, cwd))
	case "Edit", "MultiEdit", "NotebookEdit":
		a.edits++
		a.detail("Edited " + relPath(firstOf(in.FilePath, in.NotebookPath), cwd))
	case "Write":
		a.edits++
		a.detail("Wrote " + relPath(in.FilePath, cwd))
	case "Glob", "Grep":
		a.searches++
		a.detail("Searched for " + in.Pattern)
	case "WebSearch":
		a.web++
		a.detail("Searched the web for " + in.Query)
	case "WebFetch":
		a.tools++
		a.detail("Fetched " + in.URL)
	default:
		a.tools++
		a.detail(claudeTool(name, a.serverNames))
	}
}

func describeClaude(name string, in claudeInput, serverNames map[string]string) string {
	switch name {
	case "Bash":
		cmd, _, _ := strings.Cut(in.Command, "\n")
		return trimText("Running "+cmd, 80)
	case "Read":
		return "Reading " + filepath.Base(in.FilePath)
	case "Edit", "MultiEdit", "Write", "NotebookEdit":
		return "Editing " + filepath.Base(firstOf(in.FilePath, in.NotebookPath))
	case "Glob", "Grep":
		return "Searching"
	case "WebSearch", "WebFetch":
		return "Searching the web"
	}
	if strings.HasPrefix(name, "mcp__") {
		return claudeTool(name, serverNames)
	}
	return "Using " + name
}

func claudeTool(name string, serverNames map[string]string) string {
	if parts := strings.SplitN(name, "__", 3); len(parts) == 3 && parts[0] == "mcp" {
		return toolLine(firstOf(serverNames[parts[1]], parts[1]), parts[2])
	}
	return "Used " + name
}

func newUUID() string {
	var b [16]byte
	rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}
