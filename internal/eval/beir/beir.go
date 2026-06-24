// Package beir loads BEIR retrieval benchmark datasets (SciFact, MS MARCO, ...)
// for manual, opt-in retrieval-quality measurement. It is pure Go (net/http,
// archive/zip, encoding/json) — no Python, no extra dependencies — and streams
// dataset entries directly from a cached .zip (no on-disk extraction), so even
// the ~1GB MS MARCO corpus needs no extracted copy.
//
// Source: the original BEIR distribution (Thakur et al., 2021),
// https://arxiv.org/abs/2104.08663 — hosted at TU Darmstadt. Dataset licenses
// vary (SciFact = CC BY-NC, MS MARCO = CC BY). Benchmark data is fetched at
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
	"sort"
	"strconv"
	"strings"
	"time"
)

// baseURL is the canonical BEIR dataset host (original distribution).
const baseURL = "https://public.ukp.informatik.tu-darmstadt.de/thakur/BEIR/datasets/"

// msmarcoCorpusSize is the MS MARCO v1 passage-corpus size (8,841,823). It is a
// fixed published dataset, so the constant is used only to size the stride for
// distractor subsampling — the benchmark still works if it ever drifts.
const msmarcoCorpusSize = 8841823

// evalSplit returns the standard BEIR evaluation split for a dataset. MS MARCO
// evaluates on dev; the rest on test.
func evalSplit(name string) string {
	if name == "msmarco" {
		return "dev"
	}
	return "test"
}

// Doc is one corpus passage.
type Doc struct {
	ID    string
	Title string
	Text  string
}

// Dataset is a parsed BEIR dataset: corpus, queries, and the eval-split qrels.
type Dataset struct {
	Name    string
	Corpus  map[string]Doc            // corpusID -> Doc
	Queries map[string]string         // queryID -> query text
	Qrels   map[string]map[string]int // queryID -> {corpusID: relevance}
}

// Load fully parses a small dataset (e.g. SciFact) into memory.
func Load(name, cacheDir string) (*Dataset, error) {
	zr, cleanup, err := openZip(name, cacheDir)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	split := evalSplit(name)
	ds := &Dataset{Name: name, Corpus: map[string]Doc{}, Queries: map[string]string{}, Qrels: map[string]map[string]int{}}
	if err := readCorpus(zr, name, ds, nil); err != nil {
		return nil, err
	}
	if err := readEntryJSONL(zr, name+"/queries.jsonl", func(line []byte) error {
		var r queryRow
		if json.Unmarshal(line, &r) != nil || r.ID == "" {
			return nil
		}
		ds.Queries[r.ID] = r.Text
		return nil
	}); err != nil {
		return nil, err
	}
	if err := readQrels(zr, name, split, ds); err != nil {
		return nil, err
	}
	return ds, nil
}

// LoadSubsampled parses a large dataset (MS MARCO) with a subsampled corpus so a
// full ~8.8M-passage ingest is never needed: queries + qrels load fully (small),
// then the corpus is streamed once, keeping only the relevant passages for the
// sampled queries plus a deterministic stride sample of distractors. numQueries
// caps the query sample; distractors targets roughly that many distractor
// passages (kept via a stride over the known corpus size). Sampling is
// deterministic (sorted query ids + a stride), so runs are reproducible.
func LoadSubsampled(name, cacheDir string, numQueries, distractors int) (*Dataset, error) {
	zr, cleanup, err := openZip(name, cacheDir)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	split := evalSplit(name)
	ds := &Dataset{Name: name, Corpus: map[string]Doc{}, Queries: map[string]string{}, Qrels: map[string]map[string]int{}}
	if err := readEntryJSONL(zr, name+"/queries.jsonl", func(line []byte) error {
		var r queryRow
		if json.Unmarshal(line, &r) != nil || r.ID == "" {
			return nil
		}
		ds.Queries[r.ID] = r.Text
		return nil
	}); err != nil {
		return nil, err
	}
	if err := readQrels(zr, name, split, ds); err != nil {
		return nil, err
	}

	// Deterministic stride sample of the queries that have qrels.
	qIDs := make([]string, 0, len(ds.Qrels))
	for q := range ds.Qrels {
		qIDs = append(qIDs, q)
	}
	sort.Strings(qIDs)
	sample := map[string]bool{}
	if step := len(qIDs) / numQueries; step < 1 {
		step = 1
	} else {
		for i := 0; i < len(qIDs); i += step {
			sample[qIDs[i]] = true
			if len(sample) >= numQueries {
				break
			}
		}
	}

	// Relevant corpus ids across the sampled queries.
	relevant := map[string]bool{}
	for q := range sample {
		for docID := range ds.Qrels[q] {
			relevant[docID] = true
		}
	}

	// Stream the corpus once: keep relevant docs + a stride sample of distractors.
	stride := corpusSize(name) / distractors
	if stride < 1 {
		stride = 1
	}
	line := 0
	keep := func(d Doc) bool {
		if relevant[d.ID] {
			return true
		}
		line++
		return line%stride == 0
	}
	if err := readCorpus(zr, name, ds, keep); err != nil {
		return nil, err
	}

	// Restrict queries + qrels to the sample.
	for q := range ds.Qrels {
		if !sample[q] {
			delete(ds.Qrels, q)
			delete(ds.Queries, q)
		}
	}
	return ds, nil
}

// corpusSize returns the dataset's corpus size (for stride sizing). Only MS
// MARCO needs this; small datasets use Load (keep-all), so the fallback is moot.
func corpusSize(name string) int {
	if name == "msmarco" {
		return msmarcoCorpusSize
	}
	return 100000
}

// --- zip plumbing ---

// openZip ensures <name>.zip is downloaded (cached under cacheDir/<name>/) and
// returns a *zip.Reader over it plus a cleanup that closes the underlying file.
// The file stays open so entry reads can stream from it.
func openZip(name, cacheDir string) (*zip.Reader, func(), error) {
	dir := filepath.Join(cacheDir, name)
	zipPath := filepath.Join(dir, name+".zip")
	if _, err := os.Stat(zipPath); err != nil {
		if err := download(name, dir); err != nil {
			return nil, nil, err
		}
	}
	f, err := os.Open(zipPath)
	if err != nil {
		return nil, nil, err
	}
	fi, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, nil, err
	}
	zr, err := zip.NewReader(f, fi.Size())
	if err != nil {
		f.Close()
		return nil, nil, err
	}
	return zr, func() { f.Close() }, nil
}

// download fetches <name>.zip from the BEIR host into dir.
func download(name, dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	url := baseURL + name + ".zip"
	client := &http.Client{Timeout: 10 * time.Minute}
	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("fetch %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("fetch %s: status %d", url, resp.StatusCode)
	}
	tmp := zipPath(dir, name) + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, resp.Body); err != nil {
		out.Close()
		os.Remove(tmp)
		return fmt.Errorf("read %s: %w", url, err)
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, zipPath(dir, name))
}

func zipPath(dir, name string) string { return filepath.Join(dir, name+".zip") }

// entryReader returns a reader for a zip entry by exact name.
func entryReader(zr *zip.Reader, entry string) (io.ReadCloser, error) {
	for _, f := range zr.File {
		if f.Name == entry {
			return f.Open()
		}
	}
	return nil, fmt.Errorf("zip entry %q not found", entry)
}

type corpusRow struct {
	ID    string `json:"_id"`
	Title string `json:"title"`
	Text  string `json:"text"`
}
type queryRow struct {
	ID   string `json:"_id"`
	Text string `json:"text"`
}

// readCorpus streams <name>/corpus.jsonl; keep(d) decides which docs to retain
// (nil keep = keep all).
func readCorpus(zr *zip.Reader, name string, ds *Dataset, keep func(Doc) bool) error {
	return readEntryJSONL(zr, name+"/corpus.jsonl", func(line []byte) error {
		var r corpusRow
		if json.Unmarshal(line, &r) != nil || r.ID == "" {
			return nil
		}
		d := Doc(r)
		if keep != nil && !keep(d) {
			return nil
		}
		ds.Corpus[d.ID] = d
		return nil
	})
}

// readQrels parses <name>/qrels/<split>.tsv: "query-id\tcorpus-id\tscore".
func readQrels(zr *zip.Reader, name, split string, ds *Dataset) error {
	rc, err := entryReader(zr, name+"/qrels/"+split+".tsv")
	if err != nil {
		return err
	}
	defer rc.Close()
	sc := bufio.NewScanner(rc)
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

// readEntryJSONL calls fn for each non-blank line of a zip entry.
func readEntryJSONL(zr *zip.Reader, entry string, fn func([]byte) error) error {
	rc, err := entryReader(zr, entry)
	if err != nil {
		return err
	}
	defer rc.Close()
	sc := bufio.NewScanner(rc)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
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
