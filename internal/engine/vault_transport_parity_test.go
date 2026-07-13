package engine_test

// vault_transport_parity_test.go (T021) proves the vault selector threads
// through all three transports: REST (?vault=), gRPC (QueryRequest.Vault),
// and MCP (the `vault` tool argument). Two vaults are ingested on one shared
// Engine; each transport is then asked to query vaultA and vaultB in turn.
// A correct vault selector routes the query to the named vault only — the
// other vault's content must NOT leak (isolation). This complements the
// cross-transport *result* parity suite (parity_test.go, FR-002) by pinning
// the cross-transport *vault-routing* parity (spec 052 / Step 5).
//
// The file reuses the hermetic harness already defined in parity_test.go
// (openEngine, fastFakeOllama, dialGRPC, the rest* DTO types) — same package,
// no duplication.

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/madeinoz67/go-rag/internal/config"
	"github.com/madeinoz67/go-rag/internal/engine"
	"github.com/madeinoz67/go-rag/internal/mcp"
	"github.com/madeinoz67/go-rag/internal/rest"
	goragpb "github.com/madeinoz67/go-rag/proto/gen"
)

// waitForVaultEmbeds polls Status until every named vault's async embedders
// have drained (≥1 embedding + EmbeddingsComplete). openEngine's Add ACKs after
// the durable commit but embeddings run on background workers; keyword (BM25)
// queries work as soon as the FTS index is written, but we wait for embeds too
// so the test is robust against ordering races on slower machines.
func waitForVaultEmbeds(t *testing.T, eng *engine.Engine, vaults ...string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		allDone := true
		for _, v := range vaults {
			st, err := eng.Status(v)
			if err != nil || st == nil || !st.EmbeddingsComplete || st.Embeddings == 0 {
				allDone = false
				break
			}
		}
		if allDone {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("embeddings did not drain within timeout for vaults %v", vaults)
}

// addDocTo writes content to a temp file and ingests it into the named vault
// through the Engine's public Add API (the path every transport ultimately
// drives). Returns the ingested file path.
func addDocTo(t *testing.T, eng *engine.Engine, vault, content string) {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "doc.txt")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write doc: %v", err)
	}
	if _, err := eng.Add(context.Background(), vault, p, "*"); err != nil {
		t.Fatalf("Add(%q): %v", vault, err)
	}
}

// queryOverRESTVault POSTs /v1/query with the vault selector on the query
// string — the extraction path internal/rest/engine_adapter.go uses
// (r.URL.Query().Get("vault"), defaulting to "default" when empty).
func queryOverRESTVault(t *testing.T, baseURL, q, vault string) []restQueryHit {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"query": q, "mode": "keyword", "k": 5})
	u := baseURL + "/v1/query?vault=" + url.QueryEscape(vault)
	resp, err := http.Post(u, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("REST query (vault=%q): %v", vault, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("REST status = %d for vault=%q, want 200", resp.StatusCode, vault)
	}
	var out restQueryResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode REST response (vault=%q): %v", vault, err)
	}
	return out.Hits
}

// queryOverGRPCVault drives a Query over the in-process gRPC client with the
// Vault field set — the extraction path internal/grpc uses
// (req.GetVault() on the proto message).
func queryOverGRPCVault(t *testing.T, client goragpb.GoragClient, q, vault string) *goragpb.QueryResponse {
	t.Helper()
	resp, err := client.Query(context.Background(), &goragpb.QueryRequest{
		Query: q, Mode: "keyword", K: 5, Vault: vault,
	})
	if err != nil {
		t.Fatalf("gRPC Query (vault=%q): %v", vault, err)
	}
	return resp
}

// queryOverMCPVault drives the go_rag_query MCP tool with the `vault` argument
// set — the extraction path internal/mcp/server.go uses (vaultArg(args),
// defaulting to "default" when absent/empty). Returns the rendered text body.
func queryOverMCPVault(t *testing.T, baseURL, q, vault string) string {
	t.Helper()
	args := map[string]any{"query": q, "mode": "keyword", "k": 5}
	if vault != "" {
		args["vault"] = vault
	}
	body, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "tools/call",
		"params": map[string]any{"name": "go_rag_query", "arguments": args},
	})
	resp, err := http.Post(baseURL+"/mcp", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("MCP query (vault=%q): %v", vault, err)
	}
	defer resp.Body.Close()
	var env struct {
		Result struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error,omitempty"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode MCP response (vault=%q): %v", vault, err)
	}
	if env.Error != nil {
		t.Fatalf("MCP query error (vault=%q): %s", vault, env.Error.Message)
	}
	if len(env.Result.Content) == 0 {
		t.Fatalf("MCP query (vault=%q): empty content", vault)
	}
	return env.Result.Content[0].Text
}

// TestCrossTransport_VaultParity (T021): the vault selector routes queries to
// the named vault only, identically across REST, gRPC, and MCP — all backed by
// one shared Engine. Two vaults each hold a document with a shared queryable
// term plus a unique marker token. For every transport:
//
//   - querying vaultA returns avaultToken and NOT bvaultToken (no leak)
//   - querying vaultB returns bvaultToken and NOT avaultToken (no leak)
//
// This is the FR-002 vault-routing analog: not "same hits" (parity_test.go)
// but "same correct vault isolation across transports".
func TestCrossTransport_VaultParity(t *testing.T) {
	eng := openEngine(t, fastFakeOllama(t).URL)

	const (
		vaultA = "teamA"
		vaultB = "teamB"
	)
	// Both docs share the query term so the only way to distinguish them is the
	// vault selector; each carries a unique marker for the isolation assertion.
	addDocTo(t, eng, vaultA, "team alpha quarterly report sharedterm avaulttoken revenue data")
	addDocTo(t, eng, vaultB, "team beta quarterly report sharedterm bvaulttoken revenue data")
	waitForVaultEmbeds(t, eng, vaultA, vaultB)

	const query = "sharedterm"

	// --- REST: vault selector on the query string. ---
	restSrv := httptest.NewServer(rest.New(eng, "").Handler())
	defer restSrv.Close()

	// --- gRPC: vault selector on the proto message (in-process bufconn). ---
	grpcClient := dialGRPC(t, eng)

	// --- MCP: vault selector as a tool argument (daemon mode, NewWithEngine). ---
	mcpSrv := httptest.NewServer(mcp.NewWithEngine("", eng, config.Default()).HTTPHandler(""))
	defer mcpSrv.Close()

	type transportCase struct {
		name        string
		queryVaultA func() string // returns the concatenation of hit content for vaultA
		queryVaultB func() string // returns the concatenation of hit content for vaultB
	}

	restContent := func(hits []restQueryHit) string {
		var b strings.Builder
		for _, h := range hits {
			b.WriteString(h.Content)
		}
		return b.String()
	}
	grpcContent := func(resp *goragpb.QueryResponse) string {
		var b strings.Builder
		for _, h := range resp.GetHits() {
			b.WriteString(h.GetContent())
		}
		return b.String()
	}

	cases := []transportCase{
		{
			name:        "REST",
			queryVaultA: func() string { return restContent(queryOverRESTVault(t, restSrv.URL, query, vaultA)) },
			queryVaultB: func() string { return restContent(queryOverRESTVault(t, restSrv.URL, query, vaultB)) },
		},
		{
			name:        "gRPC",
			queryVaultA: func() string { return grpcContent(queryOverGRPCVault(t, grpcClient, query, vaultA)) },
			queryVaultB: func() string { return grpcContent(queryOverGRPCVault(t, grpcClient, query, vaultB)) },
		},
		{
			name:        "MCP",
			queryVaultA: func() string { return queryOverMCPVault(t, mcpSrv.URL, query, vaultA) },
			queryVaultB: func() string { return queryOverMCPVault(t, mcpSrv.URL, query, vaultB) },
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			aContent := tc.queryVaultA()
			bContent := tc.queryVaultB()

			if !strings.Contains(aContent, "avaulttoken") {
				t.Errorf("%s: vaultA query did not return avaulttoken (content=%q)", tc.name, aContent)
			}
			if strings.Contains(aContent, "bvaulttoken") {
				t.Errorf("%s: vaultA query LEAKED bvaulttoken — vault selector routed to the wrong vault (content=%q)", tc.name, aContent)
			}

			if !strings.Contains(bContent, "bvaulttoken") {
				t.Errorf("%s: vaultB query did not return bvaulttoken (content=%q)", tc.name, bContent)
			}
			if strings.Contains(bContent, "avaulttoken") {
				t.Errorf("%s: vaultB query LEAKED avaulttoken — vault selector routed to the wrong vault (content=%q)", tc.name, bContent)
			}
		})
	}
}

// TestCrossTransport_VaultDefaultsToDefault (T021 companion): when NO vault is
// supplied, every transport falls back to the "default" vault. A doc ingested
// into "default" is retrievable over all three transports with an empty/absent
// vault selector, proving the default-resolution code path is consistent across
// transports (and compiles + routes).
func TestCrossTransport_VaultDefaultsToDefault(t *testing.T) {
	eng := openEngine(t, fastFakeOllama(t).URL)
	addDocTo(t, eng, "default", "default vault doc defaulttoken unique marker content")
	waitForVaultEmbeds(t, eng, "default")

	const query = "defaulttoken"

	restSrv := httptest.NewServer(rest.New(eng, "").Handler())
	defer restSrv.Close()
	grpcClient := dialGRPC(t, eng)
	mcpSrv := httptest.NewServer(mcp.NewWithEngine("", eng, config.Default()).HTTPHandler(""))
	defer mcpSrv.Close()

	// REST with no ?vault= → default.
	restHits := queryOverRESTVault(t, restSrv.URL, query, "")
	if len(restHits) == 0 {
		t.Errorf("REST default-vault query returned no hits (vault selector default-resolution broken)")
	}
	// gRPC with Vault="" → default (GetVault returns "" → adapter defaults).
	grpcResp := queryOverGRPCVault(t, grpcClient, query, "")
	if len(grpcResp.GetHits()) == 0 {
		t.Errorf("gRPC default-vault query returned no hits (vault selector default-resolution broken)")
	}
	// MCP with no vault arg → default.
	mcpText := queryOverMCPVault(t, mcpSrv.URL, query, "")
	if !strings.Contains(mcpText, "defaulttoken") {
		t.Errorf("MCP default-vault query did not return defaulttoken (text=%q)", mcpText)
	}
}
