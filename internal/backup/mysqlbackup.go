package backup

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/DigitalTolk/keel/internal/runner"
)

// MySQLBackup dumps a single database, archives it as tar.gz, uploads it to a
// Destination, and enforces retention. mysqldump is the only external tool;
// archiving, upload, and purge are native.
type MySQLBackup struct {
	Runner     runner.Runner
	Dest       Destination
	Config     MySQLConfig
	KeyPrefix  string // e.g. "mysqldumps/prod/"
	FilePrefix string // e.g. "app"
	Keep       int    // retain this many newest backups under KeyPrefix (0 = no purge)
	Now        func() time.Time
}

// Run performs the backup and returns the destination key written.
func (b MySQLBackup) Run(ctx context.Context) (string, error) {
	now := b.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}

	tmp, err := os.MkdirTemp("", "keel-mysql-")
	if err != nil {
		return "", fmt.Errorf("temp dir: %w", err)
	}
	defer os.RemoveAll(tmp)

	// Write a 0600 defaults-extra-file so the password never hits argv.
	cnfPath := filepath.Join(tmp, "my.cnf")
	if err := os.WriteFile(cnfPath, []byte(RenderMyCnf(b.Config)), 0o600); err != nil {
		return "", fmt.Errorf("write my.cnf: %w", err)
	}

	// Dump to a temp .sql file.
	dumpPath := filepath.Join(tmp, "dump.sql")
	dumpFile, err := os.Create(dumpPath)
	if err != nil {
		return "", fmt.Errorf("create dump file: %w", err)
	}
	err = b.Runner.Stream(ctx, dumpFile, "mysqldump", MysqldumpArgs(cnfPath, b.Config.DB)...)
	closeErr := dumpFile.Close()
	if err != nil {
		return "", fmt.Errorf("mysqldump %s: %w", b.Config.DB, err)
	}
	if closeErr != nil {
		return "", closeErr
	}

	// Archive natively to tar.gz.
	archivePath := filepath.Join(tmp, "archive.tar.gz")
	archiveFile, err := os.Create(archivePath)
	if err != nil {
		return "", fmt.Errorf("create archive: %w", err)
	}
	err = WriteTarGz(archiveFile, b.Config.DB+".sql", dumpPath)
	if cerr := archiveFile.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return "", fmt.Errorf("archive: %w", err)
	}

	// Upload + retention (shared with DirBackup).
	return storeArchive(ctx, b.Dest, archivePath, b.KeyPrefix, b.FilePrefix, b.Keep, now)
}

// Purge deletes objects under prefix beyond the newest keep, returning the keys
// it deleted.
func Purge(ctx context.Context, dest Destination, prefix string, keep int) ([]string, error) {
	objs, err := dest.List(ctx, prefix)
	if err != nil {
		return nil, err
	}
	var deleted []string
	for _, o := range SelectForDeletion(objs, keep) {
		if err := dest.Delete(ctx, o.Key); err != nil {
			return deleted, fmt.Errorf("delete %s: %w", o.Key, err)
		}
		deleted = append(deleted, o.Key)
	}
	return deleted, nil
}
