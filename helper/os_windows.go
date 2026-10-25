package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

var (
	wowCandidates = []string{
		filepath.Join(os.Getenv("ProgramFiles(x86)"), "World of Warcraft", foreverFolder),
		filepath.Join(os.Getenv("ProgramFiles"), "World of Warcraft", foreverFolder),
		filepath.Join(os.Getenv("ProgramFiles(x86)"), "World of Warcraft", betaFolder),
		filepath.Join(os.Getenv("ProgramFiles"), "World of Warcraft", betaFolder),
	}
	extraPath []string
)

func processPaths() []string {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil
	}
	defer windows.CloseHandle(snap)
	var paths []string
	entry := windows.ProcessEntry32{Size: uint32(unsafe.Sizeof(windows.ProcessEntry32{}))}
	for err = windows.Process32First(snap, &entry); err == nil; err = windows.Process32Next(snap, &entry) {
		if entry.ExeFile[0] != 'W' && entry.ExeFile[0] != 'w' {
			continue
		}
		name := windows.UTF16ToString(entry.ExeFile[:])
		if slices.ContainsFunc(foreverExes, func(exe string) bool { return strings.EqualFold(name, exe) }) {
			if path := imagePath(entry.ProcessID); path != "" {
				paths = append(paths, path)
			}
		}
	}
	return paths
}

// A Windows reader can deny the rename while WoW loads a payload or saves AddOns. Keep the previous complete file
// until the rename succeeds. Never truncate it.
// openShared opens a file for reading and lets WoW delete or replace it while it is open. os.Open denies delete
// sharing on Windows, so a WoW save that lands during a read fails.
func openShared(path string) (*os.File, error) {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	handle, err := windows.CreateFile(name, windows.GENERIC_READ, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	return os.NewFile(uintptr(handle), path), nil
}

func replaceFile(from, to string) error {
	deadline := time.Now().Add(time.Second)
	for delay := time.Millisecond; ; delay = min(delay*2, 32*time.Millisecond) {
		err := os.Rename(from, to)
		if err == nil || !time.Now().Before(deadline) ||
			!errors.Is(err, windows.ERROR_SHARING_VIOLATION) && !errors.Is(err, windows.ERROR_ACCESS_DENIED) {
			return err
		}
		time.Sleep(delay)
	}
}

func imagePath(pid uint32) string {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return ""
	}
	defer windows.CloseHandle(h)
	buf := make([]uint16, windows.MAX_LONG_PATH)
	n := uint32(len(buf))
	if windows.QueryFullProcessImageName(h, 0, &buf[0], &n) != nil {
		return ""
	}
	return windows.UTF16ToString(buf[:n])
}

const folderScript = `[Console]::OutputEncoding = [Text.Encoding]::UTF8
Add-Type -AssemblyName System.Windows.Forms
$d = New-Object System.Windows.Forms.FolderBrowserDialog
$d.Description = 'Choose your World of Warcraft folder'
$owner = New-Object System.Windows.Forms.Form -Property @{TopMost = $true; Text = 'thott.bot chat'}
if ($d.ShowDialog($owner) -eq 'OK') { $d.SelectedPath }`

func chooseFolder() (string, error) {
	cmd := exec.Command("powershell.exe", "-NoProfile", "-STA", "-Command", folderScript)
	hideWindow(cmd)
	out, err := cmd.Output()
	path := strings.TrimSpace(string(out))
	if err == nil && path == "" {
		err = errors.New("no folder chosen")
	}
	return path, err
}

func openURL(url string) {
	cmd := exec.Command("rundll32.exe", "url.dll,FileProtocolHandler", url)
	hideWindow(cmd)
	if cmd.Start() == nil {
		go cmd.Wait()
	}
}

var runKey = `Software\Microsoft\Windows\CurrentVersion\Run`

func setAutostart(on bool, exe string) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	if on {
		return k.SetStringValue("thott.bot chat", `"`+exe+`" --watch`)
	}
	if err := k.DeleteValue("thott.bot chat"); err != nil && !errors.Is(err, registry.ErrNotExist) {
		return err
	}
	return nil
}

func findInstalledOS(name string) string {
	for _, p := range []string{
		filepath.Join(os.Getenv("USERPROFILE"), ".local", "bin", name+".exe"),
		filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs", "OpenAI", "Codex", "bin", name+".exe"),
		filepath.Join(os.Getenv("APPDATA"), "npm", name+".cmd"),
	} {
		if isFile(p) {
			return p
		}
	}
	return ""
}

func agentCommand(path string, args ...string) *exec.Cmd {
	if strings.EqualFold(filepath.Ext(path), ".cmd") {
		// npm's Windows shims are batch files. Run their JS entry points directly, so paths and arguments do not go
		// through cmd.exe's shell quoting.
		name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		scripts := map[string]string{
			"codex":  filepath.Join("@openai", "codex", "bin", "codex.js"),
			"claude": filepath.Join("@anthropic-ai", "claude-code", "cli.js"),
		}
		if script := scripts[name]; script != "" {
			entry := filepath.Join(filepath.Dir(path), "node_modules", script)
			return exec.Command("node.exe", append([]string{entry}, args...)...)
		}
	}
	return exec.Command(path, args...)
}

// lockFile takes the lock, or with wait blocks until its holder releases it or exits.
func lockFile(path string, wait bool) (release func(), ok bool) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, false
	}
	how := uint32(windows.LOCKFILE_EXCLUSIVE_LOCK | windows.LOCKFILE_FAIL_IMMEDIATELY)
	if wait {
		how = windows.LOCKFILE_EXCLUSIVE_LOCK
	}
	var ol windows.Overlapped
	if windows.LockFileEx(windows.Handle(f.Fd()), how, 0, 1, 0, &ol) != nil {
		f.Close()
		return nil, false
	}
	return func() { f.Close() }, true
}

func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
}

func startDetached(cmd *exec.Cmd) error {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.DETACHED_PROCESS | windows.CREATE_NEW_PROCESS_GROUP}
	return cmd.Start()
}

// The app never stops an agent, so do not put the process in a job that ends it with the app.
func startAgent(cmd *exec.Cmd) error {
	hideWindow(cmd)
	return cmd.Start()
}
