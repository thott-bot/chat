package main

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

const (
	appName      = "thott.bot chat.exe"
	releaseAsset = "thott.bot-chat-Windows.zip"
)

var (
	// The per-user Programs folder (FOLDERID_UserProgramFiles) needs no administrator approval.
	appHomes = func() []string {
		dir, err := windows.KnownFolderPath(windows.FOLDERID_UserProgramFiles, windows.KF_FLAG_CREATE)
		if err != nil {
			return nil
		}
		return []string{filepath.Join(dir, "thott-bot-chat")}
	}
	startMenuDir = func() (string, error) {
		return windows.KnownFolderPath(windows.FOLDERID_Programs, windows.KF_FLAG_CREATE)
	}
	runningApp = func() (string, error) {
		exe, err := os.Executable()
		if err == nil {
			exe, err = filepath.EvalSymlinks(exe)
		}
		return exe, err
	}
	askToMove = func(app, home string) (bool, error) { return true, nil }
)

func appExe(app string) string { return app }

func copyApp(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	return writeExe(in, dst)
}

func writeExe(r io.Reader, dst string) error {
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, r); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func extractApp(zipPath, dir string) (string, error) {
	z, err := zip.OpenReader(zipPath)
	if err != nil {
		return "", err
	}
	defer z.Close()
	for _, f := range z.File {
		if f.Name != appName || !f.Mode().IsRegular() {
			continue
		}
		r, err := f.Open()
		if err != nil {
			return "", err
		}
		defer r.Close()
		exe := filepath.Join(dir, appName)
		return exe, writeExe(r, exe)
	}
	return "", fmt.Errorf("the download has no %s", appName)
}

// swapApp renames the installed executable out of the way, because Windows cannot replace a running executable but
// can rename it. The old executable ends beside staged, and leftovers removes it after its process exits.
func swapApp(staged, dest string) error {
	old := filepath.Join(filepath.Dir(staged), "previous.exe")
	moved := true
	if err := replaceFile(dest, old); errors.Is(err, fs.ErrNotExist) {
		moved = false
	} else if err != nil {
		return err
	}
	if err := replaceFile(staged, dest); err != nil {
		if moved {
			if undo := replaceFile(old, dest); undo != nil {
				return fmt.Errorf("%w, and putting back the previous version failed: %v", err, undo)
			}
		}
		return err
	}
	return nil
}

const shortcutScript = `$s = (New-Object -ComObject WScript.Shell).CreateShortcut($env:THOTT_BOT_CHAT_SHORTCUT)
$s.TargetPath = $env:THOTT_BOT_CHAT_TARGET
$s.WorkingDirectory = Split-Path -Parent $env:THOTT_BOT_CHAT_TARGET
$s.Save()`

func addShortcut(exe string) error {
	dir, err := startMenuDir()
	if err != nil {
		return err
	}
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", shortcutScript)
	cmd.Env = append(os.Environ(), "THOTT_BOT_CHAT_SHORTCUT="+filepath.Join(dir, "thott.bot chat.lnk"), "THOTT_BOT_CHAT_TARGET="+exe)
	hideWindow(cmd)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func originalPath(app string) (string, error) { return app, nil }
