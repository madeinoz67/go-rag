package ui

import (
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/madeinoz67/go-rag/internal/auth"
	"github.com/madeinoz67/go-rag/internal/engine"
	"github.com/madeinoz67/go-rag/internal/model"
	"github.com/madeinoz67/go-rag/internal/storage/keys"
)

// documents_test.go (package ui) proves spec 047 US1: GET /api/documents lists
// documents with status/tag filters + pagination, 401s without a Bearer on an
// initialized vault, 400s on bad args, returns an empty array for an empty
// corpus, and never emits Set-Cookie. Documents are inserted directly under
// prefix 0x02 (the UI reads engine.ListDocuments, which reads documents, so
// direct insertion is the hermetic setup — no ingest pipeline needed).

var uiDocBase = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// putUIDoc writes a document under prefix 0x02 with the given id/status/tags,
// at a deterministic ingested_at (seq seconds after uiDocBase) for stable ordering.
func putUIDoc(t *testing.T, eng *engine.Engine, id, status string, tags []string, seq int) {
	t.Helper()
	ws := eng.DB().ResolveVaultPrefix("default")
	d := model.Document{
		ID:          id,
		FilePath:    id + ".txt",
		FileName:    id + ".txt",
		FileType:    "text",
		ContentHash: id,
		Status:      status,
		IngestedAt:  uiDocBase.Add(time.Duration(seq) * time.Second),
	}
	if tags != nil {
		d.Enrichment = &model.EnrichInfo{Tags: tags, Status: model.EnrichStatusDone, Model: "test"}
	}
	raw, err := json.Marshal(d)
	if err != nil {
		t.Fatalf("marshal %s: %v", id, err)
	}
	if err := eng.DB().Set(keys.DocumentKey(ws, id), raw); err != nil {
		t.Fatalf("putUIDoc %s: %v", id, err)
	}
}

func uiDocIDs(docs []documentDTO) []string {
	out := make([]string, len(docs))
	for i, d := range docs {
		out[i] = d.ID
	}
	return out
}

// authedDocServer initializes the vault (admin → loopback bypass off) and returns
// the UI test server URL plus a logged-in bearer for guarded routes.
func authedDocServer(t *testing.T, eng *engine.Engine) (srvURL, token string) {
	t.Helper()
	if _, err := auth.CreateAdmin(auth.NewStore(eng.DB()), auth.DefaultAdminUsername, "s3cret"); err != nil {
		t.Fatalf("CreateAdmin: %v", err)
	}
	srv := newUITest(t, eng)
	tok, _ := login(t, srv.URL, auth.DefaultAdminUsername, "s3cret", http.StatusOK)
	return srv.URL, tok
}

// TestDocuments_ListAndFilter covers US1 (a)/(c): list count matches the engine,
// ingested_at ASC ordering, status + tag filters narrow. [FR-001, FR-006, FR-008, FR-013]
func TestDocuments_ListAndFilter(t *testing.T) {
	eng := newTestEngine(t)
	putUIDoc(t, eng, "d1", "embedded", nil, 1)
	putUIDoc(t, eng, "d2", "pending", nil, 2)
	putUIDoc(t, eng, "d3", "embedded", []string{"security"}, 3)
	srvURL, tok := authedDocServer(t, eng)

	// (a) list total == engine count (3), ordered ingested_at ASC: d1, d2, d3.
	resp := bearerGet(t, srvURL+"/api/documents?page_size=10", tok)
	defer resp.Body.Close()
	var list documentsListResponse
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(list.Documents) != 3 {
		t.Fatalf("len=%d want 3", len(list.Documents))
	}
	got := uiDocIDs(list.Documents)
	if got[0] != "d1" || got[1] != "d2" || got[2] != "d3" {
		t.Errorf("order: got %v want [d1 d2 d3]", got)
	}
	// Cross-source parity: count matches a direct engine.ListDocuments call.
	res, err := eng.ListDocuments(engine.ListDocumentsRequest{})
	if err != nil {
		t.Fatalf("eng.ListDocuments: %v", err)
	}
	if len(list.Documents) != len(res.Documents) {
		t.Errorf("ui != engine counts: %d vs %d", len(list.Documents), len(res.Documents))
	}

	// (c1) status=pending → only d2.
	resp = bearerGet(t, srvURL+"/api/documents?status=pending", tok)
	defer resp.Body.Close()
	list = documentsListResponse{}
	json.NewDecoder(resp.Body).Decode(&list)
	if got := uiDocIDs(list.Documents); len(got) != 1 || got[0] != "d2" {
		t.Errorf("status=pending: got %v want [d2]", got)
	}

	// (c2) tag=security → only d3.
	resp = bearerGet(t, srvURL+"/api/documents?tag=security", tok)
	defer resp.Body.Close()
	list = documentsListResponse{}
	json.NewDecoder(resp.Body).Decode(&list)
	if got := uiDocIDs(list.Documents); len(got) != 1 || got[0] != "d3" {
		t.Errorf("tag=security: got %v want [d3]", got)
	}
}

// TestDocuments_EmptyCorpus: no documents → {"documents":[]} (empty array, not
// null, not an error).
func TestDocuments_EmptyCorpus(t *testing.T) {
	eng := newTestEngine(t)
	srvURL, tok := authedDocServer(t, eng)
	resp := bearerGet(t, srvURL+"/api/documents", tok)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d want 200", resp.StatusCode)
	}
	var list documentsListResponse
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if list.Documents == nil || len(list.Documents) != 0 {
		t.Errorf("empty corpus: got %v want non-nil empty array", list.Documents)
	}
}

// TestDocuments_Pagination: page through with page_size=2 → every doc once,
// ascending, empty next_page_token at the end.
func TestDocuments_Pagination(t *testing.T) {
	eng := newTestEngine(t)
	for i := 1; i <= 5; i++ {
		putUIDoc(t, eng, "p"+strconv.Itoa(i), "embedded", nil, i)
	}
	srvURL, tok := authedDocServer(t, eng)

	var collected []string
	pageToken := ""
	for page := 0; page < 5; page++ {
		url := srvURL + "/api/documents?page_size=2"
		if pageToken != "" {
			url += "&page_token=" + pageToken
		}
		resp := bearerGet(t, url, tok)
		var list documentsListResponse
		json.NewDecoder(resp.Body).Decode(&list)
		resp.Body.Close()
		collected = append(collected, uiDocIDs(list.Documents)...)
		if list.NextPageToken == "" {
			break
		}
		pageToken = list.NextPageToken
	}
	want := []string{"p1", "p2", "p3", "p4", "p5"}
	if len(collected) != len(want) {
		t.Fatalf("paged: got %v want %v", collected, want)
	}
	for i := range want {
		if collected[i] != want[i] {
			t.Errorf("paged[%d]=%q want %q", i, collected[i], want[i])
		}
	}
}

// TestDocuments_GuardAndErrors: 401 without a Bearer; 400 on bad page_size
// (non-integer + out-of-range), bad status, bad after, bad page_token.
func TestDocuments_GuardAndErrors(t *testing.T) {
	eng := newTestEngine(t)
	putUIDoc(t, eng, "d1", "embedded", nil, 1)
	srvURL, tok := authedDocServer(t, eng) // initializes the vault (bypass off)

	// (d) No bearer → 401 (initialized vault: bypass must NOT fire).
	noTok := bearerGet(t, srvURL+"/api/documents", "")
	noTok.Body.Close()
	if noTok.StatusCode != http.StatusUnauthorized {
		t.Errorf("no bearer: got %d want 401", noTok.StatusCode)
	}

	// (e) 400 matrix.
	for _, q := range []string{
		"page_size=abc",   // non-integer (handler-rejected)
		"page_size=999",   // out of range (engine ErrInvalid)
		"status=bogus",    // engine ErrInvalid
		"after=yesterday", // not RFC3339 (engine ErrInvalid)
		"page_token=!!!",  // malformed (engine ErrInvalid)
	} {
		resp := bearerGet(t, srvURL+"/api/documents?"+q, tok)
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%s: got %d want 400", q, resp.StatusCode)
		}
	}
}

// TestDocuments_NoSetCookie — the document list never emits Set-Cookie
// (Bearer-only, CSRF-free — the same invariant the auth surface holds).
func TestDocuments_NoSetCookie(t *testing.T) {
	eng := newTestEngine(t)
	putUIDoc(t, eng, "d1", "embedded", nil, 1)
	srvURL, tok := authedDocServer(t, eng)
	resp := bearerGet(t, srvURL+"/api/documents", tok)
	defer resp.Body.Close()
	if c := resp.Header.Get("Set-Cookie"); c != "" {
		t.Fatalf("Set-Cookie must never be emitted on /api/documents: got %q", c)
	}
}
