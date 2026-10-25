package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestInstall(t *testing.T) {
	wow := t.TempDir()
	addOns := addOnsDir(wow)
	saved := filepath.Join(wow, "WTF", "SavedVariables")
	os.MkdirAll(saved, 0o755)
	os.WriteFile(filepath.Join(saved, "Blizzard_AddOnList.lua"), []byte("g_addonCategoriesCollapsed = {\n\t[\"Other\"] = true,\n}\nOtherVar = 1\n"), 0o644)

	if !installNeeded(wow) {
		t.Fatal("empty folder should need setup")
	}
	_, err := install(wow)
	if err != nil {
		t.Fatal(err)
	}
	if installNeeded(wow) {
		t.Fatal("still needs setup after install")
	}
	toc, _ := os.ReadFile(filepath.Join(addOns, "thott-bot-chat-slot-128", "thott-bot-chat-slot-128.toc"))
	if !strings.Contains(string(toc), `..\thott-bot-chat-payload\Payload.lua`) {
		t.Fatalf("slot toc:\n%s", toc)
	}
	bit, _ := os.ReadFile(filepath.Join(addOns, "thott-bot-chat-bit-128", "thott-bot-chat-bit-128.toc"))
	if !regexp.MustCompile(`(?m)^##\s*Interface:\s*16001\s*$`).Match(bit) || !regexp.MustCompile(`(?m)^##\s*LoadOnDemand:\s*1\s*$`).Match(bit) {
		t.Fatalf("bit toc:\n%s", bit)
	}
	payload := readPayloadFile(t, payloadPath(wow))
	if payload.V != protocolVersion || payload.Codex != "offline" {
		t.Fatalf("initial payload: %+v", payload)
	}
	list, _ := os.ReadFile(filepath.Join(saved, "Blizzard_AddOnList.lua"))
	if !regexp.MustCompile(`\["thott-bot-chat"\]\s*=\s*true`).Match(list) || !regexp.MustCompile(`\["Other"\]\s*=\s*true`).Match(list) || !regexp.MustCompile(`OtherVar\s*=\s*1`).Match(list) {
		t.Fatalf("AddOn list:\n%s", list)
	}

	os.WriteFile(payloadPath(wow), []byte("live"), 0o644)
	if done, err := install(wow); err != nil {
		t.Fatalf("second install: %q %v", done, err)
	}
	if live, _ := os.ReadFile(payloadPath(wow)); string(live) != "live" {
		t.Fatal("reinstall overwrote a payload")
	}

	os.RemoveAll(filepath.Join(addOns, "thott-bot-chat-bit-064"))
	if !installNeeded(wow) {
		t.Fatal("missing bit not detected")
	}
	os.Remove(payloadPath(wow))
	if !installNeeded(wow) {
		t.Fatal("missing payload not detected")
	}
	if done, err := install(wow); err != nil || installNeeded(wow) {
		t.Fatalf("repair: %q %v", done, err)
	}
	tocPath := filepath.Join(addOns, "thott-bot-chat", "thott-bot-chat.toc")
	os.WriteFile(tocPath, []byte("## Version: 0.0.1\n"), 0o644)
	os.WriteFile(filepath.Join(addOns, "thott-bot-chat", "Stale.lua"), []byte("x"), 0o644)
	if !installNeeded(wow) {
		t.Fatal("old version not detected")
	}
	os.WriteFile(payloadPath(wow), payloadLua([]byte(`{"v":3,"rev":0,"codex":"offline"}`)), 0o644)
	if done, err := install(wow); err != nil {
		t.Fatalf("upgrade: %q %v", done, err)
	}
	if _, err := os.Stat(filepath.Join(addOns, "thott-bot-chat", "Stale.lua")); !os.IsNotExist(err) {
		t.Fatal("upgrade kept files from the old version")
	}
	if p := readPayloadFile(t, payloadPath(wow)); p.V != protocolVersion || p.Codex != "offline" {
		t.Fatalf("upgrade kept the older app's payload: %+v", p)
	}
}

func TestCollapseCategoryCreatesFile(t *testing.T) {
	wow := t.TempDir()
	changed, err := collapseCategory(wow)
	if err != nil || !changed {
		t.Fatalf("%v %v", changed, err)
	}
	data, _ := os.ReadFile(filepath.Join(wow, "WTF", "SavedVariables", "Blizzard_AddOnList.lua"))
	if !regexp.MustCompile(`g_addonCategoriesCollapsed\s*=\s*\{\s*\["thott-bot-chat"\]\s*=\s*true`).Match(data) {
		t.Fatalf("got:\n%s", data)
	}
	if changed, _ := collapseCategory(wow); changed {
		t.Fatal("collapsed twice")
	}
}

func TestNormalizeChoice(t *testing.T) {
	root := t.TempDir()
	forever := filepath.Join(root, foreverFolder)
	os.MkdirAll(filepath.Join(forever, "Interface"), 0o755)
	if got := normalizeChoice(root); got != forever {
		t.Fatalf("root: got %q", got)
	}
	if got := normalizeChoice(forever + string(filepath.Separator)); got != forever {
		t.Fatalf("forever: got %q", got)
	}
	if got := normalizeChoice(t.TempDir()); got != "" {
		t.Fatalf("empty folder: got %q", got)
	}
}

func TestFreshForeverInstall(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, foreverFolder)
	os.MkdirAll(dir, 0o700)
	os.WriteFile(filepath.Join(dir, ".flavor.info"), []byte("Product Flavor!STRING:0\r\nwowf\r\n"), 0o600)
	if got := normalizeChoice(root); got != dir {
		t.Fatalf("fresh install before first launch = %q", got)
	}
	os.WriteFile(filepath.Join(dir, ".flavor.info"), []byte("Product Flavor!STRING:0\nwow_classic\n"), 0o600)
	os.MkdirAll(filepath.Join(dir, "Interface"), 0o700)
	if validWowDir(dir) {
		t.Fatal("accepted another WoW product")
	}
}

func TestInstallRepairsSameVersionFiles(t *testing.T) {
	dir := t.TempDir()
	if _, err := install(dir); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(addOnsDir(dir), "thott-bot-chat", "Core.lua")
	os.WriteFile(path, []byte("corrupt"), 0o600)
	if !installNeeded(dir) {
		t.Fatal("a same-version damaged addon was considered current")
	}
	if _, err := install(dir); err != nil || installNeeded(dir) {
		t.Fatalf("repair: %v", err)
	}
}
