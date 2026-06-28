//go:build integration

package modelbundle

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestDownload_FromReleaseAsset (integration, network): forces a fresh HOME (no model
// present) so Download fetches the release-asset tarball from the go-rag GitHub
// release, extracts it, and verifies the onnx SHA. Validates the same-origin fetch
// path (spec 032 T025). Run: go test -tags integration ./internal/embed/modelbundle/
func TestDownload_FromReleaseAsset(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // fresh → model absent → Download must fetch

	d, err := Download(context.Background())
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	weights := filepath.Join(d, ModelFilename)
	if _, err := os.Stat(weights); err != nil {
		t.Fatalf("weights not extracted at %s: %v", weights, err)
	}
	if err := Verify(weights); err != nil {
		t.Fatalf("Verify after Download: %v", err)
	}
	if !IsPresent() {
		t.Fatal("IsPresent false after Download")
	}
}
