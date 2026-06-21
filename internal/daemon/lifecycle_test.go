package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestStart_RejectsSecondInstance_PIDGuard verifies the primary single-instance
// enforcement (FR-011): when a live PID is recorded for the database, a second
// daemon.Start fails clearly before re-exec'ing anything. (The PID guard via
// signal-0 liveness is the reliable same-process check; the Pebble fcntl lock is
// the cross-process backstop — POSIX per-process locks make it untestable from
// the same process, but storage.Open's own lock makes the loser fail fast.)
func TestStart_RejectsSecondInstance_PIDGuard(t *testing.T) {
	dbPath := t.TempDir()
	// Record the test process itself as "running" — signal-0 confirms liveness.
	if err := WritePID(dbPath, os.Getpid()); err != nil {
		t.Fatalf("WritePID: %v", err)
	}

	err := Start(dbPath, Addrs{
		MCPAddr:  "127.0.0.1:39101",
		RESTAddr: "127.0.0.1:39102",
		GRPCAddr: "127.0.0.1:39103",
	})
	if err == nil {
		t.Fatal("expected second-instance rejection, got nil (Start would have exec'd a daemon)")
	}
	if !strings.Contains(err.Error(), "already running") {
		t.Fatalf("expected 'already running' error, got: %v", err)
	}
}

// TestStatus_ReportsAllAddresses verifies daemon.Status surfaces all three bound
// transport addresses (US3/T033), not just MCP.
func TestStatus_ReportsAllAddresses(t *testing.T) {
	dbPath := t.TempDir()
	want := Addrs{
		MCPAddr:  "127.0.0.1:39201",
		RESTAddr: "127.0.0.1:39202",
		GRPCAddr: "127.0.0.1:39203",
	}
	if err := WritePID(dbPath, os.Getpid()); err != nil {
		t.Fatalf("WritePID: %v", err)
	}
	if err := WriteAddrs(dbPath, want); err != nil {
		t.Fatalf("WriteAddrs: %v", err)
	}

	running, pid, got := Status(dbPath)
	if pid != os.Getpid() {
		t.Errorf("pid = %d, want %d", pid, os.Getpid())
	}
	if running {
		t.Logf("note: health probe unexpectedly up; addrs assertion still valid")
	}
	if got.MCPAddr != want.MCPAddr || got.RESTAddr != want.RESTAddr || got.GRPCAddr != want.GRPCAddr {
		t.Fatalf("Status addrs = %+v, want %+v", got, want)
	}
}

// TestPebbleLockHeld_AbsentNotHeld is a sanity check for the lock guard's
// not-held path (LOCK absent ⇒ false). The held path requires another process.
func TestPebbleLockHeld_AbsentNotHeld(t *testing.T) {
	if isPebbleLockHeld(filepath.Join(t.TempDir(), "LOCK")) {
		t.Fatal("expected lock not held when LOCK file is absent")
	}
}
