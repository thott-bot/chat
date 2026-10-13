package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestPayloadLua(t *testing.T) {
	for _, body := range []string{`{"v":3}`, `{"text":"]==]]===]"}`, `x]==`} {
		if got := payloadBody(t, payloadLua([]byte(body))); got != body {
			t.Fatalf("payload changed its text: %q", got)
		}
	}
}

func payloadBody(t *testing.T, data []byte) string {
	t.Helper()
	got := string(data)
	open := regexp.MustCompile(`^\s*thott_bot_chat_receive\s*\(\s*\[(=*)\[`).FindStringSubmatch(got)
	if len(open) != 2 {
		t.Fatalf("payload does not call the addon receiver: %q", got)
	}
	close := "]" + open[1] + "]"
	rest := strings.TrimPrefix(got, open[0])
	at := strings.Index(rest, close)
	if at < 0 || strings.TrimSpace(rest[at+len(close):]) != ")" {
		t.Fatalf("payload terminates its Lua string early: %q", got)
	}
	return rest[:at]
}

func bigThread(id string, turns, textLen int) payloadThread {
	th := payloadThread{ID: id, Title: id, State: "idle"}
	for i := 0; i < turns; i++ {
		th.Turns = append(th.Turns, payloadTurn{Role: "assistant", Text: strings.Repeat("x", textLen)})
	}
	return th
}

func TestEncodePayloadCaps(t *testing.T) {
	p := payload{V: protocolVersion, Codex: "ready"}
	for i := 0; i < 25; i++ {
		p.Threads = append(p.Threads, bigThread(string(rune('a'+i)), 15, 5000))
	}
	data := encodePayload(p, maxPayloadBytes)
	if len(data) > maxPayloadBytes {
		t.Fatalf("payload is %d bytes", len(data))
	}
	var got payload
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Threads) == 0 || got.Threads[0].ID != p.Threads[0].ID {
		t.Fatalf("payload lost its newest thread: %+v", got.Threads)
	}

	turns := len(got.Threads[0].Turns)
	if turns == 0 || got.Threads[0].Turns[0].Text == "" {
		t.Fatal("payload lost the conversation while fitting its size")
	}
	if p.Threads[0].Turns[0].Text != strings.Repeat("x", 5000) {
		t.Fatal("encodePayload changed its input")
	}
}

func TestEncodePayloadKeepsSmallPayloads(t *testing.T) {
	p := payload{V: protocolVersion, Codex: "ready", Threads: []payloadThread{bigThread("a", 2, 10)}}
	var got payload
	json.Unmarshal(encodePayload(p, maxPayloadBytes), &got)
	if len(got.Threads) != 1 || len(got.Threads[0].Turns) != 2 || got.Threads[0].Turns[1].Text != p.Threads[0].Turns[1].Text {
		t.Fatalf("small conversation changed: %+v", got.Threads)
	}
}

func TestEncodePayloadDropsThreadsLast(t *testing.T) {
	p := payload{V: protocolVersion, Codex: "ready"}
	for i := 0; i < 20; i++ {
		th := bigThread(string(rune('a'+i)), 0, 0)
		th.Title = strings.Repeat("t", 20000)
		p.Threads = append(p.Threads, th)
	}
	data := encodePayload(p, maxPayloadBytes)
	var got payload
	json.Unmarshal(data, &got)
	if len(data) > maxPayloadBytes || len(got.Threads) == 0 || len(got.Threads) == 20 || got.Threads[0].ID != "a" {
		t.Fatalf("%d bytes, %d threads", len(data), len(got.Threads))
	}
}

func TestPayloadWriterWritesOnlyOnChange(t *testing.T) {
	dir := filepath.Join(t.TempDir(), payloadFolder)
	w := &payloadWriter{path: filepath.Join(dir, "Payload.lua")}
	now := time.Unix(1790000000, 0)
	p := payload{V: protocolVersion, Codex: "ready", Acks: map[string][]int64{"s1": {3}}}

	if written, err := w.write(p, now); written || err == nil {
		t.Fatal("wrote into a missing folder")
	}
	os.MkdirAll(dir, 0o755)
	if written, err := w.write(p, now); !written || err != nil {
		t.Fatalf("first write: %v %v", written, err)
	}
	first := readPayloadFile(t, w.path)
	if first.Rev < now.UnixMilli() || first.At != now.Unix() || !slices.Equal(first.Acks["s1"], []int64{3}) {
		t.Fatalf("first payload %+v", first)
	}
	if written, _ := w.write(p, now.Add(time.Second)); written {
		t.Fatal("rewrote an unchanged payload")
	}

	p.Acks = map[string][]int64{"s1": {3, 4}}
	if written, _ := w.write(p, now); !written {
		t.Fatal("changed payload not written")
	}
	if got := readPayloadFile(t, w.path); got.Rev != first.Rev+1 || !slices.Equal(got.Acks["s1"], []int64{3, 4}) {
		t.Fatalf("second payload %+v", got)
	}
	if entries, err := os.ReadDir(dir); err != nil || len(entries) != 1 || entries[0].Name() != filepath.Base(w.path) {
		t.Fatalf("the payload folder holds more than the payload: %v %v", entries, err)
	}
}

func TestPayloadWriterFitsTheStampInTheCap(t *testing.T) {
	w := &payloadWriter{path: filepath.Join(t.TempDir(), "Payload.lua")}
	p := payload{V: protocolVersion, Codex: "ready", Threads: []payloadThread{bigThread("a", 0, 0)}}
	p.Threads[0].Title = strings.Repeat("t", maxPayloadBytes-len(encodePayload(p, maxPayloadBytes))-8)
	if _, err := w.write(p, time.Unix(1790000000, 0)); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(w.path)
	if err != nil {
		t.Fatal(err)
	}
	if body := payloadBody(t, data); len(body) > maxPayloadBytes {
		t.Fatalf("the stamped payload is %d bytes", len(body))
	}
}

func readPayloadFile(t *testing.T, path string) payload {
	t.Helper()
	p, err := tryPayloadFile(t, path)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// tryPayloadFile is for a test that polls while the app writes: Windows denies a read during the replacing rename.
func tryPayloadFile(t *testing.T, path string) (payload, error) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		return payload{}, err
	}
	body := payloadBody(t, data)
	var p payload
	if err := json.Unmarshal([]byte(body), &p); err != nil {
		t.Fatalf("%v: %s", err, data)
	}
	return p, nil
}
