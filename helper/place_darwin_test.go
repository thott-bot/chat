package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// A downloaded app that macOS opens from a random read-only folder finds the folder it was unzipped to, so the moved
// copy can remove it.
func TestTranslocatedAppFindsWhereItWasUnzipped(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	app := filepath.Join(dir, "Downloads", appName)
	if err := os.MkdirAll(filepath.Dir(appExe(app)), 0o755); err != nil {
		t.Fatal(err)
	}
	copyTestProcess(t, filepath.Dir(appExe(app)), "thott.bot chat")
	plist := `<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0"><dict>
<key>CFBundleIdentifier</key><string>bot.thott.chat.test</string>
<key>CFBundleExecutable</key><string>thott.bot chat</string>
<key>CFBundlePackageType</key><string>APPL</string>
<key>LSUIElement</key><true/>
</dict></plist>`
	if err := os.WriteFile(filepath.Join(app, "Contents", "Info.plist"), []byte(plist), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("codesign", "--force", "-s", "-", app).CombinedOutput(); err != nil {
		t.Fatalf("codesign: %v: %s", err, out)
	}
	// 00c3 marks a download that the player already allowed to open, so Gatekeeper shows no prompt.
	filepath.WalkDir(app, func(path string, _ os.DirEntry, err error) error {
		if err == nil {
			err = unix.Lsetxattr(path, "com.apple.quarantine", []byte("00c3;00000000;Safari;"), 0)
		}
		if err != nil {
			t.Fatal(err)
		}
		return nil
	})
	marker := filepath.Join(dir, "marker")
	out, err := exec.Command("open", "-n", "-W", "--env", "THOTT_BOT_CHAT_TEST_MODE=original-path", "--env", "THOTT_BOT_CHAT_TEST_MARKER="+marker, app).CombinedOutput()
	if err != nil {
		t.Fatalf("open: %v: %s", err, out)
	}
	got, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}
	running, original, _ := strings.Cut(string(got), "\n")
	if running == app {
		t.Skip("macOS opened the quarantined app where it is, so there is nothing to find")
	}
	if original != app {
		t.Fatalf("opened from %s, found %q, want %s", running, original, app)
	}
}
