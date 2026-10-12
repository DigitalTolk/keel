package cli

import (
	"bytes"
	"errors"
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
	stdins  []string
	closed  bool
	execErr error
	respond func(cmd string) error // optional: per-command result, overrides execErr
}

func (s *fakeSession) Exec(cmd, stdin string) (string, error) {
	s.cmds = append(s.cmds, cmd)
	s.stdins = append(s.stdins, stdin)
	if s.respond != nil {
		return "", s.respond(cmd)
	}
	return "", s.execErr
}

// notASudoer fakes the reported host: the user is not in the sudoers file,
// while su (given the right root password) works.
func notASudoer(rootPassword string, sess *fakeSession) func(cmd string) error {
	return func(cmd string) error {
		switch {
		case strings.Contains(cmd, "sudo"):
			return errors.New("Process exited with status 1: customer is not in the sudoers file.")
		case strings.HasPrefix(cmd, "su "):
			if sess.stdins[len(sess.stdins)-1] != rootPassword+"\n" {
				return errors.New("Process exited with status 1")
			}
		}
		return nil
	}
}

// suStdin returns what the su commands were given on stdin.
func suStdin(sess *fakeSession) []string {
	var in []string
	for i, cmd := range sess.cmds {
		if strings.HasPrefix(cmd, "su ") {
			in = append(in, sess.stdins[i])
		}
	}
	return in
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

// TestBootstrapFallsBackToSuWithoutLeakingThePassword reproduces the reported
// run: a user who is not in the sudoers file, with a password. keel reaches
// root through su using the same password, and the password is only ever
// written to stdin, never onto a command line.
func TestBootstrapFallsBackToSuWithoutLeakingThePassword(t *testing.T) {
	const secret = "Edzfvbsvo4el!"
	t.Setenv("KEEL_SSH_PASSWORD", secret)
	a, _ := newTestApp()
	sess := &fakeSession{}
	sess.respond = notASudoer(secret, sess)
	a.dialer = func(ssh.Target, ssh.DialOptions) (SSHSession, error) { return sess, nil }

	if err := runCmd(a, "bootstrap", "-u", "customer", "--pubkey", "ssh-ed25519 K u@h", "web1"); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	for _, cmd := range sess.cmds {
		if strings.Contains(cmd, secret) {
			t.Errorf("password leaked onto command line %q", cmd)
		}
	}
	if in := suStdin(sess); len(in) < 2 || in[0] != secret+"\n" {
		t.Errorf("su should be given the ssh password on stdin, got %q", in)
	}
}

func TestBootstrapAskRootPass(t *testing.T) {
	t.Setenv("KEEL_SSH_PASSWORD", "userpw")
	a, _ := newTestApp()
	sess := &fakeSession{}
	sess.respond = notASudoer("rootpw", sess)
	a.dialer = func(ssh.Target, ssh.DialOptions) (SSHSession, error) { return sess, nil }
	var prompts []string
	a.readPassword = func(prompt string) (string, error) { prompts = append(prompts, prompt); return "rootpw", nil }

	if err := runCmd(a, "bootstrap", "-u", "customer", "--ask-root-pass", "web1"); err != nil {
		t.Fatalf("bootstrap --ask-root-pass: %v", err)
	}
	if len(prompts) != 1 || !strings.Contains(prompts[0], "root password") {
		t.Errorf("--ask-root-pass should prompt for the root password once, prompts: %q", prompts)
	}
	if in := suStdin(sess); len(in) == 0 || in[0] != "rootpw\n" {
		t.Errorf("su should be given the root password, got %q", in)
	}
}

func TestBootstrapAskRootPassError(t *testing.T) {
	a, _ := newTestApp()
	a.dialer = func(ssh.Target, ssh.DialOptions) (SSHSession, error) { return &fakeSession{}, nil }
	a.readPassword = func(string) (string, error) { return "", io.ErrUnexpectedEOF }

	if err := runCmd(a, "bootstrap", "-u", "customer", "--ask-root-pass", "web1"); err == nil {
		t.Fatal("a failed root password prompt should stop the run")
	}
}

func TestBootstrapRootPasswordFromEnv(t *testing.T) {
	t.Setenv("KEEL_SSH_PASSWORD", "userpw")
	t.Setenv("KEEL_ROOT_PASSWORD", "rootpw")
	a, _ := newTestApp()
	sess := &fakeSession{}
	sess.respond = notASudoer("rootpw", sess)
	a.dialer = func(ssh.Target, ssh.DialOptions) (SSHSession, error) { return sess, nil }

	if err := runCmd(a, "bootstrap", "-u", "customer", "web1"); err != nil {
		t.Fatalf("bootstrap with KEEL_ROOT_PASSWORD: %v", err)
	}
	if in := suStdin(sess); len(in) == 0 || in[0] != "rootpw\n" {
		t.Errorf("su should be given KEEL_ROOT_PASSWORD, got %q", in)
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
