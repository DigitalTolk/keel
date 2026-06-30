package ssh

import (
	"strings"
	"testing"
)

const sampleSSHConfig = `
Host web1
    HostName 192.168.50.39
    User deploy
    Port 2222
    IdentityFile ~/.ssh/web1_ed25519
    ProxyJump bastion@jump.example.com:2200

Host bare
    HostName 10.0.0.9
`

func TestResolveHostFromFull(t *testing.T) {
	hc, err := resolveHostFrom(strings.NewReader(sampleSSHConfig), "web1")
	if err != nil {
		t.Fatalf("resolveHostFrom: %v", err)
	}
	if hc.HostName != "192.168.50.39" {
		t.Errorf("HostName = %q", hc.HostName)
	}
	if hc.User != "deploy" {
		t.Errorf("User = %q", hc.User)
	}
	if hc.Port != 2222 {
		t.Errorf("Port = %d", hc.Port)
	}
	if len(hc.IdentityFiles) != 1 || !strings.HasSuffix(hc.IdentityFiles[0], "/.ssh/web1_ed25519") {
		t.Errorf("IdentityFiles = %v (should expand ~)", hc.IdentityFiles)
	}
	if hc.ProxyJump != "bastion@jump.example.com#2200" {
		t.Errorf("ProxyJump = %q, want bastion@jump.example.com#2200", hc.ProxyJump)
	}
}

// TestResolveHostFromUnsetFieldsStayZero verifies no implicit ssh defaults leak
// in: a host with only HostName must leave User/Port/jump empty so keel's own
// defaults apply.
func TestResolveHostFromUnsetFieldsStayZero(t *testing.T) {
	hc, err := resolveHostFrom(strings.NewReader(sampleSSHConfig), "bare")
	if err != nil {
		t.Fatalf("resolveHostFrom: %v", err)
	}
	if hc.HostName != "10.0.0.9" {
		t.Errorf("HostName = %q", hc.HostName)
	}
	if hc.User != "" {
		t.Errorf("User should be empty (no implicit default), got %q", hc.User)
	}
	if hc.Port != 0 {
		t.Errorf("Port should be 0 (no implicit default), got %d", hc.Port)
	}
	if hc.ProxyJump != "" || len(hc.IdentityFiles) != 0 {
		t.Errorf("unset jump/identity should be empty, got jump=%q ids=%v", hc.ProxyJump, hc.IdentityFiles)
	}
}

func TestResolveHostFromUnknownAlias(t *testing.T) {
	hc, _ := resolveHostFrom(strings.NewReader(sampleSSHConfig), "nope")
	if hc.HostName != "" || hc.User != "" || hc.Port != 0 {
		t.Errorf("unknown alias should yield zero config, got %+v", hc)
	}
}

func TestProxyJumpToTarget(t *testing.T) {
	cases := map[string]string{
		"jump.example.com": "jump.example.com",
		"user@jump":        "user@jump",
		"user@jump:2200":   "user@jump#2200",
		"a@h1:22,b@h2:33":  "a@h1#22", // first hop only
		"none":             "",
		"":                 "",
	}
	for in, want := range cases {
		if got := proxyJumpToTarget(in); got != want {
			t.Errorf("proxyJumpToTarget(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestExpandHome(t *testing.T) {
	if got := expandHome("/abs/path"); got != "/abs/path" {
		t.Errorf("absolute path should be unchanged, got %q", got)
	}
	got := expandHome("~/x")
	if strings.HasPrefix(got, "~") || !strings.HasSuffix(got, "/x") {
		t.Errorf("~ should expand, got %q", got)
	}
}
