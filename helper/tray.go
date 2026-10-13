package main

import (
	_ "embed"

	"fyne.io/systray"
)

var (
	//go:embed assets/tray-template.png
	trayTemplate []byte
	//go:embed assets/tray.ico
	trayIcon []byte
	//go:embed assets/warning-template.png
	warningTemplate []byte
	//go:embed assets/warning.ico
	warningIcon []byte
)

func (a *app) trayReady() {
	systray.SetTemplateIcon(trayTemplate, trayIcon)
	systray.SetTooltip("thott.bot chat")

	// The tray library cannot remove an item's icon, so a status that needs the player has its own item.
	status := systray.AddMenuItem("Waiting for World of Warcraft", "")
	status.Disable()
	alert := systray.AddMenuItem("", "")
	alert.SetTemplateIcon(warningTemplate, warningIcon)
	alert.Disable()
	alert.Hide()
	detail := systray.AddMenuItem("", "")
	detail.Disable()
	detail.Hide()
	installCodex := systray.AddMenuItem("Install Codex CLI…", "")
	installCodex.Hide()
	installClaude := systray.AddMenuItem("Install Claude Code CLI…", "")
	installClaude.Hide()
	systray.AddSeparator()
	runWith := systray.AddMenuItemCheckbox("Run with WoW", "Open thott.bot chat when World of Warcraft starts, and quit when it quits", a.view().runWithWoW)
	choose := systray.AddMenuItem("Choose WoW Folder…", "")
	choose.Hide()
	systray.AddSeparator()
	update := systray.AddMenuItem("Update", "")
	update.Hide()
	check := systray.AddMenuItem("Check for Updates…", "")
	github := systray.AddMenuItem("Source on GitHub", "")
	quit := systray.AddMenuItem("Quit thott.bot chat", "")

	a.mu.Lock()
	a.show = func(v trayView) {
		status.SetTitle(v.status)
		alert.SetTitle(v.status)
		visible(status, !v.alert)
		visible(alert, v.alert)
		detail.SetTitle(v.detail)
		visible(detail, v.detail != "")
		visible(choose, v.choose)
		visible(installCodex, v.install)
		visible(installClaude, v.install)
		update.SetTitle(updateTitle(v))
		visible(update, v.update != "")
		if v.updating == updateReady || v.updating == updateFailed {
			update.Enable()
		} else {
			update.Disable()
		}
		check.SetTitle(map[updateCheck]string{checkIdle: "Check for Updates…", checkRunning: "Checking for Updates…", checkUpToDate: "Up to Date"}[v.check])
		if v.check == checkIdle {
			check.Enable()
		} else {
			check.Disable()
		}
		if v.runWithWoW {
			runWith.Check()
		} else {
			runWith.Uncheck()
		}
	}
	runWithWoW := a.settings.RunWithWoW
	a.mu.Unlock()

	if runWithWoW && !a.fromWatch {
		a.startWatcher()
	}
	go a.loop()
	go a.checkUpdates()

	for {
		select {
		case <-a.ctx.Done():
			return
		case <-runWith.ClickedCh:
			a.setRunWithWoW(!a.view().runWithWoW)
		case <-choose.ClickedCh:
			go a.chooseFolder()
		case <-update.ClickedCh:
			a.mu.Lock()
			stage, url := a.updateStage, a.updateURL
			a.mu.Unlock()
			if stage == updateFailed {
				openURL(url)
			} else {
				go a.installUpdate(true)
			}
		case <-check.ClickedCh:
			a.requestUpdateCheck()
		case <-installCodex.ClickedCh:
			openURL(codexURL)
		case <-installClaude.ClickedCh:
			openURL(claudeURL)
		case <-github.ClickedCh:
			openURL(repoURL)
		case <-quit.ClickedCh:
			a.exit()
			return
		}
	}
}

func updateTitle(v trayView) string {
	switch v.updating {
	case updateDownloading:
		return "Downloading thott.bot chat " + v.update + "…"
	case updateReady:
		return "Install " + v.update + " and Restart"
	case updateInstalling:
		return "Installing thott.bot chat " + v.update + "…"
	default:
		return "Update Failed: Download " + v.update + " from GitHub…"
	}
}

func visible(item *systray.MenuItem, show bool) {
	if show {
		item.Show()
	} else {
		item.Hide()
	}
}
