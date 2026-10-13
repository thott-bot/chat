//go:build !darwin && !windows

package main

import (
	"errors"
	"os"
)

// No release is built for this system, so the app neither moves nor updates itself.
const (
	appName      = "thott.bot chat"
	releaseAsset = ""
)

var (
	appHomes   = func() []string { return nil }
	runningApp = os.Executable
	askToMove  = func(app, home string) (bool, error) { return false, nil }
)

func appExe(app string) string { return app }

func copyApp(src, dst string) error { return errors.ErrUnsupported }

func extractApp(zip, dir string) (string, error) { return "", errors.ErrUnsupported }

func swapApp(staged, dest string) error { return errors.ErrUnsupported }

func addShortcut(string) error { return nil }

func originalPath(app string) (string, error) { return app, nil }
