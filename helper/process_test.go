package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	switch os.Getenv("THOTT_BOT_CHAT_TEST_MODE") {
	case "args":
		json.NewEncoder(os.Stdout).Encode(os.Args[1:])
		os.Exit(0)
	case "claude":
		fakeClaude()
		os.Exit(0)
	case "codex":
		if strings.Join(os.Args[1:], " ") == "auth status" {
			fmt.Print(`{"loggedIn":true}`)
			os.Exit(0)
		}
		fakeCodex()
		os.Exit(0)
	case "claude-auth":
		if strings.Join(os.Args[1:], " ") != "auth status" {
			os.Exit(2)
		}
		fmt.Print(os.Getenv("THOTT_BOT_CHAT_AUTH_RESULT"))
		if os.Getenv("THOTT_BOT_CHAT_AUTH_FAILED") == "1" {
			os.Exit(1)
		}
		os.Exit(0)
	case "main":
		main()
		os.Exit(0)
	case "wait-lock":
		if _, ok := lockFile(os.Getenv("THOTT_BOT_CHAT_TEST_LOCK"), true); ok {
			os.WriteFile(os.Getenv("THOTT_BOT_CHAT_TEST_MARKER"), []byte("locked"), 0o600)
		}
		os.Exit(0)
	case "relaunched":
		os.WriteFile(os.Getenv("THOTT_BOT_CHAT_TEST_MARKER"), []byte(os.Getenv(handoffEnv)+"\n"+os.Getenv(movedFromEnv)), 0o600)
		os.Exit(0)
	case "original-path":
		app, err := runningApp()
		if err == nil {
			var from string
			from, err = originalPath(app)
			app += "\n" + from
		}
		if err != nil {
			app = err.Error()
		}
		os.WriteFile(os.Getenv("THOTT_BOT_CHAT_TEST_MARKER"), []byte(app), 0o600)
		os.Exit(0)
	case "child":
		for {
			time.Sleep(time.Hour)
		}
	}
	os.Exit(m.Run())
}

// fakeCodex acts as a Codex app server on the same pipes, with replies in Codex's shape. It logs each line it gets to
// CODEX_LOG, and logs "eof" when its input closes and it exits. While the file CODEX_HOLD exists, thread/list and the
// notifications of a started turn wait. With "auth status", it answers as a signed-in Claude Code.
func fakeCodex() {
	log, err := os.OpenFile(os.Getenv("CODEX_LOG"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		panic(err)
	}
	defer log.Close()
	out := json.NewEncoder(os.Stdout)
	send := func(method string, params any) { out.Encode(map[string]any{"method": method, "params": params}) }
	threads := []map[string]any{{"id": "thread-1", "name": "Show mana ticks", "cwd": os.Getenv("CODEX_WORK"), "status": map[string]string{"type": "notLoaded"}, "updatedAt": 1790000000}}
	var held func()
	lines := bufio.NewScanner(os.Stdin)
	for lines.Scan() {
		fmt.Fprintln(log, lines.Text())
		var m struct {
			ID     json.RawMessage
			Method string
			Params map[string]any
		}
		json.Unmarshal(lines.Bytes(), &m)
		if len(m.ID) == 0 {
			continue
		}
		var result any
		switch m.Method {
		case "initialize":
			result = map[string]string{"userAgent": "fake-codex/0.160.0"}
		case "account/read":
			result = map[string]any{"account": map[string]string{"type": "apiKey"}, "requiresOpenaiAuth": true}
		case "model/list":
			efforts := []map[string]string{{"reasoningEffort": "low"}, {"reasoningEffort": "medium"}, {"reasoningEffort": "high"}}
			result = map[string]any{"data": []map[string]any{{"id": "gpt-5.5", "displayName": "GPT-5.5", "isDefault": true, "defaultReasoningEffort": "medium", "supportedReasoningEfforts": efforts}}, "nextCursor": nil}
		case "mcpServerStatus/list", "thread/turns/list":
			result = map[string]any{"data": []any{}, "nextCursor": nil}
		case "thread/list":
			for _, err := os.Stat(os.Getenv("CODEX_HOLD")); err == nil; _, err = os.Stat(os.Getenv("CODEX_HOLD")) {
				time.Sleep(10 * time.Millisecond)
			}
			if held != nil {
				held()
				held = nil
			}
			result = map[string]any{"data": threads, "nextCursor": nil}
		case "thread/start":
			thread := map[string]any{"id": "thread-2", "cwd": m.Params["cwd"], "model": m.Params["model"], "reasoningEffort": "medium", "status": map[string]string{"type": "idle"}, "updatedAt": 1790000100}
			threads = append([]map[string]any{thread}, threads...)
			result = map[string]any{"thread": thread}
		case "turn/start":
			turn := map[string]any{"id": "turn-1", "status": "inProgress", "items": []any{}, "startedAt": 1790000100}
			out.Encode(map[string]any{"id": m.ID, "result": map[string]any{"turn": turn}})
			thread := m.Params["threadId"]
			threads[0]["reasoningEffort"] = m.Params["effort"]
			held = func() {
				send("turn/started", map[string]any{"threadId": thread, "turn": turn})
				send("thread/status/changed", map[string]any{"threadId": thread, "status": map[string]any{"type": "active", "activeFlags": []string{}}})
				send("item/completed", map[string]any{"threadId": thread, "turnId": "turn-1", "item": map[string]string{"type": "agentMessage", "id": "item-1", "text": "Mana ticks are on."}})
				send("turn/completed", map[string]any{"threadId": thread, "turn": map[string]any{"id": "turn-1", "status": "completed", "items": []any{}, "completedAt": 1790000160}})
				send("thread/status/changed", map[string]any{"threadId": thread, "status": map[string]any{"type": "idle"}})
			}
			continue
		default:
			out.Encode(map[string]any{"id": m.ID, "error": map[string]any{"code": -32601, "message": "fake Codex can't answer " + m.Method}})
			continue
		}
		out.Encode(map[string]any{"id": m.ID, "result": result})
	}
	fmt.Fprintln(log, "eof")
}

// fakeClaude replays real Claude Code stream fixtures from testdata.
func fakeClaude() {
	log, err := os.OpenFile(os.Getenv("CLAUDE_LOG"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		panic(err)
	}
	defer log.Close()
	cwd, _ := os.Getwd()
	fmt.Fprintf(log, "start %s %s\n", cwd, strings.Join(os.Args[1:], " "))
	play := func(name string) {
		data, err := os.ReadFile(filepath.Join(os.Getenv("THOTT_BOT_CHAT_TESTDATA"), name+".jsonl"))
		if err != nil {
			panic(err)
		}
		escaped, _ := json.Marshal(cwd)
		fmt.Print(strings.ReplaceAll(string(data), "WORK", string(escaped[1:len(escaped)-1])))
	}
	lines := bufio.NewScanner(os.Stdin)
	for lines.Scan() {
		line := lines.Text()
		fmt.Fprintln(log, line)
		switch {
		case strings.Contains(line, `"behavior":"deny"`):
			play("denied")
		case strings.Contains(line, `"behavior":"allow"`):
			play("running")
		case strings.Contains(line, `"subtype":"interrupt"`):
			play("interrupted")
		case strings.Contains(line, `"content":`) && strings.Contains(line, "crash"):
			fmt.Fprintln(os.Stderr, "boom")
			os.Exit(3)
		case strings.Contains(line, `"content":`) && strings.Contains(line, "notes"):
			play("ask")
		case strings.Contains(line, `"content":`) && strings.Contains(line, "ping"):
			play("ping")
		case strings.Contains(line, `"type":"user"`):
			play("ls")
		}
	}
}
