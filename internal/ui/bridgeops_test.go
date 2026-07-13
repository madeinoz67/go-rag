package ui

// bridgeops_test.go (spec 049 T008/T010/T012/T013) covers all four user stories
// of the Operations view against the live in-process engine + audit log. The
// harness (newTestEngine/newUITest/bearerGet/login) is shared with ui_test.go.
//
// Parity is the spine: stats values match eng.Status("default") (the source `go-rag
// status` reads) and activity matches audit.Read on the resolved path (the
// source `go-rag audit` reads). Read-only + GET-only + no-Node invariants are
// pinned for the slice.

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/madeinoz67/go-rag/internal/audit"
	"github.com/madeinoz67/go-rag/internal/auth"
	"github.com/madeinoz67/go-rag/internal/engine"
)

// bridgeOpsSetup builds an initialized-vault UI test server (admin created,
// logged in) and returns the server, a bearer token, and the engine. Routes
// 401 without a session on an initialized vault (TestGuard_BypassRegime).
func bridgeOpsSetup(t *testing.T) (*httptest.Server, string, *engine.Engine) {
	t.Helper()
	eng := newTestEngine(t)
	if _, err := auth.CreateAdmin(auth.NewStore(eng.DB()), auth.DefaultAdminUsername, "s3cr3t"); err != nil {
		t.Fatalf("CreateAdmin: %v", err)
	}
	srv := newUITest(t, eng)
	tok, _ := login(t, srv.URL, auth.DefaultAdminUsername, "s3cr3t", http.StatusOK)
	return srv, tok, eng
}

// writeBridgeAudit appends JSONL audit events at the engine's default audit
// path (<dbPath>/audit/audit.log — the path AuditRead resolves to in tests).
func writeBridgeAudit(t *testing.T, eng *engine.Engine, events ...audit.Event) {
	t.Helper()
	path := audit.DefaultPath(eng.Config().DBPath)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir audit: %v", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		t.Fatalf("open audit: %v", err)
	}
	defer f.Close()
	for _, e := range events {
		line, err := e.Marshal()
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if _, err := f.Write(line); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
}

// drainAndClose discards the body (best practice) then closes.
func drainAndClose(resp *http.Response) {
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
}

// --- US1: stats (T008) ------------------------------------------------------

// TestBridgeOpsStats_ProjectsStatus — 200 + the operational fields present and
// matching eng.Status("default") (parity with `go-rag status`).
func TestBridgeOpsStats_ProjectsStatus(t *testing.T) {
	srv, tok, eng := bridgeOpsSetup(t)

	want, err := eng.Status("default")
	if err != nil {
		t.Fatalf("eng.Status: %v", err)
	}
	resp := bearerGet(t, srv.URL+"/api/bridge-ops/stats", tok)
	defer drainAndClose(resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d want 200", resp.StatusCode)
	}
	var dto bridgeOpsStatsDTO
	if err := json.NewDecoder(resp.Body).Decode(&dto); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// Backlog parity (the centre of the operational view).
	if dto.Backlog.Pending != want.EmbedPending || dto.Backlog.Failed != want.EmbedFailed {
		t.Fatalf("backlog: dto=%+v want pending=%d failed=%d", dto.Backlog, want.EmbedPending, want.EmbedFailed)
	}
	if dto.Backlog.Complete != want.EmbeddingsComplete {
		t.Fatalf("backlog.complete: dto=%v want=%v", dto.Backlog.Complete, want.EmbeddingsComplete)
	}
	// Drift parity.
	if dto.Drift.Verdict != want.DriftVerdict || dto.Drift.Hard != want.HardDrift || dto.Drift.Version != want.VersionDrift {
		t.Fatalf("drift: dto=%+v want verdict=%s hard=%v version=%v",
			dto.Drift, want.DriftVerdict, want.HardDrift, want.VersionDrift)
	}
	if dto.Drift.LiveOllamaVer != want.LiveOllamaVersion {
		t.Fatalf("live_ollama_ver: dto=%q want=%q", dto.Drift.LiveOllamaVer, want.LiveOllamaVersion)
	}
	// Subsystem parity.
	if dto.Subsystems.Poisoning.Enabled != want.PoisoningEnabled || dto.Subsystems.Poisoning.Flagged != want.PoisonFlagged {
		t.Fatalf("poisoning: dto=%+v want enabled=%v flagged=%d", dto.Subsystems.Poisoning, want.PoisoningEnabled, want.PoisonFlagged)
	}
	if dto.Subsystems.Enrichment.Enabled != want.EnrichmentEnabled || dto.Subsystems.Enrichment.EnrichedDocs != want.EnrichedDocs {
		t.Fatalf("enrichment: dto=%+v want enabled=%v docs=%d", dto.Subsystems.Enrichment, want.EnrichmentEnabled, want.EnrichedDocs)
	}
	if dto.Subsystems.Adaptive.PoolSize != want.PoolSize || dto.Subsystems.Adaptive.Enabled != want.AdaptiveDepthEnabled {
		t.Fatalf("adaptive: dto=%+v want pool=%d enabled=%v", dto.Subsystems.Adaptive, want.PoolSize, want.AdaptiveDepthEnabled)
	}
	// Vault derived (non-empty).
	if dto.Vault == "" {
		t.Fatalf("vault must be derived (non-empty)")
	}
	// Watch surfaces the configured dirs + scan_driven=true (R5).
	if !dto.Watch.ScanDriven {
		t.Fatalf("watch.scan_driven must be true (no persistent watcher)")
	}
	if dto.Watch.Dirs == nil {
		t.Fatalf("watch.dirs must be [] not null")
	}
}

// TestBridgeOpsStats_EmptyVault — fresh/empty vault → zero backlog, drift
// verdict present, no error (a quiet vault is healthy, not broken).
func TestBridgeOpsStats_EmptyVault(t *testing.T) {
	srv, tok, eng := bridgeOpsSetup(t)
	want, err := eng.Status("default")
	if err != nil {
		t.Fatalf("eng.Status: %v", err)
	}
	resp := bearerGet(t, srv.URL+"/api/bridge-ops/stats", tok)
	defer drainAndClose(resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d want 200", resp.StatusCode)
	}
	var dto bridgeOpsStatsDTO
	if err := json.NewDecoder(resp.Body).Decode(&dto); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if dto.Backlog.Pending != 0 || dto.Backlog.Failed != 0 {
		t.Fatalf("empty vault backlog: got %+v want zeros", dto.Backlog)
	}
	if dto.Drift.Verdict == "" {
		t.Fatalf("drift.verdict must be present (got empty) — engine reports %q", want.DriftVerdict)
	}
	if dto.Drift.Verdict != want.DriftVerdict {
		t.Fatalf("drift.verdict: dto=%q want=%q", dto.Drift.Verdict, want.DriftVerdict)
	}
	if dto.LastActivity != "" {
		t.Fatalf("empty vault last_activity: got %q want empty", dto.LastActivity)
	}
}

// --- US2: activity (T010) ---------------------------------------------------

// errFailedSentinel drives audit.IngestEvent's statusOf to "error" (a non-nil
// err → status "error" → outcome "failed"). The error text never reaches the
// audit record (only counts), so any non-nil error suffices.
type sentinelErr struct{ msg string }

func (e *sentinelErr) Error() string { return e.msg }

// TestBridgeOpsActivity_OrderAndParity — events come back most-recent-first and
// match audit.Read (the source `go-rag audit --type ingest --tail N` reads) on
// the resolved path. Failures carry outcome=failed and are distinguishable.
func TestBridgeOpsActivity_OrderAndParity(t *testing.T) {
	srv, tok, eng := bridgeOpsSetup(t)
	events := []audit.Event{
		audit.IngestEvent("add", "i1.md", 1, 0, 0, nil),                        // success
		audit.QueryEvent("q1", "hybrid", 5, 3, nil),                            // query (filtered out)
		audit.IngestEvent("add", "i2.md", 2, 0, 0, nil),                        // success
		audit.IngestEvent("add", "i3.md", 0, 0, 1, &sentinelErr{"emb failed"}), // failed
	}
	writeBridgeAudit(t, eng, events...)

	resp := bearerGet(t, srv.URL+"/api/bridge-ops/activity?tail=10&type=ingest", tok)
	defer drainAndClose(resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d want 200", resp.StatusCode)
	}
	var out activityResponseDTO
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// Three ingest events (query filtered out), most-recent-first: i3, i2, i1.
	if out.Count != 3 || len(out.Events) != 3 {
		t.Fatalf("count: got %d (len %d) want 3", out.Count, len(out.Events))
	}
	if out.Events[0].Summary == "" || !strings.Contains(out.Events[0].Summary, "i3.md") {
		t.Fatalf("most-recent-first[0]: %q want i3.md", out.Events[0].Summary)
	}
	if !strings.Contains(out.Events[2].Summary, "i1.md") {
		t.Fatalf("oldest-last[2]: %q want i1.md", out.Events[2].Summary)
	}
	// Outcome: i3 failed, i2/i1 success.
	if out.Events[0].Outcome != "failed" {
		t.Fatalf("i3 outcome: got %q want failed", out.Events[0].Outcome)
	}
	if out.Events[1].Outcome != "success" || out.Events[2].Outcome != "success" {
		t.Fatalf("i2/i1 outcome: got %q / %q want success", out.Events[1].Outcome, out.Events[2].Outcome)
	}
	if out.Events[0].Timestamp == "" {
		t.Fatalf("timestamp must be present")
	}

	// Parity: identical content to a direct audit.Read on the resolved path.
	path := audit.DefaultPath(eng.Config().DBPath)
	direct, err := audit.Read(path, audit.ReadOptions{Type: audit.TypeIngest, Tail: 10})
	if err != nil {
		t.Fatalf("audit.Read: %v", err)
	}
	if len(direct) != len(out.Events) {
		t.Fatalf("parity len: activity %d direct %d", len(out.Events), len(direct))
	}
	// direct is oldest→newest; out.Events is newest→oldest.
	for i, ev := range out.Events {
		d := direct[len(direct)-1-i]
		if ev.Type != d.Type || !strings.Contains(ev.Summary, d.Path) {
			t.Fatalf("parity[%d]: dto=%+v direct path=%q", i, ev, d.Path)
		}
	}
}

// TestBridgeOpsActivity_TailClampAndTypeValidation — tail clamps to [1,100]
// (default 20); an unknown type → 400 "invalid type"; a valid non-default type
// is accepted.
func TestBridgeOpsActivity_TailClampAndTypeValidation(t *testing.T) {
	srv, tok, eng := bridgeOpsSetup(t)
	writeBridgeAudit(t, eng,
		audit.IngestEvent("add", "a.md", 1, 0, 0, nil),
		audit.QueryEvent("q", "hybrid", 5, 1, nil),
	)
	url := srv.URL + "/api/bridge-ops/activity"

	// Bogus type → 400 invalid type.
	resp := bearerGet(t, url+"?type=bogus", tok)
	drainAndClose(resp)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("bogus type: got %d want 400", resp.StatusCode)
	}

	// Huge tail clamps to 100 (and returns 200 with the 1 ingest event).
	resp = bearerGet(t, url+"?type=ingest&tail=99999", tok)
	defer drainAndClose(resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("clamp tail: got %d want 200", resp.StatusCode)
	}
	var out activityResponseDTO
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Count != 1 {
		t.Fatalf("clamp tail count: got %d want 1", out.Count)
	}

	// query type accepted, returns the 1 query event.
	resp = bearerGet(t, url+"?type=query&tail=5", tok)
	defer drainAndClose(resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("query type: got %d want 200", resp.StatusCode)
	}
	out = activityResponseDTO{}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Count != 1 || out.Events[0].Type != "query" {
		t.Fatalf("query type content: got count=%d", out.Count)
	}
}

// TestBridgeOpsActivity_MissingLogIsEmpty — fresh vault, no audit written →
// {events:[], count:0} (healthy empty, never an error).
func TestBridgeOpsActivity_MissingLogIsEmpty(t *testing.T) {
	srv, tok, _ := bridgeOpsSetup(t)
	resp := bearerGet(t, srv.URL+"/api/bridge-ops/activity", tok)
	defer drainAndClose(resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d want 200", resp.StatusCode)
	}
	var out activityResponseDTO
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Count != 0 || len(out.Events) != 0 {
		t.Fatalf("missing log: got count=%d len=%d want 0", out.Count, len(out.Events))
	}
}

// --- US3: subsystems + drift detail (T012) ---------------------------------

// TestBridgeOpsStats_DriftDetail — the baseline (corpus baseline the index was
// built under) + cause are present and match StatusInfo (the Dashboard shows
// only the verdict; Operations adds the detail — R6).
func TestBridgeOpsStats_DriftDetail(t *testing.T) {
	srv, tok, eng := bridgeOpsSetup(t)
	want, err := eng.Status("default")
	if err != nil {
		t.Fatalf("eng.Status: %v", err)
	}
	resp := bearerGet(t, srv.URL+"/api/bridge-ops/stats", tok)
	defer drainAndClose(resp)
	var dto bridgeOpsStatsDTO
	if err := json.NewDecoder(resp.Body).Decode(&dto); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if dto.Drift.Baseline.Model != want.CorpusBaselineModel ||
		dto.Drift.Baseline.Dim != want.CorpusBaselineDim ||
		dto.Drift.Baseline.Convention != want.CorpusBaselineConvention ||
		dto.Drift.Baseline.OllamaVer != want.CorpusBaselineOllamaVer ||
		dto.Drift.Baseline.RecordedAt != want.CorpusBaselineRecordedAt {
		t.Fatalf("baseline: dto=%+v want model=%q dim=%d conv=%q ov=%q ra=%q",
			dto.Drift.Baseline, want.CorpusBaselineModel, want.CorpusBaselineDim,
			want.CorpusBaselineConvention, want.CorpusBaselineOllamaVer, want.CorpusBaselineRecordedAt)
	}
	if dto.Drift.Cause == "" {
		t.Fatalf("drift.cause must be non-empty (clean → \"none\")")
	}
	switch dto.Drift.Cause {
	case "model", "dimensionality", "convention", "ollama-version", "none":
	default:
		t.Fatalf("drift.cause %q not in vocabulary", dto.Drift.Cause)
	}
}

// TestBridgeOpsStats_CachesAndAdaptive — the cache + adaptive tiles carry the
// StatusInfo-sourced values; an all-off default vault renders cleanly (not as
// an error).
func TestBridgeOpsStats_CachesAndAdaptive(t *testing.T) {
	srv, tok, eng := bridgeOpsSetup(t)
	want, err := eng.Status("default")
	if err != nil {
		t.Fatalf("eng.Status: %v", err)
	}
	resp := bearerGet(t, srv.URL+"/api/bridge-ops/stats", tok)
	defer drainAndClose(resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d want 200", resp.StatusCode)
	}
	var dto bridgeOpsStatsDTO
	if err := json.NewDecoder(resp.Body).Decode(&dto); err != nil {
		t.Fatalf("decode: %v", err)
	}
	rc, ec := want.ResultCache, want.EmbeddingCache
	if dto.Subsystems.Caches.Result.Enabled != rc.Enabled || dto.Subsystems.Caches.Result.Size != rc.Size {
		t.Fatalf("result cache: dto=%+v want %+v", dto.Subsystems.Caches.Result, rc)
	}
	if dto.Subsystems.Caches.Embedding.Enabled != ec.Enabled || dto.Subsystems.Caches.Embedding.Size != ec.Size {
		t.Fatalf("embedding cache: dto=%+v want %+v", dto.Subsystems.Caches.Embedding, ec)
	}
	if dto.Subsystems.Adaptive.Utilization.Queries != want.PoolUtilization.Queries {
		t.Fatalf("pool utilization: dto=%+v want queries=%d", dto.Subsystems.Adaptive.Utilization, want.PoolUtilization.Queries)
	}
	if dto.Subsystems.Adaptive.NearDupChunks != want.NearDupChunks {
		t.Fatalf("near_dup_chunks: dto=%d want=%d", dto.Subsystems.Adaptive.NearDupChunks, want.NearDupChunks)
	}
}

// --- US4: read-only + GET-only + no-Node + 401 (T013) ----------------------

// TestBridgeOps_NoWrite — a stats + activity fetch mutates nothing (snapshot
// eng.Status("default") counts before/after → identical). The read-only invariant.
func TestBridgeOps_NoWrite(t *testing.T) {
	srv, tok, eng := bridgeOpsSetup(t)
	writeBridgeAudit(t, eng,
		audit.IngestEvent("add", "a.md", 2, 0, 0, nil),
		audit.QueryEvent("q", "hybrid", 5, 1, nil),
	)
	before := snapUI(t, eng)

	resp := bearerGet(t, srv.URL+"/api/bridge-ops/stats", tok)
	drainAndClose(resp)
	resp = bearerGet(t, srv.URL+"/api/bridge-ops/activity?tail=10", tok)
	drainAndClose(resp)

	if after := snapUI(t, eng); before != after {
		t.Fatalf("Operations mutated DB: before=%s after=%s", before, after)
	}
}

// snapUI captures the mutable Status() counts for a before/after check.
func snapUI(t *testing.T, eng *engine.Engine) string {
	t.Helper()
	s, err := eng.Status("default")
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	return "docs=" + strconv.Itoa(s.Documents) + " chunks=" + strconv.Itoa(s.Chunks) +
		" emb=" + strconv.Itoa(s.Embeddings) + " pend=" + strconv.Itoa(s.EmbedPending) +
		" fail=" + strconv.Itoa(s.EmbedFailed)
}

// TestBridgeOps_GETOnly — both routes are GET-only (read-only slice); POST →
// 405 Method Not Allowed (no write verb registered).
func TestBridgeOps_GETOnly(t *testing.T) {
	srv, tok, _ := bridgeOpsSetup(t)
	for _, path := range []string{"/api/bridge-ops/stats", "/api/bridge-ops/activity"} {
		req, _ := http.NewRequest(http.MethodPost, srv.URL+path, bytes.NewReader([]byte("{}")))
		req.Header.Set("Authorization", "Bearer "+tok)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("POST %s: %v", path, err)
		}
		drainAndClose(resp)
		if resp.StatusCode != http.StatusMethodNotAllowed {
			t.Fatalf("POST %s: got %d want 405", path, resp.StatusCode)
		}
	}
}

// TestBridgeOps_RequiresBearer — on an initialized vault both routes 401
// without a Bearer (the guard holds for the new routes exactly as it does for
// the Dashboard).
func TestBridgeOps_RequiresBearer(t *testing.T) {
	srv, _, _ := bridgeOpsSetup(t)
	for _, path := range []string{"/api/bridge-ops/stats", "/api/bridge-ops/activity"} {
		resp := bearerGet(t, srv.URL+path, "")
		drainAndClose(resp)
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("no-bearer %s: got %d want 401", path, resp.StatusCode)
		}
	}
}

// TestBridgeOps_NoNodeArtifacts — this slice's front-end additions (Alpine view
// + CSS) ship no Node/Vite/Tailwind build chain. The repo-wide TestNoNodeArtifacts
// covers it globally; this re-asserts the forbidden markers are absent under the
// web tree (belt-and-suspenders for the slice's own additions).
func TestBridgeOps_NoNodeArtifacts(t *testing.T) {
	root := findRepoRoot(t)
	webRoot := filepath.Join(root, "internal", "ui", "web")
	forbidden := []string{
		"package.json", "package-lock.json", "node_modules",
		"vite.config.js", "vite.config.ts", "tailwind.config.js", "tailwind.config.ts",
	}
	for _, name := range forbidden {
		if _, err := os.Stat(filepath.Join(webRoot, name)); err == nil {
			t.Errorf("forbidden front-end artifact present: %s", name)
		}
	}
}
