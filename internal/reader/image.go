package reader

import (
	"bytes"
	"context"
	"fmt"
	"image/jpeg"
	"image/png"
)

// JPEGReader and PNGReader extract dimensions/EXIF only — no text (OCR is deferred
// to a later version per PRD non-goal N5 / research Q1). Images are indexed by
// metadata (filename, dimensions) but contribute no searchable text.
type JPEGReader struct{}

func (r *JPEGReader) Name() string                  { return "JPEG" }
func (r *JPEGReader) SupportedExtensions() []string { return []string{".jpg", ".jpeg"} }
func (r *JPEGReader) SupportedMimeTypes() []string  { return []string{"image/jpeg"} }

func (r *JPEGReader) Read(_ context.Context, data []byte, _ string) (string, map[string]any, error) {
	cfg, err := jpeg.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return "", nil, fmt.Errorf("jpeg decode: %w", err)
	}
	return "", imageMeta("jpeg", cfg.Width, cfg.Height), nil
}

type PNGReader struct{}

func (r *PNGReader) Name() string                  { return "PNG" }
func (r *PNGReader) SupportedExtensions() []string { return []string{".png"} }
func (r *PNGReader) SupportedMimeTypes() []string  { return []string{"image/png"} }

func (r *PNGReader) Read(_ context.Context, data []byte, _ string) (string, map[string]any, error) {
	cfg, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return "", nil, fmt.Errorf("png decode: %w", err)
	}
	return "", imageMeta("png", cfg.Width, cfg.Height), nil
}

func imageMeta(format string, w, h int) map[string]any {
	return map[string]any{"format": format, "width": w, "height": h, "ocr": "deferred"}
}
