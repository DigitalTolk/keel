package backup

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"io"
	"os"
	"slices"
	"strings"
	"testing"
	"time"
)

// fakeRunner writes canned output instead of running a real command.
type fakeRunner struct {
	output   string
	gotName  string
	gotArgs  []string
	failWith error
}

func (f *fakeRunner) Stream(ctx context.Context, w io.Writer, name string, args ...string) error {
	f.gotName = name
	f.gotArgs = args
	if f.failWith != nil {
		return f.failWith
	}
	_, err := io.WriteString(w, f.output)
	return err
}

// fakeDest records operations and returns a fixed listing.
type fakeDest struct {
	listing []Object
	deleted []string
	puts    []string
}

func (d *fakeDest) Put(ctx context.Context, key, src string) error {
	d.puts = append(d.puts, key)
	return nil
}
func (d *fakeDest) List(ctx context.Context, prefix string) ([]Object, error) { return d.listing, nil }
func (d *fakeDest) Delete(ctx context.Context, key string) error {
	d.deleted = append(d.deleted, key)
	return nil
}

func fixedClock() time.Time { return time.Date(2026, 6, 19, 0, 0, 0, 0, time.UTC) }

func TestMySQLBackupDumpsArchivesUploads(t *testing.T) {
	fr := &fakeRunner{output: "-- DUMP\nSELECT 1;\n"}
	dest := NewLocalDestination(t.TempDir())
	b := MySQLBackup{
		Runner:     fr,
		Dest:       dest,
		Config:     MySQLConfig{Host: "db1", User: "u", Password: "pw", DB: "app"},
		KeyPrefix:  "mysqldumps/prod/",
		FilePrefix: "app",
		Keep:       7,
		Now:        fixedClock,
	}

	key, err := b.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if fr.gotName != "mysqldump" {
		t.Errorf("expected mysqldump to be invoked, got %q", fr.gotName)
	}
	if !slices.Contains(fr.gotArgs, "app") {
		t.Errorf("mysqldump args should include db 'app', got %v", fr.gotArgs)
	}
	wantKey := "mysqldumps/prod/app-2026-06-19.tar.gz"
	if key != wantKey {
		t.Errorf("key = %q, want %q", key, wantKey)
	}

	// The uploaded archive must contain the dump under <db>.sql.
	objs, _ := dest.List(context.Background(), "mysqldumps/prod/")
	if len(objs) != 1 {
		t.Fatalf("expected 1 stored object, got %d", len(objs))
	}
	assertArchiveEntry(t, dest.path(wantKey), "app.sql", "-- DUMP\nSELECT 1;\n")
}

func TestMySQLBackupFailsWhenDumpFails(t *testing.T) {
	fr := &fakeRunner{failWith: io.ErrClosedPipe}
	b := MySQLBackup{
		Runner: fr, Dest: NewLocalDestination(t.TempDir()),
		Config: MySQLConfig{DB: "app"}, FilePrefix: "app", Now: fixedClock,
	}
	if _, err := b.Run(context.Background()); err == nil {
		t.Fatal("Run: want error when mysqldump fails, got nil")
	}
}

func TestPurgeDeletesOldestBeyondKeep(t *testing.T) {
	dest := &fakeDest{listing: []Object{
		{Key: "p/a", ModTime: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)},
		{Key: "p/b", ModTime: time.Date(2026, 6, 2, 0, 0, 0, 0, time.UTC)},
		{Key: "p/c", ModTime: time.Date(2026, 6, 3, 0, 0, 0, 0, time.UTC)},
	}}
	deleted, err := Purge(context.Background(), dest, "p/", 1)
	if err != nil {
		t.Fatalf("Purge: %v", err)
	}
	if len(deleted) != 2 {
		t.Fatalf("deleted = %v, want 2 (keep newest 1)", deleted)
	}
	if slices.Contains(deleted, "p/c") {
		t.Errorf("newest object p/c must be kept, got deletions %v", deleted)
	}
}

func assertArchiveEntry(t *testing.T, path, entry, want string) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(gz)
	hdr, err := tr.Next()
	if err != nil {
		t.Fatal(err)
	}
	if hdr.Name != entry {
		t.Fatalf("archive entry = %q, want %q", hdr.Name, entry)
	}
	got, _ := io.ReadAll(tr)
	if string(got) != want {
		t.Fatalf("archive content = %q, want %q", got, want)
	}
	if !strings.HasSuffix(path, ".tar.gz") {
		t.Errorf("archive path %q should end in .tar.gz", path)
	}
}
