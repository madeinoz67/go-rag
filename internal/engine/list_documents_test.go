package engine

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/madeinoz67/go-rag/internal/model"
	"github.com/madeinoz67/go-rag/internal/storage"
)

// list_documents_test.go (package engine) proves spec 039: ListDocuments filters
// by an ingested_at cursor + status, orders by (ingested_at, id), and paginates.
// US1 (cursor+filter) + US2 (pagination). Documents are inserted directly with
// crafted (IngestedAt, Status) for determinism (ListDocuments reads documents, so
// direct insertion is the hermetic setup — no addDoc timing/embedding needed).

// putDoc writes a document record under prefix 0x02 with the given id/time/status.
func putDoc(t *testing.T, e *Engine, id string, ingestedAt time.Time, status string) {
	t.Helper()
	d := model.Document{
		ID:          id,
		FilePath:    id + ".txt",
		FileName:    id + ".txt",
		FileType:    "text",
		ContentHash: id,
		Status:      status,
		IngestedAt:  ingestedAt,
	}
	raw, err := json.Marshal(d)
	if err != nil {
		t.Fatalf("marshal %s: %v", id, err)
	}
	if err := e.db.SetWithPrefix(storage.PrefixDocument, []byte(id), raw); err != nil {
		t.Fatalf("putDoc %s: %v", id, err)
	}
}

// docBase is a fixed base time so test ordering is deterministic (no wall-clock
// same-millisecond races).
var docBase = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// TestListDocuments_CursorAndFilter covers US1: after-cursor + status filter (AND),
// ascending order, full metadata, empty result. [US1 #1-4; FR-002..004, FR-008/009]
func TestListDocuments_CursorAndFilter(t *testing.T) {
	e := newCacheEngine(t)
	// 5 docs at distinct times; mixed status.
	putDoc(t, e, "d1", docBase.Add(1*time.Second), "embedded")
	putDoc(t, e, "d2", docBase.Add(2*time.Second), "pending")
	putDoc(t, e, "d3", docBase.Add(3*time.Second), "embedded")
	putDoc(t, e, "d4", docBase.Add(4*time.Second), "error")
	putDoc(t, e, "d5", docBase.Add(5*time.Second), "embedded")
	mid := docBase.Add(3 * time.Second).Format(time.RFC3339)

	// (a) after=mid → docs with ingested_at > mid, ascending: d4, d5.
	res, err := e.ListDocuments(ListDocumentsRequest{After: mid})
	if err != nil {
		t.Fatalf("after: %v", err)
	}
	ids := docIDs(res.Documents)
	if len(ids) != 2 || ids[0] != "d4" || ids[1] != "d5" {
		t.Errorf("after=mid: ids=%v want [d4 d5]", ids)
	}

	// (b) status=embedded → only embedded docs, ascending: d1, d3, d5.
	res, err = e.ListDocuments(ListDocumentsRequest{Status: "embedded"})
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	ids = docIDs(res.Documents)
	if len(ids) != 3 || ids[0] != "d1" || ids[1] != "d3" || ids[2] != "d5" {
		t.Errorf("status=embedded: ids=%v want [d1 d3 d5]", ids)
	}

	// (c) after + status AND → embedded docs after mid: d5.
	res, err = e.ListDocuments(ListDocumentsRequest{After: mid, Status: "embedded"})
	if err != nil {
		t.Fatalf("after+status: %v", err)
	}
	ids = docIDs(res.Documents)
	if len(ids) != 1 || ids[0] != "d5" {
		t.Errorf("after+status: ids=%v want [d5]", ids)
	}

	// (d) every returned doc carries full metadata + a non-empty ingested_at.
	for _, d := range res.Documents {
		if d.ID == "" || d.FilePath == "" || d.IngestedAt.IsZero() {
			t.Errorf("doc %q: incomplete metadata (filepath=%q ingested=%v)", d.ID, d.FilePath, d.IngestedAt)
		}
	}

	// (e) empty result (after far future) → empty slice, no error, empty token.
	res, err = e.ListDocuments(ListDocumentsRequest{After: docBase.Add(1 * time.Hour).Format(time.RFC3339)})
	if err != nil {
		t.Fatalf("future after: %v", err)
	}
	if len(res.Documents) != 0 || res.NextPageToken != "" {
		t.Errorf("future after: docs=%d token=%q want 0/empty", len(res.Documents), res.NextPageToken)
	}
}

// TestListDocuments_Pagination covers US2: multi-page iteration composes with
// after+status, every doc once, empty next_page_token at end, tie-break by id,
// page-token codec round-trip, validation. [US2 #1-4; FR-005..007]
func TestListDocuments_Pagination(t *testing.T) {
	e := newCacheEngine(t)
	// 7 embedded docs at distinct times.
	for i := 1; i <= 7; i++ {
		putDoc(t, e, "p"+itoa(i), docBase.Add(time.Duration(i)*time.Second), "embedded")
	}

	// (a) page through with page_size=3 → 3+3+1, every doc once, ascending, empty token at end.
	var got []string
	tok := ""
	for page := 0; page < 5; page++ { // guard against infinite loop
		res, err := e.ListDocuments(ListDocumentsRequest{PageSize: 3, PageToken: tok, Status: "embedded"})
		if err != nil {
			t.Fatalf("page %d: %v", page, err)
		}
		got = append(got, docIDs(res.Documents)...)
		if res.NextPageToken == "" {
			break
		}
		tok = res.NextPageToken
	}
	want := []string{"p1", "p2", "p3", "p4", "p5", "p6", "p7"}
	if len(got) != len(want) {
		t.Fatalf("paged: got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("paged[%d]=%q want %q", i, got[i], want[i])
		}
	}

	// (b) pagination composes with after (only docs after p3's time: p4..p7).
	got = nil
	tok = ""
	for page := 0; page < 5; page++ {
		res, err := e.ListDocuments(ListDocumentsRequest{PageSize: 2, PageToken: tok, After: docBase.Add(3 * time.Second).Format(time.RFC3339), Status: "embedded"})
		if err != nil {
			t.Fatalf("after+page %d: %v", page, err)
		}
		got = append(got, docIDs(res.Documents)...)
		if res.NextPageToken == "" {
			break
		}
		tok = res.NextPageToken
	}
	if len(got) != 4 || got[0] != "p4" || got[3] != "p7" {
		t.Errorf("after+paged: got %v, want [p4 p5 p6 p7]", got)
	}

	// (c) page_size boundaries.
	if _, err := e.ListDocuments(ListDocumentsRequest{PageSize: MaxListPageSize()}); err != nil {
		t.Errorf("page_size=%d: %v", MaxListPageSize(), err)
	}
	if _, err := e.ListDocuments(ListDocumentsRequest{PageSize: MaxListPageSize() + 1}); !errors.Is(err, ErrInvalid) {
		t.Errorf("page_size=%d: err=%v want ErrInvalid", MaxListPageSize()+1, err)
	}
	if _, err := e.ListDocuments(ListDocumentsRequest{PageSize: 0}); err != nil {
		t.Errorf("page_size=0 (default): err=%v", err) // 0 → default 50
	}

	// (d) tie-break: two docs with identical ingested_at ordered by id.
	e2 := newCacheEngine(t)
	putDoc(t, e2, "zzz", docBase, "embedded")
	putDoc(t, e2, "aaa", docBase, "embedded")
	res, err := e2.ListDocuments(ListDocumentsRequest{})
	if err != nil {
		t.Fatalf("tie: %v", err)
	}
	ids := docIDs(res.Documents)
	if len(ids) != 2 || ids[0] != "aaa" || ids[1] != "zzz" {
		t.Errorf("tie-break: ids=%v want [aaa zzz]", ids)
	}

	// (e) malformed page_token → ErrInvalid.
	if _, err := e.ListDocuments(ListDocumentsRequest{PageToken: "not-valid-base64!!!"}); !errors.Is(err, ErrInvalid) {
		t.Errorf("malformed token: err=%v want ErrInvalid", err)
	}

	// (f) codec round-trip.
	tok = encodePageToken(docBase.Add(2*time.Second), "p2")
	rt, id, err := decodePageToken(tok)
	if err != nil || id != "p2" || !rt.Equal(docBase.Add(2*time.Second)) {
		t.Errorf("codec round-trip: tok=%q → (%v,%q,%v)", tok, rt, id, err)
	}
}

// TestListDocuments_InvalidInput covers the validation rules. [FR-003..006]
func TestListDocuments_InvalidInput(t *testing.T) {
	e := newCacheEngine(t)
	cases := []struct {
		name string
		req  ListDocumentsRequest
	}{
		{"bad status", ListDocumentsRequest{Status: "bogus"}},
		{"bad after (not RFC3339)", ListDocumentsRequest{After: "yesterday"}},
		{"page_size negative", ListDocumentsRequest{PageSize: -1}},
		{"page_size too large", ListDocumentsRequest{PageSize: 999}},
		{"malformed token", ListDocumentsRequest{PageToken: "!!!"}},
	}
	for _, c := range cases {
		if _, err := e.ListDocuments(c.req); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: err=%v want ErrInvalid", c.name, err)
		}
	}
}

// docIDs collects the ids of a document slice (test helper).
func docIDs(docs []model.Document) []string {
	out := make([]string, len(docs))
	for i, d := range docs {
		out[i] = d.ID
	}
	return out
}

// itoa is a strconv-free int→string for test indices.
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	neg := i < 0
	if neg {
		i = -i
	}
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	if neg {
		b = append([]byte{'-'}, b...)
	}
	return string(b)
}
