# Chunking Strategy

go-rag splits each ingested document into chunks before embedding. A chunk is a
contiguous span of text that gets its own vector and its own BM25 postings entry.
The chunker takes a target size and an overlap so that meaning is not severed at
an arbitrary boundary: consecutive chunks share a window of overlapping words,
which keeps a sentence that straddles a cut retrievable from both sides.

Every chunk records its position: an index within its document, start and end
character offsets, an optional page number for PDFs, and links to its previous
and next chunk. This linked list lets a caller expand a hit into its neighbouring
context without storing duplicate parent text. Chunks are the unit of retrieval:
a query returns ranked chunks, each carrying the source file and page it came
from, so a downstream reader can cite exactly where an answer originated.
