package audit

// rotate.go implements size-capped rotation (spec 021 / audit H18, research D6). When
// the active log exceeds the cap, the file is renamed audit.log → audit-1.log (older
// archives shift up; the oldest beyond N is dropped) and a fresh audit.log starts.
// Append-only is PRESERVED — rotation only renames whole files; no record is ever
// rewritten or deleted.

import (
	"fmt"
	"os"
	"path/filepath"
)

// archiveKeep is how many rotated archives are retained (worst case ≈ (N+1) × cap).
const archiveKeep = 3

// needsRotate reports whether the active file + the incoming line would exceed the cap.
func (a *Appender) needsRotate(incoming int) bool {
	if a.maxBytes <= 0 || a.f == nil {
		return false
	}
	st, err := a.f.Stat()
	if err != nil {
		return false
	}
	return st.Size()+int64(incoming) > int64(a.maxBytes)
}

// rotateLocked renames audit.log → audit-1.log (shifting older archives up by one),
// drops the archive beyond archiveKeep, and opens a fresh active file. Caller holds mu.
func (a *Appender) rotateLocked() {
	if a.f != nil {
		_ = a.f.Close()
		a.f = nil
	}
	// Shift archives up: audit-N → audit-(N+1), …, audit-1 → audit-2. Highest first so
	// nothing is overwritten before it moves.
	for i := archiveKeep; i >= 1; i-- {
		_ = os.Rename(archiveName(a.path, i), archiveName(a.path, i+1))
	}
	// Active → audit-1.log.
	_ = os.Rename(a.path, archiveName(a.path, 1))
	// Drop anything beyond what we keep.
	_ = os.Remove(archiveName(a.path, archiveKeep+1))
	// Fresh active file.
	f, err := os.OpenFile(a.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err == nil {
		a.f = f
	}
}

func archiveName(path string, n int) string {
	dir, _ := filepath.Split(path)
	return filepath.Join(dir, fmt.Sprintf("audit-%d.log", n))
}
