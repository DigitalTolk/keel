// Package jenkins provides Jenkins maintenance helpers, porting
// jenkins-batch-edit.sh.
package jenkins

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// BatchReplace walks root recursively and, in every file named fileName that
// contains search, replaces all literal occurrences of search with replace,
// rewriting the file in place. It returns the paths of files that changed.
//
// Replacement is literal (not regex), which is safer than the original
// sed-based script for arbitrary config values.
func BatchReplace(root, fileName, search, replace string) ([]string, error) {
	var changed []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || d.Name() != fileName {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if !bytes.Contains(data, []byte(search)) {
			return nil
		}
		updated := strings.ReplaceAll(string(data), search, replace)

		info, err := d.Info()
		if err != nil {
			return err
		}
		if err := os.WriteFile(path, []byte(updated), info.Mode().Perm()); err != nil {
			return err
		}
		changed = append(changed, path)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("batch edit under %s: %w", root, err)
	}
	return changed, nil
}
