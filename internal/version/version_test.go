package version

import (
	"strings"
	"testing"
)

func TestStringIncludesAllFields(t *testing.T) {
	Version, Commit, Date = "1.2.3", "abc1234", "2026-06-19"
	t.Cleanup(func() { Version, Commit, Date = "dev", "none", "unknown" })

	got := String()
	for _, want := range []string{"1.2.3", "abc1234", "2026-06-19"} {
		if !strings.Contains(got, want) {
			t.Errorf("String() = %q, want it to contain %q", got, want)
		}
	}
}
