//go:build windows

package daemon

import (
	"os"
	"os/exec"
	"syscall"
)

// Windows stubs: signal-0/Setsid/fcntl have no direct equivalent. Detachment and
// lock probing are best-effort here (the daemon model is validated on Unix).
func stopProcess(proc *os.Process) error      { return proc.Kill() }
func isProcessRunning(pid int) bool           { return false }
func daemonSysProcAttr() *syscall.SysProcAttr { return &syscall.SysProcAttr{} }
func daemonExtraSetup(*exec.Cmd)              {}
func isPebbleLockHeld(lockPath string) bool   { return false }
