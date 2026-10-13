package main

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/thott-bot/chat"
)

const (
	offlinePayload = `{"v":4,"rev":0,"codex":"offline","note":"The thott.bot chat app isn't running."}`
	payloadFolder  = "thott-bot-chat-payload"
	slotCount      = 128
	bitCount       = 128
)

func slotName(n int) string { return fmt.Sprintf("thott-bot-chat-slot-%03d", n) }
func bitName(n int) string  { return fmt.Sprintf("thott-bot-chat-bit-%03d", n) }

func slotTOC(n int) string {
	return fmt.Sprintf(`## Interface: 16001
## Title: thott.bot chat live channel %03d
## Notes: thott.bot chat live update channel.
## Group: thott-bot-chat
## DefaultState: enabled
## LoadOnDemand: 1
## Dependencies: thott-bot-chat

..\%s\Payload.lua
`, n, payloadFolder)
}

func bitTOC(n int) string {
	return fmt.Sprintf("## Interface: 16001\n## Title: thott.bot chat bit %03d\n## Group: thott-bot-chat\n## LoadOnDemand: 1\n", n)
}

func payloadPath(wowDir string) string {
	return filepath.Join(addOnsDir(wowDir), payloadFolder, "Payload.lua")
}

type channel struct{ name, toc string }

func channels() []channel {
	var list []channel
	for n := 1; n <= slotCount; n++ {
		list = append(list, channel{slotName(n), slotTOC(n)})
	}
	for n := 1; n <= bitCount; n++ {
		list = append(list, channel{bitName(n), bitTOC(n)})
	}
	return list
}

func tocVersion(toc []byte) string {
	for _, line := range strings.Split(string(toc), "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), "## Version:"); ok {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func addOnsDir(wowDir string) string { return filepath.Join(wowDir, "Interface", "AddOns") }

func addonCurrent(addOns string) bool {
	want, _ := fs.ReadFile(chat.Addon, "thott-bot-chat/thott-bot-chat.toc")
	have, err := os.ReadFile(filepath.Join(addOns, "thott-bot-chat", "thott-bot-chat.toc"))
	if err != nil || tocVersion(have) != tocVersion(want) {
		return false
	}
	complete := true
	fs.WalkDir(chat.Addon, "thott-bot-chat", func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			want, _ := fs.ReadFile(chat.Addon, path)
			have, err := os.ReadFile(filepath.Join(addOns, filepath.FromSlash(path)))
			if err != nil || !bytes.Equal(have, want) {
				complete = false
				return fs.SkipAll
			}
		}
		return nil
	})
	return complete
}

func channelCurrent(addOns string, c channel) bool {
	toc, err := os.ReadFile(filepath.Join(addOns, c.name, c.name+".toc"))
	return err == nil && string(toc) == c.toc
}

func installNeeded(wowDir string) bool {
	addOns := addOnsDir(wowDir)
	if !addonCurrent(addOns) || !isFile(payloadPath(wowDir)) {
		return true
	}
	for _, c := range channels() {
		if !channelCurrent(addOns, c) {
			return true
		}
	}
	return false
}

func install(wowDir string) ([]string, error) {
	addOns := addOnsDir(wowDir)
	var done []string
	if err := os.MkdirAll(addOns, 0o755); err != nil {
		return done, err
	}
	// When anything is out of date, the payload on disk can be from an older app or be the restart notice. Replace it.
	needed := installNeeded(wowDir)
	if !addonCurrent(addOns) {
		if err := copyAddon(addOns); err != nil {
			return done, fmt.Errorf("installing the addon: %w", err)
		}
		want, _ := fs.ReadFile(chat.Addon, "thott-bot-chat/thott-bot-chat.toc")
		done = append(done, "installed thott.bot chat "+tocVersion(want))
	}
	if needed {
		path := payloadPath(wowDir)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return done, err
		}
		if err := os.WriteFile(path, payloadLua([]byte(offlinePayload)), 0o644); err != nil {
			return done, err
		}
		done = append(done, "wrote the offline payload")
	}
	updated := 0
	for _, c := range channels() {
		if channelCurrent(addOns, c) {
			continue
		}
		dir := filepath.Join(addOns, c.name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return done, err
		}
		if err := os.WriteFile(filepath.Join(dir, c.name+".toc"), []byte(c.toc), 0o644); err != nil {
			return done, err
		}
		updated++
	}
	if updated > 0 {
		done = append(done, fmt.Sprintf("prepared %d slot and bit addons", updated))
	}
	changed, err := collapseCategory(wowDir)
	if err != nil {
		return done, fmt.Errorf("collapsing the thott.bot chat group in the AddOns list: %w", err)
	}
	if changed {
		done = append(done, "collapsed the thott.bot chat group in the AddOns list")
	}
	return done, nil
}

func copyAddon(addOns string) error {
	target := filepath.Join(addOns, "thott-bot-chat")
	tmp := filepath.Join(addOns, ".thott-bot-chat.new")
	old := filepath.Join(addOns, ".thott-bot-chat.old")
	os.RemoveAll(tmp)
	os.RemoveAll(old)
	err := fs.WalkDir(chat.Addon, "thott-bot-chat", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		dest := filepath.Join(tmp, filepath.FromSlash(strings.TrimPrefix(path, "thott-bot-chat")))
		if d.IsDir() {
			return os.MkdirAll(dest, 0o755)
		}
		data, err := fs.ReadFile(chat.Addon, path)
		if err != nil {
			return err
		}
		return os.WriteFile(dest, data, 0o644)
	})
	if err != nil {
		os.RemoveAll(tmp)
		return err
	}
	if err := os.Rename(target, old); err != nil && !errors.Is(err, fs.ErrNotExist) {
		os.RemoveAll(tmp)
		return err
	}
	if err := os.Rename(tmp, target); err != nil {
		os.Rename(old, target)
		return err
	}
	return os.RemoveAll(old)
}

var collapsedDeclaration = regexp.MustCompile(`g_addonCategoriesCollapsed\s*=\s*\{`)

func collapseCategory(wowDir string) (bool, error) {
	dir := filepath.Join(wowDir, "WTF", "SavedVariables")
	path := filepath.Join(dir, "Blizzard_AddOnList.lua")
	source, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return false, err
	}

	if bytes.Contains(source, []byte(`["thott-bot-chat"] =`)) {
		return false, nil
	}
	if !collapsedDeclaration.Match(source) {
		source = append(source, "g_addonCategoriesCollapsed = {\n}\n"...)
	}
	loc := collapsedDeclaration.FindIndex(source)
	updated := append(append(append([]byte{}, source[:loc[1]]...), "\n\t[\"thott-bot-chat\"] = true,"...), source[loc[1]:]...)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return false, err
	}
	return true, writeFileAtomic(path, updated)
}
