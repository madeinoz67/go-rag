package embed

import (
	"fmt"
	"strings"
)

// Role is the purpose of a text at embed time. Instruction-tuned embedding
// models (nomic-embed-text, E5, BGE) are trained on asymmetric query/passage
// prefixes; go-rag must present each text in its trained role (audit H07, book
// §4.2). The Role is a transient parameter to the Prefixer; it is not persisted.
type Role int

const (
	// RoleQuery marks a retrieval query.
	RoleQuery Role = iota
	// RoleDocument marks an indexed passage.
	RoleDocument
)

// Mode controls whether instruction prefixes are applied and how they are chosen.
type Mode string

const (
	// ModeAuto derives prefixes from the configured model via the default map;
	// unknown models get no prefix. The default.
	ModeAuto Mode = "auto"
	// ModeOn is an explicit opt-in: same resolution as ModeAuto (map lookup),
	// distinguished from ModeAuto only for intent/logging. Use with explicit
	// overrides to force a specific convention.
	ModeOn Mode = "on"
	// ModeOff never prefixes, regardless of model.
	ModeOff Mode = "off"
)

// ParseMode parses a mode string. "" defaults to ModeAuto. It returns an error
// for any value that is not auto|on|off so config.Set can reject typos.
func ParseMode(s string) (Mode, error) {
	switch Mode(s) {
	case "", ModeAuto:
		return ModeAuto, nil
	case ModeOn:
		return ModeOn, nil
	case ModeOff:
		return ModeOff, nil
	}
	return "", fmt.Errorf("invalid embedding_prefix mode %q (want auto|on|off)", s)
}

// convention is a model family's role prefixes plus a short provenance label
// stored per-vector so a half-prefixed corpus is detectable. An empty document
// prefix means passages are embedded unprefixed (e.g. BGE query-only instruction).
type convention struct {
	name     string // short label ("" = none/legacy); stored as embedding provenance
	query    string
	document string
}

// defaultConventions maps a lowercased model-name substring to its convention.
// Matched in order; the first hit wins. A model matching none embeds unprefixed
// (FR-003 — never corrupt a non-prefix model).
var defaultConventions = []struct {
	match string
	conv  convention
}{
	{"nomic-embed-text", convention{"nomic", "search_query: ", "search_document: "}},
	{"nomic-embed", convention{"nomic", "search_query: ", "search_document: "}},
	{"multilingual-e5", convention{"e5", "query: ", "passage: "}},
	{"e5-", convention{"e5", "query: ", "passage: "}},
	{"bge-", convention{"bge", "Represent this sentence for searching relevant passages: ", ""}},
}

// lookupConvention returns the default-map convention for model ("" if unknown).
func lookupConvention(model string) convention {
	m := strings.ToLower(model)
	for _, e := range defaultConventions {
		if strings.Contains(m, e.match) {
			return e.conv
		}
	}
	return convention{}
}

// Prefixer resolves and applies instruction prefixes for a configured model. It
// is pure: no I/O, no config import — the caller passes the resolved config
// values. The Embedder interface is unchanged (constitution Principle V);
// prefixing happens only at the call boundary.
type Prefixer struct {
	model       string
	mode        Mode
	queryPrefix string // explicit override (empty = derive)
	docPrefix   string // explicit override (empty = derive)
}

// NewPrefixer builds a Prefixer from the configured values.
func NewPrefixer(model string, mode Mode, queryOverride, docOverride string) *Prefixer {
	return &Prefixer{
		model:       model,
		mode:        mode,
		queryPrefix: queryOverride,
		docPrefix:   docOverride,
	}
}

// resolve returns the convention in effect, applying per-role override
// precedence: explicit override (per role) > mode-derived (default map) > none.
// A role whose override is unset falls back to the map convention for that role,
// so a user can override just the query prefix and keep the model's document
// prefix. The label is "custom" whenever any override is set, so a corpus
// re-embedded under different overrides is detected as a convention change
// (conservatively — see audit H07 US3).
func (p *Prefixer) resolve() convention {
	var mapConv convention
	if p.mode != ModeOff {
		mapConv = lookupConvention(p.model)
	}
	q := mapConv.query
	d := mapConv.document
	if p.queryPrefix != "" {
		q = p.queryPrefix
	}
	if p.docPrefix != "" {
		d = p.docPrefix
	}
	name := mapConv.name // "" when off or unknown model
	if p.queryPrefix != "" || p.docPrefix != "" {
		name = "custom"
	}
	return convention{name: name, query: q, document: d}
}

// ForRole returns the prefix string for the given role ("" if none in effect).
func (p *Prefixer) ForRole(r Role) string {
	c := p.resolve()
	switch r {
	case RoleQuery:
		return c.query
	case RoleDocument:
		return c.document
	}
	return ""
}

// Convention returns the short provenance label for the active convention
// ("" = none/legacy). Stored per-vector so a half-prefixed corpus is detectable.
func (p *Prefixer) Convention() string {
	return p.resolve().name
}

// Active reports whether any prefix is in effect for the given role.
func (p *Prefixer) Active(r Role) bool {
	return p.ForRole(r) != ""
}

// Apply prepends the role's prefix to a single text, idempotently.
func (p *Prefixer) Apply(r Role, text string) string {
	return Prepend(p.ForRole(r), text)
}

// ApplyAll prepends the role's prefix to each text. It returns the input slice
// unchanged when no prefix is in effect, otherwise a new prefixed slice.
func (p *Prefixer) ApplyAll(r Role, texts []string) []string {
	pre := p.ForRole(r)
	if pre == "" {
		return texts
	}
	out := make([]string, len(texts))
	for i, t := range texts {
		out[i] = Prepend(pre, t)
	}
	return out
}

// Prepend idempotently prepends prefix to text: if text already starts with the
// prefix it is returned unchanged (so a query literally beginning with
// "search_query:" is not double-prefixed). An empty prefix is a no-op.
func Prepend(prefix, text string) string {
	if prefix == "" {
		return text
	}
	if strings.HasPrefix(text, prefix) {
		return text
	}
	return prefix + text
}
