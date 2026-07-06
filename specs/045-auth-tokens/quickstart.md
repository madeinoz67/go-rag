# Quickstart: Authentication & Tokens

Runnable validation scenarios proving the feature end-to-end. **Prereq**: `make build` succeeds; use an isolated temp vault (never the global `~/.go-rag/vaults/default`).

## 1. Bootstrap creates the admin user

```bash
V=/tmp/gorag-auth-qt && rm -rf $V
GORAG_ADMIN_PASSWORD=secret ./bin/go-rag init --db-path $V
# (with no env: a generated password is printed once; no password/root default ships)
```
**Expect**: an `admin` user exists; bootstrap is idempotent on re-run.

## 2. Create an API key; use it on REST, gRPC, and MCP

```bash
TOK=$(./bin/go-rag auth create --label bridge --mode write --db-path $V | sed -n 's/.*\(gorag_[A-Za-z0-9_-]*\).*/\1/p')
./bin/go-rag auth list --db-path $V          # shows "bridge", never the secret
./bin/go-rag start --db-path $V --rest-addr 127.0.0.1:7879 --grpc-addr 127.0.0.1:7880 &
curl -s -H "Authorization: Bearer $TOK" http://127.0.0.1:7879/api/status   # 200
# same $TOK also authenticates the gRPC + MCP transports (US2)
./bin/go-rag auth revoke $(./bin/go-rag auth list --db-path $V | awk '/bridge/{print $1}') --db-path $V
curl -s -H "Authorization: Bearer $TOK" http://127.0.0.1:7879/api/status   # 401
```
**Expect**: identical acceptance across REST/gRPC/MCP; revoked key → 401 everywhere.

## 3. UI login → Bearer session (no cookie ever)

```bash
./bin/go-rag start --db-path $V --ui-addr 127.0.0.1:7881 &        # /api/auth/* ships here; full UI is spec 046
SESS=$(curl -s -X POST http://127.0.0.1:7881/api/auth/login -H 'Content-Type: application/json' \
       -d '{"username":"admin","password":"secret"}' | jq -r .token)   # gorags_…
curl -s -H "Authorization: Bearer $SESS" http://127.0.0.1:7881/api/status             # 200
curl -s -D - -o /dev/null -X POST http://127.0.0.1:7881/api/auth/logout \
       -H "Authorization: Bearer $SESS" | grep -i set-cookie                         # (no output — SC-003)
curl -s -H "Authorization: Bearer $SESS" http://127.0.0.1:7881/api/status            # 401 (session revoked)
```
**Expect**: login returns a `gorags_…` token; **no `Set-Cookie`** on any response; logout invalidates the session.

## 4. `mcp.token` migration (zero-break upgrade)

```bash
printf 'my-old-secret' > $V/mcp.token
./bin/go-rag auth list --db-path $V                                       # shows a "legacy-mcp" key
curl -s -H "Authorization: Bearer my-old-secret" http://127.0.0.1:7879/api/status   # 200 (old value still works)
```
**Expect**: the legacy value authenticates via the SHA-256 lookup path; a deprecation notice is logged; skipped if the key store is non-empty.

## 5. Loopback bypass vs LAN fail-closed

```bash
E=/tmp/gorag-empty && rm -rf $E
./bin/go-rag start --db-path $E --rest-addr 127.0.0.1:7879 &
curl -s http://127.0.0.1:7879/api/status            # 200 (loopback + empty stores → bypass)
curl -s http://<lan-ip>:7879/api/status             # 401 (non-loopback → fail-closed)
```

## 6. Build/test/lint gates (SC-006 / SC-007)

```bash
make lint && make vet && make test                   # all clean
```

> NOTE: the exact listing/revoke command shapes are finalized in `tasks.md`; the scenarios above assert the *behavior*, not the precise flag grammar.
