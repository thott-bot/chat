package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type settings struct {
	WowDir       string `json:"wowDir"`
	RunWithWoW   bool   `json:"runWithWoW"`
	DeclinedMove bool   `json:"declinedMove,omitempty"`
}

func configDir() string {
	base, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(base, "thott-bot-chat")
}

func loadSettings() settings {
	path := filepath.Join(configDir(), "settings.json")
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return settings{}
	}
	var s settings
	if err == nil {
		err = json.Unmarshal(data, &s)
	}
	if err != nil {
		logf("Couldn't read %s, so thott.bot chat is using default settings: %v. Choosing a WoW folder or turning on Run with WoW saves new ones.", path, err)
		return settings{}
	}
	return s
}

func saveSettings(s settings) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(filepath.Join(configDir(), "settings.json"), append(data, '\n'))
}

func writeFileAtomic(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+"-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return replaceFile(f.Name(), path)
}

const logLimit = 1 << 20

var logState struct {
	sync.Mutex
	file   *os.File
	prefix string
}

func openLog(prefix string) {
	logState.Lock()
	defer logState.Unlock()
	logState.prefix = prefix
	f, err := os.OpenFile(filepath.Join(configDir(), "helper.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err == nil {
		if logState.file != nil {
			logState.file.Close()
		}
		f.Chmod(0o600)
		logState.file = f
	}
}

func logf(format string, args ...any) {
	logState.Lock()
	defer logState.Unlock()
	line := time.Now().Format("2006-01-02 15:04:05 ") + logState.prefix + fmt.Sprintf(format, args...) + "\n"
	f := logState.file
	if f == nil {
		os.Stderr.WriteString(line)
		return
	}
	if info, err := f.Stat(); err == nil && info.Size() > logLimit {
		f.Truncate(0)
	}
	f.WriteString(line)
}
