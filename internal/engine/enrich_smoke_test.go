package engine

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/madeinoz67/go-rag/internal/config"
	"github.com/madeinoz67/go-rag/internal/storage"
)

// TestSmoke_EnrichRealDoc (spec 029) is a REAL end-to-end enrichment over a real
// document using the local Ollama generation model (not hermetic — it calls the
// model). Gated on GO_RAG_SMOKE=1 so the normal suite never runs it. Run with:
//
//	GO_RAG_SMOKE=1 go test ./internal/engine/ -run TestSmoke_EnrichRealDoc -v -timeout 180s
//
// Requires Ollama at OLLAMA_URL with an embedding model + a generation model.
func TestSmoke_EnrichRealDoc(t *testing.T) {
	if os.Getenv("GO_RAG_SMOKE") != "1" {
		t.Skip("set GO_RAG_SMOKE=1 to run the real-Ollama enrichment smoke test")
	}
	url := os.Getenv("OLLAMA_URL")
	if url == "" {
		url = "http://localhost:11434"
	}
	embedModel := os.Getenv("GO_RAG_EMBED_MODEL")
	if embedModel == "" {
		embedModel = "nomic-embed-text"
	}
	genModel := os.Getenv("GO_RAG_GEN_MODEL")
	if genModel == "" {
		genModel = "llama3.1:latest"
	}

	dir := t.TempDir()
	db, err := storage.Open(filepath.Join(dir, "data"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	cfg := config.Default()
	cfg.DBPath = dir
	cfg.OllamaURL = url
	cfg.EmbeddingModel = embedModel
	cfg.EnrichmentEnabled = true
	cfg.EnrichmentModel = genModel
	cfg.WatchDirs = nil

	// A real, topical document (mirrors the BESS project context).
	docPath := filepath.Join(dir, "bess.md")
	docText := "# Home Battery Backup (BESS)\n\n" +
		"The Sigenergy SigenStor is a 32 kWh battery storage system paired with a 15 kW three-phase inverter. " +
		"It integrates with existing rooftop solar to store surplus generation for nighttime use and grid outages. " +
		"The system supports solar-assisted EV charging via the Tessie API and a custom pyscript that manages " +
		"charge cycles against live solar production and the battery state of charge. Optimal charge/discharge " +
		"automation keeps the home off-grid through the evening peak."
	if err := os.WriteFile(docPath, []byte(docText), 0o644); err != nil {
		t.Fatalf("write doc: %v", err)
	}

	eng := NewWithDB(cfg, db)
	if _, err := eng.Add(context.Background(), docPath); err != nil {
		t.Fatalf("engine.Add: %v", err)
	}
	eng.Close() // drain async embed + enrich

	// Read the stored document's enrichment sidecar.
	var found bool
	_ = db.PrefixScanByte(storage.PrefixDocument, func(_ []byte, val []byte) bool {
		var m map[string]any
		if json.Unmarshal(val, &m) != nil {
			return true
		}
		enc, ok := m["enrichment"].(map[string]any)
		if !ok || enc == nil {
			return true
		}
		found = true
		status, _ := enc["status"].(string)
		summary, _ := enc["summary"].(string)
		model, _ := enc["model"].(string)
		tagsAny, _ := enc["tags"].([]any)
		tags := make([]string, 0, len(tagsAny))
		for _, tg := range tagsAny {
			if s, ok := tg.(string); ok {
				tags = append(tags, s)
			}
		}
		t.Logf("MODEL:   %s", model)
		t.Logf("STATUS:  %s", status)
		t.Logf("TAGS:    %v", tags)
		t.Logf("SUMMARY: %s", summary)
		if status != "enriched" {
			t.Errorf("status = %q, want enriched", status)
		}
		if len(tags) == 0 {
			t.Error("expected non-empty tags")
		}
		if summary == "" {
			t.Error("expected non-empty summary")
		}
		return false
	})
	if !found {
		t.Fatal("no document with an Enrichment sidecar found — enrichment did not run (is Ollama up with the models?)")
	}

	// The US1 payoff: the auto-tags flow into the existing tag filter. Re-open the
	// engine over the now-enriched, indexed DB and query with --tags solar — the
	// BESS doc must survive the pre-fusion tag filter and be returned.
	eng2 := NewWithDB(cfg, db)
	defer eng2.Close()
	res, err := eng2.Query(context.Background(), QueryRequest{
		Query:  "battery",
		K:      5,
		Filter: NewFilter("", "", []string{"solar"}), // an auto-generated tag
	})
	if err != nil {
		t.Fatalf("query with --tags solar: %v", err)
	}
	if len(res.Hits) == 0 {
		t.Fatal("query --tags solar returned no hits — the auto-tag did not reach the filter")
	}
	t.Logf("FILTER: --tags solar returned %d hit(s); first summary: %q", len(res.Hits), res.Hits[0].Summary)
}
