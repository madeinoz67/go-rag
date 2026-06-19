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
