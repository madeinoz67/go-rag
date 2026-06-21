package eval

import (
	"fmt"
	"strings"
)

// DefaultTargets are the book's ch010 / App.C retrieval-quality targets
// (recall@10 > 0.80, precision@5 > 0.70, MRR > 0.60, NDCG@10 > 0.75). They are
// informational reference lines, NOT the gate (research.md D7).
var DefaultTargets = MetricSet{RecallAt10: 0.80, PrecisionAt5: 0.70, MRR: 0.60, NDCGAt10: 0.75}

// FormatRun renders an EvaluationRun (and optional Comparison) as the canonical
// text block shared by the `go-rag eval` CLI and the `go_rag_eval` MCP tool, so
// the two surfaces report identical numbers (Principle V parity).
func FormatRun(run *EvaluationRun, cmp *Comparison, tolerance float64) string {
	var b strings.Builder
	fmt.Fprintf(&b, "mode=%s  embedder=%s  retrieval=%s  k=%d\n", run.Mode, run.Embedder, run.RetrievalMode, run.K)
	fmt.Fprintf(&b, "queries: scored=%d skipped=%d\n\n", run.QueriesRun, run.QueriesSkipped)
	writeMetric(&b, "recall@5    ", run.Metrics.RecallAt5, DefaultTargets.RecallAt5)
	writeMetric(&b, "recall@10   ", run.Metrics.RecallAt10, DefaultTargets.RecallAt10)
	writeMetric(&b, "precision@5 ", run.Metrics.PrecisionAt5, DefaultTargets.PrecisionAt5)
	writeMetric(&b, "mrr         ", run.Metrics.MRR, DefaultTargets.MRR)
	writeMetric(&b, "ndcg@10     ", run.Metrics.NDCGAt10, DefaultTargets.NDCGAt10)
	b.WriteString("\n")
	if cmp != nil {
		if cmp.Pass {
			fmt.Fprintf(&b, "verdict: PASS (recall@10 drop %.2fpt <= tolerance %.2fpt)\n", cmp.RecallAt10Drop, tolerance)
		} else {
			fmt.Fprintf(&b, "verdict: FAIL (recall@10 drop %.2fpt > tolerance %.2fpt)\n", cmp.RecallAt10Drop, tolerance)
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func writeMetric(b *strings.Builder, name string, val, target float64) {
	if target > 0 {
		mark := ""
		if val >= target {
			mark = " ok"
		}
		fmt.Fprintf(b, "  %s: %.3f   (target %.2f)%s\n", name, val, target, mark)
	} else {
		fmt.Fprintf(b, "  %s: %.3f\n", name, val)
	}
}
