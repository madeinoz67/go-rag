// Package poison scores chunk text for indirect-prompt-injection / retrieval-
// poisoning risk (spec 019 / audit H04).
//
// The default HeuristicScorer (research D1) normalizes text and combines three
// additive signals — verbatim repetition, keyword/phrase stuffing, and
// instruction-phrase match — into a deterministic [0,1] score mapped to a
// PoisonLevel via configurable thresholds. Pure stdlib, no I/O, no new dependency
// (Constitution III). Idempotent: identical text yields an identical verdict, so
// a re-score / corpus rescan is a no-op for unchanged content (Constitution II).
//
// Detection is heuristic defense-in-depth, NOT a guarantee (FR-008): a
// deliberately obfuscated payload can evade lexical signals. Quarantine-by-default
// (Q1=A) plus per-hit verdict surfacing (FR-005) layer the defense.
package poison

import "github.com/madeinoz67/go-rag/internal/model"

// Detector scores a chunk's text for injection-poisoning risk, returning a
// PoisonVerdict. Implementations MUST be pure functions of text (deterministic,
// no I/O) so the verdict is content-addressed and a re-score is a no-op. The
// default implementation is HeuristicScorer; future detectors (e.g. a
// model-backed one behind an adapter) implement this interface without touching
// the pipeline (Constitution V — extension by interface).
type Detector interface {
	Score(text string) model.PoisonVerdict
}
