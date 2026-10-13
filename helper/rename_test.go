package main

import (
	"bytes"
	"compress/zlib"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenamePacketUsesCodexNameAPI(t *testing.T) {
	s := newSession("", nil, func() {})
	s.upsert(cxThread{ID: "thread1", Name: "Old title"})
	calls := 0
	s.client = rpcClientFor(t, func(method string, params json.RawMessage) (json.RawMessage, *rpcError) {
		calls++
		var p map[string]string
		json.Unmarshal(params, &p)
		if method != "thread/name/set" || p["threadId"] != "thread1" || p["name"] != "New title" {
			t.Fatalf("request: %s %s", method, params)
		}
		return json.RawMessage(`{}`), nil
	})
	var compressed bytes.Buffer
	writer := zlib.NewWriter(&compressed)
	writer.Write([]byte(`{"type":"rename","session":"s1","seq":2,"thread":"thread1","title":"  New title\nsecond line "}`))
	writer.Close()
	p, err := decodePacket(compressed.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	s.handle(p)
	if calls != 1 || s.threads["thread1"].payload(nil).Title != "New title" {
		t.Fatal("rename not returned in payload")
	}
	s.handle(packet{Type: "rename", Thread: "thread1", Title: " \n "})
	if calls != 1 {
		t.Fatal("empty rename called Codex")
	}
	s.client = rpcClientFor(t, func(string, json.RawMessage) (json.RawMessage, *rpcError) { return nil, &rpcError{Message: "denied"} })
	s.handle(packet{Type: "rename", Thread: "thread1", Title: "Rejected"})
	if p := s.threads["thread1"].payload(nil); p.Title != "New title" || !strings.Contains(p.Error, "denied") {
		t.Fatalf("failure: %+v", p)
	}
}

func TestClaudeRenameAppendsItsCustomTitleRecord(t *testing.T) {
	config := t.TempDir()
	project := filepath.Join(config, "projects", "encoded-project")
	os.MkdirAll(project, 0o700)
	id := "00112233-4455-4677-8899-aabbccddeeff"
	path := filepath.Join(project, id+".jsonl")
	original := `{"type":"user","message":{"content":"hello"}}` + "\n"
	os.WriteFile(path, []byte(original), 0o600)
	s := newSession("", nil, func() {})
	s.claudeConfig = config
	s.claude[id] = &claudeThread{ID: id, Title: "Old title", Started: true}
	s.handle(packet{Type: "rename", Thread: id, Title: "  Azeroth ✓  "})
	data, _ := os.ReadFile(path)
	if !strings.HasPrefix(string(data), original) {
		t.Fatal("rename rewrote the transcript")
	}
	var entry struct{ Type, CustomTitle, SessionID string }
	if err := json.Unmarshal(data[len(original):], &entry); err != nil {
		t.Fatal(err)
	}
	if entry.Type != "custom-title" || entry.CustomTitle != "Azeroth ✓" || entry.SessionID != id || s.claude[id].payload().Title != entry.CustomTitle {
		t.Fatalf("entry: %+v", entry)
	}
	s.handle(packet{Type: "rename", Thread: id, Title: entry.CustomTitle})
	again, _ := os.ReadFile(path)
	if string(again) != string(data) {
		t.Fatal("unchanged title appended again")
	}
	if err := renameClaudeSession(config, "../outside", "bad"); err == nil {
		t.Fatal("invalid session ID accepted")
	}
	if err := renameClaudeSession(config, newUUID(), "missing"); err == nil {
		t.Fatal("missing transcript accepted")
	}
}

func TestClaudeModelsRequireInstalledAndSignedIn(t *testing.T) {
	path, _ := os.Executable()
	t.Setenv("THOTT_BOT_CHAT_TEST_MODE", "claude-auth")
	t.Setenv("THOTT_BOT_CHAT_AUTH_RESULT", `{"loggedIn":false}`)
	t.Setenv("THOTT_BOT_CHAT_AUTH_FAILED", "1")
	s := newSession("", nil, func() {})
	s.codex = "signed-out"
	if note := s.payload().Note; note != "Sign in to the Codex CLI on this computer." {
		t.Fatalf("signed-out Codex note: %q", note)
	}
	s.codex = "missing"
	if note := s.payload().Note; note != "Install the Codex CLI or the Claude Code CLI." {
		t.Fatalf("no CLI note: %q", note)
	}
	s.setClaudeAvailability(checkClaude(context.Background(), path))
	if !s.claudeInstalled || len(s.payload().Models) != 0 || s.payload().Note != "Sign in to the Claude Code CLI on this computer." {
		t.Fatal("signed-out Claude offered models or wasn't explained")
	}
	t.Setenv("THOTT_BOT_CHAT_AUTH_FAILED", "0")
	t.Setenv("THOTT_BOT_CHAT_AUTH_RESULT", `{"loggedIn":true,"configDirectory":"test-config"}`)
	s.setClaudeAvailability(checkClaude(context.Background(), path))
	if len(s.payload().Models) != 2 || s.claudeConfig != "test-config" {
		t.Fatal("signed-in Claude not offered")
	}
	t.Setenv("THOTT_BOT_CHAT_AUTH_RESULT", "not JSON")
	s.setClaudeAvailability(checkClaude(context.Background(), path))
	if len(s.payload().Models) != 0 {
		t.Fatal("malformed auth response offered Claude")
	}
	s.setClaudeAvailability(checkClaude(context.Background(), ""))
	if s.claudeInstalled || len(s.payload().Models) != 0 {
		t.Fatal("missing Claude offered models")
	}
}
