package engine

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/madeinoz67/go-rag/internal/model"
	"github.com/madeinoz67/go-rag/internal/storage"
	"github.com/madeinoz67/go-rag/internal/storage/keys"
)

// list_documents.go implements spec 039 (bridge backlog BL-007): ListDocuments —
// a reliable incremental document listing over the document prefix (0x02), with an
// `after` (ingested_at) cursor, a `status` filter, and page_token pagination. The
// bridge's change-poll + crash-recovery primitive.
//
// Pagination is NEW to go-rag: an opaque page_token encodes the last-returned
// (ingested_at, document_id) so the next page resumes strictly after it under the
// total ordering (ingested_at ASC, id ASC). The token carries ONLY the resume
// point — the client re-sends after/status/page_size on every page (research.md R1).
//
// Pure read: one PrefixScan over prefix 0x02 + in-memory filter/sort/paginate. No
// new key, no migration, no new deps (Constitution I-V; research.md R2/R3).

const (
	defaultListPageSize = 50
	maxListPageSize     = 200
)

// pageTokenSep separates ingested_at and document_id inside the unencoded page
// token; \x1f (unit separator) appears in neither RFC3339 timestamps nor hex ids.
const pageTokenSep = "\x1f"

// ListDocumentsRequest is the engine-level filter + page request (spec 039).
type ListDocumentsRequest struct {
	PageSize  int
	PageToken string
	After     string   // RFC3339; "" → unbounded below (all documents)
	Status    string   // "embedded"|"pending"|"error"|"" (all)
	Tags      []string // spec 047 R3: match-any tag filter; nil/empty = all
}

// ListDocumentsResult is one page of documents + the opaque cursor for the next
// page (empty NextPageToken ⇒ last page). Documents are in (ingested_at ASC, id ASC).
type ListDocumentsResult struct {
	Documents     []model.Document
	NextPageToken string
}

// DefaultListPageSize / MaxListPageSize are exposed for the transport adapters +
// tests (mirrors the spec-037 window helpers).
func DefaultListPageSize() int { return defaultListPageSize }
func MaxListPageSize() int     { return maxListPageSize }

// docHasAnyTag reports whether d carries at least one of the filter tags
// (match-any semantics, spec 047 R3). Un-enriched documents (nil Enrichment)
// carry no tags and so fail every tag filter.
func docHasAnyTag(d model.Document, tags []string) bool {
	if d.Enrichment == nil || len(d.Enrichment.Tags) == 0 {
		return false
	}
	wanted := make(map[string]struct{}, len(tags))
	for _, t := range tags {
		wanted[t] = struct{}{}
	}
	for _, t := range d.Enrichment.Tags {
		if _, ok := wanted[t]; ok {
			return true
		}
	}
	return false
}

// encodePageToken renders an opaque, URL-safe page token for (t, id): the
// base64-url-no-pad encoding of "<RFC3339Nano>\x1f<id>".
func encodePageToken(t time.Time, id string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(t.UTC().Format(time.RFC3339Nano) + pageTokenSep + id))
}

// decodePageToken reverses encodePageToken; a malformed token → ErrInvalid.
func decodePageToken(s string) (time.Time, string, error) {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return time.Time{}, "", fmt.Errorf("page_token decode: %w", ErrInvalid)
	}
	parts := strings.SplitN(string(raw), pageTokenSep, 2)
	if len(parts) != 2 {
		return time.Time{}, "", fmt.Errorf("page_token format: %w", ErrInvalid)
	}
	t, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil || parts[1] == "" {
		return time.Time{}, "", fmt.Errorf("page_token value: %w", ErrInvalid)
	}
	return t, parts[1], nil
}

// ListDocuments returns a page of documents matching the after-cursor + status
// filter, ordered by (ingested_at ASC, id ASC), plus an opaque next_page_token.
//
// Errors (all ErrInvalid): page_size <1 or >200 (0 → default 50); after non-empty
// and not RFC3339; status not in {embedded,pending,error,""}; page_token non-empty
// and malformed. An empty result is NOT an error (empty Documents + empty token).
func (e *Engine) ListDocuments(vault string, req ListDocumentsRequest) (*ListDocumentsResult, error) {
	// Validate page_size (0 → default).
	pageSize := req.PageSize
	if pageSize == 0 {
		pageSize = defaultListPageSize
	}
	if pageSize < 1 || pageSize > maxListPageSize {
		return nil, fmt.Errorf("page_size must be 1..%d, got %d: %w", maxListPageSize, req.PageSize, ErrInvalid)
	}
	// Validate status.
	switch req.Status {
	case "", "embedded", "pending", "error":
	default:
		return nil, fmt.Errorf("status must be embedded|pending|error|\"\", got %q: %w", req.Status, ErrInvalid)
	}
	// Validate after (RFC3339).
	var afterT time.Time
	if strings.TrimSpace(req.After) != "" {
		t, err := time.Parse(time.RFC3339, req.After)
		if err != nil {
			return nil, fmt.Errorf("after must be RFC3339, got %q: %w", req.After, ErrInvalid)
		}
		afterT = t
	}
	// Validate + decode page_token (resume point).
	var resumeT time.Time
	var resumeID string
	if strings.TrimSpace(req.PageToken) != "" {
		t, id, err := decodePageToken(req.PageToken)
		if err != nil {
			return nil, err
		}
		resumeT, resumeID = t, id
	}

	// 1. Scan this vault's documents (one range scan over 0x02|ws).
	ws := e.db.ResolveVaultPrefix(vault)
	lower, upper, _ := keys.VaultKindRange(storage.PrefixDocument, ws)
	var docs []model.Document
	_ = e.db.RangeScan(lower, upper, func(_, val []byte) bool {
		var d model.Document
		if json.Unmarshal(val, &d) == nil {
			docs = append(docs, d)
		}
		return true
	})

	// 2. Filter (after + status, AND) — in place.
	filtered := docs[:0]
	for _, d := range docs {
		if req.Status != "" && d.Status != req.Status {
			continue
		}
		if !afterT.IsZero() && !d.IngestedAt.After(afterT) {
			continue
		}
		if len(req.Tags) > 0 && !docHasAnyTag(d, req.Tags) {
			continue
		}
		filtered = append(filtered, d)
	}

	// 3. Order by (ingested_at ASC, id ASC) — a total order.
	sort.SliceStable(filtered, func(i, j int) bool {
		if filtered[i].IngestedAt.Equal(filtered[j].IngestedAt) {
			return filtered[i].ID < filtered[j].ID
		}
		return filtered[i].IngestedAt.Before(filtered[j].IngestedAt)
	})

	// 4. Skip-to-resume-point (page_token): first doc strictly AFTER (resumeT, resumeID).
	start := 0
	if req.PageToken != "" {
		for start < len(filtered) {
			d := filtered[start]
			if d.IngestedAt.After(resumeT) || (d.IngestedAt.Equal(resumeT) && d.ID > resumeID) {
				break
			}
			start++
		}
	}

	// 5. Take the page; emit next_page_token iff more remain.
	end := start + pageSize
	if end > len(filtered) {
		end = len(filtered)
	}
	page := filtered[start:end]
	out := make([]model.Document, len(page)) // copy out — `page` aliases the scratch buffer
	copy(out, page)

	res := &ListDocumentsResult{Documents: out}
	if end < len(filtered) {
		last := page[len(page)-1]
		res.NextPageToken = encodePageToken(last.IngestedAt, last.ID)
	}
	return res, nil
}
