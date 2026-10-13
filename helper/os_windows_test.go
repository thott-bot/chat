package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows/registry"
)

func useTestHome(t *testing.T, root string) {
	t.Setenv("APPDATA", root)
	oldKey, oldStart := runKey, startMenuDir
	runKey = `Software\thott-bot-chat-test\Run`
	startMenu := filepath.Join(root, "Start Menu")
	os.MkdirAll(startMenu, 0o755)
	startMenuDir = func() (string, error) { return startMenu, nil }
	t.Cleanup(func() {
		registry.DeleteKey(registry.CURRENT_USER, runKey)
		registry.DeleteKey(registry.CURRENT_USER, `Software\thott-bot-chat-test`)
		runKey, startMenuDir = oldKey, oldStart
	})
}

func testHomes(t *testing.T, root string) []string {
	return []string{filepath.Join(root, "Programs", "thott-bot-chat")}
}

// Windows marks a downloaded file with a Zone.Identifier stream, which SmartScreen reads.
func quarantine(t *testing.T, path string) {
	if err := os.WriteFile(path+":Zone.Identifier", []byte("[ZoneTransfer]\r\nZoneId=3\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func quarantined(t *testing.T, path string) bool {
	_, err := os.Stat(path + ":Zone.Identifier")
	return err == nil
}

func loginItemTarget(t *testing.T) string {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE)
	if err != nil {
		t.Fatal(err)
	}
	defer k.Close()
	v, _, err := k.GetStringValue("thott.bot chat")
	if err != nil {
		t.Fatal(err)
	}
	exe, ok := strings.CutSuffix(strings.TrimPrefix(v, `"`), `" --watch`)
	if !ok {
		t.Fatalf("Run with WoW command: %q", v)
	}
	return exe
}

func checkShortcut(t *testing.T, exe string) {
	dir, _ := startMenuDir()
	if !isFile(filepath.Join(dir, "thott.bot chat.lnk")) {
		t.Fatalf("no Start menu shortcut for %s", exe)
	}
}

func TestWindowsWoWDetection(t *testing.T) {
	t.Setenv("THOTT_BOT_CHAT_TEST_MODE", "child")
	for _, name := range []string{"WowB.exe", "Unrelated.exe"} {
		path := copyTestProcess(t, t.TempDir(), name)
		cmd := exec.Command(path)
		if err := startAgent(cmd); err != nil {
			t.Fatal(err)
		}
		if got := wowRunning(filepath.Dir(path)); got != (name == "WowB.exe") {
			t.Errorf("%s: WoW running = %v", name, got)
		}
		cmd.Process.Kill()
		cmd.Wait()
		if wowRunning(filepath.Dir(path)) {
			t.Fatal("exited WoW was still detected")
		}
	}
}

func TestWindowsNpmArgumentsDoNotPassThroughAShell(t *testing.T) {
	node := copyTestProcess(t, t.TempDir(), "node.exe")
	t.Setenv("PATH", filepath.Dir(node))
	t.Setenv("THOTT_BOT_CHAT_TEST_MODE", "args")
	for name, script := range map[string]string{
		"codex":  filepath.Join("@openai", "codex", "bin", "codex.js"),
		"claude": filepath.Join("@anthropic-ai", "claude-code", "cli.js"),
	} {
		path := filepath.Join(t.TempDir(), "space & 日本語", name+".cmd")
		args := []string{"hello & echo wrong", `%PATH%`, `a"b`, `C:\space path\`}
		cmd := agentCommand(path, args...)
		hideWindow(cmd)
		out, err := cmd.Output()
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		json.Unmarshal(out, &got)
		want := append([]string{filepath.Join(filepath.Dir(path), "node_modules", script)}, args...)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("arguments changed: %q, want %q", got, want)
		}
	}
}

func TestWindowsFindsInstalledCodexWithoutPATH(t *testing.T) {
	root := t.TempDir()
	t.Setenv("LOCALAPPDATA", root)
	t.Setenv("APPDATA", root)
	t.Setenv("USERPROFILE", root)
	desktop := filepath.Join(root, "OpenAI", "Codex", "bin", "1", "codex.exe")
	os.MkdirAll(filepath.Dir(desktop), 0o700)
	os.WriteFile(desktop, []byte("fixture"), 0o600)
	if got := findInstalledOS("codex"); got != "" {
		t.Fatalf("found the desktop app's copy %q", got)
	}
	want := filepath.Join(root, "Programs", "OpenAI", "Codex", "bin", "codex.exe")
	os.MkdirAll(filepath.Dir(want), 0o700)
	os.WriteFile(want, []byte("fixture"), 0o600)
	if got := findInstalledOS("codex"); got != want {
		t.Fatalf("installed Codex = %q, want %q", got, want)
	}
}

func TestWindowsAtomicReplacementWaitsForReader(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Payload.lua")
	os.WriteFile(path, []byte("old"), 0o600)
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- writeFileAtomic(path, []byte("new")) }()
	select {
	case err := <-done:
		f.Close()
		t.Fatalf("replacement finished while its reader was still open: %v", err)
	case <-time.After(150 * time.Millisecond):
	}
	if data, _ := os.ReadFile(path); string(data) != "old" {
		f.Close()
		t.Fatalf("locked reader lost the complete old payload: %q", data)
	}
	f.Close()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(path); string(data) != "new" {
		t.Fatalf("replacement = %q", data)
	}
}

func TestWindowsAtomicReplacementStopsWaitingForLockedReader(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Payload.lua")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	finished := make(chan error, 1)
	go func() { finished <- writeFileAtomic(path, []byte("new")) }()
	select {
	case err := <-finished:
		if err == nil {
			t.Fatal("replaced a payload while its reader was locked")
		}
	case <-time.After(3 * time.Second):
		f.Close()
		<-finished
		t.Fatal("a locked reader blocked the helper indefinitely")
	}
	if data, _ := os.ReadFile(path); string(data) != "old" {
		t.Fatalf("failed replacement changed the old payload: %q", data)
	}
	if pending, _ := filepath.Glob(filepath.Join(filepath.Dir(path), ".Payload.lua-*")); len(pending) != 0 {
		t.Fatalf("failed replacement left temporary payloads behind: %v", pending)
	}
}
