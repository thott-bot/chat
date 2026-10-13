//go:build !darwin && !windows

package main

import (
	"errors"
	"os/exec"
)

var (
	wowCandidates []string
	extraPath     []string
)

func processPaths() []string { return nil }

func chooseFolder() (string, error) { return "", errors.ErrUnsupported }

func openURL(url string) {
	cmd := exec.Command("xdg-open", url)
	if cmd.Start() == nil {
		go cmd.Wait()
	}
}

func setAutostart(bool, string) error { return errors.ErrUnsupported }

func findInstalledOS(string) string { return "" }
