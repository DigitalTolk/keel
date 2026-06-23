package backup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestWriteTarGzDirRespectsExcludes(t *testing.T) {
	root := t.TempDir()
	write := func(rel, content string) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("config.xml", "keep me")
	write("sub/data.txt", "keep me too")
	write("cache/junk.bin", "drop")
	write("logs/app.log", "drop")
	write("workspace/build/out", "drop")

	var buf bytes.Buffer
	if err := WriteTarGzDir(&buf, root, []string{"cache/*", "logs/*", "workspace/*"}); err != nil {
		t.Fatalf("WriteTarGzDir: %v", err)
	}

	var names []string
	gz, _ := gzip.NewReader(&buf)
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		names = append(names, hdr.Name)
	}

	if !slices.Contains(names, "config.xml") || !slices.Contains(names, "sub/data.txt") {
		t.Errorf("expected kept files present, got %v", names)
	}
	for _, n := range names {
		if n == "cache/junk.bin" || n == "logs/app.log" || n == "workspace/build/out" {
			t.Errorf("excluded file leaked into archive: %q", n)
		}
	}
}
