package backup

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// WriteTarGzDir writes a gzip-compressed tar of every file under root, using
// slash-separated paths relative to root. Files matching any exclude pattern
// are skipped. A pattern ending in "/*" excludes a whole subtree (e.g.
// "cache/*" drops cache/ and everything beneath it); other patterns are matched
// with path.Match. This replaces the zip + --exclude shell-out in
// backup-jenkins.sh.
func WriteTarGzDir(w io.Writer, root string, excludes []string) error {
	gz := gzip.NewWriter(w)
	tw := tar.NewWriter(gz)

	walkErr := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		key := filepath.ToSlash(rel)

		if isExcluded(key, excludes) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return err
		}
		// Skip non-regular files (sockets, devices) which can't be archived.
		if !info.Mode().IsRegular() {
			return nil
		}

		hdr := &tar.Header{Name: key, Mode: 0o600, Size: info.Size(), ModTime: info.ModTime()}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		f, err := os.Open(p)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(tw, f)
		f.Close()
		return copyErr
	})
	if walkErr != nil {
		return fmt.Errorf("archive dir %s: %w", root, walkErr)
	}

	if err := tw.Close(); err != nil {
		return fmt.Errorf("close tar: %w", err)
	}
	if err := gz.Close(); err != nil {
		return fmt.Errorf("close gzip: %w", err)
	}
	return nil
}

func isExcluded(key string, excludes []string) bool {
	for _, pat := range excludes {
		if strings.HasSuffix(pat, "/*") {
			dir := strings.TrimSuffix(pat, "/*")
			if key == dir || strings.HasPrefix(key, dir+"/") {
				return true
			}
			continue
		}
		if ok, _ := path.Match(pat, key); ok {
			return true
		}
	}
	return false
}
