package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

func runWatcher() {
	release, ok := lockFile(filepath.Join(configDir(), "watch.lock"), false)
	if !ok {
		return
	}
	defer release()
	openLog("watch: ")
	exe, err := os.Executable()
	var self os.FileInfo
	if err == nil {
		self, err = fileIdentity(exe)
	}
	if err != nil {
		logf("Couldn't find the thott.bot chat app: %v", err)
		return
	}
	logf("Watching for World of Warcraft")
	was := false
	for range time.Tick(3 * time.Second) {
		if now, err := os.Stat(exe); err == nil && !os.SameFile(self, now) {
			logf("thott.bot chat was updated; starting the new watcher")
			release()
			if err := startDetached(exec.Command(exe, "--watch")); err != nil {
				logf("Couldn't start the new watcher: %v", err)
			}
			return
		}
		s := loadSettings()
		if !s.RunWithWoW {
			logf("Run with WoW is off; stopping")
			return
		}
		running := wowRunning(resolveWowDir(s.WowDir))
		if running && !was && !trayRunning() {
			launchTray(exe)
		}
		was = running
	}
}

func trayRunning() bool {
	release, ok := lockFile(filepath.Join(configDir(), "tray.lock"), false)
	if ok {
		release()
	}
	return !ok
}

func launchTray(exe string) {
	cmd := exec.Command(exe)
	cmd.Env = append(os.Environ(), fromWatchEnv+"=1")
	if err := startDetached(cmd); err != nil {
		logf("Couldn't open thott.bot chat: %v", err)
		return
	}
	go cmd.Wait()
	logf("World of Warcraft started; opened thott.bot chat")
}

// fileIdentity reads the file's identity now. On Windows, os.Stat loads it only when os.SameFile first compares,
// which after an update would read the new file.
func fileIdentity(path string) (os.FileInfo, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return f.Stat()
}
