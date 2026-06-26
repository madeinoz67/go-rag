package reader

// ImageRef is a transient reference to an embedded image extracted by a reader
// (spec 031 US4), carrying the raw bytes + page position so the pipeline can
// caption the image post-ACK. Bytes are NEVER persisted to Pebble (0x03/0x04)
// and NEVER enter the document identity hash — processFile pops metadata["images"]
// before GenerateID (the heading_spans precedent, Constitution II). The slice
// lives only in the in-memory job queue and is GC'd after captioning.
type ImageRef struct {
	PageNr   int
	Bytes    []byte
	Width    int
	Height   int
	FileType string
}
