package upgrade

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// VerifyChecksum computes the SHA-256 of the file at path and compares it to
// expected. An empty expected is fatal (ErrNoChecksum): go-rag never installs an
// unverified binary (constitution Principle II — content-addressed identity).
func VerifyChecksum(path, expected string) error {
	if expected == "" {
		return ErrNoChecksum
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
	if got != strings.ToLower(expected) {
		return fmt.Errorf("checksum mismatch: expected %s, got %s", expected, got)
	}
	return nil
}

// VerifyExecutable checks that path is a runnable file (Unix exec bit). Windows
// does not use the execute bit, so the check is skipped there.
func VerifyExecutable(path string) error {
	fi, err := os.Stat(path)
	if err != nil {
		return err
	}
	if runtime.GOOS != "windows" && fi.Mode()&0o111 == 0 {
		return fmt.Errorf("%s is not executable", path)
	}
	return nil
}

// VerifyRunsVersion executes "<path> version" and confirms the output contains
// the expected version tag. This is a functional smoke check layered on top of
// the cryptographic checksum (MuninnDB verifyBinary). It is best-effort: callers
// treat checksum failure as the hard gate and a version-smoke failure as a
// warning, since the binary may not be runnable in every environment.
func VerifyRunsVersion(path, expectedVersion string) error {
	out, err := exec.Command(path, "version").CombinedOutput()
	if err != nil {
		return fmt.Errorf("version smoke check failed: %w", err)
	}
	if !strings.Contains(string(out), strings.TrimPrefix(expectedVersion, "v")) {
		return fmt.Errorf("version smoke check: expected %s in %q", expectedVersion, out)
	}
	return nil
}
