package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestSessionPublishesChangesAndRetriesFailedWrites(t *testing.T) {
	s := newSession("", map[string]string{}, func() {})
	s.writer = &payloadWriter{path: filepath.Join(t.TempDir(), "missing", "Payload.lua")}
	now := time.Unix(1790000000, 0)
	s.write(now)
	if s.writeErr == "" {
		t.Fatal("missing payload folder wasn't reported")
	}
	if err := os.MkdirAll(filepath.Dir(s.writer.path), 0o700); err != nil {
		t.Fatal(err)
	}
	s.write(now)
	if s.writeErr != "" {
		t.Fatal(s.writeErr)
	}
	p := readPayloadFile(t, s.writer.path)
	s.write(now.Add(time.Second))
	if got := readPayloadFile(t, s.writer.path); got.Rev != p.Rev {
		t.Fatal("idle session rewrote its payload")
	}
	flush := func() payload {
		t.Helper()
		s.write(now)
		next := readPayloadFile(t, s.writer.path)
		if next.Rev <= p.Rev {
			t.Fatal("changed session wasn't published")
		}
		p = next
		return p
	}
	s.setCodex("ready", "")
	if flush().Codex != "ready" {
		t.Fatal("connection state wasn't published")
	}
	s.ack(packet{Session: "s1", Seq: 7})
	if !slices.Contains(flush().Acks["s1"], 7) {
		t.Fatal("packet wasn't acknowledged")
	}
	s.upsert(cxThread{ID: "t1", Name: "Quest", UpdatedAt: 1, Status: cxStatus{Type: "idle"}})
	s.order = []string{"t1"}
	flush()
	s.notify("thread/status/changed", json.RawMessage(`{"threadId":"t1","status":{"type":"active"}}`))
	if flush().Threads[0].State != "active" {
		t.Fatal("active thread wasn't published")
	}
	s.notify("item/completed", json.RawMessage(`{"threadId":"t1","turnId":"turn1","item":{"id":"a1","type":"agentMessage","text":"Hello"}}`))
	if turns := flush().Threads[0].Turns; len(turns) != 1 || turns[0].Text != "Hello" {
		t.Fatalf("reply wasn't published: %+v", turns)
	}
	s.request(&codexClient{id: 1}, json.RawMessage(`3`), "item/commandExecution/requestApproval", json.RawMessage(`{"threadId":"t1","command":"echo hello"}`))
	if a := flush().Threads[0].Approval; a == nil || a.Text != "echo hello" {
		t.Fatalf("approval wasn't published: %+v", a)
	}
	s.notify("serverRequest/resolved", json.RawMessage(`{"requestId":3}`))
	if flush().Threads[0].Approval != nil {
		t.Fatal("resolved approval remained visible")
	}
	s.notify("thread/name/updated", json.RawMessage(`{"threadId":"t1","threadName":"Training"}`))
	if flush().Threads[0].Title != "Training" {
		t.Fatal("new title wasn't published")
	}
}

// A file change's description lasts as long as its turn, even if Codex never completes the item.
func TestFileChangeDescriptionsEndWithTheirTurn(t *testing.T) {
	s := newSession("", map[string]string{}, func() {})
	s.upsert(cxThread{ID: "t1", Cwd: "/repo", Status: cxStatus{Type: "active"}})
	ask := func(id string) string {
		s.request(&codexClient{id: 1}, json.RawMessage(id), "item/fileChange/requestApproval", json.RawMessage(`{"threadId":"t1","itemId":"f1"}`))
		return s.approvals[len(s.approvals)-1].text
	}
	started := json.RawMessage(`{"threadId":"t1","turnId":"turn1","item":{"id":"f1","type":"fileChange","changes":[{"path":"/repo/UI.lua","kind":{"type":"update"}}]}}`)
	s.notify("item/started", started)
	if got := ask(`1`); got != "Change UI.lua" {
		t.Fatalf("the approval read %q", got)
	}
	s.notify("turn/completed", json.RawMessage(`{"threadId":"t1","turn":{"id":"turn1","status":"interrupted"}}`))
	if got := ask(`2`); got != "Change files" {
		t.Fatalf("the change outlived its turn: %q", got)
	}
	s.notify("item/started", started)
	exited := make(chan struct{})
	close(exited)
	s.client = &codexClient{agentProcess: agentProcess{done: exited}}
	s.superviseCodex(time.Now())
	if got := ask(`3`); got != "Change files" {
		t.Fatalf("the change outlived Codex: %q", got)
	}
}

func TestSessionBridgesCodexEndToEnd(t *testing.T) {
	home, wow := t.TempDir(), t.TempDir()
	for _, name := range []string{"HOME", "USERPROFILE", "APPDATA", "XDG_CONFIG_HOME"} {
		t.Setenv(name, home)
	}
	if _, err := install(wow); err != nil {
		t.Fatal(err)
	}
	addOns := filepath.Join(wow, "WTF", "Account", "ACCOUNT", "AddOns.txt")
	if err := os.MkdirAll(filepath.Dir(addOns), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(addOns, []byte(addOnsText([frameBytes]byte{})), 0o600); err != nil {
		t.Fatal(err)
	}
	fake, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	log, hold, work := filepath.Join(t.TempDir(), "codex.log"), filepath.Join(t.TempDir(), "hold"), filepath.Join(home, "ManaBar")
	t.Setenv("THOTT_BOT_CHAT_TEST_MODE", "codex")
	t.Setenv("CODEX_LOG", log)
	t.Setenv("CODEX_HOLD", hold)
	t.Setenv("CODEX_WORK", work)
	logged := func() string {
		data, _ := os.ReadFile(log)
		return string(data)
	}

	s := newSession(wow, map[string]string{}, func() {})
	s.findAgent = func(name string) string {
		if name == "codex" {
			return fake
		}
		return ""
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stopped := make(chan struct{})
	go func() {
		s.run(ctx)
		close(stopped)
	}()
	waitFor := func(what string, ok func(payload) bool) payload {
		t.Helper()
		var p payload
		for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
			if got, err := tryPayloadFile(t, payloadPath(wow)); err == nil {
				if p = got; ok(p) {
					return p
				}
			}
		}
		t.Fatalf("%s: %+v", what, p)
		return p
	}

	waitFor("Codex ready with its threads", func(p payload) bool {
		return p.Codex == "ready" && len(p.Models) == 1 && len(p.Threads) == 1 && p.Threads[0].Cwd == work
	})
	if err := os.WriteFile(hold, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	create, err := json.Marshal(packet{Type: "create", Session: "s1", Seq: 1, At: 1790000100, Text: "Show mana ticks", Cwd: work, Model: "gpt-5.5", Effort: "high", ContextText: "Character: level 12 Night Elf Druid"})
	if err != nil {
		t.Fatal(err)
	}
	for _, frame := range encodeFrames(string(create), 0) {
		if err := os.WriteFile(addOns, []byte(addOnsText(frame)), 0o600); err != nil {
			t.Fatal(err)
		}
		time.Sleep(40 * time.Millisecond)
	}
	created := func(th payloadThread) bool { return th.Origin == "s1:1" }
	first := waitFor("the game's create", func(p payload) bool { return slices.ContainsFunc(p.Threads, created) })
	if th := first.Threads[slices.IndexFunc(first.Threads, created)]; th.State != "active" || th.Effort != "high" || th.Model != "gpt-5.5" {
		t.Fatalf("the new thread's first payload doesn't show its running turn: %+v", th)
	}
	if err := os.Remove(hold); err != nil {
		t.Fatal(err)
	}
	p := waitFor("Codex's answer to the game's create", func(p payload) bool {
		i := slices.IndexFunc(p.Threads, func(th payloadThread) bool { return th.Origin == "s1:1" })
		return i >= 0 && slices.ContainsFunc(p.Threads[i].Turns, func(turn payloadTurn) bool { return turn.Text == "Mana ticks are on." })
	})
	if !slices.Contains(p.Acks["s1"], 1) {
		t.Fatalf("the create wasn't acked: %+v", p.Acks)
	}
	for _, want := range []string{`"method":"thread/start"`, `"model":"gpt-5.5"`, `"method":"turn/start"`, `"effort":"high"`, "wow_character_state", "Character: level 12 Night Elf Druid"} {
		if !strings.Contains(logged(), want) {
			t.Fatalf("Codex was not sent %s:\n%s", want, logged())
		}
	}

	cancel()
	select {
	case <-stopped:
	case <-time.After(10 * time.Second):
		t.Fatal("the session didn't stop")
	}
	if p := readPayloadFile(t, payloadPath(wow)); p.Codex != "offline" || p.V != protocolVersion || p.Note != offlineNote || !slices.Contains(p.Acks["s1"], 1) {
		t.Fatalf("offline payload: %+v", p)
	}
	for deadline := time.Now().Add(10 * time.Second); !strings.HasSuffix(logged(), "eof\n"); time.Sleep(20 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("Codex never saw its input close, so it was stopped some other way:\n%s", logged())
		}
	}
}

// Codex's first thread list can take seconds. Until it and the Claude Code check arrive, the payload says starting,
// so an addon that loads the payload then keeps reading.
func TestPayloadSaysStartingUntilThreadsAndClaudeLand(t *testing.T) {
	home, wow := t.TempDir(), t.TempDir()
	for _, name := range []string{"HOME", "USERPROFILE", "APPDATA", "XDG_CONFIG_HOME"} {
		t.Setenv(name, home)
	}
	if _, err := install(wow); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(wow, "WTF", "Account"), 0o700); err != nil {
		t.Fatal(err)
	}
	fake, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	hold := filepath.Join(t.TempDir(), "hold")
	t.Setenv("THOTT_BOT_CHAT_TEST_MODE", "codex")
	t.Setenv("CODEX_LOG", filepath.Join(t.TempDir(), "codex.log"))
	t.Setenv("CODEX_HOLD", hold)
	t.Setenv("CODEX_WORK", home)
	if err := os.WriteFile(hold, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	claudeAsked := make(chan struct{})
	s := newSession(wow, map[string]string{}, func() {})
	s.findAgent = func(name string) string {
		if name == "claude" {
			<-claudeAsked
		}
		return fake
	}
	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	go func() {
		s.run(ctx)
		close(stopped)
	}()
	defer func() {
		cancel()
		<-stopped
	}()
	waitFor := func(what string, ok func(payload) bool) payload {
		t.Helper()
		var p payload
		for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
			if got, err := tryPayloadFile(t, payloadPath(wow)); err == nil {
				if p = got; ok(p) {
					return p
				}
			}
		}
		t.Fatalf("%s: %+v", what, p)
		return p
	}

	if p := waitFor("the app's first payload", func(p payload) bool { return p.Codex != "offline" }); !p.Starting {
		t.Fatalf("the first payload doesn't say the app is starting: %+v", p)
	}
	if p := waitFor("Codex's models", func(p payload) bool { return len(p.Models) == 1 }); !p.Starting || len(p.Threads) != 0 {
		t.Fatalf("Codex's models before its threads didn't say the app is starting: %+v", p)
	}
	close(claudeAsked)
	if p := waitFor("Claude Code's models while Codex lists its threads", func(p payload) bool { return len(p.Models) == 3 }); !p.Starting || len(p.Threads) != 0 {
		t.Fatalf("the payload stopped saying the app is starting before Codex's threads landed: %+v", p)
	}
	if err := os.Remove(hold); err != nil {
		t.Fatal(err)
	}
	if p := waitFor("Codex's threads", func(p payload) bool { return len(p.Threads) == 1 }); p.Starting {
		t.Fatalf("the payload still says the app is starting after both landed: %+v", p)
	}
}

func TestUnavailableCodexDoesNotBlockClaude(t *testing.T) {
	work := t.TempDir()
	t.Setenv("HOME", work)
	t.Setenv("USERPROFILE", work)
	h := newClaudeHarness(t, t.TempDir())
	h.send(packet{Type: "create", Text: "Waiting for Codex"})
	h.send(packet{Type: "create", Text: "Hello", Model: "claude:sonnet", Cwd: work})
	if th := h.idle("Claude while Codex is unavailable"); th.Agent != "claude" {
		t.Fatalf("wrong agent: %+v", th)
	}
	if len(h.s.queue) != 1 || h.s.queue[0].Text != "Waiting for Codex" {
		t.Fatal("Codex's queued request was lost")
	}
	h.s.drain(time.Now().Add(11 * time.Minute))
	if len(h.s.queue) != 0 {
		t.Fatal("expired request remained queued")
	}
}

type rpcSink func([]byte)

func (f rpcSink) Write(p []byte) (int, error) { f(p); return len(p), nil }
func (rpcSink) Close() error                  { return nil }

func TestRefreshReleasesOldTranscripts(t *testing.T) {
	s := newSession("", map[string]string{}, func() {})
	c := &codexClient{ctx: context.Background(), agentProcess: agentProcess{done: make(chan struct{})}, pending: map[int64]chan rpcMessage{}}
	c.stdin = rpcSink(func(data []byte) {
		var m rpcMessage
		if err := json.Unmarshal(data, &m); err != nil {
			t.Fatal(err)
		}
		if m.Method != "thread/list" {
			t.Fatalf("unexpected call %q", m.Method)
		}
		c.resolve(rpcMessage{ID: m.ID, Result: json.RawMessage(`{"data":[{"id":"current","status":{"type":"idle"}}]}`)})
	})
	s.client, s.codex = c, "ready"
	for i := range 100 {
		s.upsert(cxThread{ID: string(rune('a' + i)), Status: cxStatus{Type: "idle"}}).turns = []cxTurn{{ID: "old"}}
	}
	running := s.upsert(cxThread{ID: "running", Status: cxStatus{Type: "active"}})
	running.activeTurn, running.live = "turn", &cxTurn{ID: "turn", Status: "inProgress"}
	s.upsert(cxThread{ID: "approval", Status: cxStatus{Type: "active"}})
	s.approvals = []*approval{{thread: "approval"}}
	s.refresh(time.Now())
	if len(s.threads) != 3 || s.threads["current"] == nil || s.threads["running"] != running || s.threads["approval"] == nil {
		t.Fatalf("retained %d threads; lost current work or kept old transcripts", len(s.threads))
	}
}

func TestCreatesRunOnlyInOfferedFoldersAndModels(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	s := newSession("", map[string]string{}, func() {})
	s.claudePath = "claude"
	if why := s.refuse(packet{Type: "create", Cwd: home, Model: "claude:opus", Effort: "high"}); why != "" {
		t.Fatalf("an offered folder and model were refused: %s", why)
	}
	for name, p := range map[string]packet{
		"folder": {Type: "create", Cwd: filepath.Join(home, "elsewhere"), Model: "claude:opus"},
		"model":  {Type: "create", Cwd: home, Model: "gpt-9"},
		"effort": {Type: "create", Cwd: home, Model: "claude:opus", Effort: "turbo"},
	} {
		if s.refuse(p) == "" {
			t.Fatalf("a %s the app never offered was accepted", name)
		}
	}
	s.handle(packet{Type: "create", Cwd: filepath.Join(home, "elsewhere"), Model: "claude:opus", Text: "Plan it", Session: "s1", Seq: 3})
	s.mu.Lock()
	p := s.payload()
	s.mu.Unlock()
	if len(p.Threads) != 1 || p.Threads[0].State != "error" || p.Threads[0].Origin != "s1:3" || !strings.Contains(p.Threads[0].Error, "folder") {
		t.Fatalf("a refused task should reach the game as an error it adopts: %+v", p.Threads)
	}
}
