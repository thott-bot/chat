package main

import (
	"path/filepath"
	"testing"
	"time"
)

func BenchmarkWoWDetection(b *testing.B) {
	dir := b.TempDir()
	b.ReportAllocs()
	for b.Loop() {
		wowRunning(dir)
	}
}

func BenchmarkPayloadUnchanged(b *testing.B) {
	w := &payloadWriter{path: filepath.Join(b.TempDir(), "Payload.lua")}
	p := payload{V: protocolVersion, Codex: "ready"}
	for i := range maxThreads {
		p.Threads = append(p.Threads, bigThread(string(rune('a'+i)), maxTurnEntries, maxText))
	}
	now := time.Now()
	w.write(p, now)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		w.write(p, now)
	}
}
