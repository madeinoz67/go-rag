package engine_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/madeinoz67/go-rag/internal/rest"
)

// TestConcurrent_AddQuery_NoCorruption is the US3 concurrency guard (FR-008/009):
// many simultaneous clients issue overlapping add (distinct files) + keyword
// query requests through one running server backed by a single Engine. Under the
// race detector it must show no data races; afterward exactly N distinct
// documents are ingested (no double-writes, no corruption).
func TestConcurrent_AddQuery_NoCorruption(t *testing.T) {
	ollama := fastFakeOllama(t)
	eng := openEngine(t, ollama.URL)

	restSrv := httptest.NewServer(rest.New(eng, "").Handler())
	defer restSrv.Close()

	dir := t.TempDir()
	const (
		adders   = 24
		queriers = 12
	)

	// Pre-write distinct files from the test goroutine (avoids t.Fatal in workers).
	docs := make([]string, adders)
	for i := range docs {
		docs[i] = writeDoc(t, dir, fmt.Sprintf("concurrent-%d.txt", i),
			fmt.Sprintf("concurrency document %d about go-rag multi transport server", i))
	}

	postJSON := func(url string, body any) error {
		b, _ := json.Marshal(body)
		resp, err := http.Post(url, "application/json", bytes.NewReader(b))
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("status %d", resp.StatusCode)
		}
		return nil
	}

	var wg sync.WaitGroup
	var errs atomic.Int32

	for i := range docs {
		wg.Add(1)
		go func(path string) {
			defer wg.Done()
			if err := postJSON(restSrv.URL+"/v1/add", map[string]any{"path": path}); err != nil {
				errs.Add(1)
			}
		}(docs[i])
	}
	for i := 0; i < queriers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := postJSON(restSrv.URL+"/v1/query", map[string]any{"query": "go-rag", "mode": "keyword", "k": 5}); err != nil {
				errs.Add(1)
			}
		}()
	}
	wg.Wait()

	if n := errs.Load(); n > 0 {
		t.Fatalf("%d concurrent requests failed", n)
	}

	// Single-writer invariant: each distinct file ingested exactly once.
	st, err := eng.Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if st.Documents != adders {
		t.Fatalf("documents = %d, want %d (double-write/corruption under concurrency)", st.Documents, adders)
	}
}
