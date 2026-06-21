package eval

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTemp(t *testing.T, name, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return p
}

func TestLoadGolden_Valid(t *testing.T) {
	p := writeTemp(t, "v1.jsonl",
		`{"id":"q1","query":"how does chunking work","relevant":["abc","def"]}`+"\n"+
			`{"id":"q2","query":"what is an embedding","relevant":["ghi"]}`+"\n")
	gs, err := LoadGolden(p)
	if err != nil {
		t.Fatalf("LoadGolden: %v", err)
	}
	if len(gs) != 2 {
		t.Fatalf("expected 2 records, got %d", len(gs))
	}
	if gs[0].ID != "q1" || len(gs[0].Relevant) != 2 {
		t.Fatalf("unexpected first record: %+v", gs[0])
	}
}

func TestLoadGolden_BlankAndCommentLinesIgnored(t *testing.T) {
	p := writeTemp(t, "v1.jsonl",
		"# this is a comment\n\n"+
			`{"id":"q1","query":"q","relevant":["a"]}`+"\n")
	gs, err := LoadGolden(p)
	if err != nil || len(gs) != 1 {
		t.Fatalf("expected 1 record (blank/comment ignored), got %v (%v)", gs, err)
	}
}

func TestLoadGolden_DuplicateID(t *testing.T) {
	p := writeTemp(t, "v1.jsonl",
		`{"id":"q1","query":"a","relevant":["x"]}`+"\n"+
			`{"id":"q1","query":"b","relevant":["y"]}`+"\n")
	if _, err := LoadGolden(p); err == nil {
		t.Fatal("expected error for duplicate id")
	}
}

func TestLoadGolden_EmptyQuery(t *testing.T) {
	p := writeTemp(t, "v1.jsonl", `{"id":"q1","query":"","relevant":["x"]}`+"\n")
	if _, err := LoadGolden(p); err == nil {
		t.Fatal("expected error for empty query")
	}
}

func TestLoadGolden_EmptyRelevantID(t *testing.T) {
	p := writeTemp(t, "v1.jsonl", `{"id":"q1","query":"q","relevant":["x",""]}`+"\n")
	if _, err := LoadGolden(p); err == nil {
		t.Fatal("expected error for empty relevant chunk_id")
	}
}

func TestLoadGolden_EmptyRelevantListAllowed(t *testing.T) {
	// Empty relevant list is valid (the query is skipped at scoring time, FR-008).
	p := writeTemp(t, "v1.jsonl", `{"id":"q1","query":"q","relevant":[]}`+"\n")
	gs, err := LoadGolden(p)
	if err != nil || len(gs) != 1 {
		t.Fatalf("expected 1 record with empty relevant list, got %v (%v)", gs, err)
	}
}

func TestLoadGolden_EmptyFile(t *testing.T) {
	p := writeTemp(t, "v1.jsonl", "")
	if _, err := LoadGolden(p); err == nil {
		t.Fatal("expected error for empty golden file")
	}
}
