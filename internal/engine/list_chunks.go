package engine

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/madeinoz67/go-rag/internal/model"
	"github.com/madeinoz67/go-rag/internal/storage"
)

// list_chunks.go implements spec 047 (Slice 1): ListChunks — a paginated listing
// of one document's chunks over the chunk prefix (0x03), with a chunk_index
// cursor and page_token pagination. The Documents-view detail surface; the
// direct analogue of ListDocuments (spec 039). Research: research.md R1/R7.
//
// Pagination mirrors ListDocuments: an opaque page_token encodes the last-
// returned (chunk_index, chunk_id) so the next page resumes strictly after it
// under the total ordering (chunk_index ASC, chunk_id ASC). The token carries
// ONLY the resume point — the client re-sends document_id/page_size/page_token
// on every page.
//
// Pure read: one PrefixScan over prefix 0x03 + in-memory filter/sort/paginate.
// No new key, no migration, no new deps (Constitution I-V). A corpus-wide chunk
// scan that filters one document in memory mirrors ListDocuments' scan-all-then-
// filter approach and is fine for go-rag's single-operator scale; a future
// hardening pass may switch to the PrefixDocChunks (0x0B) ordered index.

// ListChunksRequest is the engine-level chunk-list request for one document
// (spec 047 / research.md R1).
type ListChunksRequest struct {
	PageSize  int
	PageToken string
}

// ListChunksResult is one page of a document's chunks + the opaque cursor for
// the next page (empty NextPageToken ⇒ last page). Chunks are ordered
// (chunk_index ASC, chunk_id ASC).
type ListChunksResult struct {
	Chunks        []model.Chunk
	NextPageToken string
}

// encodeChunkPageToken renders an opaque, URL-safe page token for (idx, id):
// the base64-url-no-pad encoding of "<idx>\x1f<id>".
func encodeChunkPageToken(idx int, id string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.Itoa(idx) + pageTokenSep + id))
}

// decodeChunkPageToken reverses encodeChunkPageToken; a malformed token → ErrInvalid.
func decodeChunkPageToken(s string) (int, string, error) {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return 0, "", fmt.Errorf("page_token decode: %w", ErrInvalid)
	}
	parts := strings.SplitN(string(raw), pageTokenSep, 2)
	if len(parts) != 2 {
		return 0, "", fmt.Errorf("page_token format: %w", ErrInvalid)
	}
	idx, err := strconv.Atoi(parts[0])
	if err != nil || parts[1] == "" {
		return 0, "", fmt.Errorf("page_token value: %w", ErrInvalid)
	}
	return idx, parts[1], nil
}

// ListChunks returns a page of one document's chunks matching the chunk_index
// cursor, ordered by (chunk_index ASC, chunk_id ASC), plus an opaque
// next_page_token.
//
// Errors (all ErrInvalid): empty document_id; page_size <1 or >200 (0 → default
// 50); page_token non-empty and malformed. An empty result is NOT an error
// (unknown document_id, or a document with no chunks → empty Chunks + empty
// token).
func (e *Engine) ListChunks(documentID string, req ListChunksRequest) (*ListChunksResult, error) {
	// Validate document_id.
	if strings.TrimSpace(documentID) == "" {
		return nil, fmt.Errorf("document_id must be non-empty: %w", ErrInvalid)
	}
	// Validate page_size (0 → default).
	pageSize := req.PageSize
	if pageSize == 0 {
		pageSize = defaultListPageSize
	}
	if pageSize < 1 || pageSize > maxListPageSize {
		return nil, fmt.Errorf("page_size must be 1..%d, got %d: %w", maxListPageSize, req.PageSize, ErrInvalid)
	}
	// Validate + decode page_token (resume point).
	var resumeIdx int
	var resumeID string
	if strings.TrimSpace(req.PageToken) != "" {
		idx, id, err := decodeChunkPageToken(req.PageToken)
		if err != nil {
			return nil, err
		}
		resumeIdx, resumeID = idx, id
	}

	// 1. Scan this document's chunks (one PrefixScan over prefix 0x03).
	var chunks []model.Chunk
	_ = e.db.PrefixScanByte(storage.PrefixChunk, func(_, val []byte) bool {
		var c model.Chunk
		if json.Unmarshal(val, &c) == nil && c.DocumentID == documentID {
			chunks = append(chunks, c)
		}
		return true
	})

	// 2. Order by (chunk_index ASC, chunk_id ASC) — a total order.
	sort.SliceStable(chunks, func(i, j int) bool {
		if chunks[i].ChunkIndex == chunks[j].ChunkIndex {
			return chunks[i].ID < chunks[j].ID
		}
		return chunks[i].ChunkIndex < chunks[j].ChunkIndex
	})

	// 3. Skip-to-resume-point (page_token): first chunk strictly AFTER (resumeIdx, resumeID).
	start := 0
	if req.PageToken != "" {
		for start < len(chunks) {
			c := chunks[start]
			if c.ChunkIndex > resumeIdx || (c.ChunkIndex == resumeIdx && c.ID > resumeID) {
				break
			}
			start++
		}
	}

	// 4. Take the page; emit next_page_token iff more remain.
	end := start + pageSize
	if end > len(chunks) {
		end = len(chunks)
	}
	page := chunks[start:end]
	out := make([]model.Chunk, len(page)) // copy out — `page` aliases the scratch buffer
	copy(out, page)

	res := &ListChunksResult{Chunks: out}
	if end < len(chunks) {
		last := page[len(page)-1]
		res.NextPageToken = encodeChunkPageToken(last.ChunkIndex, last.ID)
	}
	return res, nil
}
