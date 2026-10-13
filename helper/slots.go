package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	maxThreads      = 20
	maxTurnEntries  = 12
	maxText         = 4000
	maxDetailItems  = 12
	maxPayloadBytes = 256 << 10
	protocolVersion = 4
	// The addon asks about a seq until it is delivered or for 2 s. It can send about 7 short packets in 2 s (15 bytes
	// per 40 ms frame).
	maxAcks = 32
)

type payload struct {
	V        int                `json:"v"`
	Rev      int64              `json:"rev,omitempty"`
	At       int64              `json:"at,omitempty"`
	Helper   string             `json:"helper,omitempty"`
	Codex    string             `json:"codex"`
	Note     string             `json:"note"`
	Starting bool               `json:"starting,omitempty"`
	Acks     map[string][]int64 `json:"acks,omitempty"`
	Models   []payloadModel     `json:"models,omitempty"`
	Projects []payloadProject   `json:"projects,omitempty"`
	Threads  []payloadThread    `json:"threads,omitempty"`
}

type payloadModel struct {
	ID      string   `json:"id"`
	Label   string   `json:"label"`
	Default bool     `json:"default"`
	Effort  string   `json:"effort"`
	Efforts []string `json:"efforts"`
}

type payloadProject struct {
	Label string `json:"label"`
	Cwd   string `json:"cwd"`
}

type payloadThread struct {
	ID          string           `json:"id"`
	Agent       string           `json:"agent"`
	Title       string           `json:"title"`
	State       string           `json:"state"`
	Project     string           `json:"project"`
	Cwd         string           `json:"cwd"`
	Model       string           `json:"model"`
	Effort      string           `json:"effort"`
	ContextLeft *int             `json:"contextLeft,omitempty"`
	Updated     int64            `json:"updated"`
	Unread      bool             `json:"unread"`
	Origin      string           `json:"origin,omitempty"`
	Since       int64            `json:"since,omitempty"`
	Activity    string           `json:"activity,omitempty"`
	Approval    *payloadApproval `json:"approval,omitempty"`
	Error       string           `json:"error,omitempty"`
	Turns       []payloadTurn    `json:"turns"`
}

type payloadApproval struct {
	ID     string `json:"id"`
	Kind   string `json:"kind"`
	Text   string `json:"text"`
	Reason string `json:"reason"`
}

type payloadTurn struct {
	Role    string   `json:"role"`
	Text    string   `json:"text"`
	At      int64    `json:"at,omitempty"`
	Context bool     `json:"context,omitempty"`
	Items   []string `json:"items,omitempty"`
}

func marshalPlain(v any) []byte {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil
	}
	return bytes.TrimRight(buf.Bytes(), "\n")
}

func encodePayload(p payload, size int) []byte {
	if len(p.Threads) > maxThreads {
		p.Threads = p.Threads[:maxThreads]
	}
	threads := p.Threads
	for _, limit := range []int{maxTurnEntries, 8, 4, 2, 1, 0} {
		p.Threads = capTurns(threads, limit)
		if data := marshalPlain(p); len(data) <= size {
			return data
		}
	}
	for len(p.Threads) > 0 {
		p.Threads = p.Threads[:len(p.Threads)-1]
		if data := marshalPlain(p); len(data) <= size {
			return data
		}
	}
	return marshalPlain(p)
}

func capTurns(threads []payloadThread, limit int) []payloadThread {
	out := make([]payloadThread, len(threads))
	for i, t := range threads {
		turns := t.Turns
		if len(turns) > limit {
			turns = turns[len(turns)-limit:]
		}
		t.Turns = make([]payloadTurn, len(turns))
		for j, turn := range turns {
			turn.Text = trimText(turn.Text, maxText)
			if len(turn.Items) > maxDetailItems {
				turn.Items = turn.Items[:maxDetailItems]
			}
			t.Turns[j] = turn
		}
		out[i] = t
	}
	return out
}

func trimText(s string, limit int) string {
	if utf8.RuneCountInString(s) <= limit {
		return s
	}
	if limit <= 0 {
		return ""
	}
	for i := range s {
		if limit--; limit == 0 {
			return s[:i] + "…"
		}
	}
	return s
}

func luaLongString(s string) string {
	eq := "=="
	for strings.Contains(s+"]"+eq, "]"+eq+"]") {
		eq += "="
	}
	return "[" + eq + "[" + s + "]" + eq + "]"
}

func payloadLua(data []byte) []byte {
	return []byte("thott_bot_chat_receive(" + luaLongString(string(data)) + ")\n")
}

type payloadWriter struct {
	path string
	rev  int64
	body []byte
}

// The rev and time stamp replace the JSON's opening brace. So the writer encodes the payload once and compares it
// without its stamp.
func (w *payloadWriter) write(p payload, now time.Time) (bool, error) {
	p.Rev, p.At = 0, 0
	rev := max(w.rev+1, now.UnixMilli())
	stamp := fmt.Appendf(nil, `{"rev":%d,"at":%d,`, rev, now.Unix())
	body := encodePayload(p, maxPayloadBytes-len(stamp)+1)
	if bytes.Equal(body, w.body) {
		return false, nil
	}
	if err := writeFileAtomic(w.path, payloadLua(append(stamp, body[1:]...))); err != nil {
		return false, err
	}
	w.rev, w.body = rev, body
	return true, nil
}
