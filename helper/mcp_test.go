package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func rpcClientFor(t *testing.T, respond func(string, json.RawMessage) (json.RawMessage, *rpcError)) *codexClient {
	t.Helper()
	c := &codexClient{ctx: context.Background(), agentProcess: agentProcess{done: make(chan struct{})}, pending: map[int64]chan rpcMessage{}}
	c.stdin = rpcSink(func(data []byte) {
		var m rpcMessage
		if err := json.Unmarshal(data, &m); err != nil {
			t.Fatal(err)
		}
		result, err := respond(m.Method, m.Params)
		c.resolve(rpcMessage{ID: m.ID, Result: result, Error: err})
	})
	return c
}

func TestCodexServerNamesUseMetadataAndConfiguredNames(t *testing.T) {
	s := newSession("", nil, func() {})
	calls := 0
	s.client = rpcClientFor(t, func(method string, raw json.RawMessage) (json.RawMessage, *rpcError) {
		calls++
		var p struct {
			Cursor, Detail, ThreadID string
			Limit                    int
		}
		json.Unmarshal(raw, &p)
		if method != "mcpServerStatus/list" || p.Detail != "toolsAndAuthOnly" || p.Limit != 100 {
			t.Fatalf("unexpected request %s %s", method, raw)
		}
		if p.Cursor == "" {
			return json.RawMessage(`{"data":[{"name":"configured-one","serverInfo":{"title":"thott.bot","name":"legacy"}},{"name":"other","serverInfo":{"title":"Other friendly title","name":"different"}}],"nextCursor":"page2"}`), nil
		}
		return json.RawMessage(`{"data":[{"name":"configured-two","serverInfo":{"title":null,"name":"thott.bot"}},{"name":"unknown","serverInfo":null}],"nextCursor":null}`), nil
	})
	s.loadServerNames("")
	want := map[string]string{"configured-one": "thott.bot", "configured-two": "thott.bot", "other": "other", "unknown": "unknown"}
	if calls != 2 || !reflect.DeepEqual(s.serverNames, want) {
		t.Fatalf("calls=%d names=%v", calls, s.serverNames)
	}
	a := activity{serverNames: s.serverNames}
	a.add(cxItem{Type: "mcpToolCall", Server: "configured-one", Tool: "search"}, "")
	if a.details[0] != "thott.bot · search" || describe(cxItem{Type: "mcpToolCall", Server: "other", Tool: "lookup"}, s.serverNames) != "other · lookup" {
		t.Fatalf("tool lines: %v", a.details)
	}
}

func TestClaudeServerNamesFollowConfigScopes(t *testing.T) {
	config, project := t.TempDir(), t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", config)
	user := map[string]any{"mcpServers": map[string]any{
		"service":   map[string]string{"url": "https://mcp.thott.bot/mcp"},
		"other":     map[string]string{"url": "https://other.example/mcp"},
		"lookalike": map[string]string{"url": "https://mcp.thott.bot.attacker.example/mcp"},
	}, "projects": map[string]any{filepath.ToSlash(project): map[string]any{"mcpServers": map[string]any{"local": map[string]string{"url": "https://mcp.thott.bot:443/mcp"}}}}}
	data, _ := json.Marshal(user)
	os.WriteFile(filepath.Join(config, ".claude.json"), data, 0o600)
	os.WriteFile(filepath.Join(project, ".mcp.json"), []byte(`{"mcpServers":{"service":{"url":"https://project.example/mcp"},"project":{"url":"https://MCP.THOTT.BOT/mcp"}}}`), 0o600)
	names := claudeServerNames(config, project)
	want := map[string]string{"service": "service", "other": "other", "lookalike": "lookalike", "project": "thott.bot", "local": "thott.bot"}
	if !reflect.DeepEqual(names, want) || claudeTool("mcp__local__search", names) != "thott.bot · search" || claudeTool("mcp__other__lookup", names) != "other · lookup" {
		t.Fatalf("names=%v", names)
	}
}
