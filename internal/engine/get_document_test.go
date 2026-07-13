package engine

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/madeinoz67/go-rag/internal/model"
	"github.com/madeinoz67/go-rag/internal/storage/keys"
)

// get_document_test.go (package engine) proves spec 047 US2: Engine.GetDocument
// resolves a document by id (+ its source for source_path), 404s unknown ids,
// rejects empty ids, and tolerates a missing source row.

// putDocWithSource writes a document under 0x02 and its source under 0x01 so
// GetDocument can resolve source_path.
func putDocWithSource(t *testing.T, e *Engine, docID, sourceID, sourcePath string) {
	t.Helper()
	ws := engineWS(e)
	putRaw := func(key []byte, v any) {
		raw, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if err := e.db.Set(key, raw); err != nil {
			t.Fatalf("set: %v", err)
		}
	}
	putRaw(keys.SourceKey(ws, sourceID), model.Source{ID: sourceID, Path: sourcePath, Kind: "directory"})
	putRaw(keys.DocumentKey(ws, docID), model.Document{
		ID: docID, SourceID: sourceID, FilePath: docID + ".txt", FileName: docID + ".txt",
		FileType: "text", ContentHash: docID, Status: "embedded",
	})
}

func TestGetDocument_ResolvesDocAndSource(t *testing.T) {
	e := newCacheEngine(t)
	putDocWithSource(t, e, "doc1", "src1", "/abs/source/dir")

	res, err := e.GetDocument("default", "doc1")
	if err != nil {
		t.Fatalf("GetDocument: %v", err)
	}
	if res.Document.ID != "doc1" {
		t.Errorf("doc id: got %q want doc1", res.Document.ID)
	}
	if res.Source.Path != "/abs/source/dir" {
		t.Errorf("source_path: got %q want /abs/source/dir", res.Source.Path)
	}
}

func TestGetDocument_UnknownID(t *testing.T) {
	e := newCacheEngine(t)
	if _, err := e.GetDocument("default", "nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown id: err=%v want ErrNotFound", err)
	}
}

func TestGetDocument_EmptyID(t *testing.T) {
	e := newCacheEngine(t)
	for _, id := range []string{"", "  "} {
		if _, err := e.GetDocument("default", id); !errors.Is(err, ErrInvalid) {
			t.Errorf("id=%q: err=%v want ErrInvalid", id, err)
		}
	}
}

// TestGetDocument_ToleratesMissingSource — a document whose source row is absent
// returns the document with a zero-valued Source (source_path empty), not an error.
func TestGetDocument_ToleratesMissingSource(t *testing.T) {
	e := newCacheEngine(t)
	ws := engineWS(e)
	raw, _ := json.Marshal(model.Document{ID: "orphan", SourceID: "ghost", FilePath: "orphan.txt", Status: "embedded"})
	if err := e.db.Set(keys.DocumentKey(ws, "orphan"), raw); err != nil {
		t.Fatalf("set: %v", err)
	}
	res, err := e.GetDocument("default", "orphan")
	if err != nil {
		t.Fatalf("orphan doc: %v", err)
	}
	if res.Document.ID != "orphan" {
		t.Errorf("doc id: got %q want orphan", res.Document.ID)
	}
	if res.Source.Path != "" {
		t.Errorf("missing source: got path %q want empty", res.Source.Path)
	}
}
