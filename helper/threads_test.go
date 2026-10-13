package main

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
)

func TestStateFor(t *testing.T) {
	cases := []struct {
		status string
		want   string
	}{
		{`{"type":"active","activeFlags":["waitingOnApproval"]}`, "needs_you"},
		{`{"type":"active","activeFlags":["waitingOnUserInput"]}`, "needs_you"},
		{`{"type":"active","activeFlags":[]}`, "active"},
		{`{"type":"idle"}`, "idle"},
		{`{"type":"systemError"}`, "error"},
		{`{"type":"notLoaded"}`, "history"},
	}
	for _, c := range cases {
		var s cxStatus
		if err := json.Unmarshal([]byte(c.status), &s); err != nil {
			t.Fatal(err)
		}
		if got := stateFor(s); got != c.want {
			t.Errorf("%s: got %s, want %s", c.status, got, c.want)
		}
	}
}

const turnJSON = `{"id":"turn1","status":"completed","startedAt":1790000000,"completedAt":1790000060,"error":null,"items":[
 {"type":"userMessage","id":"u1","clientId":null,"content":[{"type":"text","text":"<wow_character_state captured=\"2026-09-21T14:13:20Z\" source=\"thott.bot chat addon\">\nCharacter: level 12\n</wow_character_state>\n\nTrim the index","text_elements":[]}]},
 {"type":"reasoning","id":"r1","summary":["thinking"],"content":["raw"]},
 {"type":"commandExecution","id":"c1","command":"sed -n 1,80p /repo/lib/a.mjs","cwd":"/repo","status":"completed","commandActions":[{"type":"read","command":"sed -n 1,80p /repo/lib/a.mjs","name":"a.mjs","path":"/repo/lib/a.mjs"}],"aggregatedOutput":"...","exitCode":0},
 {"type":"commandExecution","id":"c2","command":"du -h build/index.sqlite","cwd":"/repo","status":"completed","commandActions":[{"type":"unknown","command":"du -h build/index.sqlite"}]},
 {"type":"fileChange","id":"f1","status":"completed","changes":[{"path":"/repo/UI.lua","kind":{"type":"update","move_path":null},"diff":"..."},{"path":"/repo/New.lua","kind":{"type":"add"},"diff":"..."}]},
 {"type":"mcpToolCall","id":"m1","server":"thott","tool":"search","status":"completed","arguments":{}},
 {"type":"webSearch","id":"w1","query":"wow forever patch notes","action":null},
 {"type":"agentMessage","id":"a1","text":"Looking now.","phase":"commentary"},
 {"type":"agentMessage","id":"a2","text":"Done: trimmed it.","phase":"final_answer"}
]}`

func TestTranscript(t *testing.T) {
	var turn cxTurn
	if err := json.Unmarshal([]byte(turnJSON), &turn); err != nil {
		t.Fatal(err)
	}
	failed := cxTurn{ID: "turn2", Status: "failed", StartedAt: 1790000100, CompletedAt: 1790000101,
		Items: []cxItem{{Type: "userMessage", Content: json.RawMessage(`[{"type":"text","text":"Again"}]`)}}}
	failed.Error = &struct {
		Message string `json:"message"`
	}{"stream disconnected"}

	got := transcript([]cxTurn{turn, failed}, "/repo", map[string]string{"thott": "thott.bot"})
	want := []payloadTurn{
		{Role: "user", Text: "Trim the index", At: 1790000000, Context: true},
		{Role: "activity", Text: "Read 1 file, ran 1 command, changed 2 files, used 1 tool, searched the web", Items: []string{
			"Read lib/a.mjs", "Ran du -h build/index.sqlite", "Edited UI.lua", "Created New.lua",
			"thott.bot · search", "Searched the web for wow forever patch notes",
		}},
		{Role: "assistant", Text: "Done: trimmed it.", At: 1790000060},
		{Role: "user", Text: "Again", At: 1790000100},
		{Role: "error", Text: "stream disconnected", At: 1790000101},
	}
	if !reflect.DeepEqual(got, want) {
		gotJSON, _ := json.MarshalIndent(got, "", " ")
		t.Fatalf("got %s", gotJSON)
	}
}

func TestThreadPayloadOverrides(t *testing.T) {
	th := &threadState{info: cxThread{ID: "t1", Preview: "<wow_character_state>\nx\n</wow_character_state>\n\nFix the bags\nplease", Cwd: "~/code/thott.bot",
		UpdatedAt: 100, Status: cxStatus{Type: "active"}}, readAt: 50, activity: "Running npm test", since: 90, origin: "s1:4"}
	p := th.payload(nil)
	if p.Title != "Fix the bags" || p.Project != "thott.bot" || p.State != "active" || !p.Unread || p.Since != 90 || p.Activity != "Running npm test" || p.Origin != "s1:4" {
		t.Fatalf("got %+v", p)
	}
	p = th.payload(&approval{id: "1-5", kind: "command", text: "npm test", reason: "Runs the tests"})
	if p.State != "needs_you" || p.Approval == nil || p.Approval.Text != "npm test" {
		t.Fatalf("approval: %+v", p)
	}
	th.err = openElsewhere
	th.info.Status = cxStatus{Type: "notLoaded"}
	if p = th.payload(nil); p.State != "error" || p.Error != openElsewhere || p.Since != 0 {
		t.Fatalf("error: %+v", p)
	}
}

func TestLiveTurnFillsLaggingReads(t *testing.T) {
	user := cxItem{Type: "userMessage", ID: "u", Content: json.RawMessage(`[{"type":"text","text":"Hi"}]`)}
	answer := cxItem{Type: "agentMessage", ID: "a", Text: "Hello"}
	th := &threadState{turns: []cxTurn{{ID: "old", Status: "completed"}, {ID: "t2", Status: "inProgress"}}}
	th.live = &cxTurn{ID: "t2", Status: "inProgress"}
	th.addLive("t2", user)
	th.addLive("t2", answer)
	th.addLive("t2", answer)
	got := th.payload(nil).Turns
	if len(got) != 2 || got[0].Text != "Hi" || got[1].Text != "Hello" {
		t.Fatalf("live turn not used: %+v", got)
	}

	th.live.Status = "completed"
	th.setTurns([]cxTurn{{ID: "t2", Status: "inProgress", Items: []cxItem{user}}})
	if got := th.payload(nil).Turns; len(got) != 2 || got[1].Text != "Hello" {
		t.Fatal("dropped the live turn while the read still lagged")
	}
	th.setTurns([]cxTurn{{ID: "t2", Status: "completed", Items: []cxItem{user, answer}}})
	if got := th.payload(nil).Turns; len(got) != 2 || got[0].Text != "Hi" || got[1].Text != "Hello" {
		t.Fatal("kept the live turn after the read caught up")
	}

	th.addLive("t3", user)
	if got := th.payload(nil).Turns; len(got) != 3 || got[2].Text != "Hi" {
		t.Fatalf("new live turn: %+v", got)
	}
}

func TestTokenUsageSetsContextLeft(t *testing.T) {
	s := newSession("", map[string]string{}, func() {})
	s.upsert(cxThread{ID: "t1"})
	s.order = []string{"t1"}
	if s.payload().Threads[0].ContextLeft != nil {
		t.Fatal("context left before Codex reported any usage")
	}
	usage := `{"totalTokens":%d,"inputTokens":0,"cachedInputTokens":0,"cacheWriteInputTokens":0,"outputTokens":0,"reasoningOutputTokens":0}`
	s.notify("thread/tokenUsage/updated", json.RawMessage(fmt.Sprintf(`{"threadId":"t1","turnId":"u1","tokenUsage":{"total":`+usage+`,"last":`+usage+`,"modelContextWindow":272000}}`, 900000, 77000)))
	if left := s.payload().Threads[0].ContextLeft; left == nil || *left != 75 {
		t.Fatalf("got %v, want 75", left)
	}
}
