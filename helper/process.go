package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type agentProcess struct {
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	stderr  tailBuffer
	done    chan struct{}
	exitErr error
	writeMu sync.Mutex
}

func (p *agentProcess) start(path, dir string, args []string, onLine func([]byte), onExit func()) error {
	cmd := agentCommand(path, args...)
	cmd.Dir = dir
	cmd.Env = agentEnv(path)
	cmd.WaitDelay = 2 * time.Second
	cmd.Stderr = &p.stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		return err
	}
	if err := startAgent(cmd); err != nil {
		stdin.Close()
		stdout.Close()
		return err
	}
	p.cmd, p.stdin, p.done = cmd, stdin, make(chan struct{})
	go p.read(stdout, onLine, onExit)
	return nil
}

func (p *agentProcess) read(stdout io.Reader, onLine func([]byte), onExit func()) {
	lines := bufio.NewReader(stdout)
	for {
		line, err := lines.ReadBytes('\n')
		if len(bytes.TrimSpace(line)) > 0 {
			onLine(line)
		}
		if err != nil {
			break
		}
	}
	p.exitErr = p.cmd.Wait()
	close(p.done)
	if onExit != nil {
		onExit()
	}
}

func (p *agentProcess) send(v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	p.writeMu.Lock()
	defer p.writeMu.Unlock()
	_, err = p.stdin.Write(append(data, '\n'))
	return err
}

func (p *agentProcess) exited() bool {
	select {
	case <-p.done:
		return true
	default:
		return false
	}
}

// The app never stops an agent. When its input closes, Codex or Claude Code finishes its work and exits (observed
// when idle: Codex within 1 s, Claude Code within 2 s).
func (p *agentProcess) close() { p.stdin.Close() }

func (p *agentProcess) exitReason() string {
	reason := "exited"
	if p.exited() && p.exitErr != nil {
		reason = p.exitErr.Error()
	}
	if tail := strings.TrimSpace(p.stderr.String()); tail != "" {
		lines := strings.Split(tail, "\n")
		reason += ": " + strings.TrimSpace(lines[len(lines)-1])
	}
	return reason
}

func agentEnv(bin string) []string {
	dirs := append([]string{filepath.Dir(bin)}, extraPath...)
	return append(os.Environ(), "PATH="+strings.Join(append(dirs, os.Getenv("PATH")), string(os.PathListSeparator)))
}

func findInstalled(name string) string {
	if p, err := exec.LookPath(name); err == nil {
		if abs, err := filepath.Abs(p); err == nil {
			return abs
		}
	}
	return findInstalledOS(name)
}

func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

type tailBuffer struct {
	mu  sync.Mutex
	buf []byte
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, p...)
	if len(t.buf) > 4096 {
		t.buf = append([]byte(nil), t.buf[len(t.buf)-4096:]...)
	}
	return len(p), nil
}

func (t *tailBuffer) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return string(t.buf)
}
