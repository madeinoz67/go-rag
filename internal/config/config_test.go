package config

import (
	"path/filepath"
	"testing"
)

func TestDefault_HasExpectedValues(t *testing.T) {
	c := Default()
	if c.OllamaURL != "http://localhost:11434" {
		t.Errorf("ollama_url: got %q", c.OllamaURL)
	}
	if c.ChunkSize != 512 {
		t.Errorf("chunk_size: got %d", c.ChunkSize)
	}
	if c.ChunkOverlap != 50 {
		t.Errorf("chunk_overlap: got %d", c.ChunkOverlap)
	}
	if c.PollIntervalSec != 60 {
		t.Errorf("poll_interval_secs: got %d", c.PollIntervalSec)
	}
	// MCPAddr MUST default to loopback (spec 007 / audit H13). This exact-value
	// check is the regression guard against a silent revert to ":7878"
	// (all-interfaces) — the original accidental-exposure footgun.
	if c.MCPAddr != "127.0.0.1:7878" {
		t.Errorf("mcp_addr default must be loopback (127.0.0.1:7878), got %q", c.MCPAddr)
	}
	if err := c.Validate(); err != nil {
		t.Errorf("default config must validate: %v", err)
	}
}

func TestValidate_RejectsBadValues(t *testing.T) {
	good := "http://localhost:11434"
	cases := map[string]Config{
		"bad url":        {OllamaURL: "not a url"},
		"empty url":      {OllamaURL: ""},
		"zero chunk":     {OllamaURL: good, ChunkSize: 0},
		"neg overlap":    {OllamaURL: good, ChunkSize: 512, ChunkOverlap: -1},
		"zero poll":      {OllamaURL: good, PollIntervalSec: 0},
	}
	for name, c := range cases {
		if err := c.Validate(); err == nil {
			t.Errorf("%s: expected validation error, got nil", name)
		}
	}
}

func TestLoadSave_RoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	c := Default()
	c.EmbeddingModel = "nomic-embed-text"
	c.ChunkSize = 1024

	if err := Save(path, c); err != nil {
		t.Fatalf("save: %v", err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.EmbeddingModel != "nomic-embed-text" || loaded.ChunkSize != 1024 || loaded.OllamaURL != c.OllamaURL {
		t.Errorf("round-trip mismatch: %+v", loaded)
	}
}

func TestSet_GetRoundTrip(t *testing.T) {
	c := Default()
	if err := c.Set("chunk_size", "256"); err != nil {
		t.Fatalf("set chunk_size: %v", err)
	}
	if v, ok := c.Get("chunk_size"); !ok || v != "256" {
		t.Errorf("get chunk_size: ok=%v v=%q", ok, v)
	}
	if err := c.Set("chunk_size", "bogus"); err == nil {
		t.Error("set non-numeric chunk_size must fail")
	}
	if err := c.Set("chunk_size", "0"); err == nil {
		t.Error("set zero chunk_size must fail")
	}
	if err := c.Set("no_such_key", "x"); err == nil {
		t.Error("set unknown key must fail")
	}
}

// TestEmbeddingPrefix_Config covers the H07 prefix-config surface: the default
// mode, mode-enum validation, malformed-prefix rejection, and Get/Set round-trip.
func TestEmbeddingPrefix_Config(t *testing.T) {
	// Default mode is "auto" (FR-002) and normalizes "" to "auto" on Get.
	c := Default()
	if c.EmbeddingPrefix != "auto" {
		t.Errorf("default embedding_prefix = %q, want auto", c.EmbeddingPrefix)
	}
	if v, _ := c.Get("embedding_prefix"); v != "auto" {
		t.Errorf("get embedding_prefix = %q, want auto", v)
	}

	// Mode enum: auto/on/off/"" accepted; anything else rejected.
	for _, ok := range []string{"auto", "on", "off", ""} {
		var cc Config
		if err := cc.Set("embedding_prefix", ok); err != nil {
			t.Errorf("Set embedding_prefix=%q should succeed: %v", ok, err)
		}
	}
	for _, bad := range []string{"AUTO", "yes", "true", "1"} {
		var cc Config
		if err := cc.Set("embedding_prefix", bad); err == nil {
			t.Errorf("Set embedding_prefix=%q must fail (not auto|on|off)", bad)
		}
	}

	// Malformed prefixes (newline / control char) are rejected; clean ones accepted.
	for _, bad := range []string{"bad\nprefix", "x\ry", "ctrl\x00x"} {
		var cc Config
		if err := cc.Set("embedding_query_prefix", bad); err == nil {
			t.Errorf("Set embedding_query_prefix=%q must fail (malformed)", bad)
		}
		if err := cc.Set("embedding_doc_prefix", bad); err == nil {
			t.Errorf("Set embedding_doc_prefix=%q must fail (malformed)", bad)
		}
	}
	var cc Config
	if err := cc.Set("embedding_query_prefix", "query: "); err != nil {
		t.Fatalf("set clean query prefix: %v", err)
	}
	if v, _ := cc.Get("embedding_query_prefix"); v != "query: " {
		t.Errorf("get embedding_query_prefix = %q, want %q", v, "query: ")
	}
	// Empty clears the override and is valid.
	if err := cc.Set("embedding_doc_prefix", ""); err != nil {
		t.Fatalf("set empty doc prefix should clear: %v", err)
	}
}

// TestRRFK_Config covers the H08/spec 009 RRF-constant config surface: the
// default, the zero-value sentinel (absent key = default), Get/Set round-trip,
// and validation (negative rejected; zero accepted as "use default").
func TestRRFK_Config(t *testing.T) {
	// Default is 60 and validates.
	c := Default()
	if c.RRFK != 60 {
		t.Errorf("default rrf_k = %d, want 60", c.RRFK)
	}
	if c.EffectiveRRFK() != 60 {
		t.Errorf("default EffectiveRRFK = %d, want 60", c.EffectiveRRFK())
	}

	// Absent key (zero value) resolves to default — backward compat for existing
	// configs that omit rrf_k.
	var zero Config
	if zero.EffectiveRRFK() != 60 {
		t.Errorf("zero-value EffectiveRRFK = %d, want 60", zero.EffectiveRRFK())
	}

	// Set + Get round-trip (Get reports the effective value).
	if err := c.Set("rrf_k", "120"); err != nil {
		t.Fatalf("set rrf_k 120: %v", err)
	}
	if c.EffectiveRRFK() != 120 {
		t.Errorf("after set 120, EffectiveRRFK = %d, want 120", c.EffectiveRRFK())
	}
	if v, ok := c.Get("rrf_k"); !ok || v != "120" {
		t.Errorf("get rrf_k = %q (ok=%v), want 120", v, ok)
	}

	// Set 0 = "use default" (clears the override); valid.
	if err := c.Set("rrf_k", "0"); err != nil {
		t.Fatalf("set rrf_k 0 should be valid (means default): %v", err)
	}
	if c.EffectiveRRFK() != 60 {
		t.Errorf("after set 0, EffectiveRRFK = %d, want 60", c.EffectiveRRFK())
	}

	// Negative is rejected by Set...
	if err := c.Set("rrf_k", "-1"); err == nil {
		t.Error("set rrf_k -1 must fail")
	}
	// ...and by Validate.
	c.RRFK = -5
	if err := c.Validate(); err == nil {
		t.Error("Validate must reject negative rrf_k")
	}
	c.RRFK = 0
	if err := c.Validate(); err != nil {
		t.Errorf("Validate must accept rrf_k=0 (default): %v", err)
	}

	// Non-numeric is rejected.
	if err := c.Set("rrf_k", "bogus"); err == nil {
		t.Error("set non-numeric rrf_k must fail")
	}
}
