package main

import (
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func useTestHome(t *testing.T, root string) { t.Setenv("HOME", root) }

// testHomes returns an Applications folder the user can't write, then one in the home folder.
func testHomes(t *testing.T, root string) []string {
	system := filepath.Join(root, "system", "Applications")
	if err := os.MkdirAll(system, 0o755); err != nil {
		t.Fatal(err)
	}
	os.Chmod(system, 0o555)
	t.Cleanup(func() { os.Chmod(system, 0o755) })
	return []string{system, filepath.Join(root, "Applications")}
}

func quarantine(t *testing.T, path string) {
	if err := unix.Setxattr(path, "com.apple.quarantine", []byte("0081;00000000;Safari;"), 0); err != nil {
		t.Fatal(err)
	}
}

func quarantined(t *testing.T, path string) bool {
	_, err := unix.Getxattr(path, "com.apple.quarantine", nil)
	return err == nil
}

func loginItemTarget(t *testing.T) string {
	data, err := os.ReadFile(launchAgentPath())
	if err != nil {
		t.Fatal(err)
	}
	var plist struct {
		Dict struct {
			Array struct {
				Args []string `xml:"string"`
			} `xml:"array"`
		} `xml:"dict"`
	}
	if err := xml.Unmarshal(data, &plist); err != nil || len(plist.Dict.Array.Args) != 2 {
		t.Fatalf("login item: %v %s", err, data)
	}
	return plist.Dict.Array.Args[0]
}

func checkShortcut(*testing.T, string) {}

func TestFoundAgentsAreKeptWhileTheirFileExists(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	name := fmt.Sprintf("thott-bot-chat-test-agent-%d", time.Now().UnixNano())
	install := func(dir string) string {
		path := filepath.Join(home, dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o700); err != nil {
			t.Fatal(err)
		}
		return path
	}
	npm := install(".npm-global/bin")
	if got := findInstalledOS(name); got != npm {
		t.Fatalf("found %q, want %q", got, npm)
	}
	local := install(".local/bin")
	if got := findInstalledOS(name); got != npm {
		t.Fatalf("looked again while %s was still there: found %q", npm, got)
	}
	if err := os.Remove(npm); err != nil {
		t.Fatal(err)
	}
	if got := findInstalledOS(name); got != local {
		t.Fatalf("found %q after the kept path went away, want %q", got, local)
	}
}

func TestLoginItemOnlyStartsTheWatcher(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	exe := "/Applications/thott.bot chat & Friends.app/Contents/MacOS/thott.bot chat"
	if err := setAutostart(true, exe); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(launchAgentPath())
	if err != nil {
		t.Fatal(err)
	}
	var plist struct {
		Dict struct {
			Array struct {
				Args []string `xml:"string"`
			} `xml:"array"`
		} `xml:"dict"`
	}
	if err := xml.Unmarshal(data, &plist); err != nil {
		t.Fatal(err)
	}
	args := plist.Dict.Array.Args
	if len(args) != 2 || args[0] != exe || args[1] != "--watch" {
		t.Fatalf("login item arguments: %q", args)
	}
	for range 2 {
		if err := setAutostart(false, ""); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(launchAgentPath()); !os.IsNotExist(err) {
		t.Fatal("disabling left the login item installed")
	}
}
