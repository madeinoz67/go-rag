// Package enrich abstracts document enrichment: a background, local-model step
// that tags and summarizes each ingested document (spec 029). It is the
// document-level sibling of package embed, but for generation (it produces tags +
// summary text), not embedding (vectors). The v1 provider is a local Ollama
// generation client; the Enricher interface keeps the door open for future
// providers without coupling the pipeline to a model.
package enrich

import (
	"context"
	"errors"
)

// Enricher produces a document's enrichment (tags + summary) from its text using
// the local model (spec 029). Document-level — one call per document, not per
// chunk (the cost profile that makes local enrichment viable). The returned tags
// and summary are written by the pipeline onto Document.Enrichment as a
// non-identity sidecar.
type Enricher interface {
	// Enrich generates a small tag set and a one-line summary for a document.
	// docText is a bounded representation of the document's content (e.g. its
	// chunk text concatenated, capped). Returns ErrNothingToEnrich for a document
	// with no meaningful content; ErrPermanent (use WrapPermanent) for a permanent
	// failure (bad/unparseable output); any other error is transient (model
	// unreachable, circuit open, ctx cancelled) and retried later.
	Enrich(ctx context.Context, docText string) (tags []string, summary string, err error)
	// Model returns the generation model identifier (provenance on the sidecar).
	Model() string
}

// ErrNothingToEnrich signals the document has no meaningful content to enrich
// (empty/trivial). Distinct from a failure: the document is marked
// nothing-to-enrich (terminal, not retried), not failed.
var ErrNothingToEnrich = errors.New("enrich: nothing to enrich")

// ErrPermanent marks a permanent enrichment failure (bad/unparseable model
// output) so the caller marks the document failed (terminal) rather than
// retrying indefinitely. Transient errors (model unreachable, circuit open,
// ctx cancelled) are returned unwrapped and retried later.
var ErrPermanent = errors.New("enrich: permanent failure")

// WrapPermanent wraps err as a permanent enrichment failure.
func WrapPermanent(err error) error { return errors.Join(ErrPermanent, err) }

// IsPermanent reports whether err is a permanent enrichment failure.
func IsPermanent(err error) bool { return errors.Is(err, ErrPermanent) }

// IsNothing reports whether err signals nothing-to-enrich.
func IsNothing(err error) bool { return errors.Is(err, ErrNothingToEnrich) }
