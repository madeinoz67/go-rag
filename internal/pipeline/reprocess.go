package pipeline

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/madeinoz67/go-rag/internal/storage"
)

// Reprocess force-reingests every tracked file under root, bypassing the SHA-256
// content-hash dedup that makes Ingest a no-op for unchanged files. It deletes
// each existing Document under root (chunks/embeddings/indexes/hash/path) and then
// re-runs the ingest pipeline so the CURRENT reader + embedder apply. Use after a
// reader change (e.g. Obsidian normalization), an embedding-model swap, or to
// refresh stale content — without `rm -rf .go-rag`. (T047)
func (p *Pipeline) Reprocess(ctx context.Context, root, glob string) (Result, error) {
	root = filepath.Clean(root)
	// Drop every tracked document whose path is under root.
	_ = p.db.PrefixScanByte(storage.PrefixPathDoc, func(key, val []byte) bool {
		path := filepath.Clean(string(key[1:])) // key = 0x0C | path
		if !isUnder(path, root) {
			return true
		}
		_ = DeleteDoc(p.db, string(val))
		return true
	})
	// Re-ingest: with the old content-hash entries gone, unchanged files are
	// processed as NEW rather than SKIPPED.
	return p.Ingest(ctx, root, glob)
}

// ReprocessAll re-ingests every tracked document (all paths in the 0x0C index),
// deleting and re-adding each so the current reader + embedder apply. Used by
// model migration (T048) when the embedding model changes.
func (p *Pipeline) ReprocessAll(ctx context.Context) (Result, error) {
	type entry struct{ path, docID string }
	var entries []entry
	_ = p.db.PrefixScanByte(storage.PrefixPathDoc, func(key, val []byte) bool {
		entries = append(entries, entry{path: string(key[1:]), docID: string(val)})
		return true
	})
	for _, e := range entries {
		_ = DeleteDoc(p.db, e.docID)
	}
	res := Result{}
	for _, e := range entries {
		r, err := p.Ingest(ctx, e.path, "*")
		if err != nil {
			res.Errors++
			continue
		}
		res.New += r.New
		res.Skipped += r.Skipped
		res.Errors += r.Errors
	}
	return res, nil
}

// isUnder reports whether path is root itself or a descendant of root. The current
// directory (".") and filesystem root ("/") contain everything.
func isUnder(path, root string) bool {
	path = filepath.Clean(path)
	root = filepath.Clean(root)
	if root == "." || root == string(filepath.Separator) {
		return true
	}
	if path == root {
		return true
	}
	return strings.HasPrefix(path, root+string(filepath.Separator))
}
