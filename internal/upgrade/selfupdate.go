package upgrade

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// SelfUpdate performs the atomic binary self-replacement with the given release.
//
// Sequence: resolve asset URL + expected checksum → download+extract to a temp
// file next to the current binary → chmod 0755 → verify checksum + exec bit →
// back up the current binary (exe → exe.prev) → atomically rename temp → exe.
// On any failure before the swap, the temp is removed and the current binary is
// left byte-identical. On Windows the caller must not reach here (the CLI prints
// the release URL and exits instead).
func SelfUpdate(latest string) error {
	goos, goarch := runtime.GOOS, runtime.GOARCH
	binaryName := "go-rag"
	if goos == "windows" {
		binaryName = "go-rag.exe"
	}

	assetURL := ReleaseAssetURL(latest, goos, goarch)
	expected, err := ExpectedSHA256(latest, goos, goarch)
	if err != nil {
		return fmt.Errorf("fetch checksum: %w", err)
	}

	exe, err := resolvedExecutable()
	if err != nil {
		return err
	}

	dir := filepath.Dir(exe)

	// Download the archive, then verify its SHA-256 against checksums.txt BEFORE
	// extracting. The release pipeline hashes the .tar.gz (sha256sum *.tar.gz), so
	// the check is over the downloaded artifact, not the extracted binary.
	archivePath, err := downloadArchive(assetURL, dir)
	if err != nil {
		return err
	}
	defer func() {
		if _, statErr := os.Stat(archivePath); statErr == nil {
			_ = os.Remove(archivePath)
		}
	}()
	if err := VerifyChecksum(archivePath, expected); err != nil {
		return fmt.Errorf("verify: %w", err)
	}

	tmpPath, err := extractBinary(archivePath, binaryName, dir)
	if err != nil {
		return err
	}
	// Ensure the extracted binary is cleaned up on any error path. After a
	// successful swap the rename has consumed it, so the Stat guard avoids
	// removing the new binary.
	defer func() {
		if _, statErr := os.Stat(tmpPath); statErr == nil {
			_ = os.Remove(tmpPath)
		}
	}()

	if err := os.Chmod(tmpPath, 0o755); err != nil {
		return fmt.Errorf("chmod temp: %w", err)
	}
	if err := VerifyExecutable(tmpPath); err != nil {
		return err
	}

	return applySwap(exe, tmpPath)
}

// applySwap backs up the binary at exePath to exePath+".prev" (retaining exactly
// one prior version), then atomically renames tmpPath into exePath. Both paths
// MUST be on the same filesystem (the caller places the temp next to the exe).
// On rename failure it attempts to restore the backup so the install is never
// left without a binary.
func applySwap(exePath, tmpPath string) error {
	prev := exePath + ".prev"
	_ = os.Remove(prev) // overwrite any stale prior backup (retention N=1)
	if err := os.Rename(exePath, prev); err != nil {
		return fmt.Errorf("backup current binary: %w", err)
	}
	// Invariant: tmpPath is in filepath.Dir(exePath) (same filesystem — see
	// extractBinary), so os.Rename is atomic and cannot hit EXDEV. The old
	// binary was moved aside above, so this rename targets a free path (no
	// ETXTBSY from a running executable).
	if err := os.Rename(tmpPath, exePath); err != nil {
		// On install failure, restore the previous binary. If THAT also fails the
		// binary is missing on disk — surface both errors loudly rather than report
		// only the install error and silently brick the install (FR-014).
		if rerr := os.Rename(prev, exePath); rerr != nil {
			return fmt.Errorf("install failed (%w) and restoring the previous binary also failed (%v): the binary at %s may be missing — recover from %s", err, rerr, exePath, prev)
		}
		return fmt.Errorf("install: %w (previous binary restored)", err)
	}
	return nil
}

// executablePath locates the running binary; tests override it so SelfUpdate /
// Rollback operate on a throwaway temp binary instead of the real go-rag.
var executablePath = os.Executable

// resolvedExecutable returns the absolute path of the current binary with
// symlinks resolved (so the rename lands on the real file, not a symlink).
func resolvedExecutable() (string, error) {
	exe, err := executablePath()
	if err != nil {
		return "", fmt.Errorf("cannot locate current binary: %w", err)
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return "", fmt.Errorf("cannot resolve symlink: %w", err)
	}
	return exe, nil
}

// Rollback restores the previous binary from {exe}.prev (retained by SelfUpdate's
// backup step). Offline — no network. It swaps: current → .broken, .prev →
// current, then removes .broken. Returns a clear error if no backup exists.
func Rollback() error {
	exe, err := resolvedExecutable()
	if err != nil {
		return err
	}
	return rollbackSwap(exe)
}

// rollbackSwap is the testable core of Rollback, operating on an explicit path.
func rollbackSwap(exePath string) error {
	prev := exePath + ".prev"
	if _, err := os.Stat(prev); err != nil {
		return fmt.Errorf("no previous binary to roll back to (%s): %w", prev, err)
	}
	broken := exePath + ".broken"
	_ = os.Remove(broken) // clear any stale side-aside
	if err := os.Rename(exePath, broken); err != nil {
		return fmt.Errorf("set aside current binary: %w", err)
	}
	if err := os.Rename(prev, exePath); err != nil {
		// Mirror applySwap: if restore of the current binary also fails, the binary
		// is missing on disk — surface both failures, do not silently brick.
		if rerr := os.Rename(broken, exePath); rerr != nil {
			return fmt.Errorf("rollback failed (%w) and restoring the current binary also failed (%v): the binary at %s may be missing — recover from %s", err, rerr, exePath, broken)
		}
		return fmt.Errorf("restore previous binary: %w (current binary restored)", err)
	}
	_ = os.Remove(broken)
	return nil
}
