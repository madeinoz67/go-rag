package observe

// spans.go provides the tracing entry point. StartSpan returns a no-op span when no
// tracer provider is registered (tracing off), so engine call sites invoke it
// unconditionally without nil checks.

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// Span names — single source of truth (contracts/metrics.md §3).
const (
	SpanQuery   = "gorag.Query"
	SpanIngest  = "gorag.Ingest"
	SpanMigrate = "gorag.Migrate"
)

// StartSpan starts a tracing span (no-op when tracing is off). Always returns a
// non-nil span; callers MUST defer-end it.
func StartSpan(ctx context.Context, name string, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	return otel.Tracer("gorag").Start(ctx, name, trace.WithAttributes(attrs...))
}

// Attribute helpers — keep OTel's attribute package inside observe so callers
// (the engine) never import OTel directly (Constitution V: only observe imports OTel).
func ModeAttr(v string) attribute.KeyValue { return attribute.String("mode", v) }
func OpAttr(v string) attribute.KeyValue   { return attribute.String("op", v) }
func KAttr(k int) attribute.KeyValue       { return attribute.Int("k", k) }

// SpanError records an error on a span and sets its status (call before End).
func SpanError(span trace.Span, err error) {
	if span == nil || err == nil {
		return
	}
	span.SetStatus(codes.Error, err.Error())
	span.RecordError(err)
}
