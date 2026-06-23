package engine

import (
	"context"
	"encoding/json"
	"time"

	"github.com/madeinoz67/go-rag/internal/storage"
)

// CorpusBaseline is the persisted snapshot of the embedding profile the corpus
// was built under (audit H11/spec 017). One record per vault, stored under
// PrefixCorpusMeta (0x10). The boot drift check compares it to the live config
// + live Ollama version; it is written on first embed, refreshed on successful
// migrate, and backfilled on first boot for a pre-H11 corpus.
//
// It is distinct from the per-embedding provenance on the 0x04 record (H07) —
// this is a corpus-level header carrying the Ollama server version (stored
// nowhere else today), and a point-in-time snapshot of the majority profile.
type CorpusBaseline struct {
	Model         string    `json:"model,omitempty"`
	Dim           int       `json:"dim,omitempty"`
	Convention    string    `json:"convention,omitempty"`
	OllamaVersion string    `json:"ollama_version,omitempty"`
	RecordedAt    time.Time `json:"recorded_at"`
}

// corpusBaselineKey is the single fixed key under PrefixCorpusMeta (one record
// per vault; the prefix denotes the subsystem, matching the codebase idiom).
const corpusBaselineKey = "default"

// LoadBaseline reads the corpus baseline. ok is false when no baseline exists
// (a pre-H11 corpus before backfill, or an empty vault).
func LoadBaseline(db *storage.DB) (*CorpusBaseline, bool) {
	raw, ok, _ := db.GetWithPrefix(storage.PrefixCorpusMeta, []byte(corpusBaselineKey))
	if !ok || len(raw) == 0 {
		return nil, false
	}
	var b CorpusBaseline
	if err := json.Unmarshal(raw, &b); err != nil {
		return nil, false
	}
	return &b, true
}

// SaveBaseline writes (overwrites) the corpus baseline, stamping RecordedAt to
// now when unset.
func SaveBaseline(db *storage.DB, b *CorpusBaseline) error {
	if b.RecordedAt.IsZero() {
		b.RecordedAt = time.Now().UTC()
	}
	data, err := json.Marshal(b)
	if err != nil {
		return err
	}
	return db.SetWithPrefix(storage.PrefixCorpusMeta, []byte(corpusBaselineKey), data)
}

// handleFirstEmbed persists the corpus baseline on the first successful embed
// (audit H11/spec 017), bound to the pipeline's OnFirstEmbed hook. It no-ops
// once a baseline exists (so it doesn't overwrite on every ingest) and stamps
// the live Ollama version captured at boot (CachedLiveVersion). kept in the
// engine package so the baseline store stays out of internal/pipeline.
func (e *Engine) handleFirstEmbed(model string, dim int, convention string) {
	if _, ok := LoadBaseline(e.db); ok {
		return // baseline already exists — don't overwrite on routine ingests
	}
	_ = SaveBaseline(e.db, &CorpusBaseline{
		Model:         model,
		Dim:           dim,
		Convention:    convention,
		OllamaVersion: e.CachedLiveVersion(),
	})
}

// refreshBaselineAfterMigrate rewrites the corpus baseline to the current
// embedding profile + live Ollama version after a successful migrate (audit
// H11/spec 017) — post-migrate the corpus is uniform under the new model, so
// the baseline is freshly authoritative. Also refreshes the cached verdict so
// the daemon flips to clean without a restart.
func (e *Engine) refreshBaselineAfterMigrate(ctx context.Context) {
	var model string
	var dim int
	if em := e.embedderOrOllama(); em != nil {
		model = em.Model()
		dim = em.Dimensions()
	}
	conv := ""
	if pre := e.cfg.Prefixer(); pre != nil {
		conv = pre.Convention()
	}
	live := e.CachedLiveVersion()
	if live == "" {
		live = ollamaVersion(ctx, e.cfg.OllamaURL)
	}
	_ = SaveBaseline(e.db, &CorpusBaseline{
		Model: model, Dim: dim, Convention: conv, OllamaVersion: live,
	})
	e.RefreshDriftVerdict(ctx)
}
