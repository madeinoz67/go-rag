package ui

// observability_test.go (spec 054) proves the telemetry surface (US1), the
// posture label (US3), and the guarded/process-wide invariants (US4). The
// projection math is unit-tested hermetically against synthetic prometheus
// registries (private registries — no DefaultRegisterer pollution), and the
// histogram-quantile helper is pure. Handler tests cover the 200/cold-shape +
// 401 ungated behaviour over the spec 045 authed test server.

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

// syntheticFamilies builds a private prometheus registry, lets the caller
// register instruments + record values, then gathers into the same
// name→MetricFamily index gatherTelemetry builds from the default gatherer.
// Private registry = zero leakage into other tests' DefaultGatherer view.
func syntheticFamilies(t *testing.T, register func(*prometheus.Registry)) map[string]*dto.MetricFamily {
	t.Helper()
	reg := prometheus.NewRegistry()
	register(reg)
	fams, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather synthetic registry: %v", err)
	}
	idx := make(map[string]*dto.MetricFamily, len(fams))
	for _, f := range fams {
		idx[f.GetName()] = f
	}
	return idx
}

// --- pure unit tests (T005) ---

// TestPercentileFromBuckets pins the histogram-quantile interpolation + the
// "insufficient data → -1" rule (the cold/low-sample guard, FR-008).
func TestPercentileFromBuckets(t *testing.T) {
	// buckets cumulative: le=0.1→0, le=0.5→10, le=1→10, le=2.5→10, +Inf→10
	buckets := []histoBucket{{0.1, 0}, {0.5, 10}, {1, 10}, {2.5, 10}}
	const total uint64 = 10
	// p50 target=5 → crosses at le=0.5 (cum 10); interpolate 0.1+(0.5-0.1)*5/10 = 0.3
	if p := percentileFromBuckets(buckets, total, 0.5); p < 0.25 || p > 0.35 {
		t.Errorf("p50 = %v, want ~0.3", p)
	}
	// p95 target=9.5 → crosses at le=0.5; 0.1+0.4*9.5/10 = 0.48
	if p := percentileFromBuckets(buckets, total, 0.95); p < 0.43 || p > 0.53 {
		t.Errorf("p95 = %v, want ~0.48", p)
	}
	// insufficient samples
	if p := percentileFromBuckets(buckets, 1, 0.5); p != -1 {
		t.Errorf("total<2 → want -1, got %v", p)
	}
	if p := percentileFromBuckets(nil, total, 0.5); p != -1 {
		t.Errorf("no buckets → want -1, got %v", p)
	}
	// all in the first bucket → the prior-bound interpolation still yields a
	// finite value inside [0, bound]; never -1 when total>=2.
	allFirst := []histoBucket{{0.5, 10}, {1, 10}}
	if p := percentileFromBuckets(allFirst, 10, 0.5); p < 0 || p > 0.5 {
		t.Errorf("all-in-first-bucket p50 = %v, want within [0,0.5]", p)
	}
}

// TestProjectOperations_Synthetic: counts + errors come from the operations
// counter (by op + status); query latency percentiles come from the query
// duration histogram merged across labels.
func TestProjectOperations_Synthetic(t *testing.T) {
	idx := syntheticFamilies(t, func(reg *prometheus.Registry) {
		ops := prometheus.NewCounterVec(prometheus.CounterOpts{Name: "gorag_operations"}, []string{"op", "status"})
		reg.MustRegister(ops)
		ops.WithLabelValues("query", "ok").Add(8)
		ops.WithLabelValues("query", "error").Add(2)
		qd := prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "gorag_query_duration_seconds",
			Buckets: []float64{0.1, 0.5, 1, 2.5},
		}, []string{"mode", "status"})
		reg.MustRegister(qd)
		for i := 0; i < 10; i++ {
			qd.WithLabelValues("hybrid", "ok").Observe(0.2)
		}
	})
	got := projectOperations(idx)
	if len(got) == 0 || got[0].Op != "query" {
		t.Fatalf("want query first, got %+v", got)
	}
	q := got[0]
	if q.Count != 10 {
		t.Errorf("query count = %d, want 10", q.Count)
	}
	if q.Errors != 2 {
		t.Errorf("query errors = %d, want 2", q.Errors)
	}
	if q.P50 < 0.2 || q.P50 > 0.4 {
		t.Errorf("query p50 = %v, want ~0.3", q.P50)
	}
	if q.P99 < 0.4 || q.P99 > 0.5 {
		t.Errorf("query p99 = %v, want ~0.49", q.P99)
	}
}

// TestProjectOperations_Cold: no families → empty (caller seeds the stable
// query tile).
func TestProjectOperations_Cold(t *testing.T) {
	if got := projectOperations(map[string]*dto.MetricFamily{}); len(got) != 0 {
		t.Errorf("cold projectOperations = %+v, want empty", got)
	}
}

// TestProjectCache_Synthetic: hit/miss tallied per cache kind + rate derived.
func TestProjectCache_Synthetic(t *testing.T) {
	idx := syntheticFamilies(t, func(reg *prometheus.Registry) {
		hits := prometheus.NewCounterVec(prometheus.CounterOpts{Name: "gorag_cache_hits"}, []string{"cache"})
		miss := prometheus.NewCounterVec(prometheus.CounterOpts{Name: "gorag_cache_misses"}, []string{"cache"})
		reg.MustRegister(hits, miss)
		hits.WithLabelValues("result").Add(7)
		miss.WithLabelValues("result").Add(3)
		hits.WithLabelValues("embedding").Add(4)
		miss.WithLabelValues("embedding").Add(0)
	})
	c := projectCache(idx)
	if c.Result.Hits != 7 || c.Result.Misses != 3 {
		t.Errorf("result cache = %+v, want hits=7 misses=3", c.Result)
	}
	if c.Result.Rate < 0.69 || c.Result.Rate > 0.71 {
		t.Errorf("result rate = %v, want ~0.7", c.Result.Rate)
	}
	if c.Embedding.Hits != 4 || c.Embedding.Misses != 0 {
		t.Errorf("embedding cache = %+v, want hits=4 misses=0", c.Embedding)
	}
	if c.Embedding.Rate != 1.0 {
		t.Errorf("embedding rate = %v, want 1.0", c.Embedding.Rate)
	}
}

// --- gatherTelemetry posture (US3 / T009) ---

func TestGatherTelemetry_Posture(t *testing.T) {
	resp := gatherTelemetry(true, 16*1024*1024)
	if !resp.ProcessWide {
		t.Error("ProcessWide must be true (one daemon, all vaults)")
	}
	p := resp.Posture
	if !p.MetricsLocal || !p.AuditLocal || !p.QueryHashed {
		t.Errorf("posture = %+v, want all-local + hashed true", p)
	}
	if !p.AuditEnabled {
		t.Error("AuditEnabled must reflect the passed flag (true)")
	}
	if p.RetentionBytes != 16*1024*1024 {
		t.Errorf("RetentionBytes = %d, want 16MiB", p.RetentionBytes)
	}
	// a cold gather still yields a stable query tile (FR-008 healthy zero state).
	if len(resp.Operations) == 0 || resp.Operations[0].Op != "query" {
		t.Errorf("cold Operations = %+v, want a leading query tile", resp.Operations)
	}
}

// --- handler wiring (US1 shape + US4 guarded) ---

// TestObservabilityMetrics_Handler200: the metrics route is guarded + returns
// the telemetry shape. Cold (no recorded ops) is a healthy 200, not an error.
func TestObservabilityMetrics_Handler200(t *testing.T) {
	eng := newWriteTestEngine(t)
	srvURL, tok := authedDocServer(t, eng)

	resp := bearerGet(t, srvURL+"/api/observability/metrics", tok)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("metrics: status %d, want 200", resp.StatusCode)
	}
	var tr telemetryResponse
	if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
		t.Fatalf("decode telemetry: %v", err)
	}
	if !tr.ProcessWide {
		t.Error("response must be labelled process_wide")
	}
	if tr.Operations == nil {
		t.Error("response must include operations (even if cold/zero-valued)")
	}
}

// TestObservabilityMetrics_401Unguarded: no Bearer → 401 (US4 / FR-006).
func TestObservabilityMetrics_401Unguarded(t *testing.T) {
	eng := newWriteTestEngine(t)
	srvURL, _ := authedDocServer(t, eng)
	resp := bearerGet(t, srvURL+"/api/observability/metrics", "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("metrics without bearer: status %d, want 401", resp.StatusCode)
	}
}
