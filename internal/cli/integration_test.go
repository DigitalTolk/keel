package cli

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	gossh "golang.org/x/crypto/ssh"

	"github.com/DigitalTolk/keel/internal/ssh"
)

var errScan = errors.New("scan failed")

// startMiniSSHServer starts an in-process SSH server accepting any password,
// answering exec requests with empty success. Returns host and port.
func startMiniSSHServer(t *testing.T) (string, int) {
	t.Helper()
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	signer, _ := gossh.NewSignerFromSigner(priv)
	cfg := &gossh.ServerConfig{
		PasswordCallback: func(gossh.ConnMetadata, []byte) (*gossh.Permissions, error) {
			return &gossh.Permissions{}, nil
		},
	}
	cfg.AddHostKey(signer)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go serveMini(conn, cfg)
		}
	}()

	host, portStr, _ := net.SplitHostPort(ln.Addr().String())
	port, _ := strconv.Atoi(portStr)
	return host, port
}

func serveMini(conn net.Conn, cfg *gossh.ServerConfig) {
	sconn, chans, reqs, err := gossh.NewServerConn(conn, cfg)
	if err != nil {
		return
	}
	defer sconn.Close()
	go gossh.DiscardRequests(reqs)
	for newCh := range chans {
		if newCh.ChannelType() != "session" {
			_ = newCh.Reject(gossh.UnknownChannelType, "")
			continue
		}
		ch, chReqs, _ := newCh.Accept()
		go func() {
			defer ch.Close()
			for req := range chReqs {
				if req.WantReply {
					_ = req.Reply(req.Type == "exec", nil)
				}
				if req.Type == "exec" {
					_, _ = ch.SendRequest("exit-status", false, gossh.Marshal(struct{ S uint32 }{0}))
					return
				}
			}
		}()
	}
}

// TestRealSeamsAgainstSSHServer exercises the production seams (real ssh.Dial,
// ScanHostKey, config.Load, log.New) end-to-end against an in-process server.
func TestRealSeamsAgainstSSHServer(t *testing.T) {
	host, port := startMiniSSHServer(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("KEEL_SSH_PASSWORD", "anything")

	// known-hosts uses the real ScanHostKey seam.
	if err := runCmd(newApp(), "known-hosts", "-p", strconv.Itoa(port), host); err != nil {
		t.Fatalf("known-hosts (real scan): %v", err)
	}
	kh, err := os.ReadFile(filepath.Join(home, ".ssh", "known_hosts"))
	if err != nil || !strings.Contains(string(kh), "ssh-ed25519") {
		t.Fatalf("known_hosts not written by real scan: %q err=%v", kh, err)
	}

	// bootstrap uses the real ssh.Dial seam + real provisioning over SSH.
	inv := filepath.Join(home, "inventory")
	err = runCmd(newApp(), "bootstrap", "-u", "root", "-p", strconv.Itoa(port),
		"--pubkey", "ssh-ed25519 K u@h", "--inventory", inv, host)
	if err != nil {
		t.Fatalf("bootstrap run (real dial): %v", err)
	}
	if data, _ := os.ReadFile(inv); !strings.Contains(string(data), "ansible_user=bofh") {
		t.Fatalf("inventory not written: %q", data)
	}
}

// --- error paths -------------------------------------------------------------

func TestRunKnownHostsScanError(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	a, _ := newTestApp()
	a.scanHostKey = func(string, int, time.Duration) (string, error) {
		return "", errScan
	}
	if err := runCmd(a, "known-hosts", "badhost"); err == nil {
		t.Fatal("scan error should propagate")
	}
}

func TestRunKnownHostsHomeError(t *testing.T) {
	t.Setenv("HOME", "")
	a, _ := newTestApp()
	a.scanHostKey = func(string, int, time.Duration) (string, error) { return "x", nil }
	if err := runCmd(a, "known-hosts", "h"); err == nil {
		t.Fatal("empty HOME should fail known_hosts path resolution")
	}
}

func TestCollectPubkeysMissingFile(t *testing.T) {
	if _, err := collectPubkeys(nil, filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Fatal("missing pubkey file should error")
	}
}

func TestWriteFileAtomicError(t *testing.T) {
	// A path whose parent is a file (not a dir) cannot be created.
	dir := t.TempDir()
	notADir := filepath.Join(dir, "file")
	_ = os.WriteFile(notADir, []byte("x"), 0o600)
	if err := writeFileAtomic(filepath.Join(notADir, "sub", "f"), []byte("y"), 0o644); err == nil {
		t.Fatal("writeFileAtomic into non-dir parent should error")
	}
}

func TestExecuteErrorReturnsOne(t *testing.T) {
	old := os.Args
	os.Args = []string{"keel", "bootstrap"} // missing required HOST arg
	defer func() { os.Args = old }()
	if code := Execute(); code != 1 {
		t.Fatalf("Execute(invalid) = %d, want 1", code)
	}
}

func TestBootstrapRunProvisionError(t *testing.T) {
	a, _ := newTestApp()
	a.dialer = func(ssh.Target, ssh.DialOptions) (SSHSession, error) {
		return &fakeSession{execErr: errScan}, nil
	}
	err := runCmd(a, "bootstrap", "-u", "root",
		"--inventory", filepath.Join(t.TempDir(), "inv"), "web1")
	if err == nil {
		t.Fatal("provision failure should propagate")
	}
}

func TestConfigLoadErrorPropagates(t *testing.T) {
	// An explicit, non-existent --config must fail in PersistentPreRunE.
	a, _ := newTestApp()
	a.log = nil // force the real PersistentPreRunE config-load path
	a.dialer = func(ssh.Target, ssh.DialOptions) (SSHSession, error) { return &fakeSession{}, nil }
	err := runCmd(a, "--config", filepath.Join(t.TempDir(), "missing.yaml"),
		"bootstrap", "-u", "root", "--inventory", filepath.Join(t.TempDir(), "inv"), "web1")
	if err == nil {
		t.Fatal("missing --config should error")
	}
}

func TestLogFormatOverride(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	a, _ := newTestApp()
	a.log = nil // force the real PersistentPreRunE so --log-format is applied
	a.scanHostKey = func(host string, port int, _ time.Duration) (string, error) {
		return "[" + host + "]:22 ssh-ed25519 K", nil
	}
	// --log-format exercises the override branch in buildRoot's pre-run.
	if err := runCmd(a, "--log-format", "json", "known-hosts", "web1"); err != nil {
		t.Fatalf("log-format override run: %v", err)
	}
}

func TestBootstrapRunPubkeyFileError(t *testing.T) {
	a, _ := newTestApp()
	a.dialer = func(ssh.Target, ssh.DialOptions) (SSHSession, error) { return &fakeSession{}, nil }
	err := runCmd(a, "bootstrap", "-u", "root",
		"--pubkey-file", filepath.Join(t.TempDir(), "nope"),
		"--inventory", filepath.Join(t.TempDir(), "inv"), "web1")
	if err == nil {
		t.Fatal("missing --pubkey-file should error")
	}
}

func TestBootstrapRunAskPass(t *testing.T) {
	a, _ := newTestApp()
	sess := &fakeSession{}
	a.dialer = func(ssh.Target, ssh.DialOptions) (SSHSession, error) { return sess, nil }
	called := false
	a.readPassword = func(string) (string, error) { called = true; return "secret", nil }

	inv := filepath.Join(t.TempDir(), "inv")
	if err := runCmd(a, "bootstrap", "-u", "deploy", "--ask-pass", "--inventory", inv, "web1"); err != nil {
		t.Fatalf("bootstrap run --ask-pass: %v", err)
	}
	if !called {
		t.Error("--ask-pass should invoke the password prompt seam")
	}
}

func TestWriteFileAtomicRenameError(t *testing.T) {
	// Target path is an existing directory; renaming a file onto it must fail.
	dir := filepath.Join(t.TempDir(), "target")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeFileAtomic(dir, []byte("x"), 0o644); err == nil {
		t.Fatal("writeFileAtomic onto an existing directory should fail")
	}
}

func TestPromptPasswordNonTerminalErrors(t *testing.T) {
	// Point stdin at a pipe (not a terminal); ReadPassword fails fast, no hang.
	old := os.Stdin
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	_ = w.Close()
	os.Stdin = r
	defer func() { os.Stdin = old; _ = r.Close() }()

	if _, err := promptPassword("pw: "); err == nil {
		t.Fatal("expected error reading password from a non-terminal")
	}
}

func TestRunKnownHostsReadError(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	// Make ~/.ssh/known_hosts a directory so os.ReadFile fails (not IsNotExist).
	if err := os.MkdirAll(filepath.Join(home, ".ssh", "known_hosts"), 0o755); err != nil {
		t.Fatal(err)
	}
	a, _ := newTestApp()
	a.scanHostKey = func(string, int, time.Duration) (string, error) {
		return "[h]:22 ssh-ed25519 K", nil
	}
	if err := runCmd(a, "known-hosts", "h"); err == nil {
		t.Fatal("expected error when known_hosts is unreadable")
	}
}
