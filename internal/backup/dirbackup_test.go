package backup

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestDirBackupArchivesAndUploads(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "config.xml"), []byte("<jenkins/>"), 0o644); err != nil {
		t.Fatal(err)
	}
	dest := NewLocalDestination(t.TempDir())

	b := DirBackup{
		Root:       root,
		Excludes:   []string{"cache/*"},
		Dest:       dest,
		KeyPrefix:  "jenkins/",
		FilePrefix: "jenkins",
		Keep:       7,
		Now:        fixedClock,
	}
	key, err := b.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if key != "jenkins/jenkins-2026-06-19.tar.gz" {
		t.Fatalf("key = %q", key)
	}
	if _, err := os.Stat(dest.path(key)); err != nil {
		t.Fatalf("archive not stored: %v", err)
	}
}
