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
# CLI create/list/revoke hold the Pebble write lock, so they run with the daemon
# STOPPED. While the daemon runs, manage keys via the go_rag_auth_* MCP tools.
TOK=$(./bin/go-rag auth create --label bridge --mode write --db-path $V --json | jq -r .secret)
./bin/go-rag auth list --db-path $V          # shows "bridge", never the secret
./bin/go-rag start --db-path $V --mcp-addr 127.0.0.1:7878 --rest-addr 127.0.0.1:7879 --grpc-addr 127.0.0.1:7880 &
sleep 2
curl -s -o /dev/null -w '%{http_code}\n' -H "Authorization: Bearer $TOK" http://127.0.0.1:7879/v1/status   # 200
# same $TOK also authenticates the gRPC + MCP transports (US2)
./bin/go-rag stop --db-path $V
./bin/go-rag auth revoke $(./bin/go-rag auth list --db-path $V | awk '/bridge/{print $1}') --db-path $V
./bin/go-rag start --db-path $V --mcp-addr 127.0.0.1:7878 --rest-addr 127.0.0.1:7879 --grpc-addr 127.0.0.1:7880 &
sleep 2
curl -s -o /dev/null -w '%{http_code}\n' -H "Authorization: Bearer $TOK" http://127.0.0.1:7879/v1/status   # 401
```
**Expect**: identical acceptance across REST/gRPC/MCP; revoked key → 401 everywhere.

## 3. UI login → Bearer session (no cookie ever)

> `/api/auth/*` ships on the REST transport (`:7879`) now; the dedicated UI port
> (`:7881`) and the SPA are spec 046. The no-cookie contract is identical.

```bash
./bin/go-rag start --db-path $V --mcp-addr 127.0.0.1:7878 --rest-addr 127.0.0.1:7879 &
sleep 2
SESS=$(curl -s -X POST http://127.0.0.1:7879/api/auth/login -H 'Content-Type: application/json' \
       -d '{"username":"admin","password":"secret"}' | jq -r .token)         # gorags_…
curl -s -o /dev/null -w '%{http_code}\n' -H "Authorization: Bearer $SESS" http://127.0.0.1:7879/v1/status   # 200
curl -s -D - -o /dev/null -X POST http://127.0.0.1:7879/api/auth/logout \
       -H "Authorization: Bearer $SESS" | grep -i set-cookie                 # (no output — SC-003)
curl -s -o /dev/null -w '%{http_code}\n' -H "Authorization: Bearer $SESS" http://127.0.0.1:7879/v1/status   # 401
```
**Expect**: login returns a `gorags_…` token; **no `Set-Cookie`** on any response; logout invalidates the session.

## 4. `mcp.token` migration (zero-break upgrade)

The import runs once on first open (init/start) when the API-key store is empty.
So seed `mcp.token` on a FRESH vault (before any key is minted):

```bash
F=/tmp/gorag-leg && rm -rf $F && mkdir -p $F
printf 'my-old-secret' > $F/mcp.token
GORAG_ADMIN_PASSWORD=secret ./bin/go-rag init --db-path $F --embedding-provider ollama  # imports mcp.token as a legacy-mcp key (90-day expiry)
./bin/go-rag auth list --db-path $F                                       # shows a "legacy-mcp" key, expires ~90d
./bin/go-rag start --db-path $F --mcp-addr 127.0.0.1:7878 --rest-addr 127.0.0.1:7879 &
sleep 2
curl -s -o /dev/null -w '%{http_code}\n' -H "Authorization: Bearer my-old-secret" http://127.0.0.1:7879/v1/status   # 200
```
**Expect**: the unchanged legacy value authenticates via the SHA-256 raw-hash lookup; a one-time deprecation notice is logged at import; the key sunsets in ~90 days (decommission by minting a `gorag_` key and revoking `legacy-mcp`).

## 5. Loopback bypass vs LAN fail-closed

The bypass applies to the DATA API (`/v1/*`) when the peer is loopback and NO API
key has been minted (the bootstrap admin alone does not disable it — it's a login
identity, not an enforcement signal). Minting any key disables it everywhere.

```bash
E=/tmp/gorag-bp && rm -rf $E
GORAG_ADMIN_PASSWORD=pw ./bin/go-rag init --db-path $E --embedding-provider ollama
./bin/go-rag start --db-path $E --mcp-addr 127.0.0.1:7878 --rest-addr 127.0.0.1:7879 &
sleep 2
curl -s -o /dev/null -w '%{http_code}\n' http://127.0.0.1:7879/v1/status            # 200 (loopback + no key → bypass)
curl -s -o /dev/null -w '%{http_code}\n' http://127.0.0.1:7879/api/auth/session     # 401 (management is strict — no bypass)
curl -s -o /dev/null -w '%{http_code}\n' http://<lan-ip>:7879/v1/status             # 401 (non-loopback → fail-closed)
```

## 6. Build/test/lint gates (SC-006 / SC-007)

```bash
make lint && make vet && make test                   # all clean
```

> NOTE: the exact listing/revoke command shapes are finalized in `tasks.md`; the scenarios above assert the *behavior*, not the precise flag grammar.
