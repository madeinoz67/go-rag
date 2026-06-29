package upgrade

import (
	"os"
	"syscall"

	"github.com/madeinoz67/go-rag/internal/daemon"
	"github.com/madeinoz67/go-rag/internal/vault"
)

// DaemonRunning reports whether a go-rag daemon is running on the default vault.
// The upgrade is safe to perform with the daemon running (replacing the on-disk
// binary does not affect the running process), but the new code only takes
// effect after a daemon restart — see FR-010.
func DaemonRunning() bool {
	pid, err := daemon.ReadPID(vault.Path("default"))
	if err != nil || pid <= 0 {
		return false
	}
	return processAlive(pid)
}

// processAlive reports whether a process with the given PID currently exists,
// using the Unix signal-0 probe. On Windows it returns false (self-replace is
// not supported there anyway). Note: signal-0 is subject to a PID-reuse race —
// a recycled PID owned by an unrelated process reads as alive. DaemonRunning is
// advisory only (FR-010: warn), so such a false-positive warning is acceptable
// noise, not a correctness failure.
func processAlive(pid int) bool {
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return p.Signal(syscall.Signal(0)) == nil
}
