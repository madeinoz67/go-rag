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
./bin/go-rag init --db-path /tmp/gorag-ui-smoke   # creates the vault + admin
./bin/go-rag start \
  --db-path /tmp/gorag-ui-smoke \
  --mcp-addr 127.0.0.1:18788 \
  --rest-addr 127.0.0.1:18789 \
  --ui-addr  127.0.0.1:7881
```

**Expected:** the daemon binds three loopback ports; the bound-address log line
includes `UI 127.0.0.1:7881`. (`--mcp-addr`/`--rest-addr` are overridden only to
avoid colliding with any running default daemon.)

## 2. Smoke — bypass regime (bare vault, loopback)

On a freshly-init'd vault the spec 045 loopback bypass admits loopback requests:

```bash
curl -s -o /dev/null -w '%{http_code}\n' http://127.0.0.1:7881/
```

**Expected:** `200` (the shell HTML). Then:

```bash
curl -s http://127.0.0.1:7881/api/dashboard/stats | jq .
```

**Expected:** a `DashboardDTO` with `documents: 0`, `embeddings_complete: true`
(empty corpus is "complete"), `vault: "gorag-ui-smoke"` (derived from the DB
basename), and no `Set-Cookie` header (`curl -i …` to confirm).

## 3. Smoke — auth regime (initialized vault)

Once an admin exists, the bypass is disabled and Bearer is required:

```bash
# No bearer → 401 (shell not served over the API; the SPA shows login client-side)
curl -s -o /dev/null -w '%{http_code}\n' http://127.0.0.1:7881/api/dashboard/stats   # after admin bootstrap on a fresh loopback session

# Login → mint a gorags_ session
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
