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

// bootstrapAdmin ensures the admin user exists at dbPath. Safe to call on every
// init/start — it only acts when there is no admin, or when the operator sets
// GORAG_ADMIN_PASSWORD to (re)set the password. It also imports any legacy
// mcp.token as a `legacy-mcp` admin API key (spec 045 US4) so pre-upgrade
// scripts keep authenticating through the new validator.
func bootstrapAdmin(dbPath string) error {
	_, db, err := openDB(dbPath)
	if err != nil {
		return err
	}
	defer db.Close()
	store := auth.NewStore(db)

	// Spec 045 US4: zero-break upgrade — import a pre-existing mcp.token as a
	// real API key before the daemon ever validates a request. One-shot +
	// idempotent; no-op when the file is absent or the store already holds keys.
	if _, err := auth.ImportLegacyTokenFromFile(store, daemon.TokenPath(dbPath), ""); err != nil {
		fmt.Fprintf(os.Stderr, "  warning: legacy token import failed: %v\n", err)
	}

	envPw := os.Getenv(adminPasswordEnv)
	exists, err := auth.AdminExists(store, auth.DefaultAdminUsername)
	if err != nil {
		return err
	}

	switch {
	case envPw != "":
		if _, err := auth.CreateAdmin(store, auth.DefaultAdminUsername, envPw); err != nil {
			return err
		}
		if exists {
			fmt.Println("Admin password rotated (GORAG_ADMIN_PASSWORD).")
		} else {
			fmt.Println("Admin user created (GORAG_ADMIN_PASSWORD).")
		}
		return nil
	case exists:
		// Already bootstrapped and no rotation requested — nothing to do.
		return nil
	default:
		// No admin, no env: generate a password and surface it exactly once.
		pw, err := generatedPassword()
		if err != nil {
			return err
		}
		if _, err := auth.CreateAdmin(store, auth.DefaultAdminUsername, pw); err != nil {
			return err
		}
		fmt.Println("Generated admin password — copy it now, it will not be shown again:")
		fmt.Println("  " + pw)
		return nil
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
