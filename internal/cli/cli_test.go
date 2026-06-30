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
	a.interactive = func() bool { return false } // deterministic: non-interactive in tests
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

	err := runCmd(a, "bootstrap", "-u", "root", "--pubkey", "ssh-ed25519 KEY user@host", "web1")
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
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
	err := runCmd(a, "bootstrap", "-u", "root", "web1")
	if err == nil {
		t.Fatal("dial failure should propagate")
	}
}

// TestBootstrapInteractiveSeedsFromArgs verifies that in a terminal the guided
// TUI runs, pre-filled with any HOST args (and flags) passed on the command line.
func TestBootstrapInteractiveSeedsFromArgs(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // no ~/.ssh/config interference
	a, _ := newTestApp()
	a.interactive = func() bool { return true }
	var seed bootstrapParams
	called := false
	a.tui = func(_ *app, p bootstrapParams) error { called = true; seed = p; return nil }

	if err := runCmd(a, "bootstrap", "--user", "root", "--pubkey", "ssh-ed25519 K u@h", "1.2.3.4"); err != nil {
		t.Fatalf("interactive bootstrap: %v", err)
	}
	if !called {
		t.Error("interactive bootstrap should open the TUI")
	}
	if len(seed.hosts) != 1 || seed.hosts[0] != "1.2.3.4" {
		t.Errorf("TUI should be seeded with the host arg, got %v", seed.hosts)
	}
	if seed.user != "root" || !contains(seed.keys, "ssh-ed25519 K u@h") {
		t.Errorf("TUI should be seeded with flags, got %+v", seed)
	}
}

// TestBootstrapInteractiveTUIError surfaces an error returned by the TUI seam.
func TestBootstrapInteractiveTUIError(t *testing.T) {
	a, _ := newTestApp()
	a.interactive = func() bool { return true }
	a.tui = func(_ *app, _ bootstrapParams) error { return io.ErrUnexpectedEOF }
	if err := runCmd(a, "bootstrap", "web1"); err == nil {
		t.Fatal("tui error should propagate")
	}
}

// TestBootstrapNoArgsNonInteractive: piped/no-TTY with no host must error, not hang.
func TestBootstrapNoArgsNonInteractive(t *testing.T) {
	a, _ := newTestApp() // interactive() forced false by the harness
	if err := runCmd(a, "bootstrap"); err == nil {
		t.Fatal("bootstrap with no args and no TTY should error")
	}
}

// TestResolveTargetMergesSSHConfig checks the ssh_config + flag + default
// precedence used at dial time.
func TestResolveTargetMergesSSHConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	sshDir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		t.Fatal(err)
	}
	cfg := "Host web1\n  HostName 10.0.0.5\n  User deploy\n  Port 2222\n  ProxyJump bastion@jump:2200\n"
	if err := os.WriteFile(filepath.Join(sshDir, "config"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	a, _ := newTestApp()

	// No explicit params -> ssh_config fills in HostName/User/Port/ProxyJump.
	tgt, opts := a.resolveTarget("web1", bootstrapParams{})
	if tgt.Host != "10.0.0.5" || tgt.User != "deploy" || tgt.Port != 2222 {
		t.Errorf("ssh_config not applied: %+v", tgt)
	}
	if opts.JumpHost != "bastion@jump#2200" {
		t.Errorf("ProxyJump = %q, want bastion@jump#2200", opts.JumpHost)
	}

	// Explicit flags/TUI values win over ssh_config.
	tgt2, _ := a.resolveTarget("web1", bootstrapParams{user: "root", port: 22})
	if tgt2.User != "root" || tgt2.Port != 22 {
		t.Errorf("explicit values should override ssh_config: %+v", tgt2)
	}

	// Unknown alias -> used as-is, with keel's own defaults.
	tgt3, _ := a.resolveTarget("rawhost", bootstrapParams{})
	if tgt3.Host != "rawhost" || tgt3.User != "bofh" || tgt3.Port != 22 {
		t.Errorf("unknown alias should fall back to defaults: %+v", tgt3)
	}
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
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
