package jenkins

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestBatchReplaceOnlyTouchesMatchingFiles(t *testing.T) {
	root := t.TempDir()
	write := func(rel, content string) string {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	match1 := write("a/config.xml", "<url>http://old.example</url>")
	match2 := write("b/c/config.xml", "host=old.example port=8080")
	noMatch := write("d/config.xml", "<url>http://keep.example</url>")
	otherName := write("e/settings.xml", "old.example here") // wrong filename, must be ignored

	changed, err := BatchReplace(root, "config.xml", "old.example", "new.example")
	if err != nil {
		t.Fatalf("BatchReplace: %v", err)
	}

	slices.Sort(changed)
	want := []string{match1, match2}
	slices.Sort(want)
	if !slices.Equal(changed, want) {
		t.Fatalf("changed = %v, want %v", changed, want)
	}

	if got, _ := os.ReadFile(match1); string(got) != "<url>http://new.example</url>" {
		t.Errorf("match1 not replaced: %q", got)
	}
	if got, _ := os.ReadFile(noMatch); string(got) != "<url>http://keep.example</url>" {
		t.Errorf("non-matching file changed: %q", got)
	}
	if got, _ := os.ReadFile(otherName); string(got) != "old.example here" {
		t.Errorf("wrong-named file changed: %q", got)
	}
}

func TestBatchReplaceReplacesAllOccurrences(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "config.xml")
	_ = os.WriteFile(p, []byte("x=A y=A z=A"), 0o644)

	if _, err := BatchReplace(root, "config.xml", "A", "B"); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(p); string(got) != "x=B y=B z=B" {
		t.Fatalf("not all occurrences replaced: %q", got)
	}
}

func TestBatchReplaceMissingRootErrors(t *testing.T) {
	if _, err := BatchReplace(filepath.Join(t.TempDir(), "nope"), "config.xml", "a", "b"); err == nil {
		t.Fatal("missing root should error")
	}
}

func TestBatchReplaceReadErrorOnDanglingSymlink(t *testing.T) {
	root := t.TempDir()
	link := filepath.Join(root, "config.xml")
	if err := os.Symlink(filepath.Join(root, "missing-target"), link); err != nil {
		t.Skip("symlinks unsupported on this platform")
	}
	if _, err := BatchReplace(root, "config.xml", "a", "b"); err == nil {
		t.Fatal("expected read error on a dangling config.xml symlink")
	}
}
