package backup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteTarGzSingleEntryRoundTrips(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "dump.sql")
	content := []byte("-- mysql dump\nCREATE TABLE t (id INT);\n")
	if err := os.WriteFile(src, content, 0o600); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := WriteTarGz(&buf, "app.sql", src); err != nil {
		t.Fatalf("WriteTarGz: %v", err)
	}

	// Decompress and read the single entry back.
	gz, err := gzip.NewReader(&buf)
	if err != nil {
		t.Fatalf("gzip: %v", err)
	}
	tr := tar.NewReader(gz)
	hdr, err := tr.Next()
	if err != nil {
		t.Fatalf("tar next: %v", err)
	}
	if hdr.Name != "app.sql" {
		t.Errorf("entry name = %q, want app.sql", hdr.Name)
	}
	got, _ := io.ReadAll(tr)
	if !bytes.Equal(got, content) {
		t.Errorf("entry content = %q, want %q", got, content)
	}
	if _, err := tr.Next(); err != io.EOF {
		t.Errorf("expected exactly one entry, got more")
	}
}
