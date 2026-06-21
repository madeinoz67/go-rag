package embed

import "testing"

func TestParseMode(t *testing.T) {
	cases := []struct {
		in   string
		want Mode
		ok   bool
	}{
		{"", ModeAuto, true},
		{"auto", ModeAuto, true},
		{"on", ModeOn, true},
		{"off", ModeOff, true},
		{"AUTO", "", false}, // case-sensitive: rejected
		{"bogus", "", false},
	}
	for _, c := range cases {
		got, err := ParseMode(c.in)
		if c.ok && err != nil {
			t.Errorf("ParseMode(%q): unexpected error %v", c.in, err)
			continue
		}
		if !c.ok && err == nil {
			t.Errorf("ParseMode(%q): expected error, got %v", c.in, got)
			continue
		}
		if c.ok && got != c.want {
			t.Errorf("ParseMode(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestPrefixer_NomicConvention(t *testing.T) {
	p := NewPrefixer("nomic-embed-text", ModeAuto, "", "")
	if got := p.ForRole(RoleQuery); got != "search_query: " {
		t.Errorf("nomic query prefix = %q, want %q", got, "search_query: ")
	}
	if got := p.ForRole(RoleDocument); got != "search_document: " {
		t.Errorf("nomic document prefix = %q, want %q", got, "search_document: ")
	}
	if got := p.Convention(); got != "nomic" {
		t.Errorf("nomic convention = %q, want %q", got, "nomic")
	}
	// tag suffix still matches
	p2 := NewPrefixer("nomic-embed-text:latest", ModeAuto, "", "")
	if got := p2.Convention(); got != "nomic" {
		t.Errorf("nomic:latest convention = %q, want nomic", got)
	}
}

func TestPrefixer_E5Convention(t *testing.T) {
	for _, model := range []string{"intfloat/e5-large-v2", "multilingual-e5-large"} {
		p := NewPrefixer(model, ModeAuto, "", "")
		if got := p.ForRole(RoleQuery); got != "query: " {
			t.Errorf("%s query prefix = %q, want %q", model, got, "query: ")
		}
		if got := p.ForRole(RoleDocument); got != "passage: " {
			t.Errorf("%s document prefix = %q, want %q", model, got, "passage: ")
		}
		if got := p.Convention(); got != "e5" {
			t.Errorf("%s convention = %q, want e5", model, got)
		}
	}
}

func TestPrefixer_BGEQueryOnly(t *testing.T) {
	p := NewPrefixer("bge-large-en-v1.5", ModeAuto, "", "")
	if got := p.ForRole(RoleQuery); got == "" {
		t.Errorf("bge query prefix empty; want an instruction")
	}
	// BGE documents embed unprefixed (asymmetric query-only).
	if got := p.ForRole(RoleDocument); got != "" {
		t.Errorf("bge document prefix = %q, want empty", got)
	}
	if !p.Active(RoleQuery) || p.Active(RoleDocument) {
		t.Errorf("bge active flags wrong: query=%v document=%v", p.Active(RoleQuery), p.Active(RoleDocument))
	}
}

func TestPrefixer_UnknownModelNoPrefix(t *testing.T) {
	p := NewPrefixer("all-MiniLM-L6-v2", ModeAuto, "", "")
	if got := p.ForRole(RoleQuery); got != "" {
		t.Errorf("unknown model query prefix = %q, want empty (FR-003)", got)
	}
	if got := p.ForRole(RoleDocument); got != "" {
		t.Errorf("unknown model document prefix = %q, want empty", got)
	}
	if got := p.Convention(); got != "" {
		t.Errorf("unknown model convention = %q, want empty", got)
	}
}

func TestPrefixer_ModeOffNoPrefix(t *testing.T) {
	// Off suppresses prefixing even for a prefix model.
	p := NewPrefixer("nomic-embed-text", ModeOff, "", "")
	if got := p.ForRole(RoleQuery); got != "" {
		t.Errorf("off+ nomic query prefix = %q, want empty", got)
	}
	if got := p.Convention(); got != "" {
		t.Errorf("off convention = %q, want empty", got)
	}
}

func TestPrefixer_ExplicitOverrides(t *testing.T) {
	p := NewPrefixer("all-MiniLM-L6-v2", ModeOn, "q::", "d::")
	if got := p.ForRole(RoleQuery); got != "q::" {
		t.Errorf("override query = %q, want %q", got, "q::")
	}
	if got := p.ForRole(RoleDocument); got != "d::" {
		t.Errorf("override document = %q, want %q", got, "d::")
	}
	if got := p.Convention(); got != "custom" {
		t.Errorf("override convention = %q, want custom", got)
	}
	// A single-role override still wins for that role; the other derives/maps.
	p2 := NewPrefixer("nomic-embed-text", ModeAuto, "Q::", "")
	if got := p2.ForRole(RoleQuery); got != "Q::" {
		t.Errorf("query override = %q, want Q::", got)
	}
	if got := p2.ForRole(RoleDocument); got != "search_document: " {
		t.Errorf("document derived = %q, want search_document: ", got)
	}
	// Once any override is present the label is "custom" (conservative drift signal).
	if got := p2.Convention(); got != "custom" {
		t.Errorf("mixed-override convention = %q, want custom", got)
	}
}

func TestPrepend_Idempotent(t *testing.T) {
	pre := "search_query: "
	out := Prepend(pre, "what is bitcoin")
	if out != "search_query: what is bitcoin" {
		t.Errorf("prepend = %q", out)
	}
	// A text already beginning with the prefix is not double-prefixed.
	again := Prepend(pre, out)
	if again != out {
		t.Errorf("double-prepend = %q, want %q", again, out)
	}
	// Empty prefix is a no-op.
	if got := Prepend("", "x"); got != "x" {
		t.Errorf("empty prepend = %q", got)
	}
}

func TestPrefixer_ApplyAll(t *testing.T) {
	p := NewPrefixer("nomic-embed-text", ModeAuto, "", "")
	in := []string{"a", "b", "c"}
	out := p.ApplyAll(RoleDocument, in)
	want := []string{"search_document: a", "search_document: b", "search_document: c"}
	for i := range want {
		if out[i] != want[i] {
			t.Errorf("ApplyAll[%d] = %q, want %q", i, out[i], want[i])
		}
	}
	// No-prefix model returns the input slice unchanged (no allocation).
	np := NewPrefixer("all-MiniLM-L6-v2", ModeAuto, "", "")
	if got := np.ApplyAll(RoleQuery, in); &got[0] != &in[0] {
		t.Errorf("ApplyAll no-prefix should return input slice unchanged")
	}
}

func TestPrefixer_EmptyText(t *testing.T) {
	p := NewPrefixer("nomic-embed-text", ModeAuto, "", "")
	// Empty/whitespace text does not error; it yields the prefix token. Embedding
	// an empty string is the embedder's concern, not the prefixer's.
	if got := p.Apply(RoleQuery, ""); got != "search_query: " {
		t.Errorf("empty-text apply = %q, want %q", got, "search_query: ")
	}
}
