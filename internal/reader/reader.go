// Package reader extracts text content from files (PRD §8).
//
// Every format implements FileReader and self-registers; adding a format is one
// new file + a Register call (PRD §8.6). Built-in readers (PDF/text/markdown/
// docx/image) are implemented in later tasks.
package reader

import "context"

// FileReader extracts text content from a file (PRD §8.1).
type FileReader interface {
	// SupportedExtensions returns extensions this reader handles, e.g. [".pdf"].
	SupportedExtensions() []string
	// SupportedMimeTypes returns MIME types this reader handles.
	SupportedMimeTypes() []string
	// Read extracts text content from raw bytes, returning full text + metadata.
	Read(ctx context.Context, data []byte, path string) (content string, metadata map[string]any, err error)
	// Name returns a human-readable name ("PDF Reader").
	Name() string
}

var registry = make(map[string]FileReader)

// Register registers a reader for each of its supported extensions.
func Register(r FileReader) {
	for _, ext := range r.SupportedExtensions() {
		registry[ext] = r
	}
}

// Get returns the reader registered for an extension (e.g. ".pdf").
func Get(ext string) (FileReader, bool) {
	r, ok := registry[ext]
	return r, ok
}

// DefaultReaders registers the built-in readers into the registry. Safe to call
// once at startup (e.g. from the CLI and the pipeline).
func DefaultReaders() {
	registry = make(map[string]FileReader)
	Register(&TextReader{})
	Register(&MarkdownReader{})
	Register(&PDFReader{})
	Register(&DocxReader{})
	Register(&JPEGReader{})
	Register(&PNGReader{})
}
