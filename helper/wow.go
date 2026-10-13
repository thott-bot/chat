package main

import (
	"os"
	"path/filepath"
	"strings"
)

const foreverFolder = "_classic_beta_"

// Battle.net records the game product in the folder's .flavor.info.
func flavorOf(dir string) (string, bool) {
	flavor, err := os.ReadFile(filepath.Join(dir, ".flavor.info"))
	if err != nil {
		return "", false
	}
	_, product, ok := strings.Cut(string(flavor), "\n")
	return strings.TrimSpace(product), ok
}

// Use Blizzard's own marks for a Forever folder: the flavor file, or else the executable.
func foreverInstall(dir string) bool {
	if product, ok := flavorOf(dir); ok {
		return product == "wow_classic_beta"
	}
	return isFile(filepath.Join(dir, "WowB.exe"))
}

// On macOS the game runs from an .app inside the _classic_beta_ folder, so the executable's own folder is never the
// install.
func installOf(path string) string {
	for dir := filepath.Dir(path); ; dir = filepath.Dir(dir) {
		if filepath.Base(dir) == foreverFolder && foreverInstall(dir) {
			return dir
		}
		if filepath.Dir(dir) == dir {
			return ""
		}
	}
}

func validWowDir(dir string) bool {
	if dir == "" {
		return false
	}
	if _, ok := flavorOf(dir); ok || isFile(filepath.Join(dir, "WowB.exe")) {
		return foreverInstall(dir)
	}
	for _, name := range []string{"Interface", "WTF", "Logs"} {
		if info, err := os.Stat(filepath.Join(dir, name)); err == nil && info.IsDir() {
			return true
		}
	}
	return false
}

func resolveWowDir(saved string) string {
	for _, dir := range append([]string{saved}, wowCandidates...) {
		if validWowDir(dir) {
			return filepath.Clean(dir)
		}
	}
	for _, path := range processPaths() {
		if dir := installOf(path); dir != "" {
			return dir
		}
	}
	return ""
}

func normalizeChoice(dir string) string {
	dir = filepath.Clean(strings.TrimSpace(dir))
	if inner := filepath.Join(dir, foreverFolder); validWowDir(inner) {
		return inner
	}
	if validWowDir(dir) {
		return dir
	}
	return ""
}

func wowRunning(dir string) bool {
	if dir == "" {
		return false
	}
	prefix := strings.ToLower(filepath.Clean(dir) + string(filepath.Separator))
	for _, path := range processPaths() {
		if strings.HasPrefix(strings.ToLower(path), prefix) {
			return true
		}
	}
	return false
}
