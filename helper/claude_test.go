package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type claudeHarness struct {
	t    *testing.T
	s    *session
	log  string
	file string
	seq  int
}

func newClaudeHarness(t *testing.T, dir string) *claudeHarness {
	fake, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	h := &claudeHarness{t: t, s: newSession("", map[string]string{}, func() {}), log: filepath.Join(dir, "claude.log"), file: filepath.Join(dir, "claude-threads.json")}
	t.Setenv("CLAUDE_LOG", h.log)
	t.Setenv("THOTT_BOT_CHAT_TEST_MODE", "claude")
	fixtures, err := filepath.Abs("testdata/claude")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("THOTT_BOT_CHAT_TESTDATA", fixtures)
	h.s.claudePath = fake
	h.s.loadClaude(h.file)
	t.Cleanup(h.s.detachClaude)
	return h
}

func (h *claudeHarness) send(p packet) {
	h.seq++
	p.Session, p.Seq, p.At = "s1", float64(h.seq), float64(time.Now().Unix())
	h.s.receive(p)
	h.s.drain(time.Now())
}

func (h *claudeHarness) payload() payload {
	h.s.mu.Lock()
	defer h.s.mu.Unlock()
	return h.s.payload()
}

func (h *claudeHarness) wait(what string, ok func(payloadThread) bool) payloadThread {
	h.t.Helper()
	var th payloadThread
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		threads := h.payload().Threads
		if len(threads) == 0 {
			continue
		}
		if th = threads[0]; ok(th) {
			return th
		}
	}
	h.t.Fatalf("%s: got %+v", what, th)
	return th
}

func (h *claudeHarness) exited() bool {
	h.s.mu.Lock()
	defer h.s.mu.Unlock()
	for _, t := range h.s.claude {
		if t.run != nil {
			return false
		}
	}
	return true
}

func (h *claudeHarness) idle(what string) payloadThread {
	h.t.Helper()
	return h.wait(what, func(th payloadThread) bool { return th.State == "idle" && h.exited() })
}

func (h *claudeHarness) logged() string {
	data, _ := os.ReadFile(h.log)
	return string(data)
}

func TestClaudeConversationFromTheGame(t *testing.T) {
	dir := t.TempDir()
	work := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(work); err == nil {
		work = resolved
	}
	t.Setenv("HOME", work)
	t.Setenv("USERPROFILE", work)
	h := newClaudeHarness(t, dir)

	models := h.payload().Models
	if len(models) != 2 || models[0].ID != "claude:opus" || !models[0].Default || models[1].ID != "claude:sonnet" || models[1].Effort != "high" || len(models[1].Efforts) != 5 {
		t.Fatalf("models with Codex missing: %+v", models)
	}
	h.s.codex = "missing"
	if p := h.payload(); p.Note != "" || len(p.Projects) != 1 {
		t.Fatalf("Codex missing but Claude works: note %q, projects %+v", p.Note, p.Projects)
	}

	h.send(packet{Type: "create", Text: "What should I train?\nThanks", Cwd: work, Model: "claude:opus", Effort: "high", ContextText: "Character: level 12 Night Elf Druid"})
	th := h.idle("first reply")
	if th.Agent != "claude" || th.Title != "What should I train?" || th.Model != "claude:opus" || th.Origin != "s1:1" || th.Cwd != work || th.Project != filepath.Base(work) {
		t.Fatalf("new thread: %+v", th)
	}
	if len(th.Turns) != 3 || th.Turns[0].Text != "What should I train?\nThanks" || !th.Turns[0].Context || th.Turns[1].Text != "Ran 1 command" || th.Turns[2].Role != "assistant" || th.Turns[2].Text != "done" {
		t.Fatalf("first turns: %+v", th.Turns)
	}
	if th.ContextLeft == nil || *th.ContextLeft != 96 {
		t.Fatalf("context left: %v", th.ContextLeft)
	}
	log := h.logged()
	for _, want := range []string{"start " + work + " -p ", "--model opus", "--effort high", "--permission-prompt-tool stdio", "--session-id " + th.ID, "wow_character_state captured=", "Character: level 12 Night Elf Druid", "What should I train?"} {
		if !strings.Contains(log, want) {
			t.Fatalf("claude was not given %q:\n%s", want, log)
		}
	}
	id := th.ID

	h.send(packet{Type: "reply", Thread: id, Text: "Write my notes"})
	th = h.wait("write approval", func(th payloadThread) bool { return th.State == "needs_you" })
	if a := th.Approval; a == nil || a.Kind != "file" || a.Text != "notes.txt" || a.ID != "a9c35e60-b2f8-4b21-a1c2-6b3fd0aa985b" {
		t.Fatalf("file approval: %+v", th.Approval)
	}
	if !strings.Contains(h.logged(), "--resume "+id) {
		t.Fatal("a reply didn't resume the session")
	}
	h.send(packet{Type: "deny", Thread: id, Approval: th.Approval.ID})
	th = h.idle("after deny")
	if !strings.Contains(h.logged(), `"behavior":"deny"`) || th.Turns[len(th.Turns)-2].Text != "Write my notes" || th.Turns[len(th.Turns)-1].Text != "I couldn't create notes.txt because the Write call was denied, so no file was written." {
		t.Fatalf("deny: %+v\n%s", th.Turns, h.logged())
	}

	h.send(packet{Type: "reply", Thread: id, Text: "Run ping"})
	th = h.wait("command approval", func(th payloadThread) bool { return th.State == "needs_you" })
	if a := th.Approval; a == nil || a.Kind != "command" || a.Text != "ping -c 40 127.0.0.1" || a.Reason != "Ping localhost 40 times" {
		t.Fatalf("command approval: %+v", th.Approval)
	}
	h.send(packet{Type: "approve", Thread: id, Approval: th.Approval.ID})
	th = h.wait("allowed", func(th payloadThread) bool {
		return th.State == "active" && th.Approval == nil && strings.Contains(h.logged(), `"behavior":"allow","updatedInput":{"command":"ping -c 40 127.0.0.1"`)
	})
	if th.Activity != "Running ping -c 40 127.0.0.1" || th.Since == 0 {
		t.Fatalf("allow: %+v", th)
	}
	h.send(packet{Type: "reply", Thread: id, Text: "And after that?"})
	h.send(packet{Type: "stop", Thread: id})
	th = h.idle("after stop and the queued reply")
	if th.Error != "" || th.Turns[len(th.Turns)-3].Text != "And after that?" || th.Turns[len(th.Turns)-1].Text != "done" {
		t.Fatalf("stop then queued reply: %+v", th)
	}
	if strings.Count(h.logged(), "start "+work+" ") != 3 {
		t.Fatalf("the queued reply should reuse the running process:\n%s", h.logged())
	}

	h.send(packet{Type: "reply", Thread: id, Text: "crash"})
	th = h.wait("crash", func(th payloadThread) bool { return th.State == "error" })
	if th.Error != "Claude stopped: exit status 3: boom" {
		t.Fatalf("crash: %q", th.Error)
	}

	h.send(packet{Type: "read", Thread: id})
	again := newClaudeHarness(t, dir)
	again.s.codex = "missing"
	th = again.payload().Threads[0]
	if th.ID != id || th.State != "idle" || th.Unread || len(th.Turns) != 10 || th.ContextLeft == nil {
		t.Fatalf("after a helper restart: %+v", th)
	}
	again.send(packet{Type: "reply", Thread: id, Text: "Still there?"})
	again.idle("after restart")
	if !strings.Contains(again.logged(), "--resume "+id) {
		t.Fatal("a restarted helper didn't resume the session")
	}
}
