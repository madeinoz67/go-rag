package storage

import (
	"bytes"
	"testing"
)

func TestSetGetDeleteRoundTrip(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	if err := db.SetWithPrefix(PrefixDocument, []byte("doc1"), []byte("body1")); err != nil {
		t.Fatalf("set: %v", err)
	}
	got, ok, err := db.GetWithPrefix(PrefixDocument, []byte("doc1"))
	if err != nil || !ok {
		t.Fatalf("get: ok=%v err=%v", ok, err)
	}
	if !bytes.Equal(got, []byte("body1")) {
		t.Fatalf("value mismatch: %q", got)
	}

	if err := db.DeleteWithPrefix(PrefixDocument, []byte("doc1")); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, ok, _ := db.GetWithPrefix(PrefixDocument, []byte("doc1")); ok {
		t.Fatal("deleted key still present")
	}
}

func TestPrefixScan(t *testing.T) {
	db, _ := Open(t.TempDir())
	defer db.Close()
	db.SetWithPrefix(PrefixDocument, []byte("a"), []byte("1"))
	db.SetWithPrefix(PrefixDocument, []byte("b"), []byte("2"))
	db.SetWithPrefix(PrefixChunk, []byte("c"), []byte("3")) // different prefix — excluded

	var seen []string
	db.PrefixScanByte(PrefixDocument, func(k, v []byte) bool {
		seen = append(seen, string(k[1:])) // strip prefix byte
		return true
	})
	if len(seen) != 2 {
		t.Fatalf("expected 2 document keys, got %d (%v)", len(seen), seen)
	}
}

func TestSingleWriterLock(t *testing.T) {
	dir := t.TempDir()
	db1, err := Open(dir)
	if err != nil {
		t.Fatalf("open1: %v", err)
	}
	defer db1.Close()
	if _, err := Open(dir); err == nil {
		t.Fatal("second Open on the same dir must fail (Pebble single-writer lock)")
	}
}

func TestSyncSurvivesReopen(t *testing.T) {
	dir := t.TempDir()
	db, _ := Open(dir)
	if err := db.SetWithPrefix(PrefixDocument, []byte("perm"), []byte("kept")); err != nil {
		t.Fatalf("set: %v", err)
	}
	db.Close()

	db2, err := Open(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer db2.Close()
	got, ok, _ := db2.GetWithPrefix(PrefixDocument, []byte("perm"))
	if !ok || !bytes.Equal(got, []byte("kept")) {
		t.Fatalf("Sync write lost on reopen: ok=%v got=%q", ok, got)
	}
}
