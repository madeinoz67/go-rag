package observe

// prometheus.go serves the scraped /metrics endpoint. The OTel prometheus exporter
// (initialized in Init) registers its instruments with prometheus.DefaultRegisterer,
// so promhttp.Handler() renders them in standard Prometheus text — pulled by the
// user's collector (go-rag never dials out for metrics; Constitution I).

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// MetricsHandler serves GET /metrics (Prometheus text exposition). Render reflects
// every gorag_* instrument registered in Init.
func MetricsHandler() http.Handler {
	return promhttp.Handler()
}
