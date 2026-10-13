package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
)

func claudeConfigDir() string {
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		return dir
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude")
}

var sessionUUID = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// Do the same as Claude Code's /rename and the Agent SDK's rename_session: append one custom-title record to the
// session. Do not rewrite the transcript. Source: session_mutations.py in
// https://github.com/anthropics/claude-agent-sdk-python.
func renameClaudeSession(configDir, id, title string) error {
	if !sessionUUID.MatchString(id) {
		return errors.New("invalid Claude session ID")
	}
	paths, err := filepath.Glob(filepath.Join(configDir, "projects", "*", id+".jsonl"))
	if err != nil {
		return err
	}
	var found string
	for _, path := range paths {
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() && info.Size() > 0 {
			if found != "" {
				return errors.New("Claude has more than one transcript for this session")
			}
			found = path
		}
	}
	if found == "" {
		return errors.New("Claude hasn't saved this session yet")
	}
	f, err := os.OpenFile(found, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	data, err := json.Marshal(map[string]string{"type": "custom-title", "customTitle": title, "sessionId": id})
	if err == nil {
		_, err = f.Write(append(data, '\n'))
	}
	return errors.Join(err, f.Close())
}
