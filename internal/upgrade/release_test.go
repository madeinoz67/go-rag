package upgrade

import "testing"

// swapLatestFn replaces the latestVersionFn seam for the duration of a test and
// returns a restore function.
func swapLatestFn(fn func() (string, error)) func() {
	prev := latestVersionFn
	latestVersionFn = fn
	return func() { latestVersionFn = prev }
}

func TestLatestVersionDevSkipsNetwork(t *testing.T) {
	called := false
	restore := swapLatestFn(func() (string, error) {
		called = true
		return "v9.9.9", nil
	})
	defer restore()

	// A "dev" build must short-circuit without touching the network.
	v, err := LatestVersion("dev")
	if err != nil || v != "" {
		t.Errorf("LatestVersion(dev) = (%q,%v), want (\"\",nil)", v, err)
	}
	if called {
		t.Error("network seam was called for a dev build")
	}

	// A real version delegates to the seam.
	v, err = LatestVersion("v1.0.0")
	if err != nil || v != "v9.9.9" {
		t.Errorf("LatestVersion(v1.0.0) = (%q,%v), want (\"v9.9.9\",nil)", v, err)
	}
}

func TestReleaseAssetURL(t *testing.T) {
	got := ReleaseAssetURL("v1.3.0", "darwin", "arm64")
	want := "https://github.com/madeinoz67/go-rag/releases/download/v1.3.0/go-rag_v1.3.0_darwin_arm64.tar.gz"
	if got != want {
		t.Errorf("ReleaseAssetURL = %q, want %q", got, want)
	}
}

func TestParseChecksumForAsset(t *testing.T) {
	body := "abc123  go-rag_v1.3.0_darwin_arm64.tar.gz\n" +
		"def456  go-rag_v1.3.0_linux_amd64.tar.gz\n"
	if got := parseChecksumForAsset(body, "go-rag_v1.3.0_darwin_arm64.tar.gz"); got != "abc123" {
		t.Errorf("darwin hash = %q, want abc123", got)
	}
	if got := parseChecksumForAsset(body, "go-rag_v1.3.0_windows_amd64.tar.gz"); got != "" {
		t.Errorf("absent asset = %q, want empty", got)
	}
}
