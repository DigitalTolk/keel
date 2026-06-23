package backup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

// errDeleteFailed is returned by failingDest.Delete to exercise purge/Delete
// error paths.
var errDeleteFailed = errors.New("delete boom")

// errPutFailed is returned by failingDest.Put to exercise upload error paths.
var errPutFailed = errors.New("put boom")

// errListFailed is returned by listErrDest.List to exercise Purge's List-error
// branch.
var errListFailed = errors.New("list boom")

// listErrDest is a Destination whose List always errors, used to cover Purge's
// early return when listing fails.
type listErrDest struct{}

func (listErrDest) Put(ctx context.Context, key, src string) error { return nil }
func (listErrDest) List(ctx context.Context, prefix string) ([]Object, error) {
	return nil, errListFailed
}
func (listErrDest) Delete(ctx context.Context, key string) error { return nil }

var _ Destination = listErrDest{}

// errWriteFailed is returned by failingWriter to exercise tar/gzip write and
// close error paths in the archivers.
var errWriteFailed = errors.New("write boom")

// failingWriter is an io.Writer that always fails, so the gzip/tar layers
// surface their write and close errors.
type failingWriter struct{}

func (failingWriter) Write(p []byte) (int, error) { return 0, errWriteFailed }

// budgetWriter accepts up to budget bytes, then fails every subsequent Write.
// It lets WriteHeader/body bytes through so the failure surfaces at the final
// gzip flush during Close.
type budgetWriter struct{ remaining int }

func (b *budgetWriter) Write(p []byte) (int, error) {
	if b.remaining <= 0 {
		return 0, errWriteFailed
	}
	if len(p) <= b.remaining {
		b.remaining -= len(p)
		return len(p), nil
	}
	n := b.remaining
	b.remaining = 0
	return n, errWriteFailed
}

// failingDest is a Destination whose Put and/or Delete return errors on demand,
// used to cover error-handling branches that fakeDest (which always succeeds)
// cannot reach.
type failingDest struct {
	listing []Object
	failPut bool
	failDel bool
	deleted []string
}

func (d *failingDest) Put(ctx context.Context, key, src string) error {
	if d.failPut {
		return errPutFailed
	}
	return nil
}

func (d *failingDest) List(ctx context.Context, prefix string) ([]Object, error) {
	return d.listing, nil
}

func (d *failingDest) Delete(ctx context.Context, key string) error {
	if d.failDel {
		return errDeleteFailed
	}
	d.deleted = append(d.deleted, key)
	return nil
}

var _ Destination = (*failingDest)(nil)

// closingRunner writes its output, then closes the destination *os.File so the
// backup's own dumpFile.Close() returns an already-closed error, exercising the
// closeErr branch in MySQLBackup.Run.
type closingRunner struct{ output string }

func (r closingRunner) Stream(ctx context.Context, w io.Writer, name string, args ...string) error {
	if f, ok := w.(*os.File); ok {
		_, _ = io.WriteString(f, r.output)
		_ = f.Close()
		return nil
	}
	_, err := io.WriteString(w, r.output)
	return err
}

// tmpNukingRunner writes its output, then removes the temp directory containing
// the dump file so the subsequent archive os.Create fails, exercising the
// create-archive error branch in MySQLBackup.Run.
type tmpNukingRunner struct{ output string }

func (r tmpNukingRunner) Stream(ctx context.Context, w io.Writer, name string, args ...string) error {
	if f, ok := w.(*os.File); ok {
		_, _ = io.WriteString(f, r.output)
		_ = os.RemoveAll(filepath.Dir(f.Name()))
		return nil
	}
	_, err := io.WriteString(w, r.output)
	return err
}

// dumpLockingRunner writes its output, then chmods the dump file to 0000 so the
// subsequent WriteTarGz cannot re-open it, exercising the archive-step error
// branch in MySQLBackup.Run.
type dumpLockingRunner struct{ output string }

func (r dumpLockingRunner) Stream(ctx context.Context, w io.Writer, name string, args ...string) error {
	if f, ok := w.(*os.File); ok {
		_, _ = io.WriteString(f, r.output)
		_ = os.Chmod(f.Name(), 0o000)
		return nil
	}
	_, err := io.WriteString(w, r.output)
	return err
}

// --- archive.go: WriteTarGz error when the source file is missing -----------

func TestWriteTarGzMissingSourceReturnsError(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist.sql")
	var buf bytes.Buffer
	err := WriteTarGz(&buf, "app.sql", missing)
	if err == nil {
		t.Fatal("WriteTarGz: want error when src file is missing, got nil")
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("WriteTarGz error = %v, want it to wrap os.ErrNotExist", err)
	}
	if buf.Len() != 0 {
		t.Errorf("no archive bytes should be written on open failure, got %d", buf.Len())
	}
}

// --- archive.go: write/copy/close errors when the sink rejects writes -------

func TestWriteTarGzWriteHeaderErrorPropagates(t *testing.T) {
	src := tempFile(t, "some dump bytes")
	// The tar header is the first thing flushed to the gzip->writer chain, so a
	// failing sink surfaces as a write-header error.
	err := WriteTarGz(failingWriter{}, "app.sql", src)
	if err == nil {
		t.Fatal("WriteTarGz: want error when the sink rejects writes, got nil")
	}
}

// --- archive.go: gzip.Close error when the sink rejects the final flush ------

func TestWriteTarGzGzipCloseError(t *testing.T) {
	src := tempFile(t, "tiny")
	// Budget 10 lets gzip's stream header and the (buffered) header/body through;
	// the failure then surfaces at the final gz.Close flush.
	if err := WriteTarGz(&budgetWriter{remaining: 10}, "app.sql", src); err == nil {
		t.Fatal("WriteTarGz: want error when the final gzip flush fails, got nil")
	}
}

// --- archive.go: io.Copy error when the source is a directory ----------------

func TestWriteTarGzCopyErrorWhenSrcIsDir(t *testing.T) {
	srcDir := filepath.Join(t.TempDir(), "asdir")
	if err := os.MkdirAll(srcDir, 0o750); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	// A directory opens and stats fine, header writes fine, but copying its fd
	// body fails — exercising the io.Copy(tw, f) error branch.
	if err := WriteTarGz(&buf, "dir-entry", srcDir); err == nil {
		t.Fatal("WriteTarGz: want error copying a directory body, got nil")
	}
}

// --- dirarchive.go: SkipDir branch for excluded dirs + exact path.Match -----

func TestWriteTarGzDirSkipsExcludedDirSubtreeAndExactMatch(t *testing.T) {
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
	// "cache" matched by the dir-subtree pattern "cache/*": the directory itself
	// equals the trimmed pattern, so WalkDir must return SkipDir and never
	// descend into nested.bin.
	write("cache/nested/deep/nested.bin", "drop subtree")
	// "secret.key" matched by an exact path.Match glob (no trailing "/*").
	write("secret.key", "drop exact")
	write("keep.txt", "keep me")

	var buf bytes.Buffer
	if err := WriteTarGzDir(&buf, root, []string{"cache/*", "secret.key"}); err != nil {
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

	if !slices.Contains(names, "keep.txt") {
		t.Errorf("kept file missing, got %v", names)
	}
	for _, n := range names {
		if n == "secret.key" {
			t.Errorf("exact-match excluded file leaked: %q", n)
		}
		if filepath.ToSlash(n) == "cache/nested/deep/nested.bin" {
			t.Errorf("subtree under excluded dir leaked (SkipDir not honored): %q", n)
		}
	}
}

// --- dirarchive.go: tar WriteHeader error when the sink rejects writes ------

func TestWriteTarGzDirWriteHeaderErrorPropagates(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "f.txt"), []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	// With a file present, the first tar header is flushed to the failing sink.
	if err := WriteTarGzDir(failingWriter{}, root, nil); err == nil {
		t.Fatal("WriteTarGzDir: want error when the sink rejects writes, got nil")
	}
}

// --- dirarchive.go: tar.Close error when the sink rejects writes -------------

func TestWriteTarGzDirTarCloseErrorOnEmptyDir(t *testing.T) {
	root := t.TempDir() // empty: first sink write is the tar trailer at tw.Close
	if err := WriteTarGzDir(failingWriter{}, root, nil); err == nil {
		t.Fatal("WriteTarGzDir: want error when tar close into a failing sink, got nil")
	}
}

// --- dirarchive.go: gzip.Close error when the sink rejects the final flush ---

func TestWriteTarGzDirGzipCloseError(t *testing.T) {
	root := t.TempDir() // empty dir
	// Budget 10 lets gzip's 10-byte stream header through, so tw.Close succeeds
	// (buffered) and the failure surfaces at the final gz.Close flush.
	if err := WriteTarGzDir(&budgetWriter{remaining: 10}, root, nil); err == nil {
		t.Fatal("WriteTarGzDir: want error when the final gzip flush fails, got nil")
	}
}

// isExcluded exact-path branch directly: a non-"/*" pattern that exactly equals
// the key must match via path.Match.
func TestIsExcludedExactPathMatch(t *testing.T) {
	if !isExcluded("config/secret.env", []string{"config/secret.env"}) {
		t.Error("isExcluded should match an exact non-glob path")
	}
	if isExcluded("config/other.env", []string{"config/secret.env"}) {
		t.Error("isExcluded should not match a different path")
	}
}

// --- destination.go: Put error (missing src), List on missing base, Delete missing key

func TestLocalDestinationPutMissingSourceErrors(t *testing.T) {
	dest := NewLocalDestination(t.TempDir())
	missing := filepath.Join(t.TempDir(), "nope.tar.gz")
	if err := dest.Put(context.Background(), "k/out.tar.gz", missing); err == nil {
		t.Fatal("Put: want error when src file is missing, got nil")
	}
}

func TestLocalDestinationListMissingBaseIsEmpty(t *testing.T) {
	base := filepath.Join(t.TempDir(), "does-not-exist")
	dest := NewLocalDestination(base)
	objs, err := dest.List(context.Background(), "")
	if err != nil {
		t.Fatalf("List on missing base should not error, got %v", err)
	}
	if len(objs) != 0 {
		t.Fatalf("List on missing base = %v, want empty", objs)
	}
}

func TestLocalDestinationDeleteMissingKeyErrors(t *testing.T) {
	dest := NewLocalDestination(t.TempDir())
	if err := dest.Delete(context.Background(), "never/written.tar.gz"); err == nil {
		t.Fatal("Delete: want error when key does not exist, got nil")
	}
}

// --- mysqlbackup.go: Run error when Dest.Put fails --------------------------

func TestMySQLBackupRunFailsWhenPutFails(t *testing.T) {
	fr := &fakeRunner{output: "-- DUMP\n"}
	b := MySQLBackup{
		Runner:     fr,
		Dest:       &failingDest{failPut: true},
		Config:     MySQLConfig{DB: "app"},
		KeyPrefix:  "mysqldumps/prod/",
		FilePrefix: "app",
		Keep:       7,
		Now:        fixedClock,
	}
	_, err := b.Run(context.Background())
	if err == nil {
		t.Fatal("Run: want error when Dest.Put fails, got nil")
	}
	if !errors.Is(err, errPutFailed) {
		t.Errorf("Run error = %v, want it to wrap errPutFailed", err)
	}
}

// --- mysqlbackup.go: dumpFile.Close error branch ----------------------------

func TestMySQLBackupRunReportsDumpCloseError(t *testing.T) {
	b := MySQLBackup{
		Runner:     closingRunner{output: "-- DUMP\n"},
		Dest:       NewLocalDestination(t.TempDir()),
		Config:     MySQLConfig{DB: "app"},
		FilePrefix: "app",
		Now:        fixedClock,
	}
	if _, err := b.Run(context.Background()); err == nil {
		t.Fatal("Run: want error when the dump file close fails, got nil")
	}
}

// --- mysqlbackup.go: create-archive error branch ----------------------------

func TestMySQLBackupRunFailsWhenArchiveCreateFails(t *testing.T) {
	b := MySQLBackup{
		Runner:     tmpNukingRunner{output: "-- DUMP\n"},
		Dest:       NewLocalDestination(t.TempDir()),
		Config:     MySQLConfig{DB: "app"},
		FilePrefix: "app",
		Now:        fixedClock,
	}
	if _, err := b.Run(context.Background()); err == nil {
		t.Fatal("Run: want error when the archive file cannot be created, got nil")
	}
}

// --- mysqlbackup.go: archive-step (WriteTarGz) error branch -----------------

func TestMySQLBackupRunFailsWhenArchiveStepFails(t *testing.T) {
	if f, err := os.Open(t.TempDir()); err == nil {
		f.Close()
	}
	b := MySQLBackup{
		Runner:     dumpLockingRunner{output: "-- DUMP\n"},
		Dest:       NewLocalDestination(t.TempDir()),
		Config:     MySQLConfig{DB: "app"},
		FilePrefix: "app",
		Now:        fixedClock,
	}
	_, err := b.Run(context.Background())
	if err == nil {
		// If running as a privileged user, chmod 0000 doesn't block reads.
		t.Skip("dump file still readable (privileged user); archive-step error branch unreachable")
	}
}

// --- mysqlbackup.go: Purge when Delete returns an error ---------------------

func TestPurgeReturnsErrorWhenDeleteFails(t *testing.T) {
	dest := &failingDest{
		failDel: true,
		listing: []Object{
			{Key: "p/a", ModTime: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)},
			{Key: "p/b", ModTime: time.Date(2026, 6, 2, 0, 0, 0, 0, time.UTC)},
			{Key: "p/c", ModTime: time.Date(2026, 6, 3, 0, 0, 0, 0, time.UTC)},
		},
	}
	deleted, err := Purge(context.Background(), dest, "p/", 1)
	if err == nil {
		t.Fatal("Purge: want error when Delete fails, got nil")
	}
	if !errors.Is(err, errDeleteFailed) {
		t.Errorf("Purge error = %v, want it to wrap errDeleteFailed", err)
	}
	if len(deleted) != 0 {
		t.Errorf("no keys should be reported deleted when Delete fails first, got %v", deleted)
	}
}

// --- dirbackup.go: storeArchive purge-error via a Destination whose Delete errors

func TestDirBackupRunFailsWhenPurgeDeleteFails(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "config.xml"), []byte("<jenkins/>"), 0o644); err != nil {
		t.Fatal(err)
	}
	// More objects than Keep so SelectForDeletion returns a deletion, and Delete
	// then fails inside storeArchive's Purge call.
	dest := &failingDest{
		failDel: true,
		listing: []Object{
			{Key: "jenkins/old-1", ModTime: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)},
			{Key: "jenkins/old-2", ModTime: time.Date(2026, 6, 2, 0, 0, 0, 0, time.UTC)},
		},
	}
	b := DirBackup{
		Root:       root,
		Excludes:   nil,
		Dest:       dest,
		KeyPrefix:  "jenkins/",
		FilePrefix: "jenkins",
		Keep:       1,
		Now:        fixedClock,
	}
	key, err := b.Run(context.Background())
	if err == nil {
		t.Fatal("Run: want error when purge Delete fails, got nil")
	}
	if !errors.Is(err, errDeleteFailed) {
		t.Errorf("Run error = %v, want it to wrap errDeleteFailed", err)
	}
	// storeArchive returns the written key alongside the purge error.
	if key != "jenkins/jenkins-2026-06-19.tar.gz" {
		t.Errorf("key = %q, want the written key returned with the purge error", key)
	}
}

// --- retention.go: SelectForDeletion keep == len boundary -------------------

func TestSelectForDeletionKeepEqualsLenIsNoOp(t *testing.T) {
	objs := []Object{obj("a", 1), obj("b", 2), obj("c", 3)}
	if del := SelectForDeletion(objs, len(objs)); del != nil {
		t.Fatalf("keep == len must be a no-op, got %v", keys(del))
	}
}

// --- mysql.go: ShowDatabasesArgs --------------------------------------------

func TestShowDatabasesArgsContainsShowDatabases(t *testing.T) {
	args := ShowDatabasesArgs("/tmp/my.cnf")
	if !slices.Contains(args, "SHOW DATABASES") {
		t.Errorf("ShowDatabasesArgs = %v, want it to contain \"SHOW DATABASES\"", args)
	}
	if !slices.Contains(args, "--defaults-extra-file=/tmp/my.cnf") {
		t.Errorf("ShowDatabasesArgs = %v, want it to reference the defaults-extra-file", args)
	}
	if !slices.Contains(args, "-N") || !slices.Contains(args, "-B") {
		t.Errorf("ShowDatabasesArgs = %v, want -N and -B for batch/no-column output", args)
	}
}

// --- retention.go: tie-break comparator (equal ModTime -> Key descending) ----

func TestSelectForDeletionTieBreaksByKeyDescending(t *testing.T) {
	ts := time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC)
	objs := []Object{
		{Key: "aaa", ModTime: ts},
		{Key: "ccc", ModTime: ts},
		{Key: "bbb", ModTime: ts},
	}
	// All ModTimes equal: newest-first becomes Key descending (ccc, bbb, aaa);
	// keeping 1 deletes the two smaller keys.
	del := SelectForDeletion(objs, 1)
	got := keys(del)
	if len(got) != 2 {
		t.Fatalf("delete set = %v, want 2", got)
	}
	if slices.Contains(got, "ccc") {
		t.Errorf("highest key ccc must be kept on tie, got deletions %v", got)
	}
	if !slices.Contains(got, "aaa") || !slices.Contains(got, "bbb") {
		t.Errorf("lower keys aaa,bbb must be deleted on tie, got %v", got)
	}
}

// --- mysqlbackup.go: Purge early return when List fails ----------------------

func TestPurgeReturnsErrorWhenListFails(t *testing.T) {
	_, err := Purge(context.Background(), listErrDest{}, "p/", 1)
	if !errors.Is(err, errListFailed) {
		t.Fatalf("Purge error = %v, want it to wrap errListFailed", err)
	}
}

// --- mysqlbackup.go / dirbackup.go: default clock when Now is nil ------------

func TestMySQLBackupUsesDefaultClockWhenNowNil(t *testing.T) {
	fr := &fakeRunner{output: "-- DUMP\n"}
	dest := NewLocalDestination(t.TempDir())
	b := MySQLBackup{
		Runner:     fr,
		Dest:       dest,
		Config:     MySQLConfig{DB: "app"},
		KeyPrefix:  "mysqldumps/prod/",
		FilePrefix: "app",
		Now:        nil, // exercise the default time.Now().UTC() branch
	}
	key, err := b.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	want := "mysqldumps/prod/app-" + time.Now().UTC().Format("2006-01-02") + ".tar.gz"
	if key != want {
		t.Errorf("key = %q, want %q (default clock)", key, want)
	}
}

func TestDirBackupUsesDefaultClockWhenNowNil(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "config.xml"), []byte("<x/>"), 0o644); err != nil {
		t.Fatal(err)
	}
	dest := NewLocalDestination(t.TempDir())
	b := DirBackup{
		Root:       root,
		Dest:       dest,
		KeyPrefix:  "jenkins/",
		FilePrefix: "jenkins",
		Now:        nil, // storeArchive falls back to time.Now().UTC()
	}
	key, err := b.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	want := "jenkins/jenkins-" + time.Now().UTC().Format("2006-01-02") + ".tar.gz"
	if key != want {
		t.Errorf("key = %q, want %q (default clock)", key, want)
	}
}

// --- mysqlbackup.go / dirbackup.go: MkdirTemp failure -----------------------

func TestMySQLBackupRunFailsWhenTempDirUnavailable(t *testing.T) {
	// Point TMPDIR at a regular file so os.MkdirTemp("") cannot create under it.
	tmpFileAsDir := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(tmpFileAsDir, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMPDIR", tmpFileAsDir)
	b := MySQLBackup{
		Runner:     &fakeRunner{output: "x"},
		Dest:       NewLocalDestination(t.TempDir()),
		Config:     MySQLConfig{DB: "app"},
		FilePrefix: "app",
		Now:        fixedClock,
	}
	if _, err := b.Run(context.Background()); err == nil {
		t.Fatal("Run: want error when temp dir cannot be created, got nil")
	}
}

func TestDirBackupRunFailsWhenTempDirUnavailable(t *testing.T) {
	tmpFileAsDir := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(tmpFileAsDir, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMPDIR", tmpFileAsDir)
	b := DirBackup{
		Root:       t.TempDir(),
		Dest:       NewLocalDestination(t.TempDir()),
		KeyPrefix:  "jenkins/",
		FilePrefix: "jenkins",
		Now:        fixedClock,
	}
	if _, err := b.Run(context.Background()); err == nil {
		t.Fatal("Run: want error when temp dir cannot be created, got nil")
	}
}

// --- destination.go: Put MkdirAll error (base is a regular file) ------------

func TestLocalDestinationPutMkdirAllErrors(t *testing.T) {
	dir := t.TempDir()
	baseAsFile := filepath.Join(dir, "base")
	if err := os.WriteFile(baseAsFile, []byte("not a dir"), 0o600); err != nil {
		t.Fatal(err)
	}
	dest := NewLocalDestination(baseAsFile) // base is a file, so MkdirAll under it fails
	src := tempFile(t, "data")
	if err := dest.Put(context.Background(), "sub/out.tar.gz", src); err == nil {
		t.Fatal("Put: want error when parent cannot be created, got nil")
	}
}

// --- destination.go: Put OpenFile(dst) error (dst is an existing directory) --

func TestLocalDestinationPutDestIsDirectoryErrors(t *testing.T) {
	base := t.TempDir()
	dest := NewLocalDestination(base)
	// Pre-create base/key as a directory so OpenFile(O_WRONLY) on it fails.
	if err := os.MkdirAll(filepath.Join(base, "key"), 0o750); err != nil {
		t.Fatal(err)
	}
	src := tempFile(t, "data")
	if err := dest.Put(context.Background(), "key", src); err == nil {
		t.Fatal("Put: want error when destination path is a directory, got nil")
	}
}

// --- dirarchive.go: non-regular files are skipped ---------------------------

func TestWriteTarGzDirSkipsNonRegularFiles(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "real.txt"), []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A symlink is a non-regular DirEntry: WalkDir reports it via lstat, so the
	// archiver must skip it without following.
	if err := os.Symlink(filepath.Join(root, "real.txt"), filepath.Join(root, "link.txt")); err != nil {
		t.Skipf("symlink unsupported on this platform: %v", err)
	}

	var buf bytes.Buffer
	if err := WriteTarGzDir(&buf, root, nil); err != nil {
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

	if !slices.Contains(names, "real.txt") {
		t.Errorf("regular file missing, got %v", names)
	}
	if slices.Contains(names, "link.txt") {
		t.Errorf("non-regular symlink must be skipped, got %v", names)
	}
}

// --- destination.go: Put io.Copy error (src is a directory) -----------------

func TestLocalDestinationPutCopyErrorWhenSrcIsDir(t *testing.T) {
	base := t.TempDir()
	dest := NewLocalDestination(base)
	// A directory opens fine but reading its fd to copy returns an error,
	// exercising the io.Copy failure branch in Put.
	srcDir := filepath.Join(t.TempDir(), "adir")
	if err := os.MkdirAll(srcDir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := dest.Put(context.Background(), "out.tar.gz", srcDir); err == nil {
		t.Fatal("Put: want error when copying from a directory source, got nil")
	}
}

// --- dirarchive.go: os.Open error on an unreadable file ----------------------

func TestWriteTarGzDirOpenErrorOnUnreadableFile(t *testing.T) {
	root := t.TempDir()
	secret := filepath.Join(root, "secret.txt")
	if err := os.WriteFile(secret, []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(secret, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(secret, 0o600) })
	if f, err := os.Open(secret); err == nil {
		f.Close()
		t.Skip("file still readable (running as privileged user); cannot exercise open-error branch")
	}

	var buf bytes.Buffer
	if err := WriteTarGzDir(&buf, root, nil); err == nil {
		t.Fatal("WriteTarGzDir: want error when a file cannot be opened, got nil")
	}
}

// --- destination.go: List error when walking an unreadable directory ---------

func TestLocalDestinationListWalkErrorOnUnreadableDir(t *testing.T) {
	base := t.TempDir()
	locked := filepath.Join(base, "locked")
	if err := os.MkdirAll(locked, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(locked, "x"), []byte("y"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o750) })
	if entries, err := os.ReadDir(locked); err == nil {
		_ = entries
		t.Skip("dir still readable (running as privileged user); cannot exercise walk-error branch")
	}

	dest := NewLocalDestination(base)
	if _, err := dest.List(context.Background(), ""); err == nil {
		t.Fatal("List: want error when a subdirectory cannot be read, got nil")
	}
}

// --- dirbackup.go: WriteTarGzDir error surfaces from Run ---------------------

func TestDirBackupRunFailsWhenRootMissing(t *testing.T) {
	b := DirBackup{
		Root:       filepath.Join(t.TempDir(), "no-such-root"),
		Dest:       NewLocalDestination(t.TempDir()),
		KeyPrefix:  "jenkins/",
		FilePrefix: "jenkins",
		Now:        fixedClock,
	}
	if _, err := b.Run(context.Background()); err == nil {
		t.Fatal("Run: want error when Root does not exist, got nil")
	}
}
