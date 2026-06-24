package observe

// observe_test.go verifies the OTel wiring (spec 020 / T007): Init registers the
// instruments and a /metrics scrape exposes the gorag_* families after a recorded
// operation. It exercises the SAME path the daemon serves (MetricsHandler).

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/madeinoz67/go-rag/internal/config"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// TestObserve_MetricsExposeAfterRecord proves the metric surface end-to-end: Init
// (metrics on, traces off) → record a query → scrape /metrics → gorag_* families present.
func TestObserve_MetricsExposeAfterRecord(t *testing.T) {
	cfg := config.Default()
	cfg.MetricsEnabled = true
	cfg.OTelExport = "none" // no trace exporter needed for the metrics path
	if err := Init(cfg); err != nil {
		t.Fatalf("Init: %v", err)
	}
	defer Shutdown(context.Background())

	// Record a query + an ingest (exercises the histogram + counter instruments).
	RecordQuery(context.Background(), "hybrid", 23*time.Millisecond, nil)
	RecordIngest(context.Background(), "add", 8*time.Millisecond, nil, 4)

	srv := httptest.NewServer(MetricsHandler())
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/metrics")
	if err != nil {
		t.Fatalf("scrape /metrics: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	got := string(body)

	for _, want := range []string{"gorag_query_duration_seconds", "gorag_ingest_duration_seconds",
		"gorag_operations_total", "gorag_chunks_indexed_total"} {
		if !strings.Contains(got, want) {
			t.Errorf("metrics: family %q not found in /metrics scrape output", want)
		}
	}
}

// TestObserve_StartSpan_NilSafe proves StartSpan is safe to call before Init (no
// provider registered) — returns a usable span, no panic. Engine call sites invoke
// it unconditionally.
func TestObserve_StartSpan_NilSafe(t *testing.T) {
	ctx, span := StartSpan(context.Background(), SpanQuery)
	defer span.End()
	if span == nil {
		t.Fatal("StartSpan returned nil span before Init (must be a no-op span, not nil)")
	}
	_ = ctx
}

// TestObserve_SpanEmitted (US2, FR-004) proves StartSpan emits a correctly-named
// span to the configured tracer provider. The engine's Query/Ingest/Migrate call
// StartSpan with these names, so a correctly-named span here is a correctly-named
// span on the real op path. Uses an in-memory exporter + a synchronous provider.
func TestObserve_SpanEmitted(t *testing.T) {
	exp := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp))
	defer tp.Shutdown(context.Background())
	otel.SetTracerProvider(tp)

	_, span := StartSpan(context.Background(), SpanQuery, ModeAttr("hybrid"), KAttr(5))
	span.End()

	spans := exp.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("want 1 span, got %d", len(spans))
	}
	if spans[0].Name != SpanQuery {
		t.Errorf("span name = %q, want %q", spans[0].Name, SpanQuery)
	}
}

// TestObserve_AirGap_OTLPOptIn (US3, FR-005, SC-003) proves the Constitution I
// boundary for telemetry: a default config (stdout traces) makes ZERO outbound
// connections; OTLP dials out ONLY when explicitly configured (otel_export=otlp +
// endpoint). Mirrors H04's TestThreat_Import_URL_AirGap.
func TestObserve_AirGap_OTLPOptIn(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
	}))
	defer srv.Close()
	endpoint := strings.TrimPrefix(srv.URL, "http://") // host:port for otlptracehttp

	// Default (stdout) → no OTLP exporter → canary must get ZERO requests.
	cfg := config.Default()
	cfg.MetricsEnabled = false // isolate the trace path
	cfg.OTelExport = "stdout"
	if err := Init(cfg); err != nil {
		t.Fatalf("Init stdout: %v", err)
	}
	_, span := StartSpan(context.Background(), SpanQuery, ModeAttr("hybrid"))
	span.End()
	_ = Shutdown(context.Background()) // flushes the (local) stdout batcher
	if h := hits.Load(); h != 0 {
		t.Errorf("default config: canary got %d hits (want 0 — no telemetry egress)", h)
	}

	// Opt-in OTLP → the OTLP exporter dials the canary; Shutdown flushes spans to it.
	cfg.OTelExport = "otlp"
	cfg.OTelEndpoint = endpoint
	if err := Init(cfg); err != nil {
		t.Fatalf("Init otlp: %v", err)
	}
	_, span2 := StartSpan(context.Background(), SpanQuery, ModeAttr("hybrid"))
	span2.End()
	_ = Shutdown(context.Background()) // flushes the OTLP batcher → canary
	if h := hits.Load(); h == 0 {
		t.Error("otlp config: canary got 0 hits (want >0 — OTLP is the explicit egress)")
	}
}
