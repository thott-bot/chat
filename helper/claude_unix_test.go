//go:build unix

package main

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// A Claude Code start reads its settings, and a slow or network home folder can stall that read. Here a FIFO that
// nothing wrote to causes the stall.
func TestStartingClaudeLeavesTheSessionFree(t *testing.T) {
	work := t.TempDir()
	t.Setenv("HOME", work)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	settings := filepath.Join(work, ".claude.json")
	if err := syscall.Mkfifo(settings, 0o600); err != nil {
		t.Fatal(err)
	}
	h := newClaudeHarness(t, t.TempDir())
	h.s.writer = &payloadWriter{path: filepath.Join(t.TempDir(), "Payload.lua")}
	claude := filepath.Join(t.TempDir(), "claude")
	if err := os.WriteFile(claude, nil, 0o700); err != nil {
		t.Fatal(err)
	}
	h.s.claudePath = claude
	opened := make(chan *os.File, 1)
	go func() {
		// An open for write blocks until the app opens the FIFO for read. The app then waits for the data.
		f, err := os.OpenFile(settings, os.O_WRONLY, 0)
		if err != nil {
			t.Error(err)
		}
		opened <- f
	}()
	sent := make(chan struct{})
	go func() {
		h.send(packet{Type: "create", Text: "What should I train?", Cwd: work, Model: "claude:opus", Effort: "high"})
		close(sent)
	}()
	f := <-opened
	if f == nil {
		return
	}
	written := make(chan payload, 1)
	go func() {
		h.s.write(time.Now())
		written <- readPayloadFile(t, h.s.writer.path)
	}()
	select {
	case p := <-written:
		if len(p.Threads) != 1 || p.Threads[0].State != "active" {
			t.Errorf("the starting thread isn't shown as working: %+v", p.Threads)
		}
	case <-time.After(5 * time.Second):
		t.Error("the session stayed locked while Claude's settings were read")
	}
	// Remove Claude Code before the settings arrive, so the start fails.
	if err := os.Remove(claude); err != nil {
		t.Fatal(err)
	}
	f.WriteString("{}")
	f.Close()
	<-sent
	h.s.write(time.Now())
	if p := readPayloadFile(t, h.s.writer.path); len(p.Threads) != 1 || p.Threads[0].State != "error" {
		t.Fatalf("the failed start wasn't published: %+v", p.Threads)
	}
}
