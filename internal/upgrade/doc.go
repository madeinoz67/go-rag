// Package upgrade implements go-rag's in-process binary self-upgrade.
//
// It resolves the latest release from GitHub, downloads the OS/arch asset,
// verifies it against a published SHA-256 checksum, and atomically replaces
// the running binary. The mechanism is modeled on MuninnDB's `muninn upgrade`
// (cmd/muninn/upgrade.go) with two strengthenings required by go-rag's
// constitution: cryptographic checksum verification inside the in-process path
// (MuninnDB verifies functionally only), and no package-manager delegation
// (Principle III — pure Go, single binary, no brew/apt/scoop).
//
// Schema migration of the on-disk store is a separate concern handled by
// internal/storage/migrate on store open; this package only swaps the binary.
package upgrade
