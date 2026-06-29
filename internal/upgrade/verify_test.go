package upgrade

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVerifyChecksum(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bin")
	data := []byte("hello go-rag")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	correct := hex.EncodeToString(sum[:])

	if err := VerifyChecksum(path, correct); err != nil {
		t.Errorf("matching checksum: unexpected error %v", err)
	}
	if err := VerifyChecksum(path, strings.Repeat("0", 64)); err == nil {
		t.Error("mismatched checksum: expected error, got nil")
	}
	if err := VerifyChecksum(path, ""); err != ErrNoChecksum {
		t.Errorf("empty checksum: want ErrNoChecksum, got %v", err)
	}
	// Uppercase hex must also be accepted.
	if err := VerifyChecksum(path, strings.ToUpper(correct)); err != nil {
		t.Errorf("uppercase checksum: unexpected error %v", err)
	}
}

func TestVerifyExecutable(t *testing.T) {
	dir := t.TempDir()
	data := []byte("x")

	execPath := filepath.Join(dir, "exec")
	if err := os.WriteFile(execPath, data, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := VerifyExecutable(execPath); err != nil {
		t.Errorf("0755 file: unexpected error %v", err)
	}

	noExec := filepath.Join(dir, "noexec")
	if err := os.WriteFile(noExec, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := VerifyExecutable(noExec); err == nil {
		t.Error("0644 file: expected error, got nil")
	}
}
