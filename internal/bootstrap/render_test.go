package bootstrap

import "testing"

func TestSudoersContent(t *testing.T) {
	want := "bofh ALL=(ALL) NOPASSWD:ALL\nDefaults:bofh !requiretty\n"
	if got := SudoersContent("bofh"); got != want {
		t.Fatalf("SudoersContent() = %q, want %q", got, want)
	}
}
