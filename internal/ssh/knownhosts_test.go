package ssh

import (
	"strings"
	"testing"
)

func TestUpsertKnownHostsLineAppendsToEmpty(t *testing.T) {
	out := UpsertKnownHostsLine(nil, "[web1]:2222 ssh-ed25519 AAAAKEY")
	if strings.TrimSpace(string(out)) != "[web1]:2222 ssh-ed25519 AAAAKEY" {
		t.Fatalf("got %q", out)
	}
}

func TestUpsertKnownHostsLineReplacesSameHost(t *testing.T) {
	existing := []byte("[web1]:2222 ssh-ed25519 OLDKEY\nother.host ssh-rsa KEEP\n")
	out := string(UpsertKnownHostsLine(existing, "[web1]:2222 ssh-ed25519 NEWKEY"))

	if strings.Contains(out, "OLDKEY") {
		t.Errorf("stale entry for web1 should be removed:\n%s", out)
	}
	if !strings.Contains(out, "NEWKEY") {
		t.Errorf("new entry should be present:\n%s", out)
	}
	if !strings.Contains(out, "other.host ssh-rsa KEEP") {
		t.Errorf("unrelated host should be preserved:\n%s", out)
	}
}
