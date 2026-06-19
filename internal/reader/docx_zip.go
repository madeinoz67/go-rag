package reader

import (
	"archive/zip"
	"bytes"
	"io"
)

// zipReader wraps archive/zip so the reader stays focused on extraction logic.
func zipReader(data []byte) (*zip.Reader, error) {
	return zip.NewReader(bytes.NewReader(data), int64(len(data)))
}

// readZipFile reads a single named entry from a zip (docx) archive.
func readZipFile(zr *zip.Reader, name string) ([]byte, error) {
	for _, f := range zr.File {
		if f.Name == name {
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			defer rc.Close()
			return io.ReadAll(rc)
		}
	}
	return nil, io.EOF
}
