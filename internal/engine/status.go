package engine

import (
	"encoding/json"
	"path/filepath"
	"sort"

	"github.com/madeinoz67/go-rag/internal/embed"
	"github.com/madeinoz67/go-rag/internal/model"
	"github.com/madeinoz67/go-rag/internal/storage"
)

// Status returns corpus counts, the active embedding model, and embedding
// reachability metadata. The model/dimensionality reported are the **stored**
// majority (from the embedding profile), not the configured values, so a
// model/config mismatch or a mid-migration drift is visible without querying
// (audit H03, US2).
func (e *Engine) Status() (*StatusInfo, error) {
	docs := countPrefix(e.db, storage.PrefixDocument)
	chunks := countPrefix(e.db, storage.PrefixChunk)
	embs := countPrefix(e.db, storage.PrefixEmbedding)
	prof := CorpusProfile(e.db)
	model := e.cfg.EmbeddingModel
	dims := 0
	if prof.Total > 0 {
		model = prof.MajorityModel
		dims = prof.MajorityDim
	}
	reranker := e.cfg.RerankModel
	if reranker == "" {
		reranker = "disabled"
	}
	complete := docs == 0 || embs >= chunks
	// H07: surface the resolved prefix convention in effect (config mode + the
	// role prefixes the prefixer will apply), the corpus's stored majority
	// convention, and a drift flag when more than one convention is present.
	pre := e.cfg.Prefixer()
	cfgMode := e.cfg.EmbeddingPrefix
	if cfgMode == "" {
		cfgMode = "auto"
	}
	return &StatusInfo{
		Documents:               docs,
		Chunks:                  chunks,
		Embeddings:              embs,
		Dimensions:              dims,
		EmbeddingModel:          model,
		Reranker:                reranker,
		OllamaURL:               e.cfg.OllamaURL,
		EmbeddingsComplete:      complete,
		EmbeddingDrift:          prof.Total > 0 && !prof.Consistent,
		ModelCounts:             prof.ModelCounts,
		DimCounts:               prof.DimCounts,
		EmbeddingConvention:     prof.MajorityConvention,
		EmbeddingConventionDrift: prof.Total > 0 && len(prof.ConventionCounts) > 1,
		ConventionCounts:        prof.ConventionCounts,
		ConfiguredPrefix:        cfgMode,
		QueryPrefix:             pre.ForRole(embed.RoleQuery),
		DocPrefix:               pre.ForRole(embed.RoleDocument),
	}, nil
}

// Files lists every ingested document, sorted by file path.
func (e *Engine) Files() ([]FileEntry, error) {
	var out []FileEntry
	_ = e.db.PrefixScanByte(storage.PrefixDocument, func(_, val []byte) bool {
		var d model.Document
		if json.Unmarshal(val, &d) == nil {
			out = append(out, FileEntry{
				FilePath:   d.FilePath,
				FileType:   d.FileType,
				Status:     d.Status,
				ChunkCount: d.ChunkCount,
			})
		}
		return true
	})
	sort.Slice(out, func(i, j int) bool { return out[i].FilePath < out[j].FilePath })
	return out, nil
}

// Dirs groups ingested documents by directory, returning file/chunk counts.
func (e *Engine) Dirs() ([]DirEntry, error) {
	type counts struct{ files, chunks int }
	m := map[string]*counts{}
	_ = e.db.PrefixScanByte(storage.PrefixDocument, func(_, val []byte) bool {
		var d model.Document
		if json.Unmarshal(val, &d) == nil {
			dir := filepath.Dir(d.FilePath)
			entry := m[dir]
			if entry == nil {
				entry = &counts{}
				m[dir] = entry
			}
			entry.files++
			entry.chunks += d.ChunkCount
		}
		return true
	})
	dirs := make([]string, 0, len(m))
	for d := range m {
		dirs = append(dirs, d)
	}
	sort.Strings(dirs)
	out := make([]DirEntry, 0, len(dirs))
	for _, d := range dirs {
		out = append(out, DirEntry{Dir: d, Files: m[d].files, Chunks: m[d].chunks})
	}
	return out, nil
}
