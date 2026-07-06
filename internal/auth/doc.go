// Package auth provides go-rag's credential validation for all transports
// (CLI, MCP, REST, gRPC) and the future web UI (spec 046).
//
// Two credential types are supported:
//
//   - API keys (gorag_<base64url(32)>) — long-lived, labelled, scoped, hashed
//     credentials for programmatic clients. Created via `go-rag auth create`.
//   - Sessions (gorags_<base64url(32)>) — short-lived, opaque, store-tracked
//     tokens minted by admin login, carried in the browser's sessionStorage as
//     a Bearer header (never a cookie — CSRF-free).
//
// Both are validated by SHA-256 hash-lookup: the hash of the presented secret
// is the Pebble key, so a Get hit IS the match (no secret-string comparison).
// See specs/045-auth-tokens/spec.md and docs/design/auth-tokens.md.
package auth
