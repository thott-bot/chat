package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInstallIsFoundFromTheGameProcess(t *testing.T) {
	root := t.TempDir()
	install := filepath.Join(root, "World of Warcraft", foreverFolder)
	macOS := filepath.Join(install, "World of Warcraft.app", "Contents", "MacOS")
	beta := filepath.Join(root, "World of Warcraft", betaFolder)
	betaMacOS := filepath.Join(beta, "World of Warcraft Beta.app", "Contents", "MacOS")
	retail := filepath.Join(root, "World of Warcraft", "_retail_")
	other := filepath.Join(root, "Other")
	for _, dir := range []string{macOS, betaMacOS, retail, filepath.Join(other, "Interface")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	os.WriteFile(filepath.Join(install, ".flavor.info"), []byte("Product Flavor!STRING:0\nwowf\n"), 0o600)
	os.WriteFile(filepath.Join(beta, ".flavor.info"), []byte("Product Flavor!STRING:0\nwow_classic_beta\n"), 0o600)
	os.WriteFile(filepath.Join(retail, ".flavor.info"), []byte("Product Flavor!STRING:0\nwow\n"), 0o600)
	for process, want := range map[string]string{
		filepath.Join(macOS, "World of Warcraft"):          install,
		filepath.Join(install, "Wow.exe"):                  install,
		filepath.Join(betaMacOS, "World of Warcraft Beta"): beta,
		filepath.Join(beta, "WowB.exe"):                    beta,
		filepath.Join(retail, "Wow.exe"):                   "",
		filepath.Join(other, "tool"):                       "",
	} {
		if got := installOf(process); got != want {
			t.Errorf("%s: install %q, want %q", process, got, want)
		}
	}
	if !validWowDir(other) || validWowDir(retail) || !validWowDir(install) || !validWowDir(beta) {
		t.Fatal("a chosen folder is judged by its marks when it has them, else by its folders")
	}
	if got := resolveWowDir(beta); got != install {
		t.Fatalf("a saved beta folder beside live resolved to %q", got)
	}
	if got := normalizeChoice(filepath.Join(root, "World of Warcraft")); got != install {
		t.Fatalf("the World of Warcraft folder with both resolved to %q", got)
	}
}
