// Package modelbundle pins the default embedding model (spec 032): identity, local
// storage path, a live fetch, and a SHA-256 integrity gate. The go-rag binary embeds
// only these constants (~bytes); the model weights are fetched at runtime (one-time,
// during `go-rag init` / `go-rag model install`), keeping the binary pure-Go and
// under the 25 MB budget. Verify + identity are engine-agnostic; Download uses Hugot's
// HF downloader (the chosen engine).
package modelbundle

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/knights-analytics/hugot"
)

// Pinned model identity (spec 032). Bump together to change the default model. A
// ModelID change causes existing embeddings to re-embed (model identity is stored per
// embedding, distinct from document identity — Constitution Principle II).
const (
	ModelID       = "bge-small-en-v1.5-int8"
	HFRepo        = "Xenova/bge-small-en-v1.5"
	OnnxFilePath  = "onnx/model_int8.onnx" // path within the HF repo
	ModelFilename = "model_int8.onnx"      // local filename under ModelDir
	EmbeddingDim  = 384

	// ExpectedSHA256 pins the weights' SHA-256 (supply-chain guard). Empty = dev mode
	// (Verify is a no-op). MUST be pinned before release via HashFile on the first
	// verified download.
	ExpectedSHA256 = ""
)

// ErrHashMismatch is returned when a model file fails its integrity check. The
// caller MUST delete the offending file (never install an unverified model).
type ErrHashMismatch struct {
	Got, Want, Path string
}

func (e ErrHashMismatch) Error() string {
	return fmt.Sprintf("modelbundle: %s failed integrity check (got %s, want %s)", e.Path, e.Got, e.Want)
}

// modelsParent is the parent dir under which the model repo dir is laid out:
// ~/.go-rag/models/.
func modelsParent() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".go-rag", "models"), nil
}

// ModelDir is the local HF-model directory for the pinned model. Matches Hugot's
// DownloadModel layout: <modelsParent>/<repo-with-/-as-_>/, e.g.
// ~/.go-rag/models/Xenova_bge-small-en-v1.5/.
func ModelDir() (string, error) {
	parent, err := modelsParent()
	if err != nil {
		return "", err
	}
	return filepath.Join(parent, strings.ReplaceAll(HFRepo, "/", "_")), nil
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

// Download fetches the pinned model from HuggingFace (interim source, D1a) via
// Hugot's downloader into ModelDir, then verifies the weights against ExpectedSHA256.
// On mismatch the download is deleted and ErrHashMismatch returned. Returns the model
// directory path. MUST be called only from `go-rag model install` / `init` — never
// from the add/query path (which runs offline once the model is present).
func Download(ctx context.Context) (string, error) {
	parent, err := modelsParent()
	if err != nil {
		return "", err
	}
	opts := hugot.NewDownloadOptions()
	opts.OnnxFilePath = OnnxFilePath
	modelPath, err := hugot.DownloadModel(ctx, HFRepo, parent, opts)
	if err != nil {
		return "", fmt.Errorf("modelbundle: download %s: %w", ModelID, err)
	}
	if err := Verify(filepath.Join(modelPath, ModelFilename)); err != nil {
		_ = os.RemoveAll(modelPath)
		return "", err
	}
	return modelPath, nil
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
