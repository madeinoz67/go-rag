// Package modelbundle pins the default embedding model (spec 032): identity, local
// storage path, a live fetch from the go-rag GitHub Release asset (same-origin,
// version-pinned), and a SHA-256 integrity gate. The go-rag binary embeds only
// constants (~bytes); the model weights are fetched at runtime (one-time, during
// `go-rag init` / `go-rag model install`), keeping the binary pure-Go and < 25 MB.
package modelbundle

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Pinned model identity (spec 032). Bump together to change the default model. A
// ModelID change causes existing embeddings to re-embed (model identity is stored per
// embedding, distinct from document identity — Constitution Principle II).
const (
	ModelID       = "bge-small-en-v1.5-int8"
	ModelDirName  = "Xenova_bge-small-en-v1.5" // local dir under modelsParent; matches the release-asset tarball top entry
	ModelFilename = "model_int8.onnx"          // local weights filename
	EmbeddingDim  = 384

	repoOwner      = "madeinoz67"
	repoName       = "go-rag"
	modelAssetFile = "go-rag-model-bge-small-en-v1.5-int8.tar.gz" // bundled per GitHub release

	// ExpectedSHA256 pins the SHA-256 of the int8 ONNX weights (supply-chain guard).
	// Download verifies the extracted weights against this; a mismatch
	// (tamper/corruption) is rejected and the files deleted.
	ExpectedSHA256 = "bf64d05457cb391fa88d045faf5927a15ea36d96228ddf23ea970087afdc1197"
)

// binaryVersion is the go-rag version, injected via ldflags
// (-X .../modelbundle.binaryVersion=<tag>). Used to pin the model-asset URL to the
// binary's own release; "dev"/non-semver (local builds) fall back to /latest/download/.
var binaryVersion = "dev"

var releaseTagRe = regexp.MustCompile(`^v\d+\.\d+\.\d+$`)

// downloadURL returns the GitHub Release asset URL for the model tarball:
// version-pinned for exact release-tag builds, /releases/latest/download/ otherwise.
func downloadURL() string {
	base := fmt.Sprintf("https://github.com/%s/%s", repoOwner, repoName)
	if releaseTagRe.MatchString(binaryVersion) {
		return fmt.Sprintf("%s/releases/download/%s/%s", base, binaryVersion, modelAssetFile)
	}
	return fmt.Sprintf("%s/releases/latest/download/%s", base, modelAssetFile)
}

// ErrHashMismatch is returned when a model file fails its integrity check. The
// caller MUST delete the offending file (never install an unverified model).
type ErrHashMismatch struct {
	Got, Want, Path string
}

func (e ErrHashMismatch) Error() string {
	return fmt.Sprintf("modelbundle: %s failed integrity check (got %s, want %s)", e.Path, e.Got, e.Want)
}

// modelsParent is the parent dir under which the model dir is laid out:
// ~/.go-rag/models/.
func modelsParent() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".go-rag", "models"), nil
}

// ModelDir is the local model directory: <modelsParent>/<ModelDirName>.
func ModelDir() (string, error) {
	parent, err := modelsParent()
	if err != nil {
		return "", err
	}
	return filepath.Join(parent, ModelDirName), nil
}

// ModelPath returns the path to the local ONNX weights file (ModelDir/ModelFilename).
func ModelPath() (string, error) {
	d, err := ModelDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, ModelFilename), nil
}

// IsPresent reports whether the weights file exists locally.
func IsPresent() bool {
	p, err := ModelPath()
	if err != nil {
		return false
	}
	_, err = os.Stat(p)
	return err == nil
}

// Verify hashes the file at path and compares it to ExpectedSHA256. Empty expected
// (dev mode) is a no-op. Mismatch → ErrHashMismatch (caller MUST delete the file).
func Verify(path string) error { return verifyHash(path, ExpectedSHA256) }

func verifyHash(path, expected string) error {
	if expected == "" {
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	got := hex.EncodeToString(h.Sum(nil))
	if got != expected {
		return ErrHashMismatch{Got: got, Want: expected, Path: path}
	}
	return nil
}

// HashFile computes the hex SHA-256 of a file (used to pin ExpectedSHA256 after the
// first verified download).
func HashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Download fetches the bundled model from the go-rag GitHub Release asset
// (same-origin, version-pinned for tagged builds; /latest/download/ for dev builds),
// extracts it into ModelDir, and verifies the onnx weights against ExpectedSHA256.
// On mismatch the download is deleted and ErrHashMismatch returned. MUST be called
// only from `go-rag model install` / `init` — never from the add/query path.
func Download(ctx context.Context) (string, error) {
	parent, err := modelsParent()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return "", err
	}
	if err := fetchAndExtract(ctx, downloadURL(), parent); err != nil {
		return "", fmt.Errorf("modelbundle: download %s: %w", ModelID, err)
	}
	dir, err := ModelDir()
	if err != nil {
		return "", err
	}
	if err := Verify(filepath.Join(dir, ModelFilename)); err != nil {
		_ = os.RemoveAll(dir)
		return "", err
	}
	return dir, nil
}

// fetchAndExtract downloads a .tar.gz from url and extracts it into dest, with a
// tar-slip guard (no entry may escape dest).
func fetchAndExtract(ctx context.Context, url, dest string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d fetching %s", resp.StatusCode, url)
	}
	gz, err := gzip.NewReader(resp.Body)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	cleanDest := filepath.Clean(dest) + string(os.PathSeparator)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		target := filepath.Join(dest, hdr.Name)
		if !strings.HasPrefix(filepath.Clean(target)+string(os.PathSeparator), cleanDest) {
			return fmt.Errorf("tar entry escapes destination: %q", hdr.Name)
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(hdr.Mode)&0o755)
			if err != nil {
				return err
			}
			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				return err
			}
			f.Close()
		}
	}
	return nil
}

// EnsureModel returns the model directory path, fetching first if the weights are
// absent or fail verification. Entry point for `go-rag model install` and `init`.
func EnsureModel(ctx context.Context) (string, error) {
	dir, err := ModelDir()
	if err != nil {
		return "", err
	}
	weights := filepath.Join(dir, ModelFilename)
	if IsPresent() {
		if err := Verify(weights); err == nil {
			return dir, nil
		}
		_ = os.RemoveAll(dir) // corrupt/tampered → re-fetch
	}
	return Download(ctx)
}
