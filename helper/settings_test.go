package main

import (
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
)

func TestAtomicWritesArePrivateAndConcurrent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "conversation.json")
	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := writeFileAtomic(path, []byte("complete")); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "complete" {
		t.Fatalf("incomplete write: %q, %v", data, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("conversation permissions: %o", info.Mode().Perm())
	}
}
