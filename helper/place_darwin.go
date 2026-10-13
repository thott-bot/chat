package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

const (
	appName      = "thott.bot chat.app"
	releaseAsset = "thott.bot-chat-macOS.zip"
	moveButton   = "Move to Applications Folder"
)

var (
	appHomes = func() []string {
		home, _ := os.UserHomeDir()
		return []string{"/Applications", filepath.Join(home, "Applications")}
	}
	runningApp = func() (string, error) {
		exe, err := os.Executable()
		if err == nil {
			exe, err = filepath.EvalSymlinks(exe)
		}
		if err != nil {
			return "", err
		}
		contents := filepath.Dir(filepath.Dir(exe))
		app := filepath.Dir(contents)
		if filepath.Base(filepath.Dir(exe)) != "MacOS" || filepath.Base(contents) != "Contents" || filepath.Ext(app) != ".app" {
			return "", fmt.Errorf("%s is not inside an app bundle", exe)
		}
		return app, nil
	}
	askToMove = func(app, home string) (bool, error) {
		question := "Move thott.bot chat to the Applications folder?"
		if home != "/Applications" {
			question = "Move thott.bot chat to the Applications folder in your home folder?"
		}
		text := question + "\n\nthott.bot chat removes the copy in the current folder."
		script := []string{"on run argv", `display dialog (item 1 of argv) with title "thott.bot chat" buttons {"Do Not Move", "` + moveButton + `"} default button 2`}
		args := []string{text}
		if icon := filepath.Join(app, "Contents", "Resources", "AppIcon.icns"); isFile(icon) {
			script[1] += " with icon (POSIX file (item 2 of argv))"
			args = append(args, icon)
		}
		script = append(script, "button returned of result", "end run")
		cmd := []string{}
		for _, line := range script {
			cmd = append(cmd, "-e", line)
		}
		out, err := exec.Command("osascript", append(cmd, args...)...).Output()
		return strings.TrimSpace(string(out)) == moveButton, err
	}
)

func appExe(app string) string { return filepath.Join(app, "Contents", "MacOS", "thott.bot chat") }

// copyApp copies the bundle with its code signature. It removes the quarantine flag, as Finder does when the player
// moves an app, so macOS does not open the copy from a random read-only location (App Translocation).
func copyApp(src, dst string) error {
	if out, err := exec.Command("ditto", src, dst).CombinedOutput(); err != nil {
		return fmt.Errorf("ditto: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return filepath.WalkDir(dst, func(path string, _ fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := unix.Lremovexattr(path, "com.apple.quarantine"); err != nil && !errors.Is(err, unix.ENOATTR) {
			return fmt.Errorf("removing the quarantine flag from %s: %w", path, err)
		}
		return nil
	})
}

// extractApp uses ditto, the tool that made the ZIP, so the bundle keeps its permissions and extended attributes.
func extractApp(zip, dir string) (string, error) {
	if out, err := exec.Command("ditto", "-x", "-k", zip, dir).CombinedOutput(); err != nil {
		return "", fmt.Errorf("ditto: %v: %s", err, strings.TrimSpace(string(out)))
	}
	app := filepath.Join(dir, appName)
	if !isFile(appExe(app)) {
		return "", fmt.Errorf("the download has no %s", filepath.Join(appName, "Contents", "MacOS", "thott.bot chat"))
	}
	return app, nil
}

// swapApp exchanges the two bundles in one step, so dest is always a complete app. The old bundle ends at staged.
func swapApp(staged, dest string) error {
	if _, err := os.Lstat(dest); errors.Is(err, fs.ErrNotExist) {
		return os.Rename(staged, dest)
	}
	return unix.RenamexNp(staged, dest, unix.RENAME_SWAP)
}

func addShortcut(string) error { return nil }
