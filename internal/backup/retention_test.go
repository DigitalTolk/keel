package backup

import (
	"testing"
	"time"
)

func obj(key string, day int) Object {
	return Object{Key: key, ModTime: time.Date(2026, 6, day, 0, 0, 0, 0, time.UTC)}
}

func keys(objs []Object) []string {
	out := make([]string, len(objs))
	for i, o := range objs {
		out[i] = o.Key
	}
	return out
}

func TestSelectForDeletionKeepsNewest(t *testing.T) {
	objs := []Object{obj("d1", 1), obj("d5", 5), obj("d3", 3), obj("d2", 2), obj("d4", 4)}

	del := SelectForDeletion(objs, 2) // keep d5, d4; delete d3, d2, d1

	got := keys(del)
	want := map[string]bool{"d3": true, "d2": true, "d1": true}
	if len(got) != 3 {
		t.Fatalf("delete set = %v, want 3 items", got)
	}
	for _, k := range got {
		if !want[k] {
			t.Errorf("unexpected deletion %q (newest 2 must be kept)", k)
		}
	}
}

func TestSelectForDeletionKeepAllWhenFewer(t *testing.T) {
	objs := []Object{obj("a", 1), obj("b", 2)}
	if del := SelectForDeletion(objs, 5); len(del) != 0 {
		t.Fatalf("want no deletions when count <= keep, got %v", keys(del))
	}
}

func TestSelectForDeletionZeroKeepIsSafe(t *testing.T) {
	objs := []Object{obj("a", 1), obj("b", 2)}
	// keep <= 0 must never delete everything by accident.
	if del := SelectForDeletion(objs, 0); len(del) != 0 {
		t.Fatalf("keep=0 must be a no-op, got %v", keys(del))
	}
}
