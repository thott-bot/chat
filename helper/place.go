package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	// handoffEnv tells a new copy of the app to wait until the copy that started it exits.
	handoffEnv   = "THOTT_BOT_CHAT_HANDOFF"
	movedFromEnv = "THOTT_BOT_CHAT_MOVED_FROM"
	stagePrefix  = ".thott-bot-chat-update-"
)

// placeApp moves the app to its usual folder when it opens from anywhere else. It returns the installed executable
// and the copy it came from, or "" when the app stays where it is.
func placeApp(s *settings) (exe, from string, err error) {
	if version == "dev" || s.DeclinedMove {
		return "", "", nil
	}
	app, err := runningApp()
	if err != nil {
		return "", "", nil
	}
	homes := appHomes()
	for _, home := range homes {
		if inside(app, home) {
			return "", "", nil
		}
	}
	if len(homes) == 0 {
		return "", "", nil
	}
	home, stage, err := makeStage(homes)
	if err != nil {
		return "", "", err
	}
	defer os.RemoveAll(stage)
	move, err := askToMove(app, home)
	if err != nil {
		return "", "", fmt.Errorf("asking to move: %w", err)
	}
	if !move {
		s.DeclinedMove = true
		if err := saveSettings(*s); err != nil {
			logf("Couldn't save settings: %v", err)
		}
		logf("The player chose to keep thott.bot chat in %s", filepath.Dir(app))
		return "", "", nil
	}
	staged := filepath.Join(stage, appName)
	if err := copyApp(app, staged); err != nil {
		return "", "", fmt.Errorf("copying to %s: %w", home, err)
	}
	dest := filepath.Join(home, appName)
	if err := swapApp(staged, dest); err != nil {
		return "", "", fmt.Errorf("replacing %s: %w", dest, err)
	}
	exe = appExe(dest)
	if s.RunWithWoW {
		if err := setAutostart(true, exe); err != nil {
			logf("Couldn't point Run with WoW at %s: %v", exe, err)
		}
	}
	if err := addShortcut(exe); err != nil {
		logf("Couldn't add thott.bot chat to the Start menu: %v", err)
	}
	from, err = originalPath(app)
	if err != nil {
		logf("Couldn't find the copy thott.bot chat moved from, so it stays: %v", err)
	}
	return exe, from, nil
}

// makeStage makes a folder for the new copy inside the first home folder the user can write, so the copy moves into
// place with one rename on the same volume.
func makeStage(homes []string) (home, stage string, err error) {
	for _, home = range homes {
		if err = os.MkdirAll(home, 0o755); err != nil {
			continue
		}
		if stage, err = os.MkdirTemp(home, stagePrefix+"*"); err == nil {
			return home, stage, nil
		}
	}
	return "", "", fmt.Errorf("no folder for apps can be written: %w", err)
}

func inside(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// launchApp starts the installed app. It waits for this process to exit before it opens its menu.
func launchApp(exe string, env ...string) error {
	cmd := exec.Command(exe)
	cmd.Env = append(append(os.Environ(), handoffEnv+"=1"), env...)
	if err := startDetached(cmd); err != nil {
		return err
	}
	go cmd.Wait()
	return nil
}

// leftovers lists what an earlier move or update left beside the app, and the copy the app moved from.
func leftovers(movedFrom string) []string {
	app, err := runningApp()
	if err != nil {
		return nil
	}
	paths, _ := filepath.Glob(filepath.Join(filepath.Dir(app), stagePrefix+"*"))
	if movedFrom == "" || movedFrom == app {
		return paths
	}
	info, err := os.Stat(appExe(movedFrom))
	if err == nil && (info.IsDir() || filepath.Ext(movedFrom) != filepath.Ext(appName)) {
		err = errors.New("it is not a copy of thott.bot chat")
	}
	if err != nil {
		logf("Kept %s: %v", movedFrom, err)
		return paths
	}
	return append(paths, movedFrom)
}

// removeLeftovers tries again on each pass, because Windows cannot delete an executable until its process exits. It
// logs each new cause of a failure once.
func (a *app) removeLeftovers() {
	a.mu.Lock()
	paths := a.leftovers
	a.mu.Unlock()
	if len(paths) == 0 {
		return
	}
	var kept []string
	for _, p := range paths {
		if err := os.RemoveAll(p); err != nil {
			kept = append(kept, p)
			if a.removeErrs == nil {
				a.removeErrs = map[string]string{}
			}
			if a.removeErrs[p] != err.Error() {
				a.removeErrs[p] = err.Error()
				logf("Couldn't remove %s: %v", p, err)
			}
		} else {
			logf("Removed %s", p)
		}
	}
	a.mu.Lock()
	a.leftovers = append(kept, a.leftovers[len(paths):]...)
	a.mu.Unlock()
}
