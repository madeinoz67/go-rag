package upgrade

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// downloadArchive downloads the tar.gz at assetURL to a temp file in dir and
// returns its path. The caller removes it on error or after extraction.
func downloadArchive(assetURL, dir string) (string, error) {
	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Get(assetURL)
	if err != nil {
		return "", fmt.Errorf("download failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download failed: HTTP %d", resp.StatusCode)
	}
	tmp, err := os.CreateTemp(dir, ".go-rag-archive-*.tar.gz")
	if err != nil {
		return "", fmt.Errorf("temp file: %w", err)
	}
	if _, err := io.Copy(tmp, resp.Body); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return "", fmt.Errorf("write temp: %w", err)
	}
	tmp.Close()
	return tmp.Name(), nil
}

// extractBinary extracts the file named binaryName from the tar.gz at archivePath
// to a temp file in dir (the target binary's directory, so the later os.Rename is
// same-filesystem / atomic) and returns its path.
func extractBinary(archivePath, binaryName, dir string) (string, error) {
	f, err := os.Open(archivePath)
	if err != nil {
		return "", fmt.Errorf("open archive: %w", err)
	}
	defer f.Close()
	gr, err := gzip.NewReader(f)
	if err != nil {
		return "", fmt.Errorf("gzip open: %w", err)
	}
	defer gr.Close()
	tr := tar.NewReader(gr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("tar read: %w", err)
		}
		if filepath.Base(hdr.Name) != binaryName {
			continue
		}
		tmp, err := os.CreateTemp(dir, ".go-rag-upgrade-*")
		if err != nil {
			return "", fmt.Errorf("temp file: %w", err)
		}
		if _, err := io.Copy(tmp, tr); err != nil {
			tmp.Close()
			os.Remove(tmp.Name())
			return "", fmt.Errorf("write temp: %w", err)
		}
		tmp.Close()
		return tmp.Name(), nil
	}
	return "", fmt.Errorf("binary %q not found in archive", binaryName)
}
