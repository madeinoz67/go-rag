package cli

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"

	"github.com/madeinoz67/go-rag/internal/auth"
	"github.com/madeinoz67/go-rag/internal/daemon"
)

// auth_bootstrap.go implements the spec 045 US6 first-run escape hatch: ensure
// an admin user exists before any transport accepts a login. Called from init
// (and the first start).
//
// Password source (in priority order):
//  1. GORAG_ADMIN_PASSWORD env — sets the admin password on first run AND
//     rotates it on a subsequent run (the documented operator path).
//  2. generated — 24 random bytes, base64url, printed to stdout exactly once.
//
// No implicit "password"/"root"/"admin" default ever ships (Constitution I — no
// insecure defaults). bootstrapAdmin is a no-op when an admin already exists
// and GORAG_ADMIN_PASSWORD is unset (idempotent re-runs of init).

const adminPasswordEnv = "GORAG_ADMIN_PASSWORD"

// bootstrapAdmin ensures the admin user exists at dbPath. It opens the store
// itself (the init path, where no Pebble lock is held yet), then delegates to
// bootstrapAuth. Safe to call on every init — idempotent.
func bootstrapAdmin(dbPath string) error {
	_, db, err := openDB(dbPath)
	if err != nil {
		return err
	}
	defer db.Close()
	bootstrapAuth(auth.NewStore(db), dbPath)
	return nil
}

// bootstrapAuth runs the legacy-token import + admin bootstrap against an
// ALREADY-OPEN store. Used by the daemon serve path, which owns the single
// Pebble file lock for its lifetime and so cannot re-open the store.
//
// Errors are reported to stderr but never abort the caller: a daemon start must
// not fail the whole process because admin bootstrap hit a transient issue —
// the operator can still use API keys, and the next start retries.
func bootstrapAuth(store *auth.Store, dbPath string) {
	// Spec 045 US4: zero-break upgrade — import a pre-existing mcp.token as a
	// real API key before the daemon ever validates a request. One-shot +
	// idempotent; no-op when the file is absent or the store already holds keys.
	if _, err := auth.ImportLegacyTokenFromFile(store, daemon.TokenPath(dbPath), ""); err != nil {
		fmt.Fprintf(os.Stderr, "  warning: legacy token import failed: %v\n", err)
	}

	envPw := os.Getenv(adminPasswordEnv)
	exists, err := auth.AdminExists(store, auth.DefaultAdminUsername)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  warning: admin check failed: %v\n", err)
		return
	}

	switch {
	case envPw != "":
		if _, err := auth.CreateAdmin(store, auth.DefaultAdminUsername, envPw); err != nil {
			fmt.Fprintf(os.Stderr, "  warning: admin create failed: %v\n", err)
			return
		}
		if exists {
			fmt.Println("Admin password rotated (GORAG_ADMIN_PASSWORD).")
		} else {
			fmt.Println("Admin user created (GORAG_ADMIN_PASSWORD).")
		}
	case exists:
		// Already bootstrapped and no rotation requested — nothing to do.
	default:
		// No admin, no env: generate a password and surface it exactly once.
		pw, err := generatedPassword()
		if err != nil {
			fmt.Fprintf(os.Stderr, "  warning: password generation failed: %v\n", err)
			return
		}
		if _, err := auth.CreateAdmin(store, auth.DefaultAdminUsername, pw); err != nil {
			fmt.Fprintf(os.Stderr, "  warning: admin create failed: %v\n", err)
			return
		}
		fmt.Println("Generated admin password — copy it now, it will not be shown again:")
		fmt.Println("  " + pw)
	}
}

// generatedPassword returns a 24-byte (192-bit) base64url password. 192 bits
// exceeds bcrypt's 72-byte input limit with room and is well above any online
// guessing threshold.
func generatedPassword() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
