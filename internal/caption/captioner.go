// Package caption abstracts image captioning: a background, local-model step that
// generates a searchable text description for each embedded PDF image/chart (spec
// 031 US4). It is the image-level sibling of package enrich, but for vision
// generation (it produces a caption string), not text generation (tags+summary).
// The v1 provider is a local Ollama multimodal/vision client; the Captioner
// interface keeps the door open for future providers without coupling the pipeline
// to a model.
package caption

import (
	"context"
	"errors"
	"strings"
)

// Captioner produces a text description of an image (spec 031 US4). The local
// multimodal model (e.g. llava, llama3.2-vision, moondream) returns a caption
// that makes the image's/chart's content searchable. Background, opt-in,
// local-only. Mirrors enrich.Enricher + its error taxonomy (spec 029).
type Captioner interface {
	// Caption generates a description of imageBytes (a JPEG/PNG). hint carries
	// per-image context (e.g. "page 3, image/png"). Returns ErrNothingToCaption
	// for an empty/unreadable image (terminal, not retried); ErrPermanent (use
	// WrapPermanent) for a permanent failure (4xx, unparseable output); any other
	// error is transient (model unreachable, circuit open, ctx cancelled) and
	// retried later.
	Caption(ctx context.Context, imageBytes []byte, hint string) (string, error)
	// Model returns the vision model identifier (provenance on the sidecar).
	Model() string
}

// ErrNothingToCaption signals the image has no readable content (empty/corrupt).
// Terminal — the image is skipped, not retried.
var ErrNothingToCaption = errors.New("caption: nothing to caption")

// ErrPermanent marks a permanent captioning failure (4xx, unparseable output) so
// the caller skips the image rather than retrying indefinitely. Transient errors
// (model unreachable, circuit open, ctx cancelled) are returned unwrapped.
var ErrPermanent = errors.New("caption: permanent failure")

// WrapPermanent wraps err as a permanent captioning failure.
func WrapPermanent(err error) error { return errors.Join(ErrPermanent, err) }

// IsPermanent reports whether err is a permanent captioning failure.
func IsPermanent(err error) bool { return errors.Is(err, ErrPermanent) }

// IsNothing reports whether err signals nothing-to-caption.
func IsNothing(err error) bool { return errors.Is(err, ErrNothingToCaption) }

// New constructs the configured Captioner provider (spec 031 FU-1). provider
// "ollama" or "" (default) → the Ollama vision provider; "openai" → an
// OpenAI-compatible provider (covers OpenAI/Azure/vLLM/LM Studio). endpoint is
// the base URL (resolved by the caller — empty falls back to OllamaURL). apiKey is
// used by cloud providers (Bearer token).
func New(provider, endpoint, model, apiKey string) Captioner {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "openai", "openai-compatible":
		return NewOpenAI(endpoint, model, apiKey)
	default:
		return NewOllama(endpoint, model)
	}
}
