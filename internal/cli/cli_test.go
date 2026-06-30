package cli

import (
	"bytes"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DigitalTolk/keel/internal/config"
	"github.com/DigitalTolk/keel/internal/ssh"
)

// --- fakes -------------------------------------------------------------------

type fakeSession struct {
	cmds    []string
	closed  bool
	execErr error
}

func (s *fakeSession) Exec(cmd string) (string, error) {
	s.cmds = append(s.cmds, cmd)
	return "", s.execErr
}
func (s *fakeSession) Close() error { s.closed = true; return nil }

// --- harness -----------------------------------------------------------------

func newTestApp() (*app, *bytes.Buffer) {
	var buf bytes.Buffer
	a := newApp()
	a.cfg = config.Default()
	a.log = slog.New(slog.NewTextHandler(&buf, nil))
	return a, &buf
}

func runCmd(a *app, args ...string) error {
	root := buildRoot(a)
	root.SetArgs(args)
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	return root.Execute()
}

// --- bootstrap known-hosts (fake scanner) ------------------------------------

func TestBootstrapKnownHosts(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	a, _ := newTestApp()
	a.scanHostKey = func(host string, port int, _ time.Duration) (string, error) {
		return "[" + host + "]:22 ssh-ed25519 SCANNEDKEY", nil
	}
	if err := runCmd(a, "known-hosts", "-p", "22", "web1"); err != nil {
		t.Fatalf("known-hosts: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(home, ".ssh", "known_hosts"))
	if err != nil || !strings.Contains(string(data), "SCANNEDKEY") {
		t.Fatalf("known_hosts not written correctly: %q err=%v", data, err)
	}
}

// --- bootstrap run (fake dialer) ---------------------------------------------

func TestBootstrapRun(t *testing.T) {
	a, _ := newTestApp()
	sess := &fakeSession{}
	a.dialer = func(ssh.Target, ssh.DialOptions) (SSHSession, error) { return sess, nil }

	inv := filepath.Join(t.TempDir(), "inventory")
	err := runCmd(a, "bootstrap", "-u", "root",
		"--pubkey", "ssh-ed25519 KEY user@host", "--inventory", inv, "web1")
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	data, err := os.ReadFile(inv)
	if err != nil || !strings.Contains(string(data), "[web1]:22 ansible_user=bofh") {
		t.Fatalf("inventory wrong: %q err=%v", data, err)
	}
	if !sess.closed {
		t.Error("ssh session should be closed")
	}
	// Privileged commands are base64-wrapped for transport; the admin-user
	// check runs unwrapped. Assert both the check ran and several steps issued.
	joined := strings.Join(sess.cmds, "\n")
	if !strings.Contains(joined, "id -u bofh") {
		t.Errorf("expected admin-user check, got: %v", sess.cmds)
	}
	if len(sess.cmds) < 4 {
		t.Errorf("expected multiple provisioning steps, got %d: %v", len(sess.cmds), sess.cmds)
	}
}

func TestBootstrapRunDialFailure(t *testing.T) {
	a, _ := newTestApp()
	a.dialer = func(ssh.Target, ssh.DialOptions) (SSHSession, error) {
		return nil, io.ErrUnexpectedEOF
	}
	err := runCmd(a, "bootstrap", "-u", "root", "--inventory", filepath.Join(t.TempDir(), "inv"), "web1")
	if err == nil {
		t.Fatal("dial failure should propagate")
	}
}

func TestCollectPubkeysFromFile(t *testing.T) {
	dir := t.TempDir()
	pf := filepath.Join(dir, "keys")
	_ = os.WriteFile(pf, []byte("key-one\n\nkey-two\n"), 0o600)
	keys, err := collectPubkeys([]string{"flagkey"}, pf)
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 3 || keys[0] != "flagkey" {
		t.Fatalf("collectPubkeys = %v, want flagkey + 2 from file", keys)
	}
}

func TestExecuteReturnsExitCode(t *testing.T) {
	// A valid help invocation exits 0 via the real Execute path.
	old := os.Args
	os.Args = []string{"keel", "--help"}
	defer func() { os.Args = old }()
	if code := Execute(); code != 0 {
		t.Fatalf("Execute(--help) = %d, want 0", code)
	}
}
