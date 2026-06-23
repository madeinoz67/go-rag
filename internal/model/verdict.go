package model

// verdict.go defines the per-chunk injection-poisoning verdict (spec 019 / audit H04).
//
// A PoisonVerdict is a pure deterministic function of chunk text (Constitution II):
// identical text always yields an identical verdict, so re-score / corpus-rescan is a
// no-op for unchanged content. It is persisted ON the Chunk record (riding the chunk
// store batch — no extra fsync) and surfaced on every QueryHit across all transports
// (CLI/REST/gRPC/MCP) so downstream LLM consumers can treat retrieved text as untrusted.

// PoisonLevel is the verdict level for a chunk.
type PoisonLevel string

const (
	// PoisonClean: below the suspicious threshold — fully retrievable.
	PoisonClean PoisonLevel = "clean"
	// PoisonSuspicious: ≥ suspicious threshold — quarantined out of default results.
	PoisonSuspicious PoisonLevel = "suspicious"
	// PoisonQuarantine: ≥ quarantine threshold — quarantined out of default results.
	PoisonQuarantine PoisonLevel = "quarantine"
	// PoisonReleased: user-asserted override (sticky across rescans; outranks the
	// scorer). The chunk re-enters default retrieval; the original score is retained.
	PoisonReleased PoisonLevel = "released"
)

// Quarantined reports whether this level is excluded from default query results
// (suspicious or quarantine). clean and released are retrievable by default. This is
// the single predicate the engine's pre-fusion keep uses (FR-004, Q1=A).
func (l PoisonLevel) Quarantined() bool {
	return l == PoisonSuspicious || l == PoisonQuarantine
}

// PoisonSignals is the per-signal contribution to a verdict (research D1):
// repetition, keyword/phrase stuffing, and instruction-phrase match. Each in [0,1].
type PoisonSignals struct {
	Repetition  float64 `json:"repetition"`
	Stuffing    float64 `json:"stuffing"`
	Instruction float64 `json:"instruction"`
}

// PoisonVerdict is the per-chunk injection-risk assessment.
type PoisonVerdict struct {
	Level          PoisonLevel   `json:"level"`
	Score          float64       `json:"score"` // combined score in [0,1]
	Signals        PoisonSignals `json:"signals"`
	MatchedPhrases []string      `json:"matched_phrases,omitempty"` // instruction-phrase hits
}
