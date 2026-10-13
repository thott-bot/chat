package main

import (
	"encoding/json"
	"fmt"
	"math"
	"path/filepath"
	"slices"
	"strings"
)

type cxThread struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	Preview         string   `json:"preview"`
	Cwd             string   `json:"cwd"`
	Model           string   `json:"model"`
	ReasoningEffort string   `json:"reasoningEffort"`
	UpdatedAt       int64    `json:"updatedAt"`
	Status          cxStatus `json:"status"`
}

type cxStatus struct {
	Type        string   `json:"type"`
	ActiveFlags []string `json:"activeFlags,omitempty"`
}

type cxTurn struct {
	ID          string   `json:"id"`
	Items       []cxItem `json:"items"`
	Status      string   `json:"status"`
	StartedAt   int64    `json:"startedAt"`
	CompletedAt int64    `json:"completedAt"`
	Error       *struct {
		Message string `json:"message"`
	} `json:"error"`
}

type cxItem struct {
	Type string `json:"type"`
	ID   string `json:"id"`

	Content        json.RawMessage `json:"content"`
	Text           string          `json:"text"`
	Command        string          `json:"command"`
	CommandActions []struct {
		Type    string `json:"type"`
		Command string `json:"command"`
		Name    string `json:"name"`
		Path    string `json:"path"`
		Query   string `json:"query"`
	} `json:"commandActions"`
	Changes []struct {
		Path string `json:"path"`
		Kind struct {
			Type string `json:"type"`
		} `json:"kind"`
	} `json:"changes"`
	Server string `json:"server"`
	Tool   string `json:"tool"`
	Query  string `json:"query"`
}

type threadState struct {
	info        cxThread
	turns       []cxTurn
	live        *cxTurn
	fetched     int64
	fetchedAt   int64
	stale       bool
	activeTurn  string
	since       int64
	activity    string
	fileChanges map[string]string
	origin      string
	readAt      int64
	err         string
	contextLeft *int
	serverNames map[string]string
}

type approval struct {
	id     string
	rpcID  json.RawMessage
	client *codexClient
	thread string
	kind   string
	text   string
	reason string
}

func stateFor(s cxStatus) string {
	switch s.Type {
	case "active":
		for _, flag := range s.ActiveFlags {
			if flag == "waitingOnApproval" || flag == "waitingOnUserInput" {
				return "needs_you"
			}
		}
		return "active"
	case "idle":
		return "idle"
	case "systemError":
		return "error"
	default:
		return "history"
	}
}

// The same as Codex's percent_of_context_window_remaining, with its BASELINE_TOKENS (prompts, tools and room to
// compact). Source: codex-rs/protocol/src/protocol.rs.
func contextLeft(used, window int64) int {
	const baseline = 12000
	if window <= baseline {
		return 0
	}
	effective := window - baseline
	remaining := max(effective-max(used-baseline, 0), 0)
	return int(math.Round(float64(remaining) / float64(effective) * 100))
}

func threadTitle(t cxThread, turns []payloadTurn) string {
	title := strings.TrimSpace(t.Name)
	if title == "" {
		preview, _ := stripContext(t.Preview)
		title = oneLine(preview)
	}
	if i := slices.IndexFunc(turns, func(e payloadTurn) bool { return e.Role == "user" }); title == "" && i >= 0 {
		title = oneLine(turns[i].Text)
	}
	if title == "" {
		return "Untitled thread"
	}
	return trimText(title, 120)
}

func (t *threadState) payload(a *approval) payloadThread {
	turns := transcript(t.allTurns(), t.info.Cwd, t.serverNames)
	p := payloadThread{
		ID:          t.info.ID,
		Agent:       "codex",
		Title:       threadTitle(t.info, turns),
		State:       stateFor(t.info.Status),
		Project:     filepath.Base(t.info.Cwd),
		Cwd:         t.info.Cwd,
		Model:       t.info.Model,
		Effort:      t.info.ReasoningEffort,
		ContextLeft: t.contextLeft,
		Updated:     t.info.UpdatedAt,
		Unread:      t.info.UpdatedAt > t.readAt,
		Origin:      t.origin,
		Turns:       turns,
	}
	if p.State == "idle" && t.live != nil && t.live.Status == "inProgress" {
		p.State = "active"
	}
	if a != nil {
		p.State = "needs_you"
		p.Approval = &payloadApproval{ID: a.id, Kind: a.kind, Text: a.text, Reason: a.reason}
	}
	if p.State == "active" || p.State == "needs_you" {
		p.Activity, p.Since = t.activity, t.since
	}
	switch {
	case t.err != "":
		p.State, p.Error = "error", t.err
	case p.State == "error":
		p.Error = "Codex reported an error in this thread."
	}
	return p
}

func (t *threadState) allTurns() []cxTurn {
	if t.live == nil {
		return t.turns
	}
	turns := slices.Clone(t.turns)
	for i, turn := range turns {
		if turn.ID == t.live.ID {
			if len(t.live.Items) > len(turn.Items) {
				turns[i] = *t.live
			}
			return turns
		}
	}
	return append(turns, *t.live)
}

func (t *threadState) addLive(turnID string, it cxItem) {
	if t.live == nil || t.live.ID != turnID {
		t.live = &cxTurn{ID: turnID, Status: "inProgress"}
	}
	for i := range t.live.Items {
		if t.live.Items[i].ID == it.ID {
			t.live.Items[i] = it
			return
		}
	}
	t.live.Items = append(t.live.Items, it)
}

func (t *threadState) setTurns(turns []cxTurn) {
	t.turns = turns
	if t.live == nil || t.live.Status == "inProgress" {
		return
	}
	for _, turn := range turns {
		if turn.ID == t.live.ID && len(turn.Items) >= len(t.live.Items) {
			t.live = nil
			return
		}
	}
}

func transcript(turns []cxTurn, cwd string, serverNames map[string]string) []payloadTurn {
	var out []payloadTurn
	for _, turn := range turns {
		act := activity{serverNames: serverNames}
		var answer string
		for _, it := range turn.Items {
			switch it.Type {
			case "userMessage":
				text, hadContext := stripContext(userText(it.Content))
				out = append(out, payloadTurn{Role: "user", Text: text, At: turn.StartedAt, Context: hadContext})
			case "agentMessage":
				answer = it.Text
			default:
				act.add(it, cwd)
			}
		}
		if summary := act.summary(); summary != "" {
			out = append(out, payloadTurn{Role: "activity", Text: summary, Items: act.details})
		}
		if answer != "" {
			at := turn.CompletedAt
			if at == 0 {
				at = turn.StartedAt
			}
			out = append(out, payloadTurn{Role: "assistant", Text: answer, At: at})
		}
		if turn.Status == "failed" && turn.Error != nil && turn.Error.Message != "" {
			out = append(out, payloadTurn{Role: "error", Text: turn.Error.Message, At: turn.CompletedAt})
		}
	}
	return out
}

func userText(content json.RawMessage) string {
	var inputs []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	json.Unmarshal(content, &inputs)
	var parts []string
	for _, in := range inputs {
		if in.Type == "text" && in.Text != "" {
			parts = append(parts, in.Text)
		}
	}
	return strings.Join(parts, "\n")
}

type activity struct {
	reads, lists, searches, commands, edits, tools, web int
	details                                             []string
	serverNames                                         map[string]string
}

func (a *activity) add(it cxItem, cwd string) {
	switch it.Type {
	case "commandExecution":
		if len(it.CommandActions) == 0 {
			a.commands++
			a.detail("Ran " + it.Command)
		}
		for _, c := range it.CommandActions {
			switch c.Type {
			case "read":
				a.reads++
				a.detail("Read " + relPath(firstOf(c.Path, c.Name), cwd))
			case "listFiles":
				a.lists++
				a.detail("Listed " + relPath(firstOf(c.Path, c.Command), cwd))
			case "search":
				a.searches++
				a.detail("Searched for " + firstOf(c.Query, c.Command))
			default:
				a.commands++
				a.detail("Ran " + firstOf(c.Command, it.Command))
			}
		}
	case "fileChange":
		for _, c := range it.Changes {
			a.edits++
			verb := map[string]string{"add": "Created", "delete": "Deleted"}[c.Kind.Type]
			if verb == "" {
				verb = "Edited"
			}
			a.detail(verb + " " + relPath(c.Path, cwd))
		}
	case "mcpToolCall":
		a.tools++
		a.detail(toolLine(firstOf(a.serverNames[it.Server], it.Server), it.Tool))
	case "dynamicToolCall":
		a.tools++
		a.detail("Used " + it.Tool)
	case "webSearch":
		a.web++
		a.detail("Searched the web for " + it.Query)
	}
}

func (a *activity) detail(line string) {
	line, _, _ = strings.Cut(strings.TrimSpace(line), "\n")
	if len(a.details) < maxDetailItems {
		a.details = append(a.details, trimText(line, 160))
	}
}

func (a *activity) summary() string {
	var parts []string
	add := func(n int, one, many string) {
		if n == 1 {
			parts = append(parts, one)
		} else if n > 1 {
			parts = append(parts, fmt.Sprintf(many, n))
		}
	}
	add(a.reads, "read 1 file", "read %d files")
	add(a.lists, "listed 1 folder", "listed %d folders")
	add(a.searches, "searched 1 time", "searched %d times")
	add(a.commands, "ran 1 command", "ran %d commands")
	add(a.edits, "changed 1 file", "changed %d files")
	add(a.tools, "used 1 tool", "used %d tools")
	add(a.web, "searched the web", "searched the web %d times")
	s := strings.Join(parts, ", ")
	if s == "" {
		return ""
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func describe(it cxItem, serverNames map[string]string) string {
	switch it.Type {
	case "commandExecution":
		cmd, _, _ := strings.Cut(it.Command, "\n")
		return trimText("Running "+cmd, 80)
	case "fileChange":
		if len(it.Changes) > 0 {
			return "Editing " + filepath.Base(it.Changes[0].Path)
		}
		return "Editing files"
	case "mcpToolCall":
		return toolLine(firstOf(serverNames[it.Server], it.Server), it.Tool)
	case "dynamicToolCall":
		return "Using " + it.Tool
	case "webSearch":
		return "Searching the web"
	case "reasoning":
		return "Thinking"
	case "agentMessage":
		return "Writing"
	}
	return ""
}

func firstOf(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func relPath(path, cwd string) string {
	if cwd != "" {
		if rel, err := filepath.Rel(cwd, path); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel) {
			return filepath.ToSlash(rel)
		}
	}
	return path
}
