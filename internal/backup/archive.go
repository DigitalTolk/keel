package backup

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
)

// WriteTarGz writes a gzip-compressed tar containing srcPath as a single entry
// named entryName. This replaces the scripts' tar + pigz/zip shell-outs with a
// pure-Go implementation (no external archiver required).
func WriteTarGz(w io.Writer, entryName, srcPath string) error {
	f, err := os.Open(srcPath)
	if err != nil {
		return fmt.Errorf("open %s: %w", srcPath, err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return fmt.Errorf("stat %s: %w", srcPath, err)
	}

	gz := gzip.NewWriter(w)
	tw := tar.NewWriter(gz)

	hdr := &tar.Header{
		Name:    entryName,
		Mode:    0o600,
		Size:    info.Size(),
		ModTime: info.ModTime(),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return fmt.Errorf("write tar header: %w", err)
	}
	if _, err := io.Copy(tw, f); err != nil {
		return fmt.Errorf("write tar body: %w", err)
	}

	if err := tw.Close(); err != nil {
		return fmt.Errorf("close tar: %w", err)
	}
	if err := gz.Close(); err != nil {
		return fmt.Errorf("close gzip: %w", err)
	}
	return nil
}
