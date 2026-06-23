package backup

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// DirBackup archives a directory tree (with excludes) as tar.gz, uploads it to
// a Destination, and enforces retention. Used for the Jenkins home backup.
type DirBackup struct {
	Root       string
	Excludes   []string
	Dest       Destination
	KeyPrefix  string
	FilePrefix string
	Keep       int
	Now        func() time.Time
}

// Run performs the directory backup and returns the destination key written.
func (b DirBackup) Run(ctx context.Context) (string, error) {
	tmp, err := os.MkdirTemp("", "keel-dir-")
	if err != nil {
		return "", fmt.Errorf("temp dir: %w", err)
	}
	defer os.RemoveAll(tmp)

	archivePath := filepath.Join(tmp, "archive.tar.gz")
	f, err := os.Create(archivePath)
	if err != nil {
		return "", fmt.Errorf("create archive: %w", err)
	}
	err = WriteTarGzDir(f, b.Root, b.Excludes)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return "", fmt.Errorf("archive %s: %w", b.Root, err)
	}

	return storeArchive(ctx, b.Dest, archivePath, b.KeyPrefix, b.FilePrefix, b.Keep, b.Now)
}

// storeArchive uploads a local archive under a dated key and enforces
// retention. Shared by MySQLBackup and DirBackup.
func storeArchive(ctx context.Context, dest Destination, archivePath, keyPrefix, filePrefix string, keep int, now func() time.Time) (string, error) {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	key := fmt.Sprintf("%s%s-%s.tar.gz", keyPrefix, filePrefix, now().Format("2006-01-02"))
	if err := dest.Put(ctx, key, archivePath); err != nil {
		return "", fmt.Errorf("upload %s: %w", key, err)
	}
	if keep > 0 {
		if _, err := Purge(ctx, dest, keyPrefix, keep); err != nil {
			return key, fmt.Errorf("purge: %w", err)
		}
	}
	return key, nil
}
