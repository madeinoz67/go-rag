package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/madeinoz67/go-rag/internal/config"
	"github.com/madeinoz67/go-rag/internal/embed"
	"github.com/madeinoz67/go-rag/internal/engine"
	"github.com/madeinoz67/go-rag/internal/model"
	"github.com/madeinoz67/go-rag/internal/storage"
)

// MetricSet holds the dataset-wide averaged retrieval-quality metrics, each in
// [0,1] (data-model.md, research.md D4).
type MetricSet struct {
	RecallAt5    float64 `json:"recall_at_5"`
	RecallAt10   float64 `json:"recall_at_10"`
	PrecisionAt5 float64 `json:"precision_at_5"`
	MRR          float64 `json:"mrr"`
	NDCGAt10     float64 `json:"ndcg_at_10"`
}

// PerQueryResult is one golden query's scoring breakdown.
type PerQueryResult struct {
	ID           string  `json:"id"`
	Query        string  `json:"query"`
	RecallAt5    float64 `json:"recall_at_5"`
	RecallAt10   float64 `json:"recall_at_10"`
	PrecisionAt5 float64 `json:"precision_at_5"`
	MRR          float64 `json:"mrr"`
	NDCGAt10     float64 `json:"ndcg_at_10"`
	StaleLabels  int     `json:"stale_labels,omitempty"` // relevant ids not present in the vault
	Skipped      bool    `json:"skipped,omitempty"`
	SkipReason   string  `json:"skip_reason,omitempty"`
}

// EvaluationRun is the result of one pass over the golden dataset.
type EvaluationRun struct {
	Mode           string           `json:"mode"`           // offline | ollama
	Embedder       string           `json:"embedder"`       // model/embedder name
	RetrievalMode  string           `json:"retrieval_mode"` // hybrid | semantic | keyword
	K              int              `json:"k"`
	QueriesRun     int              `json:"queries_run"`
	QueriesSkipped int              `json:"queries_skipped"`
	PerQuery       []PerQueryResult `json:"per_query"`
	Metrics        MetricSet        `json:"metrics"`
}

// EvalRunner scores a golden dataset against a vault using a chosen embedder,
// driving the canonical engine.Query path (FR-007).
type EvalRunner struct {
	cfg config.Config
	db  *storage.DB
	em  embed.Embedder
}

// NewEvalRunner returns a runner over an open vault using embedder em (the
// deterministic offline embedder for CI, or a real Ollama embedder for a
// baseline). The caller owns db's lifetime.
func NewEvalRunner(cfg config.Config, db *storage.DB, em embed.Embedder) *EvalRunner {
	return &EvalRunner{cfg: cfg, db: db, em: em}
}

// Run scores every golden query. Zero-relevant queries and queries whose labeled
// relevant chunk_ids are all absent from the vault (stale labels) are skipped and
// reported, never scored as 0 (FR-008). Metrics are averaged over scored queries.
func (r *EvalRunner) Run(ctx context.Context, golden []GoldenQuery, mode string, k int, noRerank bool) (*EvaluationRun, error) {
	if k <= 0 {
		k = 10
	}
	existing := existingChunkIDs(r.db)

	eng := engine.NewWithEmbedder(r.cfg, r.db, r.em)
	defer eng.Close()

	run := &EvaluationRun{
		Mode:          r.em.Model(),
		Embedder:      r.em.Model(),
		RetrievalMode: mode,
		K:             k,
	}
	var sum MetricSet
	scored := 0
	for _, gq := range golden {
		labeled := toSet(gq.Relevant)
		if len(labeled) == 0 {
			run.QueriesSkipped++
			run.PerQuery = append(run.PerQuery, PerQueryResult{
				ID: gq.ID, Query: gq.Query, Skipped: true,
				SkipReason: "no relevant chunks labeled",
			})
			continue
		}
		// Keep only relevant ids that actually exist in the vault; the rest are
		// stale labels (e.g. from a chunker change) and are reported, not scored.
		rel := make(map[string]bool, len(labeled))
		stale := 0
		for id := range labeled {
			if existing[id] {
				rel[id] = true
			} else {
				stale++
			}
		}
		if len(rel) == 0 {
			run.QueriesSkipped++
			run.PerQuery = append(run.PerQuery, PerQueryResult{
				ID: gq.ID, Query: gq.Query, Skipped: true, StaleLabels: stale,
				SkipReason: "all labeled relevant chunks absent from vault (stale labels)",
			})
			continue
		}

		res, err := eng.Query(ctx, engine.QueryRequest{
			Query: gq.Query, K: k, Mode: mode, NoRerank: noRerank,
		})
		if err != nil {
			return nil, fmt.Errorf("query %q (%s): %w", gq.Query, gq.ID, err)
		}
		retrieved := make([]string, 0, len(res.Hits))
		for _, h := range res.Hits {
			retrieved = append(retrieved, h.ChunkID)
		}
		pq := PerQueryResult{
			ID:           gq.ID,
			Query:        gq.Query,
			RecallAt5:    RecallAt(retrieved, rel, 5),
			RecallAt10:   RecallAt(retrieved, rel, 10),
			PrecisionAt5: PrecisionAt(retrieved, rel, 5),
			MRR:          MRR(retrieved, rel),
			NDCGAt10:     NDCGAt(retrieved, rel, 10),
			StaleLabels:  stale,
		}
		run.PerQuery = append(run.PerQuery, pq)

		sum.RecallAt5 += pq.RecallAt5
		sum.RecallAt10 += pq.RecallAt10
		sum.PrecisionAt5 += pq.PrecisionAt5
		sum.MRR += pq.MRR
		sum.NDCGAt10 += pq.NDCGAt10
		scored++
	}
	run.QueriesRun = scored
	if scored > 0 {
		d := float64(scored)
		run.Metrics = MetricSet{
			RecallAt5:    sum.RecallAt5 / d,
			RecallAt10:   sum.RecallAt10 / d,
			PrecisionAt5: sum.PrecisionAt5 / d,
			MRR:          sum.MRR / d,
			NDCGAt10:     sum.NDCGAt10 / d,
		}
	}
	return run, nil
}

// existingChunkIDs scans the vault once and returns the set of stored chunk_ids.
// Used to detect stale golden labels cheaply.
func existingChunkIDs(db *storage.DB) map[string]bool {
	set := make(map[string]bool)
	if db == nil {
		return set
	}
	_ = db.PrefixScanByte(storage.PrefixChunk, func(_, val []byte) bool {
		var c model.Chunk
		if json.Unmarshal(val, &c) == nil {
			set[c.ID] = true
		}
		return true
	})
	return set
}

func toSet(ids []string) map[string]bool {
	m := make(map[string]bool, len(ids))
	for _, id := range ids {
		m[id] = true
	}
	return m
}

// ChunkRef is a chunk reference for golden-dataset authoring (--dump-chunks): the
// content-addressed id, its document, source file, and a short text preview.
type ChunkRef struct {
	ID         string `json:"id"`
	DocumentID string `json:"document_id"`
	FilePath   string `json:"file_path"`
	Preview    string `json:"preview"`
}

// ListChunks returns every stored chunk as a ChunkRef (id + document + file +
// preview), for golden-dataset authoring. Chunks whose document cannot be
// resolved get an empty file_path.
func ListChunks(db *storage.DB) ([]ChunkRef, error) {
	var refs []ChunkRef
	err := db.PrefixScanByte(storage.PrefixChunk, func(_, val []byte) bool {
		var c model.Chunk
		if json.Unmarshal(val, &c) != nil {
			return true
		}
		path := ""
		if raw, ok, _ := db.GetWithPrefix(storage.PrefixDocument, []byte(c.DocumentID)); ok {
			var d model.Document
			if json.Unmarshal(raw, &d) == nil {
				path = d.FilePath
			}
		}
		refs = append(refs, ChunkRef{ID: c.ID, DocumentID: c.DocumentID, FilePath: path, Preview: preview(c.Content)})
		return true
	})
	if err != nil {
		return nil, err
	}
	return refs, nil
}

func preview(s string) string {
	for i, r := range s {
		if r == '\n' {
			s = s[:i]
			break
		}
	}
	if len(s) > 80 {
		s = s[:80] + "…"
	}
	return s
}

// ProvisionCorpus builds a throwaway vault from corpusDir, ingesting it with em,
// and returns the open db plus a cleanup that closes it and removes the temp
// vault. This is the shared hermetic/reproducible setup used by both the
// `go-rag eval` CLI and the `go_rag_eval` MCP tool (Principle V parity). The
// caller MUST invoke cleanup when done. Eval is read-only with respect to any
// real vault — this temp vault is the only thing written (FR-006).
//
// prefix sets the instruction-prefix convention for the ingest (audit H07): ""
// leaves the default (auto); "off" ingests unprefixed (the baseline for an
// SC-001 prefix-off vs prefix-on comparison). The same convention applies to the
// queries the eval runner issues, since both derive their prefixer from cfg.
func ProvisionCorpus(ctx context.Context, corpusDir string, em embed.Embedder, prefix string) (config.Config, *storage.DB, func(), error) {
	tmp, err := os.MkdirTemp("", "go-rag-eval-*")
	if err != nil {
		return config.Config{}, nil, nil, err
	}
	dataDir := filepath.Join(tmp, "data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		os.RemoveAll(tmp)
		return config.Config{}, nil, nil, err
	}
	cfg := config.Default()
	cfg.DBPath = tmp
	cfg.WatchDirs = nil
	cfg.EmbeddingModel = em.Model()
	if prefix != "" {
		cfg.EmbeddingPrefix = prefix
	}

	db, err := storage.Open(dataDir)
	if err != nil {
		os.RemoveAll(tmp)
		return config.Config{}, nil, nil, err
	}
	// Ingest the corpus with the same embedder eval will query with.
	eng := engine.NewWithEmbedder(cfg, db, em)
	if _, err := eng.Add(ctx, corpusDir); err != nil {
		eng.Close()
		db.Close()
		os.RemoveAll(tmp)
		return config.Config{}, nil, nil, fmt.Errorf("ingest corpus %q: %w", corpusDir, err)
	}
	eng.Close() // drain async-after-ACK embeddings before scoring

	cleanup := func() {
		db.Close()
		os.RemoveAll(tmp)
	}
	return cfg, db, cleanup, nil
}

// --- Baseline (regression gate, US2) ---

// Baseline is a committed snapshot of an offline EvaluationRun's metrics,
// against which future runs are compared by the gate (research.md D7).
type Baseline struct {
	Mode       string    `json:"mode"`
	RecordedAt time.Time `json:"recorded_at"`
	Embedder   string    `json:"embedder"`
	Metrics    MetricSet `json:"metrics"`
}

// LoadBaseline reads a committed baseline file.
func LoadBaseline(path string) (*Baseline, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read baseline %q: %w", path, err)
	}
	var b Baseline
	if err := json.Unmarshal(data, &b); err != nil {
		return nil, fmt.Errorf("parse baseline %q: %w", path, err)
	}
	return &b, nil
}

// Save writes the baseline file (creating parent dirs).
func (b *Baseline) Save(path string) error {
	data, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// Comparison is the gate verdict of a run against a baseline.
type Comparison struct {
	Pass           bool               `json:"pass"`
	RecallAt10Drop float64            `json:"recall_at_10_drop_pct"` // positive = regression (percentage points)
	Tolerance      float64            `json:"tolerance_pct"`
	Deltas         map[string]float64 `json:"deltas_pct"` // per-metric (run - base), percentage points
}

// Compare evaluates a run against a baseline. The gate is recall@10: it fails
// when recall@10 drops by more than tolerance percentage points (research.md D7).
// MRR and NDCG@10 are reported as deltas but do not gate. A nil baseline always
// passes (no reference to compare against).
func Compare(run *EvaluationRun, base *Baseline, tolerance float64) *Comparison {
	c := &Comparison{Pass: true, Tolerance: tolerance, Deltas: map[string]float64{}}
	if base == nil {
		return c
	}
	c.Deltas["recall_at_5"] = (run.Metrics.RecallAt5 - base.Metrics.RecallAt5) * 100
	c.Deltas["recall_at_10"] = (run.Metrics.RecallAt10 - base.Metrics.RecallAt10) * 100
	c.Deltas["precision_at_5"] = (run.Metrics.PrecisionAt5 - base.Metrics.PrecisionAt5) * 100
	c.Deltas["mrr"] = (run.Metrics.MRR - base.Metrics.MRR) * 100
	c.Deltas["ndcg_at_10"] = (run.Metrics.NDCGAt10 - base.Metrics.NDCGAt10) * 100
	c.RecallAt10Drop = -c.Deltas["recall_at_10"] // positive number when recall regressed
	if c.RecallAt10Drop > tolerance {
		c.Pass = false
	}
	return c
}
