// Package backup orchestrates database and file backups: dumping/archiving
// natively, uploading to a Destination, and enforcing retention.
package backup

import (
	"sort"
	"time"
)

// Object is a stored backup artifact, used for retention decisions.
type Object struct {
	Key     string
	ModTime time.Time
}

// SelectForDeletion returns the objects that should be deleted so that only the
// `keep` newest remain. Objects are ordered newest-first by ModTime (ties
// broken by Key descending for determinism). As a safety measure, keep <= 0 is
// treated as a no-op so a misconfiguration can never wipe every backup.
func SelectForDeletion(objects []Object, keep int) []Object {
	if keep <= 0 || len(objects) <= keep {
		return nil
	}

	sorted := make([]Object, len(objects))
	copy(sorted, objects)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].ModTime.Equal(sorted[j].ModTime) {
			return sorted[i].Key > sorted[j].Key
		}
		return sorted[i].ModTime.After(sorted[j].ModTime)
	})

	return sorted[keep:]
}
