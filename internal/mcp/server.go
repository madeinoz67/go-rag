// Package mcp exposes go-rag operations as Model Context Protocol tools over stdio
// JSON-RPC (PRD G7, Principle V — every CLI op is also an agent tool). All
// operations are thin renderings of the shared internal/engine facade, so MCP
// returns identical results to the CLI, REST, and gRPC transports.
package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/madeinoz67/go-rag/internal/auth"
	"github.com/madeinoz67/go-rag/internal/config"
	"github.com/madeinoz67/go-rag/internal/embed/modelbundle"
	"github.com/madeinoz67/go-rag/internal/engine"
	"github.com/madeinoz67/go-rag/internal/eval"
	"github.com/madeinoz67/go-rag/internal/storage"
)

const protocolVersion = "2024-11-05"

// Server is an MCP server backed by a go-rag database. It can run over stdio
// (New, opens the DB per call) or as a long-lived daemon (NewWithDB, shared DB).
type Server struct {
	dbPath string
	db     *storage.DB    // nil => open per call (stdio); non-nil => shared DB, fresh engine per call
	eng    *engine.Engine // shared engine (daemon mode via NewWithEngine): reused across calls so its query cache (H06) and seeded index (H01) persist
	cfg    config.Config
}

// New returns an MCP server that opens the database per call (stdio mode).
func New(dbPath string) *Server { return &Server{dbPath: dbPath} }

// NewWithDB returns an MCP server backed by a pre-opened database (daemon mode).
// The caller owns the database's lifetime; it is NOT closed per call.
func NewWithDB(dbPath string, db *storage.DB, cfg config.Config) *Server {
	return &Server{dbPath: dbPath, db: db, cfg: cfg}
}

// NewWithEngine returns an MCP server backed by a caller-owned shared engine
// (daemon mode). All DB tool calls reuse this one engine, so the query cache
// (audit H06/spec 016) and the seeded search index (audit H01/spec 011) persist
// across calls — repeated MCP queries hit the cache, and go_rag_status reports
// the real cache stats. The caller owns the engine's lifetime (closes it on
// shutdown); the server does not close it per call.
func NewWithEngine(dbPath string, eng *engine.Engine, cfg config.Config) *Server {
	return &Server{dbPath: dbPath, eng: eng, cfg: cfg}
}

type rpcReq struct {
	JSONRPC string         `json:"jsonrpc"`
	ID      any            `json:"id"`
	Method  string         `json:"method"`
	Params  map[string]any `json:"params"`
}

// Serve reads JSON-RPC messages from in and writes responses to out until in closes.
func (s *Server) Serve(in io.Reader, out io.Writer) error {
	dec := json.NewDecoder(in)
	enc := json.NewEncoder(out)
	for {
		var req rpcReq
		if err := dec.Decode(&req); err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		if resp := s.handle(req, auth.Principal{}); resp != nil {
			if err := enc.Encode(resp); err != nil {
				return err
			}
		}
	}
}

func (s *Server) handle(req rpcReq, p auth.Principal) any {
	switch req.Method {
	case "initialize":
		return ok(req.ID, map[string]any{
			"protocolVersion": protocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "go-rag", "version": "0.1.0"},
		})
	case "notifications/initialized":
		return nil
	case "tools/list":
		return ok(req.ID, map[string]any{"tools": toolDefs()})
	case "tools/call":
		return s.callTool(req, p)
	}
	return errResp(req.ID, -32601, "method not found: "+req.Method)
}

func (s *Server) callTool(req rpcReq, p auth.Principal) any {
	name, _ := req.Params["name"].(string)
	args, _ := req.Params["arguments"].(map[string]any)
	out, err := s.dispatch(name, args, p)
	if err != nil {
		if errors.Is(err, engine.ErrNotFound) { // spec 035: a missing chunk_id is a normal client outcome → -32001 (not -32603 Internal)
			return errResp(req.ID, -32001, err.Error())
		}
		return errResp(req.ID, -32603, err.Error())
	}
	return ok(req.ID, map[string]any{
		"content": []map[string]any{{"type": "text", "text": out}},
	})
}

// dispatch routes a tool call. go_rag_init is handled before opening the DB (it
// creates the DB); go_rag_vault_list and go_rag_guide are handled without a
// specific vault's DB. The rest require an existing database and are rendered
// from the shared engine facade. In daemon mode the shared DB is reused; in
// stdio mode it is opened (and closed) per call.
func (s *Server) dispatch(name string, args map[string]any, p auth.Principal) (string, error) {
	// Auth-management tools (spec 045 US2/R6) are admin-gated and never reach
	// the engine; route them first. They use the daemon's shared DB when present
	// and open the vault's DB per call in stdio mode.
	switch name {
	case "go_rag_auth_list", "go_rag_auth_create", "go_rag_auth_revoke",
		"go_rag_auth_session_list", "go_rag_auth_session_revoke":
		return s.dispatchAuth(name, args, p)
	case "go_rag_init":
		return s.initTool(args)
	case "go_rag_vault_list":
		return s.renderVaults()
	case "go_rag_guide":
		return s.guide()
	case "go_rag_eval":
		// Self-provisions a throwaway vault from the golden corpus; does not need
		// (and does not touch) the caller's database.
		return s.renderEval(nil, args)
	case "go_rag_model_install":
		// Global model fetch (no vault DB needed); ensures the bundled pure-Go
		// embedding model is present + verified (spec 032).
		return s.modelInstallTool(args)
	}
	if s.eng != nil {
		// Daemon mode with a shared engine: reuse it (no per-call close) so the
		// query cache (H06) and seeded index (H01) persist across tool calls.
		return s.dispatchDB(s.eng, name, args, false)
	}
	if s.db != nil {
		return s.dispatchDB(engine.NewWithDB(s.cfg, s.db), name, args, true)
	}
	cfg, db, err := engine.Open(s.dbPath)
	if err != nil {
		return "", err
	}
	defer db.Close()
	return s.dispatchDB(engine.NewWithDB(cfg, db), name, args, true)
}

// dispatchAuth serves the spec 045 auth-management MCP tools. All are admin-
// gated: a caller over HTTP needs an admin credential (read/write keys get
// -32603); stdio (p.Source == "", the operator's local terminal) is trusted.
// go_rag_auth_create returns the new key's id only — the secret is printed once
// by the CLI and is never surfaced over MCP.
func (s *Server) dispatchAuth(name string, args map[string]any, p auth.Principal) (string, error) {
	// stdio (p.Source == "") is the operator's local terminal — trusted. A loopback
	// bypass principal (SourceBypass) is loopback-with-no-credential and MUST NOT
	// reach admin management — that would let any loopback peer (proxy, browser,
	// malware) mint/revoke keys on a bare vault. Require a real credential here.
	if p.Source == auth.SourceBypass {
		return "", fmt.Errorf("admin scope requires a real credential, not the loopback bypass")
	}
	if p.Source != "" && p.Mode != auth.ModeAdmin {
		return "", fmt.Errorf("admin scope required for %s", name)
	}
	store, closeStore, err := s.authStoreOrOpen()
	if err != nil {
		return "", err
	}
	if closeStore != nil {
		defer closeStore()
	}
	switch name {
	case "go_rag_auth_list":
		return s.renderAuthList(store)
	case "go_rag_auth_create":
		return s.renderAuthCreate(store, args)
	case "go_rag_auth_revoke":
		return s.renderAuthRevoke(store, args)
	case "go_rag_auth_session_list":
		return s.renderAuthSessionList(store)
	case "go_rag_auth_session_revoke":
		return s.renderAuthSessionRevoke(store, args)
	}
	return "", fmt.Errorf("unknown auth tool: %s", name)
}

// authStoreOrOpen returns the credential store from the shared engine/DB when
// present (daemon mode), or opens the vault's DB per call (stdio). The returned
// closer is non-nil in the stdio/per-call case so the caller can release it.
func (s *Server) authStoreOrOpen() (*auth.Store, func(), error) {
	if store := s.authStore(); store != nil {
		return store, nil, nil
	}
	if s.dbPath == "" {
		return nil, nil, fmt.Errorf("auth store unavailable")
	}
	_, db, err := engine.Open(s.dbPath)
	if err != nil {
		return nil, nil, err
	}
	return auth.NewStore(db), func() { _ = db.Close() }, nil
}

func (s *Server) dispatchDB(eng *engine.Engine, name string, args map[string]any, closeAfter bool) (string, error) {
	// The engine's ingest pipeline is created lazily on write and drained
	// async-after-ACK; close it here so short-lived per-dispatch engines finish
	// their background embeddings before the MCP response returns (and don't
	// leak worker goroutines). No-op for read-only engines. Skipped for the
	// shared daemon engine (closeAfter=false) — the caller owns its lifetime.
	if closeAfter {
		defer eng.Close()
	}
	switch name {
	case "go_rag_query":
		return s.renderQuery(eng, args)
	case "go_rag_status":
		return s.renderStatus(eng, args)
	case "go_rag_add":
		return s.renderAdd(eng, args)
	case "go_rag_scan":
		return s.renderScan(eng, args)
	case "go_rag_config":
		return s.renderConfig(eng, args)
	case "go_rag_files":
		return s.renderFiles(eng, args)
	case "go_rag_dirs":
		return s.renderDirs(eng, args)
	case "go_rag_reprocess":
		return s.renderReprocess(eng, args)
	case "go_rag_migrate":
		return s.renderMigrate(eng, args)
	case "go_rag_migrate_plan":
		return s.renderMigratePlan(eng, args) // H24/spec 028
	case "go_rag_poison_list":
		return s.renderPoisonList(eng, args)
	case "go_rag_poison_release":
		return s.renderPoisonRelease(eng, args)
	case "go_rag_poison_reset":
		return s.renderPoisonReset(eng, args)
	case "go_rag_poison_rescan":
		return s.renderPoisonRescan(eng, args)
	case "go_rag_get_chunk":
		return s.renderGetChunk(eng, args) // spec 035
	case "go_rag_get_chunk_context":
		return s.renderGetChunkContext(eng, args) // spec 037
	case "go_rag_batch_get_chunks":
		return s.renderBatchGetChunks(eng, args) // spec 038
	case "go_rag_list_documents":
		return s.renderListDocuments(eng, args) // spec 039
	case "go_rag_list_chunks":
		return s.renderListChunks(eng, args) // spec 047 / T008
	case "go_rag_delete_document":
		return s.renderDeleteDocument(eng, args) // spec 050 / T008
	}
	return "", fmt.Errorf("unknown tool: %s", name)
}

// vaultArg extracts the optional vault selector from a tool's args, defaulting
// to "default" when absent or empty (the single-vault common case). Every
// vault-scoped MCP tool accepts `vault` so an agent can target a non-default
// vault without reconfiguring the daemon.
func vaultArg(args map[string]any) string {
	if v, ok := args["vault"].(string); ok && v != "" {
		return v
	}
	return "default"
}

// renderEval measures retrieval quality over a golden dataset. It is read-only
// with respect to any real vault: it self-provisions a throwaway vault from the
// (default committed) golden corpus with a deterministic offline embedder, so the
// result is reproducible and needs no Ollama. Numbers are identical to the
// `go-rag eval` CLI because both drive the same engine.Query path (Principle V).
func (s *Server) renderEval(_ *engine.Engine, args map[string]any) (string, error) {
	goldenPath, _ := args["golden"].(string)
	if goldenPath == "" {
		goldenPath = "testdata/golden/v1.jsonl"
	}
	corpus, _ := args["corpus"].(string)
	if corpus == "" {
		corpus = "testdata/golden/corpus/"
	}
	mode, _ := args["mode"].(string)
	if mode == "" {
		mode = "hybrid"
	}
	k := 10
	if v, ok := args["k"].(float64); ok && v > 0 {
		k = int(v)
	}

	golden, err := eval.LoadGolden(goldenPath)
	if err != nil {
		return "", err
	}
	em := eval.NewDeterministicEmbedder()
	cfg, db, cleanup, err := eval.ProvisionCorpus(context.Background(), corpus, em, "")
	if err != nil {
		return "", err
	}
	defer cleanup()
	run, err := eval.NewRunner(cfg, db, em).Run(context.Background(), golden, mode, k, true)
	if err != nil {
		return "", err
	}
	return eval.FormatRun(run, nil, 0), nil
}

func (s *Server) renderQuery(eng *engine.Engine, args map[string]any) (string, error) {
	vault := vaultArg(args)
	req := engine.QueryRequest{Mode: "hybrid"} // H22/spec 024: K omitted → engine resolves (default 5, or classifier-recommended when adaptive depth is on)
	req.Query, _ = args["query"].(string)
	if v, ok := args["k"].(float64); ok {
		req.K = int(v)
	}
	if v, ok := args["mode"].(string); ok {
		req.Mode = v
	}
	if v, ok := args["no_rerank"].(bool); ok {
		req.NoRerank = v
	}
	if v, ok := args["threshold"].(float64); ok {
		req.Threshold = v
	}
	if v, ok := args["rrf_k"].(float64); ok && v > 0 { // H08/spec 009: per-query RRF override (>0); 0 = config/default
		req.RRFK = int(v)
	}
	if v, ok := args["pool_size"].(float64); ok && v > 0 { // H22/spec 024: per-query candidate-pool override (>0); 0 = config/default
		req.PoolSize = int(v)
	}
	// H14/spec 014: metadata filter (source/type/tags).
	var src, ftype string
	var ftags []string
	if v, ok := args["source"].(string); ok {
		src = v
	}
	if v, ok := args["type"].(string); ok {
		ftype = v
	}
	if v, ok := args["tags"].([]any); ok {
		for _, t := range v {
			if s, ok := t.(string); ok {
				ftags = append(ftags, s)
			}
		}
	}
	req.Filter = engine.NewFilter(src, ftype, ftags)
	if v, ok := args["context_window"].(float64); ok && v > 0 {
		req.ContextWindow = int(v)
	}
	if v, ok := args["no_cache"].(bool); ok { // H06/spec 016: bypass the result cache for this query
		req.NoCache = v
	}
	if v, ok := args["include_quarantined"].(bool); ok { // H04/spec 019: return poisoning-flagged chunks
		req.IncludeQuarantined = v
	}
	res, err := eng.Query(context.Background(), vault, req)
	if err != nil {
		return "", err
	}
	if len(res.Hits) == 0 {
		return "no results", nil
	}
	var b strings.Builder
	if res.RerankFailed { // H09: reranking was attempted but failed — results are fallback-ordered.
		b.WriteString("⚠ reranking failed; showing fallback-ordered results (reranker may be down or mismatched)\n\n")
	}
	for _, h := range res.Hits {
		mark := ""
		if h.Poisoning != nil && h.Poisoning.Level.Quarantined() { // H04/spec 019: flagged → untrusted
			mark = " ⚠ untrusted"
		}
		section := ""
		if len(h.SectionContext) > 0 { // H23/spec 025: heading breadcrumb (FR-004)
			section = "[" + strings.Join(h.SectionContext, " / ") + "] "
		}
		nearDup := ""
		if h.NearDup != nil && len(h.NearDup.Siblings) > 0 { // H20/spec 026: near-dup count
			nearDup = fmt.Sprintf(" ≈%d near-dup", len(h.NearDup.Siblings))
		}
		fmt.Fprintf(&b, "- (score %.3f) %s%s%s%s\n", h.Score, section, h.Preview, nearDup, mark)
		if len(h.Wikilinks) > 0 { // spec 036 / BL-004: chunk wikilink targets (FR-009)
			fmt.Fprintf(&b, "    wikilinks: %s\n", strings.Join(h.Wikilinks, ", "))
		}
		if h.Summary != "" { // spec 029: document summary
			fmt.Fprintf(&b, "    summary: %s\n", h.Summary)
		}
	}
	fmt.Fprintf(&b, "\n(effective: k=%d, pool=%d, mode=%s)\n", res.EffectiveK, res.EffectivePool, res.EffectiveMode) // H22/spec 024
	return strings.TrimSpace(b.String()), nil
}

func (s *Server) renderStatus(eng *engine.Engine, args map[string]any) (string, error) {
	vault := vaultArg(args)
	st, err := eng.Status(vault)
	if err != nil {
		return "", err
	}
	out := fmt.Sprintf("documents: %d, chunks: %d, embeddings: %d, dimensions: %d, model: %s, reranker: %s",
		st.Documents, st.Chunks, st.Embeddings, st.Dimensions, st.EmbeddingModel, st.Reranker)
	if st.EmbeddingDrift {
		out += fmt.Sprintf(", drift: mixed models/dims (%v)", st.ModelCounts)
	}
	// H06/spec 016: query-cache stats (result + embedding) so an operator or
	// agent can see hit rates and bounded footprint via `go-rag status`.
	if st.ResultCache.Enabled || st.EmbeddingCache.Enabled {
		out += fmt.Sprintf(", cache: result %s, embedding %s", cacheSummary(st.ResultCache), cacheSummary(st.EmbeddingCache))
	}
	// H22/spec 024: adaptive-retrieval knobs + aggregate pool utilization.
	out += fmt.Sprintf(", pool: size=%d, adaptive_depth=%t, utilization: queries=%d avg_fetched=%.1f avg_kept=%.1f saturated=%d",
		st.PoolSize, st.AdaptiveDepthEnabled, st.PoolUtilization.Queries, st.PoolUtilization.AvgFetched, st.PoolUtilization.AvgKept, st.PoolUtilization.Saturated)
	// H11/spec 017: corpus baseline (the profile the corpus was built under) +
	// the drift verdict, so drift is visible without a query.
	if st.CorpusBaselineModel != "" {
		out += fmt.Sprintf(", baseline: model=%s dim=%d conv=%q ollama=%s",
			st.CorpusBaselineModel, st.CorpusBaselineDim, st.CorpusBaselineConvention, orUnknown(st.CorpusBaselineOllamaVer))
		if st.LiveOllamaVersion != "" {
			out += fmt.Sprintf("/live=%s", orUnknown(st.LiveOllamaVersion))
		}
	}
	if st.DriftVerdict != "" && st.DriftVerdict != "clean" && st.DriftVerdict != "n/a" {
		out += fmt.Sprintf(", drift: %s", st.DriftVerdict)
	}
	// H04/spec 019: poisoning detection summary (enabled, flagged count, sources,
	// merged-list size, thresholds).
	out += fmt.Sprintf(", poison: enabled=%v flagged=%d sources=%d phrases=%d (thr %.2f/%.2f)",
		st.PoisoningEnabled, st.PoisonFlagged, st.PoisonSources, st.PoisonPhrases,
		st.PoisonThresholdSus, st.PoisonThresholdQua)
	// spec 029: document enrichment state.
	out += fmt.Sprintf(", enrich: enabled=%v enriched=%d", st.EnrichmentEnabled, st.EnrichedDocs)
	// H17/spec 020: observability state — metrics endpoint + trace exporter mode
	// (so an operator/agent sees whether telemetry is on and where to scrape).
	c := eng.Config()
	out += fmt.Sprintf(", obs: metrics=%v traces=%s (scrape /metrics)", c.EffectiveMetricsEnabled(), c.EffectiveOTelExport())
	return out, nil
}

// orUnknown renders an empty version string as "unknown" for display.
func orUnknown(s string) string {
	if s == "" {
		return "unknown"
	}
	return s
}

// cacheSummary formats one CacheStats as "size/cap (hits hits, misses misses)".
func cacheSummary(c engine.CacheStats) string {
	if !c.Enabled {
		return "off"
	}
	return fmt.Sprintf("%d/%d (%d hits, %d misses)", c.Size, c.Capacity, c.Hits, c.Misses)
}

func (s *Server) renderAdd(eng *engine.Engine, args map[string]any) (string, error) {
	vault := vaultArg(args)
	path, _ := args["path"].(string)
	res, err := eng.Add(context.Background(), vault, path, "")
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("new=%d skipped=%d errors=%d", res.New, res.Skipped, res.Errors), nil
}

func (s *Server) renderScan(eng *engine.Engine, args map[string]any) (string, error) {
	vault := vaultArg(args)
	res, err := eng.Scan(context.Background(), vault)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("added=%d modified=%d deleted=%d", res.New, res.Modified, res.Deleted), nil
}

func (s *Server) renderReprocess(eng *engine.Engine, args map[string]any) (string, error) {
	vault := vaultArg(args)
	path, _ := args["path"].(string)
	res, err := eng.Reprocess(context.Background(), vault, path)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("reprocessed=%d errors=%d", res.New, res.Errors), nil
}

// renderDeleteDocument is the MCP text projection of engine.DeleteDoc (spec 050
// / T008): remove a document + its chunks/embeddings from the index by
// content-addressed doc_id. Index-only — the source file on disk is untouched.
// Mirrors renderReprocess's arg + error surface; returns a one-line confirmation
// (the structured cross-transport contract is the empty gRPC response / REST 204).
func (s *Server) renderDeleteDocument(eng *engine.Engine, args map[string]any) (string, error) {
	vault := vaultArg(args)
	docID, _ := args["doc_id"].(string)
	if err := eng.DeleteDoc(context.Background(), vault, docID); err != nil {
		return "", err
	}
	return fmt.Sprintf("deleted document %s", docID), nil
}

func (s *Server) renderMigrate(eng *engine.Engine, args map[string]any) (string, error) {
	vault := vaultArg(args)
	res, err := eng.Migrate(context.Background(), vault)
	if err != nil {
		return "", err
	}
	if res.New == 0 && res.Errors == 0 {
		return fmt.Sprintf("up to date: all embeddings use %s", eng.Config().EmbeddingModel), nil
	}
	return fmt.Sprintf("migrated=%d files re-embedded to %s (%d errors)", res.New, eng.Config().EmbeddingModel, res.Errors), nil
}

// renderMigratePlan is the read-only migration preview (H24/spec 028): shows what
// a migrate would do and cost without re-embedding (and without a backend).
func (s *Server) renderMigratePlan(eng *engine.Engine, args map[string]any) (string, error) {
	vault := vaultArg(args)
	plan, err := eng.MigratePlan(vault)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "target model: %s, total embeddings: %d (stale: %d)", plan.TargetModel, plan.Total, plan.StaleTotal)
	for _, src := range plan.Sources {
		if src.Stale {
			fmt.Fprintf(&b, ", %d on %s (stale)", src.Count, src.Model)
		}
	}
	if len(plan.Dimensions) > 0 {
		parts := make([]string, 0, len(plan.Dimensions))
		for _, d := range plan.Dimensions {
			parts = append(parts, fmt.Sprintf("%dd×%d", d.Dim, d.Count))
		}
		cons := "consistent"
		if !plan.Consistent {
			cons = "MIXED"
		}
		fmt.Fprintf(&b, ", dims: %s (%s)", strings.Join(parts, ", "), cons)
	}
	if plan.StaleTotal > 0 {
		fmt.Fprintf(&b, ", estimate: ~%d to regenerate (%s)", plan.Estimate.StaleEmbeddings, plan.Estimate.Note)
	} else {
		b.WriteString(", nothing to migrate")
	}
	return b.String(), nil
}

// renderPoisonList lists chunks flagged as injection-poisoning (H04/spec 019).
func (s *Server) renderPoisonList(eng *engine.Engine, args map[string]any) (string, error) {
	vault := vaultArg(args)
	flagged, err := eng.ListPoisoned(vault)
	if err != nil {
		return "", err
	}
	if len(flagged) == 0 {
		return "no flagged chunks", nil
	}
	var b strings.Builder
	for _, f := range flagged {
		fmt.Fprintf(&b, "- %s (level %s, score %.2f) %s\n", f.ChunkID, f.Verdict.Level, f.Verdict.Score, f.Preview)
	}
	return strings.TrimSpace(b.String()), nil
}

func (s *Server) renderPoisonRelease(eng *engine.Engine, args map[string]any) (string, error) {
	vault := vaultArg(args)
	id, _ := args["chunk_id"].(string)
	if id == "" {
		return "", fmt.Errorf("chunk_id required")
	}
	if err := eng.ReleaseChunk(vault, id); err != nil {
		return "", err
	}
	return fmt.Sprintf("released %s — now retrievable by default", id), nil
}

// renderGetChunk (spec 035 / BL-001) fetches a chunk by content-addressed ID and
// renders it (with its parent document's file path/type) for an MCP agent. The
// authoritative structured shape is the gRPC/REST {chunk, document} response; MCP
// is the human/agent text surface. Returns engine.GetChunk's error verbatim —
// callTool maps ErrNotFound to JSON-RPC -32001.
func (s *Server) renderGetChunk(eng *engine.Engine, args map[string]any) (string, error) {
	vault := vaultArg(args)
	id, _ := args["chunk_id"].(string)
	if id == "" {
		return "", fmt.Errorf("chunk_id required")
	}
	res, err := eng.GetChunk(vault, id)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "chunk_id: %s\n", res.Chunk.ID)
	fmt.Fprintf(&b, "document_id: %s\n", res.Chunk.DocumentID)
	if res.Chunk.PageNumber > 0 {
		fmt.Fprintf(&b, "page: %d\n", res.Chunk.PageNumber)
	}
	if res.Chunk.Kind != "" {
		fmt.Fprintf(&b, "kind: %s\n", res.Chunk.Kind)
	}
	if len(res.Chunk.SectionContext) > 0 {
		fmt.Fprintf(&b, "section: %s\n", strings.Join(res.Chunk.SectionContext, " / "))
	}
	if len(res.Chunk.Wikilinks) > 0 { // spec 036 / BL-004
		fmt.Fprintf(&b, "wikilinks: %s\n", strings.Join(res.Chunk.Wikilinks, ", "))
	}
	if res.Document.ID != "" {
		fmt.Fprintf(&b, "document: %s (%s, status %s)\n", res.Document.FilePath, res.Document.FileType, res.Document.Status)
		if res.Document.Enrichment != nil && res.Document.Enrichment.Summary != "" {
			fmt.Fprintf(&b, "summary: %s\n", res.Document.Enrichment.Summary)
		}
	}
	b.WriteString("--- content ---\n")
	b.WriteString(res.Chunk.Content)
	return b.String(), nil
}

// renderGetChunkContext is the MCP text projection of engine.GetChunkContext
// (spec 037 / BL-002): the ordered window as a numbered list with the target
// marked (>>>), its target_index, and the parent document line. window defaults
// to 2 when absent; explicit 0..10 (0 = exactly the target). Mirrors GetChunk's
// render + error surface (NotFound → -32001 via callTool).
func (s *Server) renderGetChunkContext(eng *engine.Engine, args map[string]any) (string, error) {
	vault := vaultArg(args)
	id, _ := args["chunk_id"].(string)
	if strings.TrimSpace(id) == "" {
		return "", fmt.Errorf("chunk_id required")
	}
	window := engine.DefaultChunkContextWindow()
	if v, ok := args["window"]; ok && v != nil {
		switch n := v.(type) {
		case float64:
			window = int(n)
		case int:
			window = n
		default:
			return "", fmt.Errorf("window must be an integer 0..%d", engine.MaxChunkContextWindow())
		}
		if window < 0 || window > engine.MaxChunkContextWindow() {
			return "", fmt.Errorf("window must be 0..%d, got %d", engine.MaxChunkContextWindow(), window)
		}
	}
	res, err := eng.GetChunkContext(vault, id, window)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for i, c := range res.Chunks {
		marker := "   "
		if i == res.TargetIndex {
			marker = ">>>"
		}
		fmt.Fprintf(&b, "%s [%d] %s\n", marker, i, c.ID)
	}
	fmt.Fprintf(&b, "target_index: %d\n", res.TargetIndex)
	if res.Document.ID != "" {
		fmt.Fprintf(&b, "document: %s (%s, status %s)\n", res.Document.FilePath, res.Document.FileType, res.Document.Status)
	}
	return b.String(), nil
}

// renderBatchGetChunks is the MCP text projection of engine.BatchGetChunks (spec
// 038 / BL-003): one line per requested chunk_id — "ok (<file>)" for live ids,
// "<id>: not found" for missing ids. chunk_ids arrives as a JSON array ([]any of
// string). Per-id tolerance: the call never fails for a missing id. The engine's
// call-level error (ErrInvalid) returns verbatim → -32603 via callTool.
func (s *Server) renderBatchGetChunks(eng *engine.Engine, args map[string]any) (string, error) {
	vault := vaultArg(args)
	raw, _ := args["chunk_ids"].([]any)
	if len(raw) == 0 {
		return "", fmt.Errorf("chunk_ids is required (max %d)", engine.MaxBatchGetChunks())
	}
	if len(raw) > engine.MaxBatchGetChunks() {
		return "", fmt.Errorf("max %d chunk_ids, got %d", engine.MaxBatchGetChunks(), len(raw))
	}
	ids := make([]string, 0, len(raw))
	for i, v := range raw {
		id, ok := v.(string)
		if !ok || strings.TrimSpace(id) == "" {
			return "", fmt.Errorf("chunk_ids[%d] must be a non-empty string", i)
		}
		ids = append(ids, id)
	}
	res, err := eng.BatchGetChunks(vault, ids)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, it := range res.Results {
		if it.Err != "" {
			fmt.Fprintf(&b, "%s: %s\n", it.ChunkID, it.Err)
			continue
		}
		fmt.Fprintf(&b, "%s: ok", it.ChunkID)
		if it.Document.ID != "" {
			fmt.Fprintf(&b, " (%s)", it.Document.FilePath)
		}
		b.WriteByte('\n')
	}
	return b.String(), nil
}

// renderListDocuments is the MCP text projection of engine.ListDocuments (spec
// 039 / BL-007): one line per document (ingested_at, status, file path) in
// ingested_at ascending order, then a next_page_token line when more remain.
// Args (all optional): page_size, page_token, after, status.
func (s *Server) renderListDocuments(eng *engine.Engine, args map[string]any) (string, error) {
	vault := vaultArg(args)
	req := engine.ListDocumentsRequest{}
	if v, ok := args["page_size"].(float64); ok && v > 0 {
		req.PageSize = int(v)
	}
	if v, ok := args["page_token"].(string); ok {
		req.PageToken = v
	}
	if v, ok := args["after"].(string); ok {
		req.After = v
	}
	if v, ok := args["status"].(string); ok {
		req.Status = v
	}
	// spec 047 R3: match-any tag filter (mirrors renderQuery's tags parsing).
	if raw, ok := args["tags"].([]any); ok {
		for _, t := range raw {
			if tag, ok := t.(string); ok {
				req.Tags = append(req.Tags, tag)
			}
		}
	}
	res, err := eng.ListDocuments(vault, req)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, d := range res.Documents {
		fmt.Fprintf(&b, "%s\t%s\t%s\n", d.IngestedAt.UTC().Format("2006-01-02T15:04:05Z07:00"), d.Status, d.FilePath)
	}
	if res.NextPageToken != "" {
		fmt.Fprintf(&b, "next_page_token: %s\n", res.NextPageToken)
	}
	return b.String(), nil
}

// renderListChunks is the MCP text projection of engine.ListChunks (spec 047 /
// T008): one line per chunk in (chunk_index ASC, chunk_id ASC) order, then a
// next_page_token line when more remain. Unknown document → empty result (not
// an error), matching the engine's contract. Mirrors renderListDocuments's TSV
// shape; reuses the chunk fields surfaced by renderGetChunk (kind/page/section).
func (s *Server) renderListChunks(eng *engine.Engine, args map[string]any) (string, error) {
	vault := vaultArg(args)
	documentID, _ := args["document_id"].(string)
	if strings.TrimSpace(documentID) == "" {
		return "", fmt.Errorf("document_id required")
	}
	req := engine.ListChunksRequest{}
	if v, ok := args["page_size"].(float64); ok && v > 0 {
		req.PageSize = int(v)
	}
	if v, ok := args["page_token"].(string); ok {
		req.PageToken = v
	}
	res, err := eng.ListChunks(vault, documentID, req)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, c := range res.Chunks {
		fmt.Fprintf(&b, "%s\t#%d", c.ID, c.ChunkIndex)
		if c.Kind != "" {
			fmt.Fprintf(&b, "\tkind=%s", c.Kind)
		}
		if c.PageNumber > 0 {
			fmt.Fprintf(&b, "\tpage=%d", c.PageNumber)
		}
		if c.TokenCount > 0 {
			fmt.Fprintf(&b, "\ttokens=%d", c.TokenCount)
		}
		if len(c.SectionContext) > 0 {
			fmt.Fprintf(&b, "\tsection=%s", strings.Join(c.SectionContext, " / "))
		}
		b.WriteByte('\n')
	}
	if res.NextPageToken != "" {
		fmt.Fprintf(&b, "next_page_token: %s\n", res.NextPageToken)
	}
	return b.String(), nil
}

func (s *Server) renderPoisonReset(eng *engine.Engine, args map[string]any) (string, error) {
	vault := vaultArg(args)
	id, _ := args["chunk_id"].(string)
	if id == "" {
		return "", fmt.Errorf("chunk_id required")
	}
	if err := eng.ResetChunk(vault, id); err != nil {
		return "", err
	}
	return fmt.Sprintf("reset %s — re-evaluated against thresholds", id), nil
}

// renderPoisonRescan re-scores the whole corpus against the current detector
// (US3, FR-007, and the US4 T031 manual trigger).
func (s *Server) renderPoisonRescan(eng *engine.Engine, args map[string]any) (string, error) {
	vault := vaultArg(args)
	rescored, flagged, err := eng.RescanPoisoning(vault)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("rescan: %d chunks (re)scored, %d flagged", rescored, flagged), nil
}

func (s *Server) renderConfig(eng *engine.Engine, args map[string]any) (string, error) {
	vault := vaultArg(args)
	action, _ := args["action"].(string)
	if action == "set" {
		key, _ := args["key"].(string)
		val, _ := args["value"].(string)
		if err := eng.SetConfig(vault, key, val); err != nil {
			return "", err
		}
		return fmt.Sprintf("%s=%s (saved)", key, val), nil
	}
	if key, ok := args["key"].(string); ok && key != "" {
		vals, err := eng.GetConfig(vault, key)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("%s=%s", key, vals[key]), nil
	}
	vals, err := eng.GetConfig(vault, "")
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, k := range []string{"ollama_url", "embedding_model", "chunk_size", "chunk_overlap", "db_path", "poll_interval_secs"} {
		if v, ok := vals[k]; ok {
			fmt.Fprintf(&b, "%s=%s\n", k, v)
		}
	}
	return strings.TrimSpace(b.String()), nil
}

func (s *Server) renderFiles(eng *engine.Engine, args map[string]any) (string, error) {
	vault := vaultArg(args)
	files, err := eng.Files(vault)
	if err != nil {
		return "", err
	}
	if len(files) == 0 {
		return "no files ingested", nil
	}
	lines := make([]string, 0, len(files))
	for _, f := range files {
		lines = append(lines, fmt.Sprintf("%s (%s, %s, %d chunks)", f.FilePath, f.FileType, f.Status, f.ChunkCount))
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n"), nil
}

func (s *Server) renderDirs(eng *engine.Engine, args map[string]any) (string, error) {
	vault := vaultArg(args)
	dirs, err := eng.Dirs(vault)
	if err != nil {
		return "", err
	}
	if len(dirs) == 0 {
		return "no files ingested", nil
	}
	var b strings.Builder
	for _, d := range dirs {
		fmt.Fprintf(&b, "%s (%d files, %d chunks)\n", d.Dir, d.Files, d.Chunks)
	}
	return strings.TrimSpace(b.String()), nil
}

// renderVaults lists all vaults with doc counts. No specific vault's DB required.
func (s *Server) renderVaults() (string, error) {
	vaults, err := engine.NewWithDB(config.Config{}, nil).ListVaults("default")
	if err != nil {
		return "", err
	}
	if len(vaults) == 0 {
		return "no vaults", nil
	}
	var b strings.Builder
	for _, v := range vaults {
		fmt.Fprintf(&b, "%s (%d docs)\n", v.Name, v.Documents)
	}
	return strings.TrimSpace(b.String()), nil
}

// guide returns a context document for the AI agent — what's connected, what's
// available, what's needed. The agent should call this first.
func (s *Server) guide() (string, error) {
	cfg, db, err := engine.Open(s.dbPath)
	dbReady := err == nil

	var b strings.Builder
	b.WriteString("# go-rag Agent Guide\n\n## Status\n\n")
	if !dbReady {
		b.WriteString("**Database not initialized.** Call `go_rag_init` first with an embedding model name, then `go_rag_add` to ingest documents.\n\n## Available Tools\n\n")
		b.WriteString("- **go_rag_init** — Initialize a new database (requires: model name, e.g. `mxbai-embed-large`)\n")
		b.WriteString("- **go_rag_vault_list** — List all available vaults\n")
		b.WriteString("- **go_rag_guide** — This guide (call it after setup changes)\n")
		return b.String(), nil
	}
	defer db.Close()

	eng := engine.NewWithDB(cfg, db)
	st, _ := eng.Status("default")
	pct := 0
	if st.Documents > 0 {
		pct = st.Embeddings * 100 / st.Documents
	}
	reranker := st.Reranker

	fmt.Fprintf(&b, "- Documents: %d\n", st.Documents)
	fmt.Fprintf(&b, "- Chunks: %d\n", st.Chunks)
	fmt.Fprintf(&b, "- Embeddings: %d (%d%% complete)\n", st.Embeddings, pct)
	fmt.Fprintf(&b, "- Embedding model: %s\n", st.EmbeddingModel)
	fmt.Fprintf(&b, "- Reranker: %s\n", reranker)
	fmt.Fprintf(&b, "- Chunk size: %d tokens, overlap: %d\n", cfg.ChunkSize, cfg.ChunkOverlap)
	fmt.Fprintf(&b, "- Ollama: %s\n\n", st.OllamaURL)

	b.WriteString("## What's Needed\n\n")
	if st.Documents == 0 {
		b.WriteString("**No documents ingested.** Call `go_rag_add` with a directory path to index documents.\n\n")
	}
	if pct < 100 && st.Documents > 0 {
		fmt.Fprintf(&b, "**Embeddings incomplete (%d%%).** Background embedding may still be running, or errors occurred. Query results will be partial.\n\n", pct)
	}
	if reranker == "disabled" {
		b.WriteString("**Reranker disabled.** Set `rerank_model` via `go_rag_config` to enable cross-encoder reranking for better query precision.\n\n")
	}
	if st.Documents > 0 && pct == 100 && reranker != "disabled" {
		b.WriteString("System is fully operational — all documents indexed and embeddings complete.\n\n")
	}

	b.WriteString("## Available Tools\n\n")
	b.WriteString("- **go_rag_query** — Search the database (hybrid semantic + keyword). Params: `query` (required), `k` (results, default 5), `mode` (hybrid|semantic|keyword), `no_rerank` (skip reranker), `threshold` (min score), `rrf_k` (RRF constant override, default 60), `pool_size` (candidate-pool override, default 60).\n")
	b.WriteString("- **go_rag_add** — Ingest documents from a file or directory path.\n")
	b.WriteString("- **go_rag_status** — Database health and counts.\n")
	b.WriteString("- **go_rag_files** — List ingested file paths.\n")
	b.WriteString("- **go_rag_dirs** — Per-directory document counts.\n")
	b.WriteString("- **go_rag_scan** — Detect and apply filesystem changes (added/modified/deleted).\n")
	b.WriteString("- **go_rag_reprocess** — Force re-ingest a directory (after reader/config changes).\n")
	b.WriteString("- **go_rag_delete_document** — Delete a document by ID (index-only; source file preserved).\n")
	b.WriteString("- **go_rag_migrate** — Re-embed all documents to the current model.\n")
	b.WriteString("- **go_rag_migrate_plan** — Preview a migration (what would change + cost) without re-embedding.\n")
	b.WriteString("- **go_rag_config** — Get or set configuration.\n")
	b.WriteString("- **go_rag_init** — Initialize a new database.\n")
	b.WriteString("- **go_rag_vault_list** — List all vaults.\n")
	b.WriteString("- **go_rag_guide** — This guide.\n")
	b.WriteString("- **go_rag_eval** — Measure retrieval quality (recall@k, precision@k, MRR, NDCG@k) over a golden dataset (offline, reproducible).\n\n")

	b.WriteString("## Usage Patterns\n\n")
	b.WriteString("1. **Query**: `go_rag_query(query=\"how does authentication work?\")` — returns ranked chunks with source files.\n")
	b.WriteString("2. **Add documents**: `go_rag_add(path=\"/path/to/docs/\")` — ingests recursively.\n")
	b.WriteString("3. **After adding**: Wait for embeddings to complete (check `go_rag_status` for embedded %).\n")
	b.WriteString("4. **Quick search** (no reranker): `go_rag_query(query=\"...\", no_rerank=true)` — faster, less precise.\n")
	return b.String(), nil
}

func (s *Server) initTool(args map[string]any) (string, error) {
	cfg := config.Default()
	cfg.DBPath = s.dbPath
	if v, ok := args["ollama_url"].(string); ok && v != "" {
		cfg.OllamaURL = v
	}
	if v, ok := args["model"].(string); ok && v != "" {
		cfg.EmbeddingModel = v
	}
	if v, ok := args["watch_dir"].(string); ok && v != "" {
		cfg.WatchDirs = []string{v}
	}
	if v, ok := args["chunk_size"].(float64); ok && v > 0 {
		cfg.ChunkSize = int(v)
	}
	if v, ok := args["chunk_overlap"].(float64); ok && v >= 0 {
		cfg.ChunkOverlap = int(v)
	}
	if cfg.EmbeddingModel == "" {
		cfg.EmbeddingModel = "nomic-embed-text"
	}
	if err := cfg.Validate(); err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Join(cfg.DBPath, "data"), 0o755); err != nil {
		return "", err
	}
	if err := config.Save(filepath.Join(cfg.DBPath, "config.json"), cfg); err != nil {
		return "", err
	}
	return fmt.Sprintf("initialized go-rag at %s (model %s, url %s)", cfg.DBPath, cfg.EmbeddingModel, cfg.OllamaURL), nil
}

func (s *Server) modelInstallTool(args map[string]any) (string, error) {
	if force, ok := args["force"].(bool); ok && force {
		if dir, err := modelbundle.ModelDir(); err == nil {
			_ = os.RemoveAll(dir)
		}
	}
	dir, err := modelbundle.EnsureModel(context.Background())
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("bundled model %s ready at %s", modelbundle.ModelID, dir), nil
}

// --- JSON-RPC helpers ---

func ok(id any, result any) any {
	return map[string]any{"jsonrpc": "2.0", "id": id, "result": result}
}

func errResp(id any, code int, msg string) any {
	return map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": code, "message": msg}}
}

func toolDefs() []map[string]any {
	return []map[string]any{
		{
			"name":        "go_rag_query",
			"description": "Hybrid (semantic + keyword) search over the local document database.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"vault":               map[string]any{"type": "string", "default": "default", "description": "vault name (defaults to \"default\")"},
					"query":               map[string]any{"type": "string"},
					"k":                   map[string]any{"type": "integer", "default": 5},
					"mode":                map[string]any{"type": "string", "enum": []string{"hybrid", "semantic", "keyword"}},
					"no_rerank":           map[string]any{"type": "boolean", "default": false},
					"threshold":           map[string]any{"type": "number", "default": 0.0},
					"rrf_k":               map[string]any{"type": "integer", "default": 60},
					"pool_size":           map[string]any{"type": "integer", "default": 60, "description": "reranker candidate-pool override (0 = configured/default; shrinks with classifier-recommended k when adaptive depth is enabled)"},
					"source":              map[string]any{"type": "string"},
					"type":                map[string]any{"type": "string"},
					"tags":                map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
					"context_window":      map[string]any{"type": "integer", "default": 0},
					"no_cache":            map[string]any{"type": "boolean", "default": false},
					"include_quarantined": map[string]any{"type": "boolean", "default": false, "description": "include chunks flagged as injection-poisoning (excluded by default)"},
				},
				"required": []string{"query"},
			},
		},
		{
			"name":        "go_rag_status",
			"description": "Report document/chunk/embedding counts, model, dimensions, and reranker status.",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{
				"vault": map[string]any{"type": "string", "default": "default", "description": "vault name (defaults to \"default\")"},
			}},
		},
		{
			"name":        "go_rag_add",
			"description": "Ingest a file or directory path into the database.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"vault": map[string]any{"type": "string", "default": "default", "description": "vault name (defaults to \"default\")"},
					"path":  map[string]any{"type": "string"},
				},
				"required": []string{"path"},
			},
		},
		{
			"name":        "go_rag_model_install",
			"description": "Download and verify the bundled pure-Go embedding model (spec 032). Idempotent; force=true re-downloads.",
			"inputSchema": map[string]any{
				"type":       "object",
				"properties": map[string]any{"force": map[string]any{"type": "boolean", "default": false, "description": "re-download even if present"}},
			},
		},
		{
			"name":        "go_rag_init",
			"description": "Initialize a new go-rag database (creates config + data directory).",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"ollama_url":    map[string]any{"type": "string"},
					"model":         map[string]any{"type": "string"},
					"watch_dir":     map[string]any{"type": "string"},
					"chunk_size":    map[string]any{"type": "integer"},
					"chunk_overlap": map[string]any{"type": "integer"},
				},
			},
		},
		{
			"name":        "go_rag_scan",
			"description": "Scan watched directories once for added/modified/deleted files and apply changes.",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{
				"vault": map[string]any{"type": "string", "default": "default", "description": "vault name (defaults to \"default\")"},
			}},
		},
		{
			"name":        "go_rag_config",
			"description": "Get or set go-rag configuration values (action: get|set).",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"vault":  map[string]any{"type": "string", "default": "default", "description": "vault name (defaults to \"default\")"},
					"action": map[string]any{"type": "string", "enum": []string{"get", "set"}},
					"key":    map[string]any{"type": "string"},
					"value":  map[string]any{"type": "string"},
				},
				"required": []string{"action"},
			},
		},
		{
			"name":        "go_rag_files",
			"description": "List the file paths of every ingested document.",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{
				"vault": map[string]any{"type": "string", "default": "default", "description": "vault name (defaults to \"default\")"},
			}},
		},
		{
			"name":        "go_rag_dirs",
			"description": "List ingested directories with file and chunk counts.",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{
				"vault": map[string]any{"type": "string", "default": "default", "description": "vault name (defaults to \"default\")"},
			}},
		},
		{
			"name":        "go_rag_reprocess",
			"description": "Force re-ingest of a directory (applies the current reader/embedder; bypasses dedup).",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"vault": map[string]any{"type": "string", "default": "default", "description": "vault name (defaults to \"default\")"},
					"path":  map[string]any{"type": "string"},
				},
				"required": []string{"path"},
			},
		},
		{
			"name":        "go_rag_migrate",
			"description": "Re-embed all documents whose embeddings use a different model than the current one.",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{
				"vault": map[string]any{"type": "string", "default": "default", "description": "vault name (defaults to \"default\")"},
			}},
		},
		{
			"name":        "go_rag_migrate_plan",
			"description": "Preview a migration: which embeddings are stale, the model/dimensionality change, and an estimate — without re-embedding (read-only, no embedding backend needed).",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{
				"vault": map[string]any{"type": "string", "default": "default", "description": "vault name (defaults to \"default\")"},
			}},
		},
		{
			"name":        "go_rag_poison_list",
			"description": "List chunks flagged as injection-poisoning (excluded from default results), with the per-signal verdict breakdown.",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{
				"vault": map[string]any{"type": "string", "default": "default", "description": "vault name (defaults to \"default\")"},
			}},
		},
		{
			"name":        "go_rag_poison_release",
			"description": "Release a flagged chunk (false-positive override) — makes it retrievable by default.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"vault":    map[string]any{"type": "string", "default": "default", "description": "vault name (defaults to \"default\")"},
					"chunk_id": map[string]any{"type": "string"},
				},
				"required": []string{"chunk_id"},
			},
		},
		{
			"name":        "go_rag_poison_reset",
			"description": "Undo a release — re-quarantines the chunk if its score is flagged.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"vault":    map[string]any{"type": "string", "default": "default", "description": "vault name (defaults to \"default\")"},
					"chunk_id": map[string]any{"type": "string"},
				},
				"required": []string{"chunk_id"},
			},
		},
		{
			"name":        "go_rag_poison_rescan",
			"description": "Re-score the whole corpus against the current detector (idempotent; no re-ingest). Scores pre-feature chunks and applies threshold/list changes to the back-catalog.",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{
				"vault": map[string]any{"type": "string", "default": "default", "description": "vault name (defaults to \"default\")"},
			}},
		},
		{
			"name":        "go_rag_get_chunk",
			"description": "Fetch a single chunk by its content-addressed ID, with its parent document metadata (spec 035). Returns not-found (-32001) if the id is absent from this vault.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"vault":    map[string]any{"type": "string", "default": "default", "description": "vault name (defaults to \"default\")"},
					"chunk_id": map[string]any{"type": "string"},
				},
				"required": []string{"chunk_id"},
			},
		},
		{
			"name":        "go_rag_get_chunk_context",
			"description": "Fetch a chunk plus up to N neighbours on each side (spec 037 / BL-002), in document order, with the target index and parent document. window defaults to 2 (range 0..10; 0 returns exactly the target). Returns not-found (-32001) if the id is absent from this vault.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"vault":    map[string]any{"type": "string", "default": "default", "description": "vault name (defaults to \"default\")"},
					"chunk_id": map[string]any{"type": "string"},
					"window":   map[string]any{"type": "integer", "minimum": 0, "maximum": 10, "default": 2},
				},
				"required": []string{"chunk_id"},
			},
		},
		{
			"name":        "go_rag_batch_get_chunks",
			"description": "Resolve up to 100 chunks by id in one call (spec 038 / BL-003), one result per id in request order. A missing id yields a per-id error ('not found') — the call does not fail for one bad id. Only structurally invalid input (empty list, >100 ids, empty/whitespace element) is rejected.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"vault":     map[string]any{"type": "string", "default": "default", "description": "vault name (defaults to \"default\")"},
					"chunk_ids": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "maxItems": 100},
				},
				"required": []string{"chunk_ids"},
			},
		},
		{
			"name":        "go_rag_list_documents",
			"description": "List documents — reliable ingested_at cursor + status filter + page_token pagination (spec 039 / BL-007). Returns documents in ingested_at ascending order + a next_page_token.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"vault":      map[string]any{"type": "string", "default": "default", "description": "vault name (defaults to \"default\")"},
					"page_size":  map[string]any{"type": "integer", "minimum": 1, "maximum": 200, "default": 50},
					"page_token": map[string]any{"type": "string"},
					"after":      map[string]any{"type": "string", "format": "date-time"},
					"status":     map[string]any{"type": "string", "enum": []string{"embedded", "pending", "error"}},
					"tags":       map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "match-any tag filter (spec 047 R3); nil/empty = all"},
				},
			},
		},
		{
			"name":        "go_rag_list_chunks",
			"description": "List chunks of one document (spec 047 / T008) in (chunk_index, chunk_id) ascending order + a next_page_token. Unknown or empty document → empty result (not an error). document_id is required.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"vault":       map[string]any{"type": "string", "default": "default", "description": "vault name (defaults to \"default\")"},
					"document_id": map[string]any{"type": "string"},
					"page_size":   map[string]any{"type": "integer", "minimum": 1, "maximum": 200, "default": 50},
					"page_token":  map[string]any{"type": "string"},
				},
				"required": []string{"document_id"},
			},
		},
		{
			"name":        "go_rag_delete_document",
			"description": "Delete a document and all its chunks/embeddings from the index by content-addressed document ID (spec 050). Index-only — the source file on disk is NOT touched. Returns not-found if the id is absent from this vault. doc_id is required.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"vault":  map[string]any{"type": "string", "default": "default", "description": "vault name (defaults to \"default\")"},
					"doc_id": map[string]any{"type": "string"},
				},
				"required": []string{"doc_id"},
			},
		},
		{
			"name":        "go_rag_vault_list",
			"description": "List all available document vaults with doc counts.",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}},
		},
		{
			"name":        "go_rag_guide",
			"description": "Get a guide for the AI: system status, what's needed, available tools, and usage patterns. Call this first to understand the current state.",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}},
		},
		{
			"name":        "go_rag_eval",
			"description": "Measure retrieval quality (recall@k, precision@k, MRR, NDCG@k) over a golden dataset. Self-provisions a throwaway vault from the golden corpus with a deterministic offline embedder (no Ollama, reproducible).",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"golden": map[string]any{"type": "string", "description": "Path to golden JSONL (default testdata/golden/v1.jsonl)."},
					"corpus": map[string]any{"type": "string", "description": "Source corpus dir (default testdata/golden/corpus/)."},
					"mode":   map[string]any{"type": "string", "enum": []string{"hybrid", "semantic", "keyword"}, "default": "hybrid"},
					"k":      map[string]any{"type": "integer", "default": 10},
				},
			},
		},
		// --- spec 045 auth-management tools (admin-gated) ---
		{
			"name":        "go_rag_auth_list",
			"description": "List API keys (ids + labels + modes only; never the secret). Admin scope required.",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}},
		},
		{
			"name":        "go_rag_auth_create",
			"description": "Create a labelled API key. The full secret (gorag_<id8>.<secret>) is returned ONCE in the response — capture it now; it is never persisted or re-displayable. Admin scope required.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"label":   map[string]any{"type": "string"},
					"mode":    map[string]any{"type": "string", "enum": []string{"read", "write", "admin"}, "default": "read"},
					"expires": map[string]any{"type": "string", "description": "lifetime duration e.g. \"720h\"; empty = never expires"},
				},
				"required": []string{"label"},
			},
		},
		{
			"name":        "go_rag_auth_revoke",
			"description": "Disable an API key by its id (gorag_<id8>). Admin scope required.",
			"inputSchema": map[string]any{
				"type":       "object",
				"properties": map[string]any{"id": map[string]any{"type": "string"}},
				"required":   []string{"id"},
			},
		},
		{
			"name":        "go_rag_auth_session_list",
			"description": "List active admin-login sessions (hash, user, expires, last_seen, ip; never the token). Admin scope required.",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}},
		},
		{
			"name":        "go_rag_auth_session_revoke",
			"description": "Revoke a session by its hash (from go_rag_auth_session_list). Admin scope required.",
			"inputSchema": map[string]any{
				"type":       "object",
				"properties": map[string]any{"hash": map[string]any{"type": "string"}},
				"required":   []string{"hash"},
			},
		},
	}
}
