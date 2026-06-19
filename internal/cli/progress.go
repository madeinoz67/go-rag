package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// progressOut is where the progress bar writes — stderr, so stdout stays clean
// for piping / --json.
var progressOut io.Writer = os.Stderr

// progressBar renders an in-place progress bar (overwritten with \r) to stderr.
// Called as the pipeline's OnProgress callback during add/reprocess/migrate.
func progressBar(done, total int, path, status string) {
	name := filepath.Base(path)
	if total <= 0 {
		fmt.Fprintf(progressOut, "\rprocessed %d files…", done)
		return
	}
	const width = 24
	filled := width * done / total
	if filled > width {
		filled = width
	}
	bar := strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
	pct := done * 100 / total
	fmt.Fprintf(progressOut, "\r%s %d/%d (%d%%) %s %s   ", bar, done, total, pct, status, truncName(name, 28))
	if done >= total {
		fmt.Fprintln(progressOut)
	}
}

func truncName(s string, n int) string {
	if len(s) > n {
		return s[:n-1] + "…"
	}
	return s
}
