package ui

// observability.go (spec 054) is the console's Observability surface — the
// telemetry + audit-forensics view. It deliberately does NOT duplicate Bridge
// Ops (spec 049): 049 owns live operational health (StatusInfo) + a small
// recent-activity tail; this view owns (a) in-browser telemetry projected from
// the registered prometheus instruments (spec 020) and (b) a filterable
// full-trail audit-log browser (spec 021). UI-only: no new engine capability,
// no new transport, no Node chain.
//
// Routes (all spec 045 Bearer-guarded):
//
//	GET /api/observability/metrics   → telemetry snapshot (process-wide) (US1)
//	GET /api/observability/audit     → filtered audit page (process-wide) (US2)
//
// Telemetry is read via prometheus.DefaultGatherer.Gather() (structured
// MetricFamily) — the same instruments /metrics exposes — so the UI is a
// projection of the same source, never a parallel computation (zero drift,
// FR-009). Audit is read via the existing read-only Engine.AuditRead (spec 021).
//
// BOTH surfaces are process-wide: under spec 052 one daemon serves every vault,
// and the audit log is a single file at the unified-store root (Engine.AuditRead
// ignores the vault arg). The UI labels them process-wide so the operator is not
// misled into expecting per-vault scoping.

import (
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/madeinoz67/go-rag/internal/audit"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

// --- telemetry DTOs (US1) ---

// telemetryResponse is the GET /api/observability/metrics payload. Telemetry is
// process-wide (one daemon serves every vault under spec 052).
type telemetryResponse struct {
	ProcessWide bool        `json:"process_wide"`
	FreshAt     string      `json:"fresh_at"`
	Operations  []opStat    `json:"operations"`
	Cache       cacheStat   `json:"cache"`
	ErrorRate   float64     `json:"error_rate"`
	Posture     postureNote `json:"posture"`
}

// opStat is one operation's rolled-up telemetry: total count, error count, and
// the p50/p95/p99 latency (seconds, from the duration histograms). Percentiles
// are -1 when there are too few samples to compute them.
type opStat struct {
	Op     string  `json:"op"`
	Count  uint64  `json:"count"`
	Errors uint64  `json:"errors"`
	P50    float64 `json:"p50"`
	P95    float64 `json:"p95"`
	P99    float64 `json:"p99"`
}

type cacheStat struct {
	Result    hitMiss `json:"result"`
	Embedding hitMiss `json:"embedding"`
}

// hitMiss is a cache's hit/miss tally + the hit rate (hits / (hits+misses); 0
// when there are no events).
type hitMiss struct {
	Hits   uint64  `json:"hits"`
	Misses uint64  `json:"misses"`
	Rate   float64 `json:"rate"`
}

// postureNote is the trust label (US3): metrics are local-only (zero egress,
// Constitution I), the audit log is local + append-only, query text is hashed,
// plus the audit-enabled flag + the rotation cap.
type postureNote struct {
	MetricsLocal   bool `json:"metrics_local"`
	AuditLocal     bool `json:"audit_local"`
	QueryHashed    bool `json:"query_hashed"`
	AuditEnabled   bool `json:"audit_enabled"`
	RetentionBytes int  `json:"retention_bytes"`
}

// --- audit DTOs (US2) ---

// auditPageResponse is the GET /api/observability/audit payload: a bounded,
// newest-first page of audit events plus the filter echo and the audit-enabled
// flag (false → the UI shows "audit logging is off", not an empty table).
type auditPageResponse struct {
	Events    []auditEventDTO `json:"events"`
	Type      string          `json:"type"`
	Since     string          `json:"since"`
	Limit     int             `json:"limit"`
	Truncated bool            `json:"truncated"`
	Enabled   bool            `json:"enabled"`
}

// auditEventDTO projects audit.Event field-for-field. The source Event carries
// only QueryHash (never the raw query), so this DTO cannot leak plaintext
// (FR-003). Timestamp is RFC3339 UTC.
type auditEventDTO struct {
	Type      string `json:"type"`
	Timestamp string `json:"timestamp"`

	// query
	QueryHash string `json:"query_hash,omitempty"`
	Mode      string `json:"mode,omitempty"`
	K         int    `json:"k,omitempty"`
	Hits      int    `json:"hits,omitempty"`

	// query + ingest
	Status string `json:"status,omitempty"`

	// ingest
	Op      string `json:"op,omitempty"`
	Path    string `json:"path,omitempty"`
	New     int    `json:"new,omitempty"`
	Skipped int    `json:"skipped,omitempty"`
	Errors  int    `json:"errors,omitempty"`

	// auth-fail
	Transport string `json:"transport,omitempty"`
	Detail    string `json:"detail,omitempty"`
}

// histoBucket is one cumulative Prometheus histogram bucket (upper bound →
// cumulative count). Slices are kept sorted ascending by bound for quantile
// interpolation. Merged across label permutations before quantile computation.
type histoBucket struct {
	bound float64
	count uint64
}

// mergedHist is a label-grouped histogram reduced to sorted cumulative buckets
// + the group's total sample count.
type mergedHist struct {
	buckets []histoBucket
	total   uint64
}

// handleObservabilityMetrics — US1 telemetry snapshot. Process-wide. Always
// 200; cold/absent instruments project as zero-valued fields (a healthy "no
// data yet" state, never an error — FR-008).
func (s *Server) handleObservabilityMetrics(w http.ResponseWriter, _ *http.Request) {
	cfg := s.eng.Config()
	writeJSON(w, http.StatusOK, gatherTelemetry(cfg.EffectiveAuditLogEnabled(), cfg.EffectiveAuditLogMaxBytes()))
}

// handleObservabilityAudit — US2 filtered audit page. Process-wide (the engine
// audit log is a single file; the vault query param is accepted for
// forward-compat but currently a no-op). type ∈ {""|query|ingest|auth-fail};
// since is a Go duration (e.g. "24h"; absent/invalid/<=0 = all time); limit is
// a page size (default 50, clamped [1,200]). Events are newest-first. A
// missing/disabled log yields {events:[], enabled:<flag>} — a healthy state,
// never an error. 400 on an invalid type.
func (s *Server) handleObservabilityAudit(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	etype := q.Get("type")
	if etype != "" && !validAuditType(etype) {
		writeError(w, http.StatusBadRequest, "invalid type")
		return
	}
	limit := clampAuditLimit(q.Get("limit"))
	since := q.Get("since")
	opts := audit.ReadOptions{Type: etype, Tail: limit}
	if d, err := time.ParseDuration(since); err == nil && d > 0 {
		opts.Since = d
	}
	events, err := s.eng.AuditRead(vaultFromRequest(r), opts)
	if err != nil {
		writeEngineErr(w, err)
		return
	}
	dtos := toAuditEventDTOs(events)
	writeJSON(w, http.StatusOK, auditPageResponse{
		Events:    dtos,
		Type:      etype,
		Since:     since,
		Limit:     limit,
		Truncated: len(dtos) >= limit,
		Enabled:   s.eng.Config().EffectiveAuditLogEnabled(),
	})
}

// validAuditType accepts the three event types the browser filters on. "auth"
// (spec 045 management ops) is excluded from the operator browser by design.
func validAuditType(t string) bool {
	switch t {
	case audit.TypeQuery, audit.TypeIngest, audit.TypeAuthFail:
		return true
	}
	return false
}

// clampAuditLimit parses limit, clamping to [1,200] with a default of 50. An
// absent, non-numeric, or non-positive value yields the default (never an
// unbounded all-read).
func clampAuditLimit(raw string) int {
	const def, lo, hi = 50, 1, 200
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

// toAuditEventDTOs projects audit events (oldest→newest from audit.Read) into
// the DTO list, newest-first. Empty input yields an empty (non-nil) slice so
// JSON encodes `[]`, never `null`.
func toAuditEventDTOs(events []audit.Event) []auditEventDTO {
	out := make([]auditEventDTO, 0, len(events))
	for i := len(events) - 1; i >= 0; i-- {
		e := events[i]
		out = append(out, auditEventDTO{
			Type:      e.Type,
			Timestamp: e.TS.UTC().Format(time.RFC3339),
			QueryHash: e.QueryHash,
			Mode:      e.Mode,
			K:         e.K,
			Hits:      e.Hits,
			Status:    e.Status,
			Op:        e.Op,
			Path:      e.Path,
			New:       e.New,
			Skipped:   e.Skipped,
			Errors:    e.Errors,
			Transport: e.Transport,
			Detail:    e.Detail,
		})
	}
	return out
}

// --- telemetry projection ---

// family resolves a metric family by base name, tolerating the prometheus
// _total (counters) / _seconds (histograms) suffix convention so the projection
// is robust to the exact name the OTel→prometheus exporter emits. This is the
// mitigation for the metric-name-drift risk noted in plan.md §Risks.
func family(idx map[string]*dto.MetricFamily, base string) *dto.MetricFamily {
	if f, ok := idx[base]; ok {
		return f
	}
	for _, sfx := range []string{"_total", "_seconds"} {
		if f, ok := idx[base+sfx]; ok {
			return f
		}
		if trimmed := strings.TrimSuffix(base, sfx); trimmed != base {
			if f, ok := idx[trimmed]; ok {
				return f
			}
		}
	}
	return nil
}

// gatherTelemetry reads the registered prometheus instruments (structured
// gather) and projects them into the telemetry DTO. auditEnabled +
// retentionBytes supply the posture label (passed as scalars so the projection
// is unit-testable without a concrete config).
func gatherTelemetry(auditEnabled bool, retentionBytes int) telemetryResponse {
	fams, _ := prometheus.DefaultGatherer.Gather()
	idx := make(map[string]*dto.MetricFamily, len(fams))
	for _, f := range fams {
		idx[f.GetName()] = f
	}
	ops := projectOperations(idx)
	if len(ops) == 0 {
		ops = []opStat{{Op: "query", P50: -1, P95: -1, P99: -1}} // stable query tile on a cold daemon
	}
	resp := telemetryResponse{
		ProcessWide: true,
		FreshAt:     time.Now().UTC().Format(time.RFC3339),
		Operations:  ops,
		Cache:       projectCache(idx),
		Posture: postureNote{
			MetricsLocal:   true,
			AuditLocal:     true,
			QueryHashed:    true,
			AuditEnabled:   auditEnabled,
			RetentionBytes: retentionBytes,
		},
	}
	var total, errs uint64
	for _, o := range ops {
		total += o.Count
		errs += o.Errors
	}
	if total > 0 {
		resp.ErrorRate = float64(errs) / float64(total)
	}
	return resp
}

// projectOperations builds the per-operation telemetry from the gorag_operations
// counter (count + errors by op label) overlaid with latency percentiles from
// the query/ingest duration histograms. "query" always sorts first.
func projectOperations(idx map[string]*dto.MetricFamily) []opStat {
	byOp := map[string]*opStat{}
	ensure := func(op string) *opStat {
		if s, ok := byOp[op]; ok {
			return s
		}
		s := &opStat{Op: op, P50: -1, P95: -1, P99: -1}
		byOp[op] = s
		return s
	}
	if f := family(idx, "gorag_operations_total"); f != nil {
		for _, m := range f.GetMetric() {
			op := labelValue(m, "op")
			if op == "" {
				continue
			}
			v := uint64(m.GetCounter().GetValue())
			st := ensure(op)
			st.Count += v
			if labelValue(m, "status") == "error" {
				st.Errors += v
			}
		}
	}
	if f := family(idx, "gorag_query_duration_seconds"); f != nil {
		merged := mergeBuckets(f.GetMetric())
		q := ensure("query")
		q.P50 = percentileFromBuckets(merged.buckets, merged.total, 0.5)
		q.P95 = percentileFromBuckets(merged.buckets, merged.total, 0.95)
		q.P99 = percentileFromBuckets(merged.buckets, merged.total, 0.99)
	}
	if f := family(idx, "gorag_ingest_duration_seconds"); f != nil {
		for op, g := range groupHistogramByLabel(f.GetMetric(), "op") {
			st := ensure(op)
			st.P50 = percentileFromBuckets(g.buckets, g.total, 0.5)
			st.P95 = percentileFromBuckets(g.buckets, g.total, 0.95)
			st.P99 = percentileFromBuckets(g.buckets, g.total, 0.99)
		}
	}
	out := make([]opStat, 0, len(byOp))
	if q, ok := byOp["query"]; ok {
		out = append(out, *q)
		delete(byOp, "query")
	}
	keys := make([]string, 0, len(byOp))
	for k := range byOp {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		out = append(out, *byOp[k])
	}
	return out
}

// projectCache tallies the result + embedding caches from the hit/miss counters.
func projectCache(idx map[string]*dto.MetricFamily) cacheStat {
	return cacheStat{
		Result:    projectHitMiss(idx, "result"),
		Embedding: projectHitMiss(idx, "embedding"),
	}
}

func projectHitMiss(idx map[string]*dto.MetricFamily, cache string) hitMiss {
	hm := hitMiss{
		Hits:   sumCounterByLabel(family(idx, "gorag_cache_hits_total"), "cache", cache),
		Misses: sumCounterByLabel(family(idx, "gorag_cache_misses_total"), "cache", cache),
	}
	if hm.Hits+hm.Misses > 0 {
		hm.Rate = float64(hm.Hits) / float64(hm.Hits+hm.Misses)
	}
	return hm
}

func sumCounterByLabel(f *dto.MetricFamily, name, value string) uint64 {
	if f == nil {
		return 0
	}
	var sum uint64
	for _, m := range f.GetMetric() {
		if labelValue(m, name) == value {
			sum += uint64(m.GetCounter().GetValue())
		}
	}
	return sum
}

// mergeBuckets sums a histogram family's cumulative buckets + sample counts
// across all label permutations into one merged distribution.
func mergeBuckets(metrics []*dto.Metric) mergedHist {
	bm := map[float64]uint64{}
	var total uint64
	for _, m := range metrics {
		h := m.GetHistogram()
		if h == nil {
			continue
		}
		total += h.GetSampleCount()
		for _, b := range h.GetBucket() {
			bm[b.GetUpperBound()] += b.GetCumulativeCount()
		}
	}
	return mergedHist{buckets: toSortedBuckets(bm), total: total}
}

// groupHistogramByLabel reduces a histogram family into per-label-value merged
// distributions (e.g. ingest latency grouped by the "op" label).
func groupHistogramByLabel(metrics []*dto.Metric, label string) map[string]mergedHist {
	groups := map[string]map[float64]uint64{}
	totals := map[string]uint64{}
	for _, m := range metrics {
		v := labelValue(m, label)
		if v == "" {
			continue
		}
		h := m.GetHistogram()
		if h == nil {
			continue
		}
		g, ok := groups[v]
		if !ok {
			g = map[float64]uint64{}
			groups[v] = g
		}
		totals[v] += h.GetSampleCount()
		for _, b := range h.GetBucket() {
			g[b.GetUpperBound()] += b.GetCumulativeCount()
		}
	}
	out := make(map[string]mergedHist, len(groups))
	for v, gmap := range groups {
		out[v] = mergedHist{buckets: toSortedBuckets(gmap), total: totals[v]}
	}
	return out
}

func toSortedBuckets(m map[float64]uint64) []histoBucket {
	keys := make([]float64, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Float64s(keys)
	out := make([]histoBucket, 0, len(keys))
	for _, k := range keys {
		out = append(out, histoBucket{bound: k, count: m[k]})
	}
	return out
}

// percentileFromBuckets computes the q-quantile (0..1) from a cumulative
// histogram via standard Prometheus histogram_quantile linear interpolation.
// Returns -1 when there are too few samples (<2) or no buckets. buckets MUST be
// sorted ascending by bound (cumulative counts).
func percentileFromBuckets(buckets []histoBucket, total uint64, q float64) float64 {
	if len(buckets) == 0 || total < 2 || q < 0 || q > 1 {
		return -1
	}
	target := q * float64(total)
	for i, b := range buckets {
		if float64(b.count) < target {
			continue
		}
		var loBound float64
		var loCount uint64
		if i > 0 {
			loBound = buckets[i-1].bound
			loCount = buckets[i-1].count
		}
		if math.IsInf(b.bound, 1) {
			return loBound // cannot interpolate to +Inf; return the prior finite bound
		}
		if b.count == loCount {
			return b.bound
		}
		return loBound + (b.bound-loBound)*(target-float64(loCount))/float64(b.count-loCount)
	}
	last := buckets[len(buckets)-1].bound
	if math.IsInf(last, 1) && len(buckets) > 1 {
		last = buckets[len(buckets)-2].bound
	}
	return last
}

// labelValue returns the value of a label on a metric sample, or "" if absent.
func labelValue(m *dto.Metric, name string) string {
	for _, lp := range m.GetLabel() {
		if lp.GetName() == name {
			return lp.GetValue()
		}
	}
	return ""
}
