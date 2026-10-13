//go:build unix

package main

import (
	"os"
	"os/exec"
	"syscall"

	"golang.org/x/sys/unix"
)

// lockFile takes the lock, or with wait blocks until its holder releases it or exits.
func lockFile(path string, wait bool) (release func(), ok bool) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, false
	}
	how := unix.LOCK_EX | unix.LOCK_NB
	if wait {
		how = unix.LOCK_EX
	}
	if unix.Flock(int(f.Fd()), how) != nil {
		f.Close()
		return nil, false
	}
	return func() { f.Close() }, true
}

func hideWindow(*exec.Cmd) {}

func agentCommand(path string, args ...string) *exec.Cmd { return exec.Command(path, args...) }

func replaceFile(from, to string) error { return os.Rename(from, to) }

func openShared(path string) (*os.File, error) { return os.Open(path) }

func startDetached(cmd *exec.Cmd) error {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	return cmd.Start()
}

// Use a separate process group, so a signal to the app does not reach the agent. The app never stops an agent.
func startAgent(cmd *exec.Cmd) error {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	return cmd.Start()
}
