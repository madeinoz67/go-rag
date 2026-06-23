package audit

// reader.go reads + filters the audit log (spec 021 / audit H18, US2). Backs the
// `go-rag audit` command: tail the last N, filter by event type, and a time window;
// optionally include rotated archives (oldest→newest).

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

// ReadOptions filters a read. Zero values mean "no constraint".
type ReadOptions struct {
	Type  string        // "" = all types; else "query"|"ingest"|"auth-fail"
	Since time.Duration // 0 = all time
	Tail  int           // 0 = all (after filter); >0 = last N (after filter)
	All   bool          // include rotated archives (audit-N.log)
}

// Read returns filtered audit events from the active log (and archives when All),
// oldest→newest. Lines that fail to parse are skipped (a corrupt line never blocks).
func Read(path string, opts ReadOptions) ([]Event, error) {
	var events []Event
	for _, p := range logPaths(path, opts.All) {
		es, err := readEvents(p)
		if err != nil {
			continue // missing archive is fine
		}
		events = append(events, es...)
	}

	var cutoff time.Time
	if opts.Since > 0 {
		cutoff = time.Now().UTC().Add(-opts.Since)
	}
	out := make([]Event, 0, len(events))
	for _, e := range events {
		if opts.Type != "" && e.Type != opts.Type {
			continue
		}
		if !cutoff.IsZero() && e.TS.Before(cutoff) {
			continue
		}
		out = append(out, e)
	}
	if opts.Tail > 0 && len(out) > opts.Tail {
		out = out[len(out)-opts.Tail:]
	}
	return out, nil
}

// logPaths returns the files to read in oldest→newest order: archives N..1 then the
// active log. Archives only included when all is true.
func logPaths(path string, all bool) []string {
	var out []string
	if all {
		for i := archiveKeep; i >= 1; i-- { // audit-N (oldest) → audit-1
			if a := archiveName(path, i); fileExists(a) {
				out = append(out, a)
			}
		}
	}
	out = append(out, path) // active (newest)
	return out
}

func readEvents(path string) ([]Event, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []Event
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1<<20) // allow long lines
	for sc.Scan() {
		var e Event
		if json.Unmarshal(sc.Bytes(), &e) == nil {
			out = append(out, e)
		}
	}
	return out, sc.Err()
}

// RenderText renders events as a compact, human-readable line per event.
func RenderText(events []Event) string {
	var b strings.Builder
	for _, e := range events {
		switch e.Type {
		case TypeQuery:
			fmt.Fprintf(&b, "%s query  mode=%s k=%d hits=%d status=%s hash=%s…\n",
				e.TS.Format(time.RFC3339), e.Mode, e.K, e.Hits, e.Status, short(e.QueryHash))
		case TypeIngest:
			fmt.Fprintf(&b, "%s ingest op=%s path=%s new=%d skipped=%d errors=%d status=%s\n",
				e.TS.Format(time.RFC3339), e.Op, e.Path, e.New, e.Skipped, e.Errors, e.Status)
		case TypeAuthFail:
			fmt.Fprintf(&b, "%s auth-fail transport=%s detail=%s\n",
				e.TS.Format(time.RFC3339), e.Transport, e.Detail)
		}
	}
	return b.String()
}

// RenderJSONL re-emits events as raw JSONL (for piping to jq).
func RenderJSONL(events []Event) string {
	var b strings.Builder
	for _, e := range events {
		line, err := e.Marshal()
		if err != nil {
			continue
		}
		b.Write(line)
	}
	return b.String()
}

func short(s string) string {
	if len(s) > 12 {
		return s[:12]
	}
	return s
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
