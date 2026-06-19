// Package chunk splits extracted text into retrieval-sized chunks (PRD §4.4).
//
// Defaults: 512 tokens per chunk with 50-token overlap, using a
// paragraph -> sentence -> word cascade with a 50-token minimum. TODO(later):
// implement the splitter; token counting strategy is open question Q2.
package chunk

// Splitter splits text into overlapping chunks. Stub.
type Splitter struct {
	Size    int // target chunk size in tokens
	Overlap int // overlap between adjacent chunks in tokens
}

// NewSplitter returns a splitter with the given size and overlap.
func NewSplitter(size, overlap int) *Splitter {
	return &Splitter{Size: size, Overlap: overlap}
}
