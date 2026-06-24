package engine

import "github.com/madeinoz67/go-rag/internal/model"

// listsSibling reports whether the near-dup info nd lists chunkID among its
// siblings. Returns false when nd is nil (no near-dups / pre-feature chunk).
// Used by the opt-in collapse pass (audit H20 / spec 026, research R7/R8).
func listsSibling(nd *model.NearDupInfo, chunkID string) bool {
	if nd == nil {
		return false
	}
	for _, s := range nd.Siblings {
		if s == chunkID {
			return true
		}
	}
	return false
}
