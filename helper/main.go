package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"fyne.io/systray"
)

var version = "dev"

const fromWatchEnv = "THOTT_BOT_CHAT_FROM_WATCH"

func main() {
	dir := configDir()
	if dir == "" {
		fmt.Fprintln(os.Stderr, "thott.bot chat couldn't find your user settings folder. Set up a user profile before starting it.")
		return
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		fmt.Fprintf(os.Stderr, "thott.bot chat couldn't open your settings folder: %v\n", err)
		return
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		fmt.Fprintf(os.Stderr, "thott.bot chat couldn't protect your settings folder: %v\n", err)
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "--watch" {
		runWatcher()
		return
	}
	release, ok := lockFile(filepath.Join(configDir(), "tray.lock"), os.Getenv(handoffEnv) == "1")
	if !ok {
		return
	}
	defer release()
	openLog("")
	movedFrom := os.Getenv(movedFromEnv)
	os.Unsetenv(handoffEnv)
	os.Unsetenv(movedFromEnv)
	s := loadSettings()
	if exe, from, err := placeApp(&s); err != nil {
		logf("Couldn't move thott.bot chat to the folder for apps: %v", err)
	} else if exe != "" {
		err := launchApp(exe, movedFromEnv+"="+from)
		if err == nil {
			logf("Moved thott.bot chat to %s; opening it there", exe)
			return
		}
		logf("Couldn't open %s: %v", exe, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a := &app{
		ctx: ctx, cancel: cancel, loopDone: make(chan struct{}),
		fromWatch: os.Getenv(fromWatchEnv) == "1", settings: s, leftovers: leftovers(movedFrom),
		rescan: make(chan struct{}, 1), checkNow: make(chan struct{}, 1), origins: map[string]string{},
	}
	a.exit = a.requestQuit
	os.Unsetenv(fromWatchEnv)
	logf("thott.bot chat %s started (%s)", version, map[bool]string{true: "opened with WoW", false: "opened by hand"}[a.fromWatch])
	// Shut down after the tray loop returns, not in an exit callback. On macOS the tray library does not call the
	// exit callback for Quit. On Windows it calls it while the process exits.
	systray.Run(a.trayReady, nil)
	a.shutdown()
}

type app struct {
	ctx       context.Context
	cancel    context.CancelFunc
	loopDone  chan struct{}
	exit      func()
	fromWatch bool
	rescan    chan struct{}
	checkNow  chan struct{}
	origins   map[string]string
	// removeErrs is used only by removeLeftovers, on the loop goroutine.
	removeErrs map[string]string

	mu          sync.Mutex
	settings    settings
	wowDir      string
	running     bool
	restart     bool
	sess        *session
	stopSess    func()
	problem     string
	updateVer   string
	updateURL   string
	updateStage updateStage
	staged      string
	stageDir    string
	leftovers   []string
	checking    updateCheck
	show        func(trayView)
	shown       trayView
}

type trayView struct {
	status, detail, update string
	alert                  bool
	choose, runWithWoW     bool
	install                bool
	check                  updateCheck
	updating               updateStage
}

func (a *app) loop() {
	defer close(a.loopDone)
	tick := time.NewTicker(3 * time.Second)
	defer tick.Stop()
	for {
		if a.ctx.Err() != nil {
			return
		}
		a.poll()
		a.removeLeftovers()
		a.installWhenIdle()
		select {
		case <-a.ctx.Done():
			return
		case <-tick.C:
		case <-a.rescan:
		}
	}
}

func (a *app) poll() {
	a.mu.Lock()
	dir, saved := a.wowDir, a.settings.WowDir
	a.mu.Unlock()
	if dir == "" {
		if dir = resolveWowDir(saved); dir == "" {
			a.publish()
			return
		}
		logf("WoW folder: %s", dir)
		a.mu.Lock()
		a.wowDir = dir
		a.mu.Unlock()
		if !wowRunning(dir) {
			a.install(dir)
		}
	}
	running := wowRunning(dir)
	a.mu.Lock()
	was := a.running
	a.running = running
	a.mu.Unlock()
	switch {
	case running && !was:
		logf("World of Warcraft started")
		if installNeeded(dir) {
			logf("The addon, its payload file, or its slot and bit addons are missing or outdated; restart WoW to finish setup")
			a.mu.Lock()
			a.restart = true
			a.mu.Unlock()
			a.writeAppPayload(dir, "restart", restartNote)
		} else {
			a.startSession(dir)
		}
	case !running && was:
		logf("World of Warcraft exited")
		a.stopSession()
		a.mu.Lock()
		a.restart = false
		a.mu.Unlock()
		a.install(dir)
		a.mu.Lock()
		quitWithWoW := a.settings.RunWithWoW
		a.mu.Unlock()
		if quitWithWoW {
			logf("Quitting because Run with WoW is on")
			a.installUpdate(false)
			a.exit()
		}
	}
	a.publish()
}

func (a *app) install(dir string) {
	done, err := install(dir)
	for _, d := range done {
		logf("Setup: %s", d)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.problem = ""
	if err != nil {
		logf("Setup failed: %v", err)
		a.problem = "Setup failed: " + err.Error()
	}
}

// writeAppPayload writes the payload while no session runs. Only an installed addon reads the payload, so the app
// does not create a missing file.
func (a *app) writeAppPayload(dir, codex, note string) {
	path := payloadPath(dir)
	if !isFile(path) {
		return
	}
	w := &payloadWriter{path: path}
	if _, err := w.write(payload{V: protocolVersion, Helper: version, Codex: codex, Note: note}, time.Now()); err != nil {
		logf("Couldn't write the %s payload: %v", codex, err)
		a.mu.Lock()
		a.problem = "Couldn't write the payload: " + oneLine(err.Error())
		a.mu.Unlock()
	}
}

func (a *app) startSession(dir string) {
	ctx, cancel := context.WithCancel(a.ctx)
	s := newSession(dir, a.origins, a.publish)
	done := make(chan struct{})
	go func() {
		s.run(ctx)
		close(done)
	}()
	a.mu.Lock()
	a.sess, a.stopSess = s, func() { cancel(); <-done }
	a.mu.Unlock()
}

func (a *app) stopSession() {
	a.mu.Lock()
	stop := a.stopSess
	a.sess, a.stopSess = nil, nil
	a.mu.Unlock()
	if stop != nil {
		stop()
	}
}

func (a *app) view() trayView {
	a.mu.Lock()
	defer a.mu.Unlock()
	v := trayView{update: a.updateVer, updating: a.updateStage, runWithWoW: a.settings.RunWithWoW, detail: a.problem, check: a.checking}
	switch {
	case a.wowDir == "":
		v.status, v.choose, v.alert = "Choose your WoW folder", true, true
	case a.restart:
		v.status, v.alert = "Restart WoW to finish setup", true
	case a.sess == nil:
		v.status = "Waiting for World of Warcraft"
	default:
		sv := a.sess.view()
		switch {
		case sv.codex == "missing" && !sv.claude:
			if sv.claudeInstalled {
				v.status, v.alert = "Sign in to the Claude Code CLI", true
			} else {
				v.status, v.install, v.alert = "Codex CLI or Claude Code CLI not found", true, true
			}
		case sv.codex == "signed-out" && !sv.claude:
			v.status, v.alert = "Sign in to the Codex CLI", true
		case sv.hello.IsZero():
			v.status = "Waiting for the addon"
		default:
			v.status = "Connected"
		}
		v.detail = firstOf(a.problem, sv.problem)
		if v.detail == "" && !sv.hello.IsZero() {
			v.detail = fmt.Sprintf("%d %s", sv.threads, map[bool]string{true: "thread", false: "threads"}[sv.threads == 1])
		}
	}
	return v
}

func (a *app) publish() {
	v := a.view()
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.show != nil && v != a.shown {
		a.shown = v
		a.show(v)
	}
}

func (a *app) setRunWithWoW(on bool) {
	a.mu.Lock()
	a.settings.RunWithWoW = on
	s := a.settings
	a.mu.Unlock()
	if err := saveSettings(s); err != nil {
		logf("Couldn't save settings: %v", err)
		a.mu.Lock()
		a.settings.RunWithWoW = !on
		a.problem = "Couldn't save Run with WoW: " + oneLine(err.Error())
		a.mu.Unlock()
		a.publish()
		return
	}
	if on {
		a.startWatcher()
	} else if err := setAutostart(false, ""); err != nil {
		logf("Couldn't remove the login item: %v", err)
		a.mu.Lock()
		a.problem = "Couldn't remove the login item: " + oneLine(err.Error())
		a.mu.Unlock()
	} else {
		logf("Run with WoW is off")
	}
	a.publish()
}

func (a *app) startWatcher() {
	exe, err := os.Executable()
	if err == nil {
		exe, err = filepath.EvalSymlinks(exe)
	}
	if err == nil {
		err = setAutostart(true, exe)
	}
	if err != nil {
		logf("Couldn't turn on Run with WoW: %v", err)
		a.mu.Lock()
		a.settings.RunWithWoW = false
		a.problem = "Couldn't turn on Run with WoW: " + oneLine(err.Error())
		s := a.settings
		a.mu.Unlock()
		if err := saveSettings(s); err != nil {
			logf("Couldn't save settings: %v", err)
		}
		return
	}
	cmd := exec.Command(exe, "--watch")
	if err := startDetached(cmd); err != nil {
		logf("Couldn't start the watcher: %v", err)
		a.mu.Lock()
		a.problem = "Couldn't start Run with WoW: " + oneLine(err.Error())
		a.mu.Unlock()
		return
	}
	go cmd.Wait()
	logf("Run with WoW is on: %s --watch starts at login", exe)
}

func (a *app) chooseFolder() {
	path, err := chooseFolder()
	if err != nil {
		logf("No folder chosen: %v", err)
		return
	}
	dir := normalizeChoice(path)
	if dir == "" {
		logf("%s isn't a World of Warcraft Forever folder", path)
		a.mu.Lock()
		a.problem = "That folder isn't World of Warcraft Forever."
		a.mu.Unlock()
		a.publish()
		return
	}
	logf("Player chose %s", dir)
	a.stopSession()
	a.leaveRestart()
	a.mu.Lock()
	a.settings.WowDir, a.wowDir, a.running, a.restart, a.problem = dir, "", false, false, ""
	s := a.settings
	a.mu.Unlock()
	if err := saveSettings(s); err != nil {
		logf("Couldn't save settings: %v", err)
	}
	select {
	case a.rescan <- struct{}{}:
	default:
	}
}

// Do not touch the tray after this. On macOS a menu call then blocks, because the main thread stopped.
func (a *app) requestQuit() {
	a.mu.Lock()
	a.show = nil
	a.mu.Unlock()
	systray.Quit()
}

func (a *app) shutdown() {
	logf("Quitting")
	a.cancel()
	<-a.loopDone
	a.stopSession()
	a.leaveRestart()
}

// leaveRestart replaces the restart notice when the app stops watching a running WoW. A WoW restart without the app
// does not update the addon.
func (a *app) leaveRestart() {
	a.mu.Lock()
	restart, dir := a.restart, a.wowDir
	a.mu.Unlock()
	if restart {
		a.writeAppPayload(dir, "offline", offlineNote)
	}
}
