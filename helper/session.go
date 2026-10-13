package main

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

const (
	offlineNote      = "The thott.bot chat app isn't running."
	restartNote      = "Restart WoW to finish setup."
	claudeSignInNote = "Sign in to the Claude Code CLI on this computer."
)

var codexNotes = map[string]string{
	"missing":    "Install the Codex CLI or the Claude Code CLI.",
	"signed-out": "Sign in to the Codex CLI on this computer.",
	"error":      "Codex isn't running. thott.bot chat is starting it.",
}

type session struct {
	ctx       context.Context
	wowDir    string
	onTick    func()
	origins   map[string]string
	findAgent func(name string) string

	mu              sync.Mutex
	codex           string
	models          []payloadModel
	threads         map[string]*threadState
	order           []string
	approvals       []*approval
	failed          []payloadThread
	acks            map[string][]int64
	hello           time.Time
	codexErr        string
	writeErr        string
	watchErr        string
	listWanted      bool
	accountWanted   bool
	mcpWanted       map[string]bool
	serverNames     map[string]string
	claude          map[string]*claudeThread
	claudePath      string
	claudeInstalled bool
	claudeConfig    string
	claudeFile      string
	claudeChecked   bool
	listed          bool
	dirty           bool
	closed          bool

	writeMu sync.Mutex
	writer  *payloadWriter

	// Only the run loop uses these fields, so they need no lock.
	client      *codexClient
	queue       []queued
	lastList    time.Time
	lastAccount time.Time
	nextStart   time.Time
	backoff     time.Duration
	nextClaude  time.Time
	claudeCheck chan struct{}
}

type queued struct {
	packet
	received time.Time
}

type sessionView struct {
	codex           string
	claude          bool
	claudeInstalled bool
	hello           time.Time
	threads         int
	problem         string
}

func newSession(wowDir string, origins map[string]string, onTick func()) *session {
	return &session{
		ctx: context.Background(), wowDir: wowDir, origins: origins, onTick: onTick, findAgent: findInstalled, codex: "error",
		threads: map[string]*threadState{}, claude: map[string]*claudeThread{},
		acks: map[string][]int64{}, dirty: true,
		mcpWanted: map[string]bool{}, serverNames: map[string]string{},
	}
}

func (s *session) view() sessionView {
	s.mu.Lock()
	defer s.mu.Unlock()
	return sessionView{codex: s.codex, claude: s.claudePath != "", claudeInstalled: s.claudeInstalled, hello: s.hello, threads: len(s.order) + len(s.claude), problem: firstOf(s.watchErr, s.writeErr, s.codexErr)}
}

func (s *session) run(ctx context.Context) {
	s.ctx = ctx
	raw, packets := make(chan packet, 64), make(chan packet, 64)
	s.writer = &payloadWriter{path: payloadPath(s.wowDir)}
	s.loadClaude(filepath.Join(configDir(), "claude-threads.json"))
	watched := make(chan struct{})
	go func() {
		if err := watchOutbox(ctx, filepath.Join(s.wowDir, "WTF", "Account"), raw); err != nil {
			s.mu.Lock()
			s.watchErr = "Couldn't watch WoW's files: " + oneLine(err.Error()) + ". Restart thott.bot chat after checking folder access."
			s.mu.Unlock()
			logf("%s", s.view().problem)
			s.onTick()
		}
		close(watched)
	}()
	delivered := make(chan struct{})
	go func() {
		s.deliver(ctx, raw, packets)
		close(delivered)
	}()
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			<-watched
			<-delivered
			s.shutdown()
			return
		case p := <-packets:
			s.receive(p)
		case <-tick.C:
		}
		now := time.Now()
		s.superviseCodex(now)
		s.superviseClaude(now)
		s.drain(now)
		s.write(time.Now())
		s.refresh(now)
		s.write(now)
		s.onTick()
	}
}

// deliver runs apart from the frame reader, because the frame reader must never wait on a file write. The addon
// changes AddOns.txt every 40 ms, and a missed frame drops the packet.
func (s *session) deliver(ctx context.Context, raw <-chan packet, out chan<- packet) {
	for {
		select {
		case <-ctx.Done():
			return
		case p := <-raw:
			s.ack(p)
			s.write(time.Now())
			select {
			case out <- p:
			case <-ctx.Done():
				return
			}
		}
	}
}

func (s *session) shutdown() {
	s.detachClaude()
	if s.client != nil {
		s.client.close()
		s.client = nil
		logf("Closed the Codex connection; Codex finishes on its own")
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	s.mu.Lock()
	p := payload{V: protocolVersion, Helper: version, Codex: "offline", Note: offlineNote, Acks: s.ackList()}
	s.mu.Unlock()
	if _, err := s.writer.write(p, time.Now()); err != nil {
		logf("Couldn't write the offline payload: %v", err)
	}
}

func (s *session) receive(p packet) {
	logf("Packet %s #%d (%s) from %s", p.Type, int64(p.Seq), p.Session, p.File)
	if p.Type == "hello" {
		s.mu.Lock()
		s.hello = time.Now()
		s.mu.Unlock()
	}
	s.queue = append(s.queue, queued{p, time.Now()})
}

func (s *session) drain(now time.Time) {
	pending := s.queue[:0]
	for _, q := range s.queue {
		if q.Type != "hello" && q.Type != "read" && !s.forClaude(q.packet) && !s.ready() {
			if now.Sub(q.received) < 10*time.Minute {
				pending = append(pending, q)
			} else {
				logf("Dropped %s #%d: Codex wasn't ready for ten minutes", q.Type, int64(q.Seq))
			}
			continue
		}
		s.handle(q.packet)
	}
	clear(s.queue[len(pending):])
	s.queue = pending
}

// Keep a list of each session's newest seqs, not only the highest. Then a packet lost between two delivered packets
// does not read as delivered.
func (s *session) ack(p packet) {
	s.mu.Lock()
	defer s.mu.Unlock()
	seq, list := int64(p.Seq), s.acks[p.Session]
	if slices.Contains(list, seq) {
		return
	}
	list = append(list, seq)
	s.acks[p.Session] = list[max(0, len(list)-maxAcks):]
	s.dirty = true
	for len(s.acks) > 16 {
		delete(s.acks, slices.Min(slices.Collect(maps.Keys(s.acks))))
	}
}

func (s *session) ackList() map[string][]int64 {
	out := make(map[string][]int64, len(s.acks))
	for session, seqs := range s.acks {
		out[session] = slices.Clone(seqs)
	}
	return out
}

func (s *session) handle(p packet) {
	if p.Type == "create" {
		if why := s.refuse(p); why != "" {
			logf("Didn't start the task from %s #%d: %s", p.Session, int64(p.Seq), why)
			s.failTask(p, agentFor(p.Model), "thott.bot chat didn't start this task: "+why+".")
			return
		}
	}
	if s.forClaude(p) {
		s.handleClaude(p)
		return
	}
	switch p.Type {
	case "hello":
		logf("Addon %s is running", p.Addon)
	case "read":
		s.mu.Lock()
		if t := s.threads[p.Thread]; t != nil {
			t.readAt = t.info.UpdatedAt
			s.dirty = true
		}
		s.mu.Unlock()
	case "create":
		s.create(p)
	case "reply":
		s.reply(p)
	case "approve", "deny":
		s.answer(p, p.Type == "approve")
	case "stop":
		s.stopTurn(p)
	case "rename":
		s.renameThread(p)
	default:
		logf("Ignored unknown packet type %q", p.Type)
	}
}

func (s *session) claudeThreads() []*claudeThread {
	return slices.SortedFunc(maps.Values(s.claude), func(a, b *claudeThread) int { return cmp.Compare(b.Updated, a.Updated) })
}

func (s *session) offeredModels() []payloadModel {
	if s.claudePath == "" {
		return s.models
	}
	return append(slices.Clone(s.models), claudeModels(slices.ContainsFunc(s.models, func(m payloadModel) bool { return m.Default }))...)
}

func (s *session) offeredProjects() []payloadProject {
	var projects []payloadProject
	seen := map[string]bool{}
	add := func(cwd string) {
		if cwd != "" && !seen[cwd] {
			seen[cwd] = true
			projects = append(projects, payloadProject{Label: filepath.Base(cwd), Cwd: cwd})
		}
	}
	for _, id := range s.order {
		add(s.threads[id].info.Cwd)
	}
	for _, t := range s.claudeThreads() {
		add(t.Cwd)
	}
	if home, err := os.UserHomeDir(); err == nil && s.claudePath != "" && len(projects) == 0 {
		add(home)
	}
	return projects
}

func (s *session) payload() payload {
	p := payload{V: protocolVersion, Helper: version, Codex: s.codex, Note: codexNotes[s.codex], Acks: s.ackList(), Models: s.offeredModels(), Projects: s.offeredProjects()}
	p.Starting = !s.claudeChecked || !s.listed && s.codex != "missing" && s.codex != "signed-out" && s.codexErr == ""
	if s.claudePath != "" {
		if s.codex == "missing" {
			p.Note = ""
		}
	} else if s.codex == "missing" && s.claudeInstalled {
		p.Note = claudeSignInNote
	}
	pending := map[string]*approval{}
	for _, a := range slices.Backward(s.approvals) {
		pending[a.thread] = a
	}
	var threads []payloadThread
	for _, id := range s.order {
		threads = append(threads, s.threads[id].payload(pending[id]))
	}
	for _, t := range s.claudeThreads() {
		threads = append(threads, t.payload())
	}
	slices.SortStableFunc(threads, func(a, b payloadThread) int { return cmp.Compare(b.Updated, a.Updated) })
	p.Threads = append(slices.Clone(s.failed), threads...)
	return p
}

// Write the file under writeMu, outside s.mu. Then the acker and the loop do not hold the shared state while the disk
// or a Windows reader is slow.
func (s *session) write(now time.Time) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	s.mu.Lock()
	if !s.dirty || s.closed {
		s.mu.Unlock()
		return
	}
	p := s.payload()
	s.dirty = false
	s.mu.Unlock()
	_, err := s.writer.write(p, now)
	s.mu.Lock()
	defer s.mu.Unlock()
	if err == nil {
		s.writeErr = ""
		return
	}
	s.dirty = true
	msg := "Couldn't write the payload: " + err.Error()
	if msg != s.writeErr {
		logf("%s", msg)
	}
	s.writeErr = msg
}

// The addon adopts the failed thread by its origin.
func (s *session) failTask(p packet, agent, why string) {
	origin := fmt.Sprintf("%s:%d", p.Session, int64(p.Seq))
	title, _, _ := strings.Cut(strings.TrimSpace(p.Text), "\n")
	failed := payloadThread{
		ID: origin, Agent: agent, Origin: origin, Title: trimText(title, 120), State: "error",
		Project: filepath.Base(p.Cwd), Cwd: p.Cwd, Model: p.Model, Effort: p.Effort,
		Updated: time.Now().Unix(), Error: oneLine(why),
		Turns: []payloadTurn{{Role: "user", Text: p.Text, At: int64(p.At)}},
	}
	s.mu.Lock()
	s.failed = append([]payloadThread{failed}, s.failed[:min(len(s.failed), 4)]...)
	s.dirty = true
	s.mu.Unlock()
}

func agentFor(model string) string {
	if strings.HasPrefix(model, claudePrefix) {
		return "claude"
	}
	return "codex"
}

// Anything that runs in WoW can write a packet. So start a task only in a folder, with a model and an effort, that
// the app offered.
func (s *session) refuse(p packet) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !slices.ContainsFunc(s.offeredProjects(), func(project payloadProject) bool { return project.Cwd == p.Cwd }) {
		return "its folder isn't one the app offered"
	}
	models := s.offeredModels()
	i := slices.IndexFunc(models, func(m payloadModel) bool { return m.ID == p.Model })
	if i < 0 {
		return "its model isn't one the app offered"
	}
	if p.Effort != "" && !slices.Contains(models[i].Efforts, p.Effort) {
		return "its effort isn't one the app offered"
	}
	return ""
}

func oneLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return trimText(line, 200)
}
