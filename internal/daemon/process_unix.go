//go:build !windows

package daemon

import (
	"os"
	"os/exec"
	"syscall"
)

// stopProcess sends SIGTERM for a graceful shutdown on Unix.
func stopProcess(proc *os.Process) error {
	return proc.Signal(syscall.SIGTERM)
}

// isProcessRunning checks liveness via signal 0 (the Unix existence check).
func isProcessRunning(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(syscall.Signal(0)) == nil
}

// daemonSysProcAttr detaches the daemon into its own session so it survives the
// parent CLI exiting or losing its controlling TTY.
func daemonSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true}
}

// daemonExtraSetup is a no-op on Unix (placeholder for platform hooks).
func daemonExtraSetup(*exec.Cmd) {}

// isPebbleLockHeld probes the Pebble LOCK file with fcntl F_WRLCK — Pebble uses
// POSIX record locks (F_SETLK), not BSD flock, so a flock probe would always
// succeed. Returns false on any error (absent/inaccessible). Best-effort: there is
// an inherent TOCTOU race, acceptable since Pebble's own lock makes the loser fail
// fast.
func isPebbleLockHeld(lockPath string) bool {
	f, err := os.OpenFile(lockPath, os.O_RDWR, 0o600)
	if err != nil {
		return false
	}
	defer f.Close()
	spec := syscall.Flock_t{Type: syscall.F_WRLCK, Whence: 0, Start: 0, Len: 0}
	if err := syscall.FcntlFlock(f.Fd(), syscall.F_SETLK, &spec); err != nil {
		return true // another process holds the lock
	}
	spec.Type = syscall.F_UNLCK
	_ = syscall.FcntlFlock(f.Fd(), syscall.F_SETLK, &spec)
	return false
}
