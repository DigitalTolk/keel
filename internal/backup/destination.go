package backup

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Destination is a backup storage backend. Implementations: LocalDestination
// (filesystem) and the S3-compatible client in internal/cloud/aws (which serves
// both AWS S3 and Backblaze B2).
type Destination interface {
	// Put uploads the local file at srcPath under key.
	Put(ctx context.Context, key, srcPath string) error
	// List returns objects whose key has the given prefix.
	List(ctx context.Context, prefix string) ([]Object, error)
	// Delete removes the object at key.
	Delete(ctx context.Context, key string) error
}

// LocalDestination stores backups under a base directory.
type LocalDestination struct {
	base string
}

// NewLocalDestination returns a Destination rooted at base.
func NewLocalDestination(base string) *LocalDestination {
	return &LocalDestination{base: base}
}

func (l *LocalDestination) path(key string) string {
	return filepath.Join(l.base, filepath.FromSlash(key))
}

// Put copies srcPath to base/key, creating parent directories.
func (l *LocalDestination) Put(ctx context.Context, key, srcPath string) error {
	dst := l.path(key)
	if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
		return err
	}
	in, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o640)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// List walks base and returns objects whose slash-separated key starts with
// prefix.
func (l *LocalDestination) List(ctx context.Context, prefix string) ([]Object, error) {
	var objs []Object
	err := filepath.WalkDir(l.base, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(l.base, p)
		if err != nil {
			return err
		}
		key := filepath.ToSlash(rel)
		if !strings.HasPrefix(key, prefix) {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		objs = append(objs, Object{Key: key, ModTime: info.ModTime()})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("list %s: %w", l.base, err)
	}
	return objs, nil
}

// Delete removes base/key.
func (l *LocalDestination) Delete(ctx context.Context, key string) error {
	return os.Remove(l.path(key))
}

var _ Destination = (*LocalDestination)(nil)
