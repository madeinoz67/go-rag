package modelbundle

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestVerifyHash_EmptyExpectedIsNoOp(t *testing.T) {
	// Dev mode (ExpectedSHA256 == ""): Verify is a no-op — returns nil before Open,
	// so even a nonexistent path is fine.
	if err := verifyHash("/nonexistent/weights.bin", ""); err != nil {
		t.Fatalf("empty expected must be a no-op; got %v", err)
	}
}

func TestVerifyHash_AcceptsMatchingHash(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "weights.bin")
	body := []byte("hello modelbundle")
	if err := os.WriteFile(p, body, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := verifyHash(p, mustHash(body)); err != nil {
		t.Fatalf("matching hash should pass; got %v", err)
	}
}

func TestVerifyHash_RejectsMismatch(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "weights.bin")
	if err := os.WriteFile(p, []byte("tampered weights"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := verifyHash(p, "deadbeef")
	if err == nil {
		t.Fatal("mismatch should fail")
	}
	mm, ok := err.(ErrHashMismatch)
	if !ok {
		t.Fatalf("want ErrHashMismatch; got %T %v", err, err)
	}
	if mm.Path != p {
		t.Errorf("ErrHashMismatch.Path = %q, want %q", mm.Path, p)
	}
	if mm.Want != "deadbeef" {
		t.Errorf("ErrHashMismatch.Want = %q, want deadbeef", mm.Want)
	}
}

func TestVerifyHash_MissingFileErrors(t *testing.T) {
	if err := verifyHash("/no/such/weights.bin", "abc123"); err == nil {
		t.Fatal("missing file with non-empty expected should error")
	}
}

func mustHash(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
