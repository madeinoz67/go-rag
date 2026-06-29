package upgrade

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	githubRepo     = "madeinoz67/go-rag"
	checksumsAsset = "checksums.txt"
)

// releaseBaseURL / releasesAPIURL are package vars (not consts) so the upgrade
// E2E test can repoint them at a local httptest server instead of GitHub.
var (
	releaseBaseURL = "https://github.com/" + githubRepo
	releasesAPIURL = "https://api.github.com/repos/" + githubRepo + "/releases/latest"
)

// ErrNoChecksum means the release published no checksum for the host asset.
// go-rag treats this as fatal (Principle II) — it never installs an unverified
// binary.
var ErrNoChecksum = errors.New("no checksum published for this asset")

// latestVersionFn is the function used to fetch the latest release tag. Tests
// override it (the MuninnDB latestVersionFn seam pattern).
var latestVersionFn = latestVersionDefault

// LatestVersion returns the latest release tag (e.g. "v1.3.0"), or ("", nil) if
// currentVersion is "dev"/empty (dev build — check disabled). A network error is
// returned; callers report it rather than implying "up to date".
func LatestVersion(currentVersion string) (string, error) {
	if currentVersion == "" || currentVersion == "dev" {
		return "", nil
	}
	return latestVersionFn()
}

func latestVersionDefault() (string, error) {
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(releasesAPIURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var release struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", err
	}
	return release.TagName, nil
}

// ReleaseAssetURL returns the download URL for the given version and platform.
// The asset is a tar.gz (the in-process self-replace path is Unix-only in v1;
// Windows prints the release URL and exits — see internal/cli/upgrade.go).
func ReleaseAssetURL(version, goos, goarch string) string {
	return fmt.Sprintf(
		"%s/releases/download/%s/go-rag_%s_%s_%s.tar.gz",
		releaseBaseURL, version, version, goos, goarch,
	)
}

func checksumsURL(version string) string {
	return fmt.Sprintf("%s/releases/download/%s/%s", releaseBaseURL, version, checksumsAsset)
}

// assetName is the tar.gz filename for a version/platform pair.
func assetName(version, goos, goarch string) string {
	return fmt.Sprintf("go-rag_%s_%s_%s.tar.gz", version, goos, goarch)
}

// ExpectedSHA256 fetches the release checksums file and returns the SHA-256
// recorded for the host asset. A ("", nil) result means the checksums file
// exists but has no entry for this asset — callers MUST treat that as fatal
// (do not install unverified).
func ExpectedSHA256(version, goos, goarch string) (string, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(checksumsURL(version))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return parseChecksumForAsset(string(body), assetName(version, goos, goarch)), nil
}

// parseChecksumForAsset scans a checksums.txt body (lines of "<sha256>  <asset>")
// and returns the hash for the named asset, or "" if absent. Pure for testing.
func parseChecksumForAsset(body, asset string) string {
	for _, line := range strings.Split(body, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == asset {
			return fields[0]
		}
	}
	return ""
}
