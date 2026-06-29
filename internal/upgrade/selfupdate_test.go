package upgrade

import (
	"os"
	"path/filepath"
	"testing"
)

// TestApplySwap verifies the atomic backup+rename on temp files (it does NOT
// touch the real go-rag binary). After the swap the exe holds the new content,
// the prior binary is retained at exe.prev, and the temp is consumed.
func TestApplySwap(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "go-rag")
	tmp := filepath.Join(dir, ".tmp-new")
	if err := os.WriteFile(exe, []byte("OLD"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tmp, []byte("NEW"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := applySwap(exe, tmp); err != nil {
		t.Fatalf("applySwap: %v", err)
	}

	if got, _ := os.ReadFile(exe); string(got) != "NEW" {
		t.Errorf("exe content = %q, want NEW", got)
	}
	gotPrev, err := os.ReadFile(exe + ".prev")
	if err != nil {
		t.Errorf("prior binary not retained: %v", err)
	} else if string(gotPrev) != "OLD" {
		t.Errorf("prev content = %q, want OLD", gotPrev)
	}
	if _, err := os.Stat(tmp); !os.IsNotExist(err) {
		t.Errorf("temp should be consumed by the rename, got err=%v", err)
	}
}

// TestApplySwapOverwritesStalePrev verifies retention N=1: a stale prior backup
// is replaced, not accumulated.
func TestApplySwapOverwritesStalePrev(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "go-rag")
	tmp := filepath.Join(dir, ".tmp-new")
	if err := os.WriteFile(exe, []byte("OLD"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tmp, []byte("NEW"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exe+".prev", []byte("STALE"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := applySwap(exe, tmp); err != nil {
		t.Fatalf("applySwap: %v", err)
	}
	gotPrev, _ := os.ReadFile(exe + ".prev")
	if string(gotPrev) != "OLD" {
		t.Errorf("prev = %q, want OLD (stale backup should be overwritten)", gotPrev)
	}
}
