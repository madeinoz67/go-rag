package audit

// audit_test.go (T005) verifies the appender end-to-end: append → read the JSONL,
// privacy (query plaintext never appears — only the hash), and size-capped rotation.
// Close drains the writer goroutine, so no sleeps are needed before assertions.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAppender_AppendAndRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit", "audit.log")
	a, err := Init(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	a.Log(QueryEvent("some query", "hybrid", 5, 3, nil))
	a.Log(IngestEvent("add", "/x.md", 1, 0, 0, nil))
	a.Log(AuthFailEvent("rest", "missing bearer"))
	_ = a.Close(context.Background()) // drains the writer

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	for _, want := range []string{`"type":"query"`, `"type":"ingest"`, `"type":"auth-fail"`, `"query_hash"`} {
		if !strings.Contains(s, want) {
			t.Errorf("audit log missing %q\n%s", want, s)
		}
	}
}

func TestAppender_Privacy_NoPlaintext(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit", "audit.log")
	a, _ := Init(path, 0)
	const secret = "supersecret-sentinel-query-12345"
	a.Log(QueryEvent(secret, "keyword", 5, 0, nil))
	_ = a.Close(context.Background())

	data, _ := os.ReadFile(path)
	if strings.Contains(string(data), secret) {
		t.Fatal("audit log leaked query plaintext (must be hashed only — FR-002/SC-002)")
	}
	if !strings.Contains(string(data), QueryHash(secret)) {
		t.Error("audit log should carry the query hash")
	}
}

func TestAppender_Rotation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit", "audit.log")
	a, err := Init(path, 200) // tiny cap → forces rotation across 50 events
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 50; i++ {
		a.Log(IngestEvent("add", "/x.md", 1, 0, 0, nil))
	}
	_ = a.Close(context.Background()) // drains + flushes all events (rotations included)

	entries, err := os.ReadDir(filepath.Join(dir, "audit"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) < 2 {
		t.Fatalf("rotation: want >=2 files (active + archive), got %d", len(entries))
	}
	// No single file exceeds the cap (+ one line).
	for _, e := range entries {
		fi, _ := e.Info()
		if fi.Size() > 400 { // cap 200 + generous slack for one record + rounding
			t.Errorf("file %s is %d bytes (cap ~200) — rotation failed to bound growth", e.Name(), fi.Size())
		}
	}
}
