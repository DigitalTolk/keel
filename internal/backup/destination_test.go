package backup

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func tempFile(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "src.tar.gz")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLocalDestinationPutListDelete(t *testing.T) {
	base := t.TempDir()
	dest := NewLocalDestination(base)
	ctx := context.Background()
	src := tempFile(t, "backup-data")

	if err := dest.Put(ctx, "mysqldumps/prod/app-2026-06-19.tar.gz", src); err != nil {
		t.Fatalf("Put: %v", err)
	}

	// File landed at base/<key> with the right content.
	got, err := os.ReadFile(filepath.Join(base, "mysqldumps/prod/app-2026-06-19.tar.gz"))
	if err != nil || string(got) != "backup-data" {
		t.Fatalf("stored file wrong: content=%q err=%v", got, err)
	}

	objs, err := dest.List(ctx, "mysqldumps/prod/")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	gotKeys := make([]string, len(objs))
	for i, o := range objs {
		gotKeys[i] = o.Key
	}
	if !slices.Contains(gotKeys, "mysqldumps/prod/app-2026-06-19.tar.gz") {
		t.Fatalf("List = %v, want it to contain the stored key", gotKeys)
	}

	if err := dest.Delete(ctx, "mysqldumps/prod/app-2026-06-19.tar.gz"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := os.Stat(filepath.Join(base, "mysqldumps/prod/app-2026-06-19.tar.gz")); !os.IsNotExist(err) {
		t.Fatal("Delete did not remove the file")
	}
}

func TestLocalDestinationListPrefixFilters(t *testing.T) {
	base := t.TempDir()
	dest := NewLocalDestination(base)
	ctx := context.Background()
	src := tempFile(t, "x")

	_ = dest.Put(ctx, "a/one.tar.gz", src)
	_ = dest.Put(ctx, "b/two.tar.gz", src)

	objs, err := dest.List(ctx, "a/")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(objs) != 1 || objs[0].Key != "a/one.tar.gz" {
		t.Fatalf("prefix List = %+v, want only a/one.tar.gz", objs)
	}
}
