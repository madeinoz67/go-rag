package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/madeinoz67/go-rag/internal/config"
	"github.com/madeinoz67/go-rag/internal/embed"
	"github.com/madeinoz67/go-rag/internal/engine"
	"github.com/madeinoz67/go-rag/internal/eval"
	"github.com/madeinoz67/go-rag/internal/eval/beir"
	"github.com/madeinoz67/go-rag/internal/storage"
	"github.com/spf13/cobra"
)

func newEvalCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "eval",
		Short: "Measure retrieval quality (recall@k, precision@k, MRR, NDCG@k) over a golden dataset",
		Long: `Run go-rag's retrieval-quality evaluation harness.

By default it self-provisions a throwaway vault from the golden corpus using a
deterministic offline embedder (no Ollama, fully reproducible), scores the
committed golden dataset, and prints recall@5/10, precision@5, MRR, and NDCG@10.

Pass --baseline to fail (non-zero exit) when recall@10 regresses beyond
--tolerance versus the recorded baseline. Pass --record-baseline to write the
baseline. Pass --db-path to measure an existing vault read-only instead.`,
		SilenceUsage: true,
		RunE:         runEval,
	}
	cmd.Flags().String("golden", "testdata/golden/v1.jsonl", "path to the golden dataset (JSONL)")
	cmd.Flags().String("corpus", "testdata/golden/corpus/", "source corpus for a self-provisioned run (ignored when --db-path is set)")
	cmd.Flags().String("mode", "hybrid", "retrieval mode: hybrid|semantic|keyword")
	cmd.Flags().Int("k", 10, "top-k cutoff for recall/MRR/NDCG pooling")
	cmd.Flags().String("embedder", "auto", "embedder: offline (deterministic, no network) | ollama | auto")
	cmd.Flags().String("embedding-model", "", "embedding model for a self-provisioned ollama run (enables --embedder ollama so real-model quality can be measured)")
	cmd.Flags().String("embedding-prefix", "", "instruction-prefix mode for the run: auto|on|off (default auto; self-provision runs only — a --db-path vault keeps its own convention)")
	cmd.Flags().String("benchmark", "", "manual BEIR retrieval benchmark to run (e.g. scifact, or beir:scifact); fetches + fully ingests the corpus, so it is slow and opt-in — NOT for CI. Pair with --embedder ollama --embedding-model <model>")
	cmd.Flags().Bool("no-rerank", false, "skip cross-encoder reranking")
	cmd.Flags().String("baseline", "", "baseline file to compare against (sets the exit code)")
	cmd.Flags().Float64("tolerance", 2.0, "max allowed recall@10 drop (percentage points) before the gate fails")
	cmd.Flags().Bool("record-baseline", false, "write/overwrite the baseline from this run's metrics and exit")
	cmd.Flags().String("format", "text", "output format: text|json")
	cmd.Flags().Bool("dump-chunks", false, "print every chunk id + preview (for authoring golden labels) and exit")
	return cmd
}

func runEval(cmd *cobra.Command, _ []string) error {
	flags := cmd.Flags()
	goldenPath, _ := flags.GetString("golden")
	corpusDir, _ := flags.GetString("corpus")
	mode, _ := flags.GetString("mode")
	k, _ := flags.GetInt("k")
	embedderMode, _ := flags.GetString("embedder")
	embModel, _ := flags.GetString("embedding-model")
	embPrefix, _ := flags.GetString("embedding-prefix")
	if embPrefix != "" {
		if _, err := embed.ParseMode(embPrefix); err != nil {
			return err
		}
	}
	benchmark, _ := flags.GetString("benchmark")
	noRerank, _ := flags.GetBool("no-rerank")
	baselinePath, _ := flags.GetString("baseline")
	tolerance, _ := flags.GetFloat64("tolerance")
	recordBaseline, _ := flags.GetBool("record-baseline")
	format, _ := flags.GetString("format")
	dumpChunks, _ := flags.GetBool("dump-chunks")
	useVault := flags.Changed("db-path") // explicit --db-path → measure that vault read-only

	ctx := context.Background()

	// Manual BEIR benchmark (audit H07 SC-001): short-circuit the golden path —
	// it fetches + fully ingests a real benchmark corpus, so it is slow and opt-in.
	if benchmark != "" {
		return runBenchmarkEval(ctx, benchmark, embedderMode, embModel, embPrefix, mode, k, format)
	}

	// 1. Acquire an open database + the embedder to use with it.
	cfg, db, em, cleanup, err := openEvalDB(dbPath, useVault, corpusDir, embedderMode, embModel, embPrefix)
	if err != nil {
		return err
	}
	defer cleanup()

	if dumpChunks {
		refs, err := eval.ListChunks(db)
		if err != nil {
			return err
		}
		for _, r := range refs {
			fmt.Printf("%s\t%s\t%s\n", r.ID, r.FilePath, r.Preview)
		}
		return nil
	}

	// 2. Resolve the embedder label for offline forcing + reporting.
	offline := em.Model() == "deterministic-hash"
	if offline {
		noRerank = true // no Ollama available to rerank in offline mode
	}

	// 3. Load + validate the golden dataset.
	golden, err := eval.LoadGolden(goldenPath)
	if err != nil {
		return err
	}

	// 4. Score.
	runner := eval.NewEvalRunner(cfg, db, em)
	run, err := runner.Run(ctx, golden, mode, k, noRerank)
	if err != nil {
		return err
	}

	// 5. Optional baseline record.
	if recordBaseline {
		bl := &eval.Baseline{Mode: run.Mode, RecordedAt: time.Now().UTC(), Embedder: run.Embedder, Metrics: run.Metrics}
		if err := bl.Save(baselinePathFor(baselinePath, goldenPath)); err != nil {
			return err
		}
		if format == "json" {
			_ = json.NewEncoder(os.Stdout).Encode(bl)
		} else {
			fmt.Printf("recorded baseline → %s\n", baselinePathFor(baselinePath, goldenPath))
		}
		return nil
	}

	// 6. Optional baseline compare (gate).
	var cmp *eval.Comparison
	comparePath := baselinePathFor(baselinePath, goldenPath)
	if baselinePath != "" || baselineExists(comparePath) {
		base, err := eval.LoadBaseline(comparePath)
		if err != nil {
			return err
		}
		cmp = eval.Compare(run, base, tolerance)
	}

	// 7. Render.
	if format == "json" {
		out := map[string]any{"run": run}
		if cmp != nil {
			out["comparison"] = cmp
		}
		return json.NewEncoder(os.Stdout).Encode(out)
	}
	fmt.Println(eval.FormatRun(run, cmp, tolerance))

	if cmp != nil && !cmp.Pass {
		return fmt.Errorf("retrieval-quality gate FAILED: recall@10 dropped %.2f percentage points (tolerance %.2f)", cmp.RecallAt10Drop, tolerance)
	}
	return nil
}

// runBenchmarkEval runs a manual BEIR retrieval benchmark (audit H07 SC-001):
// fetch + parse the dataset (cached), provision a throwaway vault, fully ingest
// the corpus with the chosen embedder + prefix, map the test-split qrels to
// go-rag chunk_ids, and score. It is the discriminating measurement the tiny
// golden regression fixture cannot provide (that fixture saturates at recall 1.0).
// Progress goes to stderr so stdout stays clean for --format json.
func runBenchmarkEval(ctx context.Context, benchmark, embedderMode, model, prefix, mode string, k int, format string) error {
	name := strings.TrimPrefix(benchmark, "beir:")
	home, _ := os.UserHomeDir()
	cacheDir := filepath.Join(home, ".go-rag", "benchmarks")

	fmt.Fprintf(os.Stderr, "benchmark: loading BEIR dataset %q (cache %s)...\n", name, cacheDir)
	ds, err := beir.Load(name, cacheDir)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "benchmark: %s loaded — %d docs, %d queries, %d test-split queries with qrels\n",
		name, len(ds.Corpus), len(ds.Queries), len(ds.Qrels))

	// Resolve the embedder via the same rules as a normal run.
	cfg := config.Default()
	if model != "" {
		cfg.EmbeddingModel = model
	}
	if prefix != "" {
		cfg.EmbeddingPrefix = prefix
	}
	em, err := resolveEmbedder(cfg, embedderMode)
	if err != nil {
		return err
	}
	resolvedPrefix := cfg.EmbeddingPrefix
	if resolvedPrefix == "" {
		resolvedPrefix = "auto"
	}
	if em.Model() == "deterministic-hash" {
		fmt.Fprintln(os.Stderr, "benchmark: WARNING — offline embedder ignores instruction prefixes; use --embedder ollama --embedding-model nomic-embed-text for a real measurement")
	}
	fmt.Fprintf(os.Stderr, "benchmark: ingesting corpus with %s (prefix=%s, mode=%s, k=%d)... this takes a while\n", em.Model(), resolvedPrefix, mode, k)

	run, err := eval.RunBenchmark(ctx, ds, em, mode, k, cfg.EmbeddingPrefix)
	if err != nil {
		return err
	}
	run.Dataset = name // tag the run for the report

	if format == "json" {
		return json.NewEncoder(os.Stdout).Encode(map[string]any{"dataset": name, "run": run})
	}
	fmt.Println(eval.FormatRun(run, nil, 0))
	return nil
}

// openEvalDB returns an open database and the embedder to use with it.
//
//   - useVault=true (--db-path): open that vault read-only and use the configured
//     Ollama embedder (or offline if requested). No ingest, no mutation (FR-006).
//   - useVault=false (default): self-provision a throwaway vault from corpusDir,
//     ingesting with the chosen embedder, so the run is hermetic and reproducible.
func openEvalDB(vaultPath string, useVault bool, corpusDir, embedderMode, model, prefix string) (config.Config, *storage.DB, embed.Embedder, func(), error) {
	if useVault {
		// A --db-path vault keeps its own embedding model + prefix convention
		// (the corpus was ingested with it); flags do not override it.
		cfg, db, err := engine.Open(vaultPath)
		if err != nil {
			return config.Config{}, nil, nil, nil, err
		}
		em, err := resolveEmbedder(cfg, embedderMode)
		if err != nil {
			db.Close()
			return config.Config{}, nil, nil, nil, err
		}
		cleanup := func() { db.Close() }
		return cfg, db, em, cleanup, nil
	}

	// Self-provision: fresh tmp vault, ingest corpus with the chosen embedder.
	// Use a default config only to resolve the embedder label first (so offline
	// is reported honestly), then delegate the hermetic vault build to eval.
	// --embedding-model / --embedding-prefix apply here only.
	resolveCfg := config.Default()
	if model != "" {
		resolveCfg.EmbeddingModel = model
	}
	if prefix != "" {
		resolveCfg.EmbeddingPrefix = prefix
	}
	em, err := resolveEmbedder(resolveCfg, embedderMode)
	if err != nil {
		return config.Config{}, nil, nil, nil, err
	}
	cfg, db, cleanup, err := eval.ProvisionCorpus(context.Background(), corpusDir, em, prefix)
	if err != nil {
		return config.Config{}, nil, nil, nil, err
	}
	return cfg, db, em, cleanup, nil
}

// resolveEmbedder picks the embedder for the run. "offline" is always
// deterministic and network-free; "ollama" uses the configured endpoint; "auto"
// uses ollama if a local Ollama answers, else falls back to offline (SC-004).
func resolveEmbedder(cfg config.Config, mode string) (embed.Embedder, error) {
	switch mode {
	case "offline":
		return eval.NewDeterministicEmbedder(), nil
	case "ollama":
		if cfg.EmbeddingModel == "" {
			return nil, fmt.Errorf("--embedder ollama requires an embedding model (set embedding_model)")
		}
		return embed.NewOllama(cfg.OllamaURL, cfg.EmbeddingModel), nil
	case "auto":
		if cfg.EmbeddingModel != "" && ollamaReachable(cfg.OllamaURL) {
			return embed.NewOllama(cfg.OllamaURL, cfg.EmbeddingModel), nil
		}
		return eval.NewDeterministicEmbedder(), nil
	default:
		return nil, fmt.Errorf("unknown --embedder %q (want offline|ollama|auto)", mode)
	}
}

func ollamaReachable(url string) bool {
	client := &http.Client{Timeout: time.Second}
	resp, err := client.Get(url + "/api/tags")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// baselinePathFor resolves where the baseline lives: an explicit --baseline path,
// else a conventional location next to the golden file.
func baselinePathFor(explicit, goldenPath string) string {
	if explicit != "" {
		return explicit
	}
	return filepath.Join(filepath.Dir(goldenPath), "baseline.json")
}

func baselineExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
