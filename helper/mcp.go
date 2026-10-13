package main

import (
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

func toolLine(server, tool string) string {
	if server == "" {
		return tool
	}
	return server + " · " + tool
}

// Read MCP presentation metadata at connection time. Do not poll it with each thread list or tool call.
func (s *session) loadServerNames(threadID string) {
	names := map[string]string{}
	cursor := ""
	for {
		params := map[string]any{"limit": 100, "detail": "toolsAndAuthOnly"}
		if cursor != "" {
			params["cursor"] = cursor
		}
		if threadID != "" {
			params["threadId"] = threadID
		}
		var r struct {
			Data []struct {
				Name string `json:"name"`
				Info *struct {
					Title string `json:"title"`
					Name  string `json:"name"`
				} `json:"serverInfo"`
			} `json:"data"`
			NextCursor string `json:"nextCursor"`
		}
		if err := s.client.call("mcpServerStatus/list", params, &r); err != nil {
			logf("Couldn't read MCP server names: %v", err)
			return
		}
		for _, server := range r.Data {
			names[server.Name] = server.Name
			if server.Info != nil {
				if display := firstOf(strings.TrimSpace(server.Info.Title), server.Info.Name); display == "thott.bot" {
					names[server.Name] = display
				}
			}
		}
		if r.NextCursor == "" || r.NextCursor == cursor {
			break
		}
		cursor = r.NextCursor
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if threadID == "" {
		for name, display := range names {
			s.serverNames[name] = display
		}
	} else if t := s.threads[threadID]; t != nil {
		t.serverNames = names
	}
	s.dirty = true
}

type claudeMCPConfig struct {
	Servers map[string]struct {
		URL string `json:"url"`
	} `json:"mcpServers"`
	Projects map[string]json.RawMessage `json:"projects"`
}

func claudeServerNames(configDir, cwd string) map[string]string {
	home, _ := os.UserHomeDir()
	configFile := filepath.Join(home, ".claude.json")
	if os.Getenv("CLAUDE_CONFIG_DIR") != "" {
		configFile = filepath.Join(configDir, ".claude.json")
	}
	read := func(path string) claudeMCPConfig {
		var c claudeMCPConfig
		if data, err := os.ReadFile(path); err == nil {
			json.Unmarshal(data, &c)
		}
		return c
	}
	names := map[string]string{}
	add := func(c claudeMCPConfig) {
		for name, server := range c.Servers {
			names[name] = name
			if u, err := url.Parse(server.URL); err == nil && strings.EqualFold(u.Hostname(), "mcp.thott.bot") {
				names[name] = "thott.bot"
			}
		}
	}
	user := read(configFile)
	add(user)
	add(read(filepath.Join(cwd, ".mcp.json")))
	var local claudeMCPConfig
	for project, config := range user.Projects {
		if filepath.Clean(filepath.FromSlash(project)) == filepath.Clean(cwd) {
			json.Unmarshal(config, &local)
			break
		}
	}
	add(local) // Claude Code's precedence: local, then project, then user.
	return names
}
