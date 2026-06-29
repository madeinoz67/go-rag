package upgrade

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// makeTarGZ builds a tar.gz containing a single file named `name` with the given
// content (mode 0755), and returns the archive bytes and their SHA-256.
func makeTarGZ(t *testing.T, name string, content []byte) ([]byte, string) {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(content))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(content); err != nil {
		t.Fatal(err)
	}
	tw.Close()
	gz.Close()
	data := buf.Bytes()
	h := sha256.Sum256(data)
	return data, hex.EncodeToString(h[:])
}

// TestSelfUpdateEndToEnd exercises the REAL upgrade chain against a local mock
// release server — download → gzip/tar extract → SHA-256 verify (of the archive)
// → atomic swap (with .prev backup) → Rollback — on a throwaway temp binary, by
// overriding the executablePath / releaseBaseURL seams. It never touches the real
// go-rag binary.
func TestSelfUpdateEndToEnd(t *testing.T) {
	binaryName := "go-rag"
	if runtime.GOOS == "windows" {
		binaryName = "go-rag.exe"
	}

	newBytes := []byte("NEW-BINARY-v9.9.9")
	targz, assetSHA := makeTarGZ(t, binaryName, newBytes)
	asset := assetName("v9.9.9", runtime.GOOS, runtime.GOARCH)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/releases/download/v9.9.9/checksums.txt":
			fmt.Fprintf(w, "%s  %s\n", assetSHA, asset)
		case "/releases/download/v9.9.9/" + asset:
			_, _ = w.Write(targz)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	dir := t.TempDir()
	oldExe := filepath.Join(dir, binaryName)
	if err := os.WriteFile(oldExe, []byte("OLD-BINARY"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Repoint the seams at the mock server and the throwaway binary.
	prevBase, prevExe := releaseBaseURL, executablePath
	releaseBaseURL = srv.URL
	executablePath = func() (string, error) { return oldExe, nil }
	defer func() { releaseBaseURL = prevBase; executablePath = prevExe }()

	// Upgrade: the old binary is atomically replaced by the new one; prior retained.
	if err := SelfUpdate("v9.9.9"); err != nil {
		t.Fatalf("SelfUpdate: %v", err)
	}
	if got, _ := os.ReadFile(oldExe); string(got) != string(newBytes) {
		t.Errorf("exe after upgrade = %q, want %q", got, newBytes)
	}
	if got, _ := os.ReadFile(oldExe + ".prev"); string(got) != "OLD-BINARY" {
		t.Errorf("prev after upgrade = %q, want OLD-BINARY", got)
	}

	// Rollback restores the prior binary offline.
	if err := Rollback(); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if got, _ := os.ReadFile(oldExe); string(got) != "OLD-BINARY" {
		t.Errorf("exe after rollback = %q, want OLD-BINARY", got)
	}
}

// TestSelfUpdateRejectsBadChecksumEndToEnd: a checksum mismatch aborts BEFORE
// the swap and leaves the old binary untouched (FR-002) — verified through the
// full SelfUpdate chain, not just VerifyChecksum in isolation.
func TestSelfUpdateRejectsBadChecksumEndToEnd(t *testing.T) {
	binaryName := "go-rag"
	if runtime.GOOS == "windows" {
		binaryName = "go-rag.exe"
	}
	targz, _ := makeTarGZ(t, binaryName, []byte("NEW"))
	asset := assetName("v9.9.9", runtime.GOOS, runtime.GOARCH)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/releases/download/v9.9.9/checksums.txt":
			fmt.Fprintf(w, "%s  %s\n", strings.Repeat("a", 64), asset) // wrong hash
		case "/releases/download/v9.9.9/" + asset:
			_, _ = w.Write(targz)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	dir := t.TempDir()
	oldExe := filepath.Join(dir, binaryName)
	if err := os.WriteFile(oldExe, []byte("OLD-BINARY"), 0o755); err != nil {
		t.Fatal(err)
	}

	prevBase, prevExe := releaseBaseURL, executablePath
	releaseBaseURL = srv.URL
	executablePath = func() (string, error) { return oldExe, nil }
	defer func() { releaseBaseURL = prevBase; executablePath = prevExe }()

	if err := SelfUpdate("v9.9.9"); err == nil {
		t.Fatal("expected SelfUpdate to fail on checksum mismatch, got nil")
	}
	// Old binary untouched; no backup created (applySwap never ran).
	if got, _ := os.ReadFile(oldExe); string(got) != "OLD-BINARY" {
		t.Errorf("old binary changed after aborted upgrade: %q, want OLD-BINARY", got)
	}
	if _, err := os.Stat(oldExe + ".prev"); !os.IsNotExist(err) {
		t.Errorf(".prev should not exist after an aborted upgrade, got err=%v", err)
	}
}
