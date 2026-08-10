package engine

import "github.com/madeinoz67/go-rag/internal/model"

// bridgeChunkSource adapts the Engine's ListDocuments/ListChunks to the bridge's
// muninn.ChunkSource interface (US2 backfill). Pages through all settled documents
// + each document's chunks. Lives in the engine package because the bridge can't
// import the engine (it would cycle) — the bridge defines the interface, the
// engine supplies the adapter (Extension by Interface, Principle V).
//
// "embedded" status filter: backfill promotes only fully-ingested documents (the
// happy-path corpus). A pending/error doc is skipped until it settles; the next
// change-event promotion or a later backfill picks it up.
type bridgeChunkSource struct {
	eng   *Engine
	vault string
}

func (s *bridgeChunkSource) ListDocuments() ([]model.Document, error) {
	var docs []model.Document
	token := ""
	for {
		res, err := s.eng.ListDocuments(s.vault, ListDocumentsRequest{PageSize: 200, PageToken: token, Status: "embedded"})
		if err != nil {
			return nil, err
		}
		docs = append(docs, res.Documents...)
		if res.NextPageToken == "" {
			break
		}
		token = res.NextPageToken
	}
	return docs, nil
}

func (s *bridgeChunkSource) Chunks(docID string) ([]model.Chunk, error) {
	var chunks []model.Chunk
	token := ""
	for {
		res, err := s.eng.ListChunks(s.vault, docID, ListChunksRequest{PageSize: 200, PageToken: token})
		if err != nil {
			return nil, err
		}
		chunks = append(chunks, res.Chunks...)
		if res.NextPageToken == "" {
			break
		}
		token = res.NextPageToken
	}
	return chunks, nil
}
