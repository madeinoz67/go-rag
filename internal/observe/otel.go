// Package observe is go-rag's OpenTelemetry wiring (spec 020 / audit H17) — the
// ONLY package that imports the OTel SDK/exporters. Every other package calls the
// small helper API here (StartSpan, RecordQuery, MetricsHandler), keeping OTel
// vendor coupling in one place (Constitution V — mirrors how internal/poison
// isolated the detector).
//
// AIR-GAP (Constitution I, the decisive constraint): the trace exporter is LOCAL by
// default (stderr); the OTLP exporter — the only telemetry egress in go-rag — is
// constructed ONLY when the user sets otel_export=otlp + otel_endpoint. The /metrics
// endpoint is scraped (pulled) by the user's collector; go-rag never dials out for
// metrics. An air-gap test (US3) asserts zero outbound connections otherwise.
package observe

import (
	"context"
	"os"

	"github.com/madeinoz67/go-rag/internal/config"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

var (
	globalTP *sdktrace.TracerProvider // nil when traces off (none/no Init)
	globalMP *sdkmetric.MeterProvider // nil when metrics disabled
)

// Init builds and registers the global TracerProvider + MeterProvider from config.
// Idempotent; call Shutdown(ctx) on daemon stop to flush in-flight batches.
func Init(cfg config.Config) error {
	ctx := context.Background()
	res, err := resource.New(ctx, resource.WithAttributes(
		semconv.ServiceName("gorag"),
		semconv.ServiceVersion("0.1.0"),
	))
	if err != nil {
		return err
	}

	// --- traces: LOCAL by default; OTLP is opt-in (the only egress) ---
	switch cfg.EffectiveOTelExport() {
	case "stdout":
		exp, err := stdouttrace.New(stdouttrace.WithWriter(os.Stderr))
		if err != nil {
			return err
		}
		globalTP = sdktrace.NewTracerProvider(
			sdktrace.WithBatcher(exp),
			sdktrace.WithResource(res),
		)
	case "otlp":
		// OPT-IN remote — the ONLY telemetry egress in go-rag (Constitution I).
		// Constructed solely here, only when the user explicitly configures it.
		exp, err := otlptracehttp.New(ctx,
			otlptracehttp.WithEndpoint(cfg.OTelEndpoint),
			otlptracehttp.WithInsecure(), // local collector default; TLS is a future concern
		)
		if err != nil {
			return err
		}
		globalTP = sdktrace.NewTracerProvider(
			sdktrace.WithBatcher(exp),
			sdktrace.WithResource(res),
		)
	case "none":
		// no exporter — spans are no-ops (context still flows)
	}
	if globalTP != nil {
		otel.SetTracerProvider(globalTP)
	}

	// --- metrics: scraped /metrics (always when enabled) ---
	if cfg.EffectiveMetricsEnabled() {
		promExp, err := prometheus.New() // registers with prometheus.DefaultRegisterer
		if err != nil {
			return err
		}
		globalMP = sdkmetric.NewMeterProvider(sdkmetric.WithReader(promExp))
		otel.SetMeterProvider(globalMP)
		registerInstruments(otel.Meter("gorag"))
	}
	return nil
}

// Shutdown flushes + shuts the providers. Call on daemon stop so in-flight spans
// drain. Safe to call when Init was never called (no-op).
func Shutdown(ctx context.Context) error {
	var err error
	if globalTP != nil {
		if e := globalTP.Shutdown(ctx); e != nil {
			err = e
		}
		globalTP = nil
	}
	if globalMP != nil {
		if e := globalMP.Shutdown(ctx); e != nil {
			err = e
		}
		globalMP = nil
	}
	return err
}
