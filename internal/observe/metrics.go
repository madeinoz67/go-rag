package observe

// metrics.go defines the OTel instruments (contracts/metrics.md inventory) and the
// one-liner record helpers the engine calls. Instruments are created once in Init;
// before Init (or with metrics off) the handles are nil and the helpers no-op, so
// callers always invoke them unconditionally. Tie-in counters (H04/H06) are recorded
// at a SINGLE instrumentation point per event — no double-counting.

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

var (
	queryDuration  metric.Float64Histogram
	ingestDuration metric.Float64Histogram
	queryResults   metric.Float64Histogram
	operations     metric.Int64Counter
	chunksIndexed  metric.Int64Counter
	poisonFlagged  metric.Int64Counter
	cacheHits      metric.Int64Counter
	cacheMisses    metric.Int64Counter
)

// Latency-histogram buckets tuned to the budgets (research D3): query p50<1s/p99<3s;
// ingest dominated by the <10ms ACK.
var (
	queryBuckets  = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5}
	ingestBuckets = []float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1, 5}
)

// registerInstruments creates the metric handles from the (already-set) meter. Errors
// here are programming bugs (duplicate name / bad buckets) — surfaced at boot.
func registerInstruments(m metric.Meter) {
	queryDuration, _ = m.Float64Histogram("gorag.query.duration",
		metric.WithUnit("s"), metric.WithExplicitBucketBoundaries(queryBuckets...))
	ingestDuration, _ = m.Float64Histogram("gorag.ingest.duration",
		metric.WithUnit("s"), metric.WithExplicitBucketBoundaries(ingestBuckets...))
	queryResults, _ = m.Float64Histogram("gorag.query.results")
	operations, _ = m.Int64Counter("gorag.operations")
	chunksIndexed, _ = m.Int64Counter("gorag.chunks_indexed")
	poisonFlagged, _ = m.Int64Counter("gorag.poison_flagged")
	cacheHits, _ = m.Int64Counter("gorag.cache_hits")
	cacheMisses, _ = m.Int64Counter("gorag.cache_misses")
}

func statusAttr(err error) attribute.KeyValue {
	v := "ok"
	if err != nil {
		v = "error"
	}
	return attribute.String("status", v)
}

// RecordQuery records query latency + the op counter. No-op before Init / metrics off.
func RecordQuery(ctx context.Context, mode string, dur time.Duration, err error) {
	st := statusAttr(err)
	if queryDuration != nil {
		queryDuration.Record(ctx, dur.Seconds(), metric.WithAttributes(st, attribute.String("mode", mode)))
	}
	if operations != nil {
		operations.Add(ctx, 1, metric.WithAttributes(st, attribute.String("op", "query")))
	}
}

// RecordIngest records ingest/migrate latency + the op counter (+ chunks indexed).
func RecordIngest(ctx context.Context, op string, dur time.Duration, err error, chunks int) {
	st := statusAttr(err)
	if ingestDuration != nil {
		ingestDuration.Record(ctx, dur.Seconds(), metric.WithAttributes(st, attribute.String("op", op)))
	}
	if operations != nil {
		operations.Add(ctx, 1, metric.WithAttributes(st, attribute.String("op", op)))
	}
	if chunksIndexed != nil && chunks > 0 {
		chunksIndexed.Add(ctx, int64(chunks))
	}
}

// RecordQueryResults records the top-k returned by a query.
func RecordQueryResults(ctx context.Context, mode string, count int) {
	if queryResults != nil {
		queryResults.Record(ctx, float64(count), metric.WithAttributes(attribute.String("mode", mode)))
	}
}

// --- tie-in counters (H04 poisoning, H06 cache). One record per event. ---

// PoisonFlagged records a chunk newly flagged at the given level (H04).
func PoisonFlagged(ctx context.Context, level string) {
	if poisonFlagged != nil {
		poisonFlagged.Add(ctx, 1, metric.WithAttributes(attribute.String("level", level)))
	}
}

// CacheHit / CacheMiss record a query-cache event (cache: result|embedding) (H06).
func CacheHit(ctx context.Context, cache string) {
	if cacheHits != nil {
		cacheHits.Add(ctx, 1, metric.WithAttributes(attribute.String("cache", cache)))
	}
}

func CacheMiss(ctx context.Context, cache string) {
	if cacheMisses != nil {
		cacheMisses.Add(ctx, 1, metric.WithAttributes(attribute.String("cache", cache)))
	}
}
