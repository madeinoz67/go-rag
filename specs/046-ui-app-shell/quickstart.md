# Quickstart — go-rag UI Console, Slice 0 (validation guide)

**Spec**: [spec.md](./spec.md) · **Contract**: [contracts/ui-transport.md](./contracts/ui-transport.md)

Runnable validation that the Slice 0 console works end-to-end. Run on an
**isolated DB** with **non-default ports** so you do not collide with the user's
live daemon (per the repo CLAUDE.md smoke-test rule).

## Prerequisites

- `./bin/go-rag` built (`make build`).
- A throwaway DB path, e.g. `/tmp/gorag-ui-smoke`.
- Real Chrome via the **Interceptor** skill for the browser step.
- (Optional, for the auth regime) an admin user bootstrapped in the smoke DB.

## 1. Start the daemon with the UI transport

```bash
./bin/go-rag init --db-path /tmp/gorag-ui-smoke   # creates vault + admin (prints admin password ONCE — copy it)
./bin/go-rag start \
  --db-path /tmp/gorag-ui-smoke \
  --mcp-addr 127.0.0.1:18788 \
  --rest-addr 127.0.0.1:18789 \
  --ui-addr  127.0.0.1:7881
```

**Admin password:** `init` prints a generated admin password to stdout **exactly
once** — copy it; it is never shown again and is stored only as a bcrypt hash.
This is the credential you use to log in to the console (§3, §4). To set or
rotate it instead, run `GORAG_ADMIN_PASSWORD=<pw> ./bin/go-rag init --db-path …`
(prints "Admin password rotated"). There is no `go-rag auth` subcommand to read
or reset a forgotten password — rotation is env-var-driven.

**Expected:** the daemon binds three loopback ports; the bound-address log line
includes `UI 127.0.0.1:7881`. (`--mcp-addr`/`--rest-addr` are overridden only to
avoid colliding with any running default daemon.)

## 2. Smoke — shell + bypass posture (loopback)

The shell is served publicly (it *is* the login page — no data in the HTML), so
it loads on any vault:

```bash
curl -s -o /dev/null -w '%{http_code}\n' http://127.0.0.1:7881/
```

**Expected:** `200` (the shell HTML) — bare or initialized vault alike; the
shell must load so the login form can render.

The API is Bearer-guarded. Because `init` creates an admin (§1), the spec 045
loopback bypass is **disabled** on an init'd vault, so an unauthenticated stats
call is rejected:

```bash
curl -s -o /dev/null -w '%{http_code}\n' http://127.0.0.1:7881/api/dashboard/stats
```

**Expected:** `401` (no Bearer, admin exists → bypass disabled). The bypass
fires only on a bare **pre-init** vault (no admin record) — pinned by spec 045's
`TestBypassGuard_BareVaultBypasses_InitializedVaultDoesNot`. To see real
Dashboard data, authenticate first (§3) and confirm no `Set-Cookie` on any
response (`curl -i …`).

## 3. Smoke — auth regime (initialized vault)

Once an admin exists, the bypass is disabled and Bearer is required:

```bash
# No bearer → 401 on the API (admin exists → bypass disabled; shell at GET / is public, /api/* guarded)
curl -s -o /dev/null -w '%{http_code}\n' http://127.0.0.1:7881/api/dashboard/stats

# Login → mint a gorags_ session. <admin-pass> = the password init printed in
# §1 (or your GORAG_ADMIN_PASSWORD).
TOK=$(curl -s -X POST http://127.0.0.1:7881/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"<admin-pass>"}' | jq -r .token)
echo "$TOK"   # gorags_…

# Authenticated stats
curl -s http://127.0.0.1:7881/api/dashboard/stats -H "Authorization: Bearer $TOK" | jq .documents
```

**Expected:** `/login` returns `{"token":"gorags_…","expires_at":"…"}` with **no
`Set-Cookie`**; the Bearer-gated stats call returns `200`. A wrong password
returns `401` with the identical error body (no oracle).

## 4. Browser verify (Interceptor — mandatory for user-facing artifacts)

Drive the real flow in Chrome:

1. Open `http://127.0.0.1:7881/` → login screen renders (initialized vault).
2. Submit admin credentials → shell loads, sidebar visible.
3. **Sidebar:** exactly 8 items — Dashboard, Documents, Query, Bridge Ops,
   Vaults, Observability, Settings, Memory & Graph.
4. Click **Dashboard** → real stat tiles (documents / chunks / embeddings /
   index health) matching `./bin/go-rag status --db-path /tmp/gorag-ui-smoke`.
5. Click each other sidebar item → the standard placeholder panel renders
   ("planned — see spec NNN").
6. Navigate between views → shell chrome does not reload (client-side Alpine).
7. DevTools Network tab → all assets served from `/static/*` (no CDN origin);
   no `Set-Cookie` on any response.

## 5. No-Node assertion

```bash
test ! -e package.json && echo "no package.json (correct)"
test ! -e node_modules   && echo "no node_modules (correct)"
```

**Expected:** both print "correct" — the SPA is vendored + `go:embed`-ded, no
front-end build chain.

## 6. Teardown

```bash
./bin/go-rag stop --db-path /tmp/gorag-ui-smoke
rm -rf /tmp/gorag-ui-smoke
```

## Pass criteria (Slice 0 done)

- Bypass regime: `GET /` and `/api/dashboard/stats` return 200 on a bare loopback vault.
- Auth regime: `/login` mints a `gorags_` Bearer (no cookie); guarded routes 401 without it, 200 with it.
- Dashboard counts match `go-rag status` exactly (cross-transport parity).
- Browser: 8 sidebar items, real Dashboard, 7 placeholders, no shell reload on nav.
- No Node/Vite/Tailwind artifacts; single binary serves everything.
