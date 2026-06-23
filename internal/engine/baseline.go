package engine

import (
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
