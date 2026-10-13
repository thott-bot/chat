//go:build darwin || windows

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// A first open from the download folder moves the app to the folder for apps, points Run with WoW at the moved
// copy, and the moved copy removes the one it came from.
func TestFirstOpenMovesTheAppAndItsLoginItem(t *testing.T) {
	root := t.TempDir()
	useTestHome(t, root)
	homes := testHomes(t, root)
	downloaded := fakeApp(t, filepath.Join(root, "Downloads"), []byte("version 0.2.0"))
	quarantine(t, appExe(downloaded))
	oldVersion, oldApp, oldHomes, oldAsk := version, runningApp, appHomes, askToMove
	version = "0.2.0"
	running := downloaded
	runningApp = func() (string, error) { return running, nil }
	appHomes = func() []string { return homes }
	var asked string
	askToMove = func(app, home string) (bool, error) { asked = home; return true, nil }
	t.Cleanup(func() { version, runningApp, appHomes, askToMove = oldVersion, oldApp, oldHomes, oldAsk })

	s := settings{RunWithWoW: true}
	exe, from, err := placeApp(&s)
	if err != nil {
		t.Fatal(err)
	}
	home := homes[len(homes)-1]
	want := appExe(filepath.Join(home, appName))
	if asked != home || exe != want || from != downloaded {
		t.Fatalf("asked about %q, moved to %q from %q; want %q from %q", asked, exe, from, want, downloaded)
	}
	if got, _ := os.ReadFile(exe); string(got) != "version 0.2.0" {
		t.Fatalf("the moved app holds %q", got)
	}
	if quarantined(t, exe) {
		t.Fatal("the moved app is still quarantined, so macOS would open it from a random read-only folder")
	}
	if got := loginItemTarget(t); got != exe {
		t.Fatalf("Run with WoW starts %q, want %q", got, exe)
	}
	checkShortcut(t, exe)
	if left := stagesLeft(t, exe); len(left) != 0 {
		t.Fatalf("the move left %v", left)
	}

	running = filepath.Join(home, appName)
	a := &app{leftovers: leftovers(from)}
	a.removeLeftovers()
	if _, err := os.Stat(downloaded); !os.IsNotExist(err) {
		t.Fatalf("the moved app kept the downloaded copy: %v", err)
	}
	if exe, _, err := placeApp(&s); exe != "" || err != nil {
		t.Fatalf("the installed app moved again: %q %v", exe, err)
	}
}

func TestDeclinedMoveIsNotAskedAgain(t *testing.T) {
	root := t.TempDir()
	useTestHome(t, root)
	if err := os.MkdirAll(configDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	downloaded := fakeApp(t, filepath.Join(root, "Downloads"), []byte("version 0.2.0"))
	oldVersion, oldApp, oldHomes, oldAsk := version, runningApp, appHomes, askToMove
	version = "0.2.0"
	runningApp = func() (string, error) { return downloaded, nil }
	appHomes = func() []string { return []string{filepath.Join(root, "Applications")} }
	asks := 0
	askToMove = func(app, home string) (bool, error) { asks++; return false, nil }
	t.Cleanup(func() { version, runningApp, appHomes, askToMove = oldVersion, oldApp, oldHomes, oldAsk })

	for range 2 {
		s := loadSettings()
		if exe, _, err := placeApp(&s); exe != "" || err != nil {
			t.Fatalf("a declined move moved the app: %q %v", exe, err)
		}
	}
	if asks != 1 || !isFile(appExe(downloaded)) {
		t.Fatalf("asked %d times; app still there: %v", asks, isFile(appExe(downloaded)))
	}
	if left, _ := filepath.Glob(filepath.Join(root, "Applications", "*")); len(left) != 0 {
		t.Fatalf("a declined move left %v", left)
	}
}

// After an update replaces the app, the Run with WoW watcher hands over to the new version.
func TestWatcherRestartsAfterAnUpdate(t *testing.T) {
	root := t.TempDir()
	useTestHome(t, root)
	if err := os.MkdirAll(configDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	wow := filepath.Join(root, "WoW")
	os.MkdirAll(filepath.Join(wow, "Interface"), 0o700)
	if err := saveSettings(settings{WowDir: wow, RunWithWoW: true}); err != nil {
		t.Fatal(err)
	}
	exe := copyTestProcess(t, t.TempDir(), appName)
	t.Setenv("THOTT_BOT_CHAT_TEST_MODE", "main")
	old := exec.Command(exe, "--watch")
	if err := old.Start(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan error, 1)
	go func() { exited <- old.Wait() }()
	t.Cleanup(func() {
		saveSettings(settings{WowDir: wow})
		waitFor(t, "the watcher to stop", func() bool { return !watcherRunning() })
	})
	waitFor(t, "the watcher to start", watcherRunning)

	update := copyTestProcess(t, t.TempDir(), appName)
	if err := os.Rename(exe, filepath.Join(filepath.Dir(update), "previous")); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(update, exe); err != nil {
		t.Fatal(err)
	}
	select {
	case <-exited:
	case <-time.After(10 * time.Second):
		old.Process.Kill()
		t.Fatal("the old watcher kept running after the update")
	}
	waitFor(t, "the new watcher to start", watcherRunning)
}

// A new copy started by an update or a move opens only after the copy that started it exits.
func TestHandedOffCopyWaitsForTheOldOne(t *testing.T) {
	dir := t.TempDir()
	lock, marker := filepath.Join(dir, "tray.lock"), filepath.Join(dir, "locked")
	release, ok := lockFile(lock, false)
	if !ok {
		t.Fatal("couldn't take the lock")
	}
	t.Setenv("THOTT_BOT_CHAT_TEST_MODE", "wait-lock")
	t.Setenv("THOTT_BOT_CHAT_TEST_LOCK", lock)
	t.Setenv("THOTT_BOT_CHAT_TEST_MARKER", marker)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	next := exec.Command(exe)
	if err := next.Start(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(500 * time.Millisecond)
	if isFile(marker) {
		release()
		t.Fatal("the new copy opened while the old one still held the lock")
	}
	release()
	if err := next.Wait(); err != nil || !isFile(marker) {
		t.Fatalf("the new copy did not open after the old one released the lock: %v", err)
	}
}

func watcherRunning() bool {
	release, ok := lockFile(filepath.Join(configDir(), "watch.lock"), false)
	if ok {
		release()
	}
	return !ok
}

func waitFor(t *testing.T, what string, done func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !done() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
