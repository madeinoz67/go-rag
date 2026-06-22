// Package watcher implements two-layer change detection (PRD §7): fsnotify for
// real-time events plus periodic SHA-256 polling as a restart-safe safety net.
package watcher

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/madeinoz67/go-rag/internal/model"
	"github.com/madeinoz67/go-rag/internal/pipeline"
	"github.com/madeinoz67/go-rag/internal/reader"
	"github.com/madeinoz67/go-rag/internal/storage"
	"github.com/fsnotify/fsnotify"
)

// Change is one detected filesystem change (Kind: NEW/MODIFIED/SKIPPED/DELETED).
type Change struct {
	Path string
	Kind string
}

// ChangeDetector compares the filesystem against the stored database state using
// SHA-256 content hashes (PRD §7.2) and applies NEW/MODIFIED/DELETED actions.
type ChangeDetector struct {
	db *storage.DB
	pl *pipeline.Pipeline
}

// New returns a ChangeDetector that uses the pipeline to (re-)ingest files.
func New(db *storage.DB, pl *pipeline.Pipeline) *ChangeDetector {
	return &ChangeDetector{db: db, pl: pl}
}

// ScanOnce walks root, compares every file's SHA-256 to the stored state, and
// applies changes. Files with no registered reader are ignored. Returns the list of
// changes (including SKIPPED).
func (cd *ChangeDetector) ScanOnce(ctx context.Context, root, glob string) ([]Change, error) {
	reader.DefaultReaders()
	disk := map[string]string{}
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			if info.Name() == ".go-rag" {
				return filepath.SkipDir // never scan the database's own directory
			}
			return nil
		}
		if !matchGlob(filepath.Base(path), glob) {
			return nil
		}
		if _, ok := reader.Get(filepath.Ext(path)); !ok {
			return nil // unsupported file type — not go-rag's concern
		}
		raw, e := os.ReadFile(path)
		if e != nil {
			return nil
		}
		disk[path] = model.ContentHash(raw)
		return nil
	})
	if err != nil {
		return nil, err
	}

	type tracked struct {
		docID, hash string
	}
	dbFiles := map[string]tracked{}
	_ = cd.db.PrefixScanByte(storage.PrefixPathDoc, func(key, val []byte) bool {
		path := string(key[1:]) // key = 0x0C | path
		tr := tracked{docID: string(val)}
		if raw, ok, _ := cd.db.GetWithPrefix(storage.PrefixDocument, []byte(tr.docID)); ok {
			var d model.Document
			if json.Unmarshal(raw, &d) == nil {
				tr.hash = d.ContentHash
			}
		}
		dbFiles[path] = tr
		return true
	})

	var changes []Change
	for path, h := range disk {
		tr, exists := dbFiles[path]
		switch {
		case !exists:
			cd.ingest(ctx, path)
			changes = append(changes, Change{Path: path, Kind: "NEW"})
		case tr.hash != h:
			_ = cd.pl.DeleteDoc(tr.docID)
			cd.ingest(ctx, path)
			changes = append(changes, Change{Path: path, Kind: "MODIFIED"})
		default:
			changes = append(changes, Change{Path: path, Kind: "SKIPPED"})
		}
	}
	for path, tr := range dbFiles {
		if _, onDisk := disk[path]; !onDisk {
			_ = cd.pl.DeleteDoc(tr.docID)
			changes = append(changes, Change{Path: path, Kind: "DELETED"})
		}
	}
	return changes, nil
}

func (cd *ChangeDetector) ingest(ctx context.Context, path string) {
	_, _ = cd.pl.Ingest(ctx, path, "*")
}

// Watch runs continuously: real-time fsnotify events (500ms debounce) plus a polling
// safety net (every poll). Blocks until ctx is cancelled.
func (cd *ChangeDetector) Watch(ctx context.Context, root, glob string, poll time.Duration) error {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("fsnotify: %w", err)
	}
	defer w.Close()
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err == nil && info.IsDir() {
			_ = w.Add(path)
		}
		return nil
	})

	debounced := map[string]*time.Timer{}
	schedule := func() {
		if t, ok := debounced["__"]; ok {
			t.Reset(500 * time.Millisecond)
			return
		}
		debounced["__"] = time.AfterFunc(500*time.Millisecond, func() {
			_, _ = cd.ScanOnce(ctx, root, glob)
		})
	}

	if poll <= 0 {
		poll = 60 * time.Second
	}
	ticker := time.NewTicker(poll)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case ev, ok := <-w.Events:
			if !ok {
				return nil
			}
			if ev.Op&(fsnotify.Create|fsnotify.Write|fsnotify.Remove|fsnotify.Rename) != 0 {
				schedule()
			}
		case <-ticker.C:
			_, _ = cd.ScanOnce(ctx, root, glob)
		}
	}
}

func matchGlob(name, glob string) bool {
	if glob == "" || glob == "*" {
		return true
	}
	m, _ := filepath.Match(glob, name)
	return m
}
