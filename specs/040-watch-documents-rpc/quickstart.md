# Quickstart — WatchDocuments (BL-008)

> Phase 1 validation guide for `/speckit-plan`. Runnable scenarios that prove the streaming feature end-to-end. No implementation bodies — those live in `tasks.md`. References [data-model.md](./data-model.md) and [contracts/api.md](./contracts/api.md).

## Prerequisites

- Built binary: `make build` → `./bin/go-rag` (`CGO_ENABLED=0`).
- A throwaway vault + the daemon running with gRPC:
  ```bash
  export GR_DIR="$(mktemp -d)/vault"
  ./bin/go-rag init --db-path "$GR_DIR"
  ./bin/go-rag start --db-path "$GR_DIR" \
    --mcp-addr 127.0.0.1:17878 --rest-addr 127.0.0.1:17879 --grpc-addr 127.0.0.1:17880 &
  sleep 2
  ```
- A gRPC client that opens a server-streaming `WatchDocuments` call (e.g. `grpcurl`):
  ```bash
  # Install: go install github.com/fullstorydev/grpcurl/cmd/grpcurl@latest
  ```

## Scenario 1 — INGESTED arrives within ~500ms of `add`

```bash
# Open the stream (blocks, printing events):
grpcurl -plaintext 127.0.0.1:17880 gorag.Gorag/WatchDocuments &  # cursor="" → from now
STREAM_PID=$!
sleep 1
echo "hello world about authentication tokens" > "$GR_DIR/note.txt"
./bin/go-rag add "$GR_DIR/note.txt" --db-path "$GR_DIR"
sleep 2
kill $STREAM_PID
```

**Expected.** An `INGESTED` event for the new document arrives within ~500ms of the `add` returning, carrying `document_id`, `source_path`, an opaque `cursor`, the `after` `DocumentMeta`, and a `timestamp_ms`. (contracts/api.md §Operation contract; FR-002.)

## Scenario 2 — EMBEDDED arrives after async embedding completes

```bash
grpcurl -plaintext 127.0.0.1:17880 gorag.Gorag/WatchDocuments &
STREAM_PID=$!
echo "second doc about sessions and retrieval" > "$GR_DIR/note2.txt"
./bin/go-rag add "$GR_DIR/note2.txt" --db-path "$GR_DIR"
sleep 5   # allow async embedding (depends on Ollama)
kill $STREAM_PID
```

**Expected.** An `INGESTED` event, then (once the async embedding worker finishes) an `EMBEDDED` event for the same `document_id`, within ~500ms of embed completion. (FR-003.)

## Scenario 3 — DELETED arrives after a scan detects a deletion

```bash
grpcurl -plaintext 127.0.0.1:17880 gorag.Gorag/WatchDocuments &
STREAM_PID=$!
rm "$GR_DIR/note.txt"
./bin/go-rag scan --db-path "$GR_DIR"
sleep 2
kill $STREAM_PID
```

**Expected.** A `DELETED` event for the removed document within ~500ms of the scan detecting the deletion. (FR-004.)

## Scenario 4 — cursor resume (within the in-flight window)

```bash
# First stream: capture the last cursor, then disconnect.
grpcurl -plaintext 127.0.0.1:17880 gorag.Gorag/WatchDocuments > /tmp/watch1.txt &
P1=$!; sleep 1
echo "doc for cursor test" > "$GR_DIR/c1.txt"; ./bin/go-rag add "$GR_DIR/c1.txt" --db-path "$GR_DIR"
sleep 2; kill $P1
LAST_CURSOR=$(grep -oE 'cursor: "[^"]+"' /tmp/watch1.txt | tail -1 | sed 's/cursor: "//;s/"//')
# Add more while disconnected.
echo "doc after disconnect" > "$GR_DIR/c2.txt"; ./bin/go-rag add "$GR_DIR/c2.txt" --db-path "$GR_DIR"
sleep 1
# Reconnect with the cursor.
grpcurl -d "{\"cursor\":\"$LAST_CURSOR\"}" -plaintext 127.0.0.1:17880 gorag.Gorag/WatchDocuments &
P2=$!; sleep 2; kill $P2
```

**Expected.** The reconnect delivers the event(s) that occurred after `LAST_CURSOR` (within the bus's in-flight window) — no duplicates of events at/before the cursor. (FR-006.) **MVP caveat**: if the disconnect outlasts the buffer (>64 events), older events are dropped + resume fast-forwards (R2 honest limitation).

## Scenario 5 — concurrent streams both receive the events

```bash
grpcurl -plaintext 127.0.0.1:17880 gorag.Gorag/WatchDocuments > /tmp/s1.txt &  S1=$!
grpcurl -plaintext 127.0.0.1:17880 gorag.Gorag/WatchDocuments > /tmp/s2.txt &  S2=$!
sleep 1
echo "concurrent doc" > "$GR_DIR/c3.txt"; ./bin/go-rag add "$GR_DIR/c3.txt" --db-path "$GR_DIR"
sleep 2; kill $S1 $S2
diff <(sort /tmp/s1.txt) <(sort /tmp/s2.txt)
```

**Expected.** Both streams received the same `INGESTED` event (no diff). (FR-010.)

## Scenario 6 — slow consumer doesn't block the publisher

(Manual / integration test — hard to script with grpcurl.) Open a stream and deliberately stop reading from it (or read very slowly) while rapidly adding many documents. Assert: `add`/`scan` keep completing promptly (publisher not blocked) and a second, normally-reading stream still receives events. The slow stream's `dropped` counter climbs (logged warning). (FR-011.)

## Build & test gates

```bash
make build          # CGO_ENABLED=0 — must succeed
make vet            # go vet ./...
make lint           # golangci-lint run (CI gate)
make test           # go test -race -cover ./...  (incl. the new -race streaming test)
```

**Must include:** a `WatchDocuments` streaming test over bufconn — add/embed/delete → events in order + within ~500ms; reconnect-with-cursor (no dupes); two concurrent streams; slow-consumer publisher-not-blocked.

## Constitution affirmation (for the PR)

In-memory event bus — no on-disk key-space change, no migration, `migrate.ExpectedVersion` unchanged (FR-016); pure Go, no new deps (grpc-go already present). Principles I–V pass; Principle V's gRPC-only scope is justified (streaming has no unary equivalent; REST push = BL-011, separate). The Pebble-backed event-log follow-on (cross-restart resume) is explicitly migration-gated.
