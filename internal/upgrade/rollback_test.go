package upgrade

import (
	"os"
	"path/filepath"
	"testing"
)

// TestRollbackSwap verifies rollback restores the prior binary and consumes the
// backup (it does NOT touch the real go-rag binary — operates on temp files).
func TestRollbackSwap(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "go-rag")
	prev := exe + ".prev"
	if err := os.WriteFile(exe, []byte("NEW"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(prev, []byte("OLD"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := rollbackSwap(exe); err != nil {
		t.Fatalf("rollbackSwap: %v", err)
	}
	if got, _ := os.ReadFile(exe); string(got) != "OLD" {
		t.Errorf("exe after rollback = %q, want OLD", got)
	}
	if _, err := os.Stat(prev); !os.IsNotExist(err) {
		t.Errorf("prev should be consumed by the rollback, got err=%v", err)
	}
	if _, err := os.Stat(exe + ".broken"); !os.IsNotExist(err) {
		t.Errorf(".broken side-aside should be removed, got err=%v", err)
	}
}

// TestRollbackSwapNoPrevErrors: with no backup, rollback errors cleanly and the
// current binary is left untouched.
func TestRollbackSwapNoPrevErrors(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "go-rag")
	if err := os.WriteFile(exe, []byte("NEW"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := rollbackSwap(exe); err == nil {
		t.Error("expected error when no .prev exists, got nil")
	}
	if got, _ := os.ReadFile(exe); string(got) != "NEW" {
		t.Errorf("current binary must be untouched on the no-backup error path, got %q", got)
	}
}
