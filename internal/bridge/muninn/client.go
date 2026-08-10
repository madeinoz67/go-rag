package muninn

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// Client is the transport-agnostic surface the bridge talks to MuninnDB through
// (contracts/muninn-grpc-client.md). The gRPC implementation lives in
// grpc_client.go; tests use FakeClient, which faithfully simulates the UPSERT
// semantics so cognitive-hygiene (NFR-002) is assertable without a live server.
//
// All methods are safe for concurrent use. The caller owns the context; a
// cancelled context aborts the in-flight RPC. Healthy() never blocks.
type Client interface {
	// Hello probes server health and returns capabilities (MaxEngramContentBytes
	// feeds the mapper's sub-chunking decision).
	Hello(ctx context.Context) (*Capabilities, error)

	// Write performs a single engram write. Under UpsertMode, MuninnDB pins the
	// engram to sha256(IdempotentID) via its durable forward index and applies
	// the create/left-alone/evolve contract. Returns the engram id + created_at
	// (MuninnDB gives no outcome enum — the bridge verifies no-ops via Read).
	Write(ctx context.Context, p WriteParams) (id string, createdAt int64, err error)

	// BatchWrite writes up to 50 items in one call; per-item results report
	// index/id/error (an empty id + non-empty error marks that item's failure).
	BatchWrite(ctx context.Context, vault string, batch []WriteParams) ([]BatchItemResult, error)

	// Read fetches a full engram (used for NFR-002 verification + the view detail).
	Read(ctx context.Context, vault, id string) (*Engram, error)

	// Activate streams a recall browse over the vault (the Memory & Graph view).
	// The channel closes when the stream ends or errs; the caller MUST drain it.
	Activate(ctx context.Context, vault string, phrases []string, limit int) (<-chan Activation, error)

	// Healthy reports the last-known connection health (never blocks, never errors).
	Healthy() bool

	// Close releases the underlying transport.
	Close() error
}

// WriteParams is the bridge's domain view of a MuninnDB WriteRequest. The gRPC
// client adapts this to muninn_v1.WriteRequest; the mapper builds it from a chunk.
//
// Maintainer invariants (research.md R4) are enforced by the mapper, not here:
// Embedding MUST be nil; Stability 30.0 for reference chunks; IdempotentID MUST
// be non-empty whenever UpsertMode is true (MuninnDB rejects the bare case).
type WriteParams struct {
	Concept      string
	Content      string
	Tags         []string
	Vault        string
	Stability    float32
	Confidence   float32
	IdempotentID string // "chunk:"+chunkID under the bridge's content-addressing
	UpsertMode   bool
	TypeLabel    string
	MemoryType   uint32
	Embedding    []float32     // nil (maintainer invariant)
	Associations []Association // wikilink edges (BL-004), weight 0.6–0.8
}

// Association is one entity edge attached to a write (mirrors muninn_v1.Association).
type Association struct {
	TargetID   string
	RelType    uint32
	Weight     float32
	Confidence float32
}

// Engram is the full read-back of a promoted memory (muninn_v1.ReadResponse).
type Engram struct {
	ID          string
	Concept     string
	Content     string
	Tags        []string
	AccessCount int64
	Stability   float32
	State       string
	CreatedAt   int64
	UpdatedAt   int64
	LastAccess  int64
}

// Activation is one row of an Activate browse stream.
type Activation struct {
	EngramID   string
	Concept    string
	Score      float32
	LastAccess int64
	Tags       []string
}

// BatchItemResult is the per-item outcome of a BatchWrite.
type BatchItemResult struct {
	Index int
	ID    string
	Error string
}

// Capabilities is the subset of Hello response the bridge consults.
type Capabilities struct {
	MaxEngramContentBytes int
	ServerVersion         string
}

// contentKey is sha256(content) — the byte-identical-content test the UPSERT
// no-op relies on (research.md R1). Distinct from the idempotent_id (which is
// the bridge's "chunk:"+chunkID); the server keys on sha256(idempotent_id) but
// decides left-alone vs evolve by comparing content. The fake mirrors that.
func contentKey(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// idempotentKey is sha256(idempotent_id), matching MuninnDB's 0x2F/0x30 forward
// index key. The fake uses it to pin one engram per idempotent_id per vault.
func idempotentKey(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// FakeClient is a Client implementation for tests. It faithfully simulates
// the shipped UPSERT semantics (#556 / PR #659): for a given vault, an
// idempotent_id pins exactly one engram; a later write with the SAME idempotent_id
// and byte-identical content is a STRICT NO-OP (no AccessCount/UpdatedAt/LastAccess
// mutation); a later write with CHANGED content evolves (new engram, predecessor
// superseded). This is the contract NFR-002 asserts — a fake that bumps
// AccessCount on re-promotion MUST fail the cognitive-hygiene test.
type FakeClient struct {
	mu sync.Mutex

	// idempotent[vault][idempotentKey(id)] -> engram id (the forward index)
	idempotent map[string]map[string]string
	// engrams[vault][engramID] -> *Engram (the store)
	engrams map[string]map[string]*Engram

	health atomic.Bool
	caps   *Capabilities

	// FailN, if > 0, makes the next N Write/BatchWrite calls return errOut (simulates
	// an unreachable / erroring MuninnDB for circuit-breaker tests).
	FailN  int32
	errOut error
}

// NewFakeClient returns a healthy FakeClient with empty stores.
func NewFakeClient() *FakeClient {
	f := &FakeClient{
		idempotent: make(map[string]map[string]string),
		engrams:    make(map[string]map[string]*Engram),
		caps:       &Capabilities{MaxEngramContentBytes: 1 << 20, ServerVersion: "fake"},
	}
	f.health.Store(true)
	return f
}

// SetHealth toggles the reported health (for circuit-breaker / degrade tests).
func (f *FakeClient) SetHealth(healthy bool) { f.health.Store(healthy) }

// FailNext makes the next n Write/BatchWrite calls return err.
func (f *FakeClient) FailNext(n int32, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.FailN = n
	f.errOut = err
}

func (f *FakeClient) Hello(_ context.Context) (*Capabilities, error) {
	if !f.health.Load() {
		return nil, errFakeUnhealthy
	}
	return f.caps, nil
}

func (f *FakeClient) Healthy() bool { return f.health.Load() }

func (f *FakeClient) Close() error { return nil }

// Write applies the UPSERT contract for a single item (see FakeClient doc).
func (f *FakeClient) Write(ctx context.Context, p WriteParams) (string, int64, error) {
	res, err := f.BatchWrite(ctx, p.Vault, []WriteParams{p})
	if err != nil {
		return "", 0, err
	}
	if len(res) == 0 {
		return "", 0, nil
	}
	if res[0].Error != "" {
		return "", 0, errFakeWrite
	}
	f.mu.Lock()
	e := f.engrams[p.Vault][res[0].ID]
	createdAt := int64(0)
	if e != nil {
		createdAt = e.CreatedAt
	}
	f.mu.Unlock()
	return res[0].ID, createdAt, nil
}

// BatchWrite applies the UPSERT contract per item. The "left alone" no-op is the
// load-bearing case: an unchanged chunk re-promoted across restarts/re-ingest
// lands here and mutates NOTHING.
func (f *FakeClient) BatchWrite(_ context.Context, vault string, batch []WriteParams) ([]BatchItemResult, error) {
	if !f.health.Load() {
		return nil, errFakeUnhealthy
	}
	f.mu.Lock()
	if f.FailN > 0 {
		f.FailN--
		healthy := f.health.Load()
		err := f.errOut
		f.mu.Unlock()
		_ = healthy
		if err == nil {
			return nil, errFakeWrite
		}
		return nil, err
	}
	if f.idempotent[vault] == nil {
		f.idempotent[vault] = make(map[string]string)
	}
	if f.engrams[vault] == nil {
		f.engrams[vault] = make(map[string]*Engram)
	}
	results := make([]BatchItemResult, len(batch))
	for i, p := range batch {
		if p.UpsertMode && p.IdempotentID == "" {
			results[i] = BatchItemResult{Index: i, Error: "upsert_mode requires idempotent_id"}
			continue
		}
		key := idempotentKey(p.IdempotentID)
		if existingID, ok := f.idempotent[vault][key]; ok {
			existing := f.engrams[vault][existingID]
			if existing != nil && contentKey(existing.Content) == contentKey(p.Content) {
				// STRICT NO-OP: identical content. Touch nothing — no access bump,
				// no UpdatedAt refresh, no LastAccess change. This is the cognitive-
				// hygiene contract NFR-002 asserts.
				results[i] = BatchItemResult{Index: i, ID: existingID}
				continue
			}
			// CHANGED content: evolve — new engram supersedes the predecessor.
			nid := newFakeID()
			now := time.Now().UnixNano()
			f.engrams[vault][nid] = &Engram{
				ID: nid, Concept: p.Concept, Content: p.Content, Tags: p.Tags,
				AccessCount: 0, Stability: p.Stability, State: "active",
				CreatedAt: now, UpdatedAt: now, LastAccess: 0,
			}
			if existing != nil {
				existing.State = "superseded"
			}
			f.idempotent[vault][key] = nid // re-point forward index at the new head
			results[i] = BatchItemResult{Index: i, ID: nid}
			continue
		}
		// CREATED: fresh engram, no inherited cognitive state.
		nid := newFakeID()
		now := time.Now().UnixNano()
		f.engrams[vault][nid] = &Engram{
			ID: nid, Concept: p.Concept, Content: p.Content, Tags: p.Tags,
			AccessCount: 0, Stability: p.Stability, State: "active",
			CreatedAt: now, UpdatedAt: now, LastAccess: 0,
		}
		f.idempotent[vault][key] = nid
		results[i] = BatchItemResult{Index: i, ID: nid}
	}
	f.mu.Unlock()
	return results, nil
}

// Read returns the engram, or nil + a not-found error.
func (f *FakeClient) Read(_ context.Context, vault, id string) (*Engram, error) {
	if !f.health.Load() {
		return nil, errFakeUnhealthy
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.engrams[vault] == nil {
		return nil, errFakeNotFound
	}
	e, ok := f.engrams[vault][id]
	if !ok || e == nil {
		return nil, errFakeNotFound
	}
	// Return a copy so callers can't mutate the store.
	cp := *e
	return &cp, nil
}

// Activate streams all engrams in the vault as Activation rows (the fake ignores
// phrases/limit — it enumerates the store, which is exactly what MuninnDB's
// ListEngrams would do but does not yet expose; see research.md R5).
func (f *FakeClient) Activate(ctx context.Context, vault string, _ []string, limit int) (<-chan Activation, error) {
	if !f.health.Load() {
		return nil, errFakeUnhealthy
	}
	out := make(chan Activation, 1)
	go func() {
		defer close(out)
		f.mu.Lock()
		rows := make([]Activation, 0)
		for _, e := range f.engrams[vault] {
			if e == nil || e.State != "active" {
				continue
			}
			rows = append(rows, Activation{
				EngramID: e.ID, Concept: e.Concept, Score: 1.0,
				LastAccess: e.LastAccess, Tags: e.Tags,
			})
			if limit > 0 && len(rows) >= limit {
				break
			}
		}
		f.mu.Unlock()
		for _, r := range rows {
			select {
			case <-ctx.Done():
				return
			case out <- r:
			}
		}
	}()
	return out, nil
}

// EngramCount returns the number of stored engrams in a vault (test helper).
func (f *FakeClient) EngramCount(vault string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.engrams[vault])
}

// newFakeID returns a monotonic-ish fake engram id (tests don't need ULIDs).
var fakeIDCounter atomic.Uint64

func newFakeID() string {
	n := fakeIDCounter.Add(1)
	return "fake-" + strconv.FormatUint(n, 16)
}

var (
	errFakeUnhealthy = errFake("muninn unhealthy")
	errFakeWrite     = errFake("fake write failure")
	errFakeNotFound  = errFake("engram not found")
)

type errFake string

func (e errFake) Error() string { return string(e) }
