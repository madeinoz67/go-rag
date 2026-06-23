package engine

// threat.go implements the H04/spec 019 threat-list management (US4, FR-012/013,
// D12): a local, versioned merge of instruction-phrase sources (the built-in
// English list plus zero or more user sources — imported files/URLs or manually-
// added phrases), each independently enable/disable-able and deduped into the
// merged list the detector scores against.
//
// CONSTITUTION I BOUNDARY (the decisive constraint): go-rag is air-gapped by
// construction. The ONLY network egress in the entire system is the URL fetch
// inside ImportThreatSource — an explicit, user-initiated, one-shot GET to a
// source the user named. Every other operation (detect, query, list, rescan,
// add/remove phrases) is pure local I/O. There is no feed subscription, polling,
// or background pull — updates arrive only when the user runs `threat import`.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/madeinoz67/go-rag/internal/poison"
	"github.com/madeinoz67/go-rag/internal/storage"
)

// ThreatSource is one managed instruction-phrase source.
type ThreatSource struct {
	ID        string // stable id (origin hash, or "user" for manual phrases)
	Origin    string // file path, URL, or "manual"
	Enabled   bool
	Version   string    // content-hash of Phrases (change detection)
	FetchedAt time.Time // when last imported (zero for built-in)
	Phrases   []string  // lowercased
}

// ThreatImportResult is returned by import/add operations (carries the closed-loop
// rescan counts so a caller sees the effect immediately).
type ThreatImportResult struct {
	ID       string
	Origin   string
	Added    int // phrases in this source after the op
	Rescored int // chunks (re)scored by the triggered rescan
	Flagged  int // now flagged (suspicious/quarantine)
}

// ListThreatSources returns all managed phrase sources (sorted by ID).
func (e *Engine) ListThreatSources() ([]ThreatSource, error) {
	var out []ThreatSource
	err := e.db.ScanThreatSources(func(_ string, val []byte) bool {
		var s ThreatSource
		if json.Unmarshal(val, &s) == nil {
			out = append(out, s)
		}
		return true
	})
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, err
}

func (e *Engine) putThreatSource(s ThreatSource) error {
	bj, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return e.db.SetWithPrefix(storage.PrefixThreatSrc, []byte(s.ID), bj)
}

func (e *Engine) getThreatSource(id string) (ThreatSource, bool, error) {
	raw, ok, err := e.db.GetWithPrefix(storage.PrefixThreatSrc, []byte(id))
	if err != nil || !ok {
		return ThreatSource{}, false, err
	}
	var s ThreatSource
	if json.Unmarshal(raw, &s) != nil {
		return ThreatSource{}, false, nil
	}
	return s, true, nil
}

// RemoveThreatSource removes a source by ID and triggers a rescan (a removed
// source may un-flag chunks that only matched its phrases).
func (e *Engine) RemoveThreatSource(id string) error {
	if err := e.db.DeleteWithPrefix(storage.PrefixThreatSrc, []byte(id)); err != nil {
		return err
	}
	_, _, _ = e.RescanPoisoning()
	return nil
}

// SetThreatSourceEnabled toggles a source and triggers a rescan.
func (e *Engine) SetThreatSourceEnabled(id string, enabled bool) error {
	s, ok, err := e.getThreatSource(id)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("threat source not found: %s", id)
	}
	s.Enabled = enabled
	if err := e.putThreatSource(s); err != nil {
		return err
	}
	_, _, _ = e.RescanPoisoning()
	return nil
}

// AddPhrases appends phrases to the "user" (manual) source and triggers a rescan
// (FR-012 add). Phrases are lowercased + deduped.
func (e *Engine) AddPhrases(phrases []string) (ThreatImportResult, error) {
	src, _, _ := e.getThreatSource(threatUserID)
	src.ID = threatUserID
	if src.Origin == "" {
		src.Origin = "manual"
	}
	src.Enabled = true
	merged := dedupePhrases(append(src.Phrases, normalizePhrases(phrases)...))
	src.Phrases = merged
	src.Version = phraseHash(merged)
	src.FetchedAt = time.Now().UTC()
	if err := e.putThreatSource(src); err != nil {
		return ThreatImportResult{}, err
	}
	resc, flagged, _ := e.RescanPoisoning()
	return ThreatImportResult{ID: src.ID, Origin: src.Origin, Added: len(merged), Rescored: resc, Flagged: flagged}, nil
}

// ImportThreatSource reads phrases from a file path or URL (US4, FR-013) and
// triggers a rescan (the closed loop). URL fetch is the ONLY network egress in
// go-rag (Constitution I). Re-importing the same origin updates the source in
// place (idempotent on origin).
func (e *Engine) ImportThreatSource(origin string) (ThreatImportResult, error) {
	phrases, err := readPhrases(origin)
	if err != nil {
		return ThreatImportResult{}, err
	}
	phrases = dedupePhrases(phrases)
	s := ThreatSource{
		ID:        sourceID(origin),
		Origin:    origin,
		Enabled:   true,
		Version:   phraseHash(phrases),
		FetchedAt: time.Now().UTC(),
		Phrases:   phrases,
	}
	if err := e.putThreatSource(s); err != nil {
		return ThreatImportResult{}, err
	}
	resc, flagged, _ := e.RescanPoisoning()
	return ThreatImportResult{ID: s.ID, Origin: origin, Added: len(phrases), Rescored: resc, Flagged: flagged}, nil
}

// mergedPhrases returns the deduped, sorted union of the built-in list + all
// enabled sources' phrases — what the detector scores against.
func (e *Engine) mergedPhrases() []string {
	seen := make(map[string]bool)
	out := make([]string, 0, len(poison.DefaultPhrases))
	add := func(p string) {
		if p == "" || seen[p] {
			return
		}
		seen[p] = true
		out = append(out, p)
	}
	for _, p := range poison.DefaultPhrases {
		add(p)
	}
	if srcs, err := e.ListThreatSources(); err == nil {
		for _, s := range srcs {
			if !s.Enabled {
				continue
			}
			for _, p := range s.Phrases {
				add(p)
			}
		}
	}
	sort.Strings(out)
	return out
}

// poisonDetector builds the scorer from the current merged phrase list + the
// configured thresholds. Used at BOTH ingest (pipeline bind) and rescan so they
// score against the identical list.
func (e *Engine) poisonDetector() poison.Detector {
	return poison.NewHeuristic(e.mergedPhrases(),
		e.cfg.EffectivePoisonThresholdSuspicious(),
		e.cfg.EffectivePoisonThresholdQuarantine())
}

// --- phrase-source parsing helpers ---

const threatUserID = "user"

func sourceID(origin string) string {
	h := sha256.Sum256([]byte(origin))
	return hex.EncodeToString(h[:])[:16]
}

func phraseHash(phrases []string) string {
	h := sha256.New()
	for _, p := range phrases {
		h.Write([]byte(p))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

func dedupePhrases(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, p := range in {
		if p != "" && !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	return out
}

func normalizePhrases(in []string) []string {
	out := make([]string, 0, len(in))
	for _, p := range in {
		p = strings.ToLower(strings.TrimSpace(p))
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func isURL(s string) bool {
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

// readPhrases loads phrases from a file path or URL. The URL branch is the ONLY
// network egress in go-rag (Constitution I); the file branch is pure local I/O.
func readPhrases(origin string) ([]string, error) {
	var data []byte
	if isURL(origin) {
		cl := &http.Client{Timeout: 30 * time.Second}
		resp, err := cl.Get(origin)
		if err != nil {
			return nil, fmt.Errorf("fetch %s: %w", origin, err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("fetch %s: HTTP %d", origin, resp.StatusCode)
		}
		data, err = io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1 MiB cap
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", origin, err)
		}
	} else {
		var err error
		data, err = os.ReadFile(origin)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", origin, err)
		}
	}
	return parsePhrases(string(data)), nil
}

// parsePhrases extracts one phrase per line: lowercased, trimmed, skipping blanks
// and '#' comments.
func parsePhrases(s string) []string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		line = strings.ToLower(strings.TrimSpace(line))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, line)
	}
	return out
}
