package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/madeinoz67/go-rag/internal/embed"
	"github.com/madeinoz67/go-rag/internal/eval/beir"
	"github.com/madeinoz67/go-rag/internal/model"
	"github.com/madeinoz67/go-rag/internal/storage"
)

// RunBenchmark scores a BEIR dataset (e.g. SciFact) against a freshly provisioned
// vault, mapping each query's qrel corpus-ids to go-rag chunk_ids after ingest.
// It is the manual, opt-in retrieval-quality measurement path (audit H07 SC-001)
// — NOT run in CI, because real-model embedding of a full corpus is slow. prefix
// sets the instruction-prefix convention for the ingest and the queries alike
// (both derive their prefixer from the provisioned config).
func RunBenchmark(ctx context.Context, ds *beir.Dataset, em embed.Embedder, mode string, k int, prefix string) (*EvaluationRun, error) {
	// 1. Materialize the corpus: one .txt per doc, doc_id encoded in the filename
	//    so the qrel corpus-ids map back to go-rag chunk_ids after ingest.
	corpusDir, err := os.MkdirTemp("", "go-rag-bench-corpus-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(corpusDir)
	if err := writeBenchmarkCorpus(corpusDir, ds.Corpus); err != nil {
		return nil, err
	}

	// 2. Provision a throwaway vault and ingest with em + the chosen prefix.
	cfg, db, cleanup, err := ProvisionCorpus(ctx, corpusDir, em, prefix)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	// 3. Map each corpus doc_id -> the go-rag chunk_ids it produced.
	corpusToChunks, err := corpusChunkMap(db)
	if err != nil {
		return nil, err
	}

	// 4. Build the golden queries: relevant = union of chunks for qrel'd docs.
	//    A retrieved chunk from a relevant doc counts as a hit (doc-level
	//    relevance adapted to chunk retrieval). Queries whose qrels resolve to
	//    no materialized chunk are skipped (FR-008).
	golden := make([]GoldenQuery, 0, len(ds.Qrels))
	for qID, rels := range ds.Qrels {
		qText, ok := ds.Queries[qID]
		if !ok || strings.TrimSpace(qText) == "" {
			continue
		}
		var rel []string
		for cID := range rels {
			rel = append(rel, corpusToChunks[cID]...)
		}
		if len(rel) == 0 {
			continue
		}
		golden = append(golden, GoldenQuery{ID: qID, Query: qText, Relevant: rel})
	}
	if len(golden) == 0 {
		return nil, fmt.Errorf("benchmark %s: no scorable queries after mapping qrels to chunks", ds.Name)
	}

	// 5. Score with the same runner the golden eval uses (no rerank for a clean
	//    retrieval signal; rerank is a separate axis).
	runner := NewEvalRunner(cfg, db, em)
	return runner.Run(ctx, golden, mode, k, true)
}

// writeBenchmarkCorpus writes one .txt per corpus doc (title + blank line + text),
// named <docID>.txt so the doc_id survives as the filename stem.
func writeBenchmarkCorpus(dir string, corpus map[string]beir.Doc) error {
	for id, d := range corpus {
		content := d.Text
		if strings.TrimSpace(d.Title) != "" {
			content = d.Title + "\n\n" + d.Text
		}
		if err := os.WriteFile(filepath.Join(dir, id+".txt"), []byte(content), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// corpusChunkMap derives corpus-doc-id -> go-rag chunk_ids from the stored
// documents (FilePath carries the doc_id stem) and chunks (linked by DocumentID).
func corpusChunkMap(db *storage.DB) (map[string][]string, error) {
	// doc_id (filename stem) -> DocumentID
	stemToDoc := map[string]string{}
	_ = db.PrefixScanByte(storage.PrefixDocument, func(_, val []byte) bool {
		var d model.Document
		if json.Unmarshal(val, &d) == nil {
			stem := strings.TrimSuffix(filepath.Base(d.FilePath), filepath.Ext(d.FilePath))
			stemToDoc[stem] = d.ID
		}
		return true
	})
	// DocumentID -> []chunkID
	docToChunks := map[string][]string{}
	_ = db.PrefixScanByte(storage.PrefixChunk, func(_, val []byte) bool {
		var c model.Chunk
		if json.Unmarshal(val, &c) == nil {
			docToChunks[c.DocumentID] = append(docToChunks[c.DocumentID], c.ID)
		}
		return true
	})
	out := make(map[string][]string, len(stemToDoc))
	for stem, dID := range stemToDoc {
		out[stem] = docToChunks[dID]
	}
	return out, nil
}
