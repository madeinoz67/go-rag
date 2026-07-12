package ui

// bridgeops.go (spec 049 Slice 3) is the Bridge Ops view's two read-only
// backend routes — the operational-health surface (view 4 of the sidebar). It
// differentiates from the Dashboard (spec 046) by surfacing what the Dashboard
// omits: drift detail (baseline-vs-live), the subsystem tiles (poisoning,
// enrichment, caches, adaptive), a bounded recent-activity feed, and the watch
// configuration.
//
// Both routes call the engine in-process — the UI is a 4th adapter over
// internal/engine, NOT a REST proxy (R1):
//   - GET /api/bridge-ops/stats    → Engine.Status() (operational projection) + WatchDirs
//   - GET /api/bridge-ops/activity → Engine.AuditRead (thin wrapper over audit.Read, R3)
//
// Read-only: neither route mutates state (T013 pins it). WatchDirs come from
// the engine config (Engine.Config().WatchDirs) — the daemon runs no persistent
// watcher, so scan_driven is always true this slice (R5).

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/madeinoz67/go-rag/internal/audit"
	"github.com/madeinoz67/go-rag/internal/engine"
)

// bridgeOpsStatsDTO is the GET /api/bridge-ops/stats response — the operational
// subset of engine.StatusInfo (omitting the corpus counts the Dashboard already
// shows, except the backlog which is central here) plus the watch config. Fields
// are field-parallel to StatusInfo (specs/049 data-model.md). Stats values match
// `go-rag status` byte-for-byte (same-source guarantee, pinned by T008 parity).
type bridgeOpsStatsDTO struct {
	Vault        string        `json:"vault"`
	LastActivity string        `json:"last_activity"`
	Backlog      backlogDTO    `json:"backlog"`
	Drift        driftDTO      `json:"drift"`
	Subsystems   subsystemsDTO `json:"subsystems"`
	Watch        watchDTO      `json:"watch"`
}

// backlogDTO is the embed-backlog snapshot (spec 030 crash-safe background
// embedder): pending + permanently-failed chunks, and the completion flag.
type backlogDTO struct {
	Pending  int  `json:"pending"`
	Failed   int  `json:"failed"`
	Complete bool `json:"complete"`
}

// driftDTO is the corpus-baseline-vs-live embedding-drift story (H11 / spec 017):
// the verdict, the one-line cause (which axis drifted), and the full baseline
// expandable behind the tile (R6).
type driftDTO struct {
	Verdict       string      `json:"verdict"`
	Hard          bool        `json:"hard"`
	Version       bool        `json:"version"`
	Cause         string      `json:"cause"`
	Baseline      baselineDTO `json:"baseline"`
	LiveOllamaVer string      `json:"live_ollama_ver"`
}

// baselineDTO is the corpus baseline the index was built under (CorpusBaseline*
// projected field-parallel).
type baselineDTO struct {
	Model      string `json:"model"`
	Dim        int    `json:"dim"`
	Convention string `json:"convention"`
	OllamaVer  string `json:"ollama_ver"`
	RecordedAt string `json:"recorded_at"`
}

// subsystemsDTO groups the four operational subsystem tiles (R7): poisoning,
// enrichment, caches, adaptive retrieval.
type subsystemsDTO struct {
	Poisoning  poisoningDTO  `json:"poisoning"`
	Enrichment enrichmentDTO `json:"enrichment"`
	Caches     cachesDTO     `json:"caches"`
	Adaptive   adaptiveDTO   `json:"adaptive"`
}

// poisoningDTO — retrieval-poisoning detection summary (H04 / spec 019).
type poisoningDTO struct {
	Enabled      bool    `json:"enabled"`
	Flagged      int     `json:"flagged"`
	Sources      int     `json:"sources"`
	Phrases      int     `json:"phrases"`
	ThresholdSus float64 `json:"threshold_sus"`
	ThresholdQua float64 `json:"threshold_qua"`
}

// enrichmentDTO — background document enrichment (spec 029) + image captioning
// (spec 031 US4).
type enrichmentDTO struct {
	Enabled      bool `json:"enabled"`
	Captioning   bool `json:"captioning"`
	EnrichedDocs int  `json:"enriched_docs"`
}

// cachesDTO — the two query caches (H06 / spec 016): result cache + embedding
// cache.
type cachesDTO struct {
	Result    cacheStatsDTO `json:"result"`
	Embedding cacheStatsDTO `json:"embedding"`
}

// cacheStatsDTO projects engine.CacheStats (enabled / size / capacity / hits /
// misses).
type cacheStatsDTO struct {
	Enabled  bool   `json:"enabled"`
	Size     int    `json:"size"`
	Capacity int    `json:"capacity"`
	Hits     uint64 `json:"hits"`
	Misses   uint64 `json:"misses"`
}

// adaptiveDTO — adaptive-retrieval observability (H22 / spec 024): the pool
// ceiling, the classifier posture, the aggregate utilization, and near-dup
// count (H20 / spec 026).
type adaptiveDTO struct {
	PoolSize      int                `json:"pool_size"`
	Enabled       bool               `json:"enabled"`
	Utilization   poolUtilizationDTO `json:"utilization"`
	NearDupChunks int                `json:"near_dup_chunks"`
}

// poolUtilizationDTO projects engine.PoolUtilization as-is (R-binding in
// data-model.md). NOTE: the engine type carries {Queries, AvgFetched, AvgKept,
// Saturated} — the aggregate pool-consumption signal — not the
// {samples,p50,p95} sketched in the contract example; the struct fields win
// (projected as-is).
type poolUtilizationDTO struct {
	Queries    uint64  `json:"queries"`
	AvgFetched float64 `json:"avg_fetched"`
	AvgKept    float64 `json:"avg_kept"`
	Saturated  uint64  `json:"saturated"`
}

// watchDTO — the configured watch directories + the honest scan-driven framing
// (the daemon runs no persistent watcher this slice; R5).
type watchDTO struct {
	Dirs       []string `json:"dirs"`
	ScanDriven bool     `json:"scan_driven"`
}

// activityResponseDTO is the GET /api/bridge-ops/activity response: a bounded,
// most-recent-first list of recent audit events + the count.
type activityResponseDTO struct {
	Events []activityEventDTO `json:"events"`
	Count  int                `json:"count"`
}

// activityEventDTO projects audit.Event (spec 021) to the operator-relevant
// fields: type, RFC3339 timestamp, a one-line summary, and the derived outcome.
type activityEventDTO struct {
	Type      string `json:"type"`
	Timestamp string `json:"timestamp"`
	Summary   string `json:"summary"`
	Outcome   string `json:"outcome"`
}

// validActivityTypes are the event types the activity filter accepts (R4).
// "auth" (spec 045 management ops) is intentionally excluded — the feed is the
// operational ingest/query/auth-fail signal.
var validActivityTypes = map[string]struct{}{
	audit.TypeIngest:   {},
	audit.TypeQuery:    {},
	audit.TypeAuthFail: {},
}

// handleBridgeOpsStats — operational health snapshot. Always 200 (the vault
// always has a status). Engine error → writeEngineErr (existing helper, same
// package). last_activity is best-effort: the newest audit timestamp via a
// tail:1 read, empty when the log is missing/empty (never a stats error).
func (s *Server) handleBridgeOpsStats(w http.ResponseWriter, _ *http.Request) {
	info, err := s.eng.Status()
	if err != nil {
		writeEngineErr(w, err)
		return
	}
	lastActivity := ""
	if evs, aerr := s.eng.AuditRead(audit.ReadOptions{Tail: 1}); aerr == nil && len(evs) > 0 {
		lastActivity = evs[len(evs)-1].TS.UTC().Format(time.RFC3339)
	}
	writeJSON(w, http.StatusOK, toBridgeOpsStats(info, s.deriveVault(), s.eng.Config().WatchDirs, lastActivity))
}

// handleBridgeOpsActivity — bounded recent audit feed. tail clamped to [0,100]
// (default 20; 0 → bounded default, never unbounded); type validated against
// ingest|query|auth-fail (default ingest; unknown → 400 "invalid type").
// Missing/disabled log → {events:[], count:0} (healthy empty, R4).
func (s *Server) handleBridgeOpsActivity(w http.ResponseWriter, r *http.Request) {
	tail := clampActivityTail(r.URL.Query().Get("tail"))
	etype := r.URL.Query().Get("type")
	if etype == "" {
		etype = audit.TypeIngest
	}
	if _, ok := validActivityTypes[etype]; !ok {
		writeError(w, http.StatusBadRequest, "invalid type")
		return
	}
	events, err := s.eng.AuditRead(audit.ReadOptions{Type: etype, Tail: tail})
	if err != nil {
		writeEngineErr(w, err)
		return
	}
	dtos := toActivityEvents(events)
	writeJSON(w, http.StatusOK, activityResponseDTO{Events: dtos, Count: len(dtos)})
}

// clampActivityTail parses tail, clamping to [1,100] with a default of 20. An
// absent, non-numeric, negative, or zero value yields the default (R4: 0 →
// bounded default, not an unbounded all-read).
func clampActivityTail(raw string) int {
	const def, lo, hi = 20, 1, 100
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return def
	}
	if n > hi {
		return hi
	}
	if n < lo {
		return lo
	}
	return n
}

// toBridgeOpsStats projects the operational subset of StatusInfo + the watch
// config into the stats DTO (R2: reuses StatusInfo, differentiates by depth).
func toBridgeOpsStats(i *engine.StatusInfo, vault string, watchDirs []string, lastActivity string) bridgeOpsStatsDTO {
	dirs := watchDirs
	if dirs == nil {
		dirs = []string{}
	}
	return bridgeOpsStatsDTO{
		Vault:        vault,
		LastActivity: lastActivity,
		Backlog: backlogDTO{
			Pending:  i.EmbedPending,
			Failed:   i.EmbedFailed,
			Complete: i.EmbeddingsComplete,
		},
		Drift: driftDTO{
			Verdict:       i.DriftVerdict,
			Hard:          i.HardDrift,
			Version:       i.VersionDrift,
			Cause:         deriveDriftCause(i),
			LiveOllamaVer: i.LiveOllamaVersion,
			Baseline: baselineDTO{
				Model:      i.CorpusBaselineModel,
				Dim:        i.CorpusBaselineDim,
				Convention: i.CorpusBaselineConvention,
				OllamaVer:  i.CorpusBaselineOllamaVer,
				RecordedAt: i.CorpusBaselineRecordedAt,
			},
		},
		Subsystems: subsystemsDTO{
			Poisoning: poisoningDTO{
				Enabled:      i.PoisoningEnabled,
				Flagged:      i.PoisonFlagged,
				Sources:      i.PoisonSources,
				Phrases:      i.PoisonPhrases,
				ThresholdSus: i.PoisonThresholdSus,
				ThresholdQua: i.PoisonThresholdQua,
			},
			Enrichment: enrichmentDTO{
				Enabled:      i.EnrichmentEnabled,
				Captioning:   i.CaptioningEnabled,
				EnrichedDocs: i.EnrichedDocs,
			},
			Caches: cachesDTO{
				Result:    toCacheStatsDTO(i.ResultCache),
				Embedding: toCacheStatsDTO(i.EmbeddingCache),
			},
			Adaptive: adaptiveDTO{
				PoolSize:      i.PoolSize,
				Enabled:       i.AdaptiveDepthEnabled,
				Utilization:   toPoolUtilizationDTO(i.PoolUtilization),
				NearDupChunks: i.NearDupChunks,
			},
		},
		Watch: watchDTO{
			Dirs:       dirs,
			ScanDriven: true, // R5: no persistent watcher this slice
		},
	}
}

// deriveDriftCause reduces the drift flags + baseline mismatch to one
// act-on-able axis (R6): model / dimensionality / convention / ollama-version /
// none. On hard drift it names the mismatched axis when determinable; a soft
// version drift alone is "ollama-version"; clean is "none".
func deriveDriftCause(i *engine.StatusInfo) string {
	if i.HardDrift {
		if i.CorpusBaselineModel != "" && i.EmbeddingModel != "" && i.CorpusBaselineModel != i.EmbeddingModel {
			return "model"
		}
		if i.CorpusBaselineDim != 0 && i.Dimensions != 0 && i.CorpusBaselineDim != i.Dimensions {
			return "dimensionality"
		}
		if i.CorpusBaselineConvention != "" && i.ConfiguredPrefix != "" && i.CorpusBaselineConvention != i.ConfiguredPrefix {
			return "convention"
		}
		return "model" // hard drift set but axis indeterminate — default to the common cause
	}
	if i.VersionDrift {
		return "ollama-version"
	}
	return "none"
}

func toCacheStatsDTO(c engine.CacheStats) cacheStatsDTO {
	return cacheStatsDTO{
		Enabled:  c.Enabled,
		Size:     c.Size,
		Capacity: c.Capacity,
		Hits:     c.Hits,
		Misses:   c.Misses,
	}
}

func toPoolUtilizationDTO(p engine.PoolUtilization) poolUtilizationDTO {
	return poolUtilizationDTO{
		Queries:    p.Queries,
		AvgFetched: p.AvgFetched,
		AvgKept:    p.AvgKept,
		Saturated:  p.Saturated,
	}
}

// toActivityEvents projects audit events (oldest→newest from audit.Read) into
// the DTO list, reversing to most-recent-first for the feed. Empty input yields
// an empty (non-nil) slice so the JSON encodes `[]`, never `null`.
func toActivityEvents(events []audit.Event) []activityEventDTO {
	out := make([]activityEventDTO, 0, len(events))
	for i := len(events) - 1; i >= 0; i-- {
		out = append(out, toActivityEvent(events[i]))
	}
	return out
}

func toActivityEvent(e audit.Event) activityEventDTO {
	return activityEventDTO{
		Type:      e.Type,
		Timestamp: e.TS.UTC().Format(time.RFC3339),
		Summary:   summarizeAudit(e),
		Outcome:   deriveOutcome(e),
	}
}

// summarizeAudit renders a compact, human-readable one-line summary per event
// type, field-parallel to audit.RenderText (so the activity feed and `go-rag
// audit` carry the same operational information — T010 parity).
func summarizeAudit(e audit.Event) string {
	switch e.Type {
	case audit.TypeIngest:
		return fmt.Sprintf("%s %s: %d new, %d skipped, %d errors", e.Op, e.Path, e.New, e.Skipped, e.Errors)
	case audit.TypeQuery:
		return fmt.Sprintf("query mode=%s k=%d hits=%d", e.Mode, e.K, e.Hits)
	case audit.TypeAuthFail:
		return fmt.Sprintf("auth-fail %s: %s", e.Transport, e.Detail)
	case audit.TypeAuth:
		return fmt.Sprintf("auth %s: %s", e.Op, e.Subject)
	default:
		return e.Type
	}
}

// deriveOutcome maps the audit status to a feed outcome. ingest/query/auth
// events carry Status (ok|error) → success|failed; an auth-fail event is a
// failure by definition (it has no Status field); anything else is empty.
func deriveOutcome(e audit.Event) string {
	if e.Type == audit.TypeAuthFail {
		return "failed"
	}
	switch e.Status {
	case "ok":
		return "success"
	case "error":
		return "failed"
	}
	return ""
}
