// Package beir loads BEIR retrieval benchmark datasets (e.g. SciFact) for
// manual, opt-in retrieval-quality measurement. It is pure Go (net/http,
// archive/zip, encoding/json) — no Python, no extra dependencies — and caches
// downloads under ~/.go-rag/benchmarks/ so repeat runs are offline.
//
// Source: the original BEIR distribution (Thakur et al., 2021),
// https://arxiv.org/abs/2104.08663 — hosted at TU Darmstadt. SciFact is
// CC BY-NC (see the dataset's own license); benchmark data is fetched at
// runtime and is NOT committed to this repository.
package beir

import (
	"archive/zip"
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// baseURL is the canonical BEIR dataset host (original distribution).
const baseURL = "https://public.ukp.informatik.tu-darmstadt.de/thakur/BEIR/datasets/"

// Doc is one corpus passage.
type Doc struct {
	ID    string
	Title string
	Text  string
}

// Dataset is a parsed BEIR dataset: corpus, queries, and the test-split qrels.
type Dataset struct {
	Name    string
	Corpus  map[string]Doc         // corpusID -> Doc
	Queries map[string]string      // queryID -> query text
	Qrels   map[string]map[string]int // queryID -> {corpusID: relevance} (test split)
}

// Load fetches (caching under cacheDir) and parses a BEIR dataset by name
// (e.g. "scifact"). The three needed files — corpus.jsonl, queries.jsonl, and
// the test-split qrels — are extracted once and reused on subsequent runs.
func Load(name, cacheDir string) (*Dataset, error) {
	dir := filepath.Join(cacheDir, name)
	corpusPath := filepath.Join(dir, "corpus.jsonl")
	queriesPath := filepath.Join(dir, "queries.jsonl")
	qrelsPath := filepath.Join(dir, "qrels-test.tsv")

	if _, err := os.Stat(corpusPath); err != nil {
		if err := download(name, dir); err != nil {
			return nil, fmt.Errorf("beir %s: %w", name, err)
		}
	}

	ds := &Dataset{
		Name:    name,
		Corpus:  map[string]Doc{},
		Queries: map[string]string{},
		Qrels:   map[string]map[string]int{},
	}
	if err := parseCorpus(corpusPath, ds); err != nil {
		return nil, err
	}
	if err := parseQueries(queriesPath, ds); err != nil {
		return nil, err
	}
	if err := parseQrels(qrelsPath, ds); err != nil {
		return nil, err
	}
	return ds, nil
}

// download fetches <name>.zip from the BEIR host and extracts the three files
// the loader needs (flat under dir): corpus.jsonl, queries.jsonl, qrels-test.tsv.
func download(name, dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	url := baseURL + name + ".zip"
	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("fetch %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("fetch %s: status %d", url, resp.StatusCode)
	}
	// Stream the zip into memory (these datasets are small: a few MB).
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read %s: %w", url, err)
	}
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return fmt.Errorf("open zip: %w", err)
	}
	want := map[string]string{
		name + "/corpus.jsonl":     "corpus.jsonl",
		name + "/queries.jsonl":    "queries.jsonl",
		name + "/qrels/test.tsv":   "qrels-test.tsv",
	}
	written := 0
	for _, f := range zr.File {
		dest, ok := want[f.Name]
		if !ok {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return fmt.Errorf("open %s in zip: %w", f.Name, err)
		}
		out, err := os.Create(filepath.Join(dir, dest))
		if err != nil {
			rc.Close()
			return err
		}
		if _, err := io.Copy(out, rc); err != nil {
			rc.Close()
			out.Close()
			return err
		}
		rc.Close()
		out.Close()
		written++
	}
	if written != len(want) {
		return fmt.Errorf("zip missing expected files (got %d of %d)", written, len(want))
	}
	return nil
}

type corpusRow struct {
	ID    string `json:"_id"`
	Title string `json:"title"`
	Text  string `json:"text"`
}

func parseCorpus(path string, ds *Dataset) error {
	return scanLines(path, func(line []byte) error {
		var r corpusRow
		if err := json.Unmarshal(line, &r); err != nil {
			return err
		}
		if r.ID == "" {
			return nil
		}
		ds.Corpus[r.ID] = Doc{ID: r.ID, Title: r.Title, Text: r.Text}
		return nil
	})
}

type queryRow struct {
	ID   string `json:"_id"`
	Text string `json:"text"`
}

func parseQueries(path string, ds *Dataset) error {
	return scanLines(path, func(line []byte) error {
		var r queryRow
		if err := json.Unmarshal(line, &r); err != nil {
			return err
		}
		if r.ID == "" {
			return nil
		}
		ds.Queries[r.ID] = r.Text
		return nil
	})
}

// parseQrels reads the test-split TSV: "query-id\tcorpus-id\tscore" with a
// header row.
func parseQrels(path string, ds *Dataset) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	first := true
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		if first { // header: query-id\tcorpus-id\tscore
			first = false
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) < 2 {
			continue
		}
		qID, cID := fields[0], fields[1]
		score := 1
		if len(fields) >= 3 {
			if n, err := strconv.Atoi(strings.TrimSpace(fields[2])); err == nil {
				score = n
			}
		}
		if ds.Qrels[qID] == nil {
			ds.Qrels[qID] = map[string]int{}
		}
		ds.Qrels[qID][cID] = score
	}
	return sc.Err()
}

// scanLines calls fn for each non-blank, non-comment JSONL line of path.
func scanLines(path string, fn func([]byte) error) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024) // SciFact abstracts can be long
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		if err := fn(line); err != nil {
			return err
		}
	}
	return sc.Err()
}
