package cli

import (
	"context"
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

	"github.com/DigitalTolk/keel/internal/config"
	"github.com/DigitalTolk/keel/internal/runner"
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
	if err := runCmd(newApp(), "bootstrap", "known-hosts", "-p", strconv.Itoa(port), host); err != nil {
		t.Fatalf("known-hosts (real scan): %v", err)
	}
	kh, err := os.ReadFile(filepath.Join(home, ".ssh", "known_hosts"))
	if err != nil || !strings.Contains(string(kh), "ssh-ed25519") {
		t.Fatalf("known_hosts not written by real scan: %q err=%v", kh, err)
	}

	// bootstrap run uses the real ssh.Dial seam + real provisioning over SSH.
	inv := filepath.Join(home, "inventory")
	err = runCmd(newApp(), "bootstrap", "run", "-u", "root", "-p", strconv.Itoa(port),
		"--pubkey", "ssh-ed25519 K u@h", "--inventory", inv, host)
	if err != nil {
		t.Fatalf("bootstrap run (real dial): %v", err)
	}
	if data, _ := os.ReadFile(inv); !strings.Contains(string(data), "ansible_user=bofh") {
		t.Fatalf("inventory not written: %q", data)
	}
}

// --- backup run mysql job + unsupported type ---------------------------------

func TestBackupRunMySQLJob(t *testing.T) {
	a, _ := newTestApp()
	noTools(a)
	a.runnerFactory = func() runner.Runner {
		return &fakeRunner{responder: func(string, []string) (string, error) { return "-- DUMP\n", nil }}
	}
	base := t.TempDir()
	t.Setenv("DBPW", "pw")
	a.cfg.Backup.Jobs = map[string]config.JobConfig{
		"db": {
			Type:       "mysql",
			FilePrefix: "app",
			MySQL:      config.MySQLJob{Host: "h", DB: "app", PasswordEnv: "DBPW"},
			Dest:       config.DestConfig{Kind: "local", BaseDir: base, Prefix: "p/"},
			Retention:  config.RetentionConfig{Keep: 3},
		},
	}
	if err := runCmd(a, "backup", "run", "db"); err != nil {
		t.Fatalf("backup run db: %v", err)
	}
	if _, err := os.Stat(filepath.Join(base, "p", "app-2026-06-19.tar.gz")); err != nil {
		t.Fatalf("expected mysql job archive: %v", err)
	}
}

func TestBackupRunMySQLPasswordFileError(t *testing.T) {
	a, _ := newTestApp()
	noTools(a)
	a.runnerFactory = func() runner.Runner { return &fakeRunner{} }
	a.cfg.Backup.Jobs = map[string]config.JobConfig{
		"db": {
			Type:  "mysql",
			MySQL: config.MySQLJob{Host: "h", DB: "app", PasswordFile: filepath.Join(t.TempDir(), "missing")},
			Dest:  config.DestConfig{Kind: "local", BaseDir: t.TempDir()},
		},
	}
	if err := runCmd(a, "backup", "run", "db"); err == nil {
		t.Fatal("missing password file should error")
	}
}

func TestBackupMySQLPasswordEnvFlag(t *testing.T) {
	t.Setenv("CUSTOM_DB_PW", "pw")
	a, _ := newTestApp()
	noTools(a)
	a.runnerFactory = func() runner.Runner {
		return &fakeRunner{responder: func(string, []string) (string, error) { return "-- DUMP\n", nil }}
	}
	base := t.TempDir()
	if err := runCmd(a, "backup", "mysql", "--host", "h", "--db", "app",
		"--password-env", "CUSTOM_DB_PW", "--dest", "local", "--base-dir", base, "--file-prefix", "app"); err != nil {
		t.Fatalf("backup mysql --password-env: %v", err)
	}
}

func TestBackupRunUnsupportedType(t *testing.T) {
	a, _ := newTestApp()
	a.cfg.Backup.Jobs = map[string]config.JobConfig{
		"weird": {Type: "carrierpigeon", Dest: config.DestConfig{Kind: "local", BaseDir: t.TempDir()}},
	}
	if err := runCmd(a, "backup", "run", "weird"); err == nil {
		t.Fatal("unsupported job type should error")
	}
}

func TestBackupRunBadDest(t *testing.T) {
	a, _ := newTestApp()
	a.cfg.Backup.Jobs = map[string]config.JobConfig{
		"x": {Type: "jenkins", Dest: config.DestConfig{Kind: "ftp"}},
	}
	if err := runCmd(a, "backup", "run", "x"); err == nil {
		t.Fatal("bad dest kind should error")
	}
}

// --- error paths -------------------------------------------------------------

func TestRunKnownHostsScanError(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	a, _ := newTestApp()
	a.scanHostKey = func(string, int, time.Duration) (string, error) {
		return "", errScan
	}
	if err := runCmd(a, "bootstrap", "known-hosts", "badhost"); err == nil {
		t.Fatal("scan error should propagate")
	}
}

func TestRunKnownHostsHomeError(t *testing.T) {
	t.Setenv("HOME", "")
	a, _ := newTestApp()
	a.scanHostKey = func(string, int, time.Duration) (string, error) { return "x", nil }
	if err := runCmd(a, "bootstrap", "known-hosts", "h"); err == nil {
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

func TestBackupJenkinsBadDest(t *testing.T) {
	a, _ := newTestApp()
	if err := runCmd(a, "backup", "jenkins", "--home", t.TempDir(), "--dest", "ftp"); err == nil {
		t.Fatal("bad dest should error")
	}
}

// --- rsync / sftp ------------------------------------------------------------

func TestRunSyncThenArchiveHappy(t *testing.T) {
	a, _ := newTestApp()
	a.runnerFactory = func() runner.Runner { return &fakeRunner{} } // sync is a no-op
	work := t.TempDir()
	if err := os.WriteFile(filepath.Join(work, "data"), []byte("synced"), 0o644); err != nil {
		t.Fatal(err)
	}
	base := t.TempDir()
	// "sh" exists everywhere, so the RequireTools check passes deterministically.
	err := a.runSyncThenArchive(context.Background(), "sh", []string{"-c", "true"}, work,
		destFlags{kind: "local", baseDir: base}, "bak", 0)
	if err != nil {
		t.Fatalf("runSyncThenArchive: %v", err)
	}
	if _, err := os.Stat(filepath.Join(base, "bak-2026-06-19.tar.gz")); err != nil {
		t.Fatalf("expected archive: %v", err)
	}
}

func TestRunSyncThenArchiveMissingTool(t *testing.T) {
	a, _ := newTestApp()
	err := a.runSyncThenArchive(context.Background(), "definitely-missing-tool-xyz", nil,
		t.TempDir(), destFlags{kind: "local", baseDir: t.TempDir()}, "bak", 0)
	if err == nil {
		t.Fatal("missing sync tool should error")
	}
}

func TestRunSyncThenArchiveSyncFails(t *testing.T) {
	a, _ := newTestApp()
	a.runnerFactory = func() runner.Runner {
		return &fakeRunner{responder: func(string, []string) (string, error) { return "", errScan }}
	}
	err := a.runSyncThenArchive(context.Background(), "sh", []string{"-c", "true"},
		t.TempDir(), destFlags{kind: "local", baseDir: t.TempDir()}, "bak", 0)
	if err == nil {
		t.Fatal("sync failure should propagate")
	}
}

func TestRunSyncThenArchiveBadDest(t *testing.T) {
	a, _ := newTestApp()
	a.runnerFactory = func() runner.Runner { return &fakeRunner{} }
	err := a.runSyncThenArchive(context.Background(), "sh", []string{"-c", "true"},
		t.TempDir(), destFlags{kind: "ftp"}, "bak", 0)
	if err == nil {
		t.Fatal("bad dest kind should error")
	}
}

func TestBackupRsyncRequiredFlags(t *testing.T) {
	a, _ := newTestApp()
	if err := runCmd(a, "backup", "rsync"); err == nil {
		t.Fatal("rsync without source flags should error")
	}
}

func TestBackupSftpRequiredFlags(t *testing.T) {
	a, _ := newTestApp()
	if err := runCmd(a, "backup", "sftp"); err == nil {
		t.Fatal("sftp without source flags should error")
	}
}

func TestBackupRsyncRunsBody(t *testing.T) {
	a, _ := newTestApp()
	noTools(a)
	a.runnerFactory = func() runner.Runner { return &fakeRunner{} }
	work := t.TempDir()
	_ = os.WriteFile(filepath.Join(work, "f"), []byte("x"), 0o644)
	base := t.TempDir()
	if err := runCmd(a, "backup", "rsync", "--source-host", "h", "--source-user", "u",
		"--source-path", "/p", "--work-dir", work, "--dest", "local", "--base-dir", base); err != nil {
		t.Fatalf("backup rsync: %v", err)
	}
	if _, err := os.Stat(filepath.Join(base, "bak-2026-06-19.tar.gz")); err != nil {
		t.Fatalf("expected archive: %v", err)
	}
}

func TestBackupSftpRunsBody(t *testing.T) {
	a, _ := newTestApp()
	noTools(a)
	a.runnerFactory = func() runner.Runner { return &fakeRunner{} }
	work := t.TempDir()
	_ = os.WriteFile(filepath.Join(work, "f"), []byte("x"), 0o644)
	base := t.TempDir()
	if err := runCmd(a, "backup", "sftp", "--source-host", "h", "--source-user", "u",
		"--source-path", "/p", "--work-dir", work, "--dest", "local", "--base-dir", base, "--parallel", "4"); err != nil {
		t.Fatalf("backup sftp: %v", err)
	}
	if _, err := os.Stat(filepath.Join(base, "bak-2026-06-19.tar.gz")); err != nil {
		t.Fatalf("expected archive: %v", err)
	}
}

func TestBackupPurgeBadDest(t *testing.T) {
	a, _ := newTestApp()
	if err := runCmd(a, "backup", "purge", "--dest", "ftp", "--prefix", "p/"); err == nil {
		t.Fatal("bad dest should error")
	}
}

func TestExecuteErrorReturnsOne(t *testing.T) {
	old := os.Args
	os.Args = []string{"keel", "bootstrap", "run"} // missing required HOST arg
	defer func() { os.Args = old }()
	if code := Execute(); code != 1 {
		t.Fatalf("Execute(invalid) = %d, want 1", code)
	}
}

// TestRealAppBackupJenkins drives a real newApp() (real clock + config load +
// logger) so the default seam closures are exercised.
func TestRealAppBackupJenkins(t *testing.T) {
	home := t.TempDir()
	_ = os.WriteFile(filepath.Join(home, "config.xml"), []byte("<x/>"), 0o644)
	base := t.TempDir()
	if err := runCmd(newApp(), "backup", "jenkins", "--home", home, "--dest", "local", "--base-dir", base); err != nil {
		t.Fatalf("real-app backup jenkins: %v", err)
	}
	files, _ := filepath.Glob(filepath.Join(base, "*.tar.gz"))
	if len(files) != 1 {
		t.Fatalf("expected 1 archive, got %v", files)
	}
}

// TestRealAppMySQLConnectFailure exercises the real runnerFactory (runner.Exec)
// and the mysqldump error path against an unreachable server.
func TestRealAppMySQLConnectFailure(t *testing.T) {
	if err := runner.RequireTools("mysqldump"); err != nil {
		t.Skip("mysqldump not installed")
	}
	base := t.TempDir()
	err := runCmd(newApp(), "backup", "mysql", "--host", "127.0.0.1", "--port", "1",
		"--db", "x", "--dest", "local", "--base-dir", base)
	if err == nil {
		t.Fatal("expected mysqldump connection failure")
	}
}

func TestBootstrapRunProvisionError(t *testing.T) {
	a, _ := newTestApp()
	a.dialer = func(ssh.Target, ssh.DialOptions) (SSHSession, error) {
		return &fakeSession{execErr: errScan}, nil
	}
	err := runCmd(a, "bootstrap", "run", "-u", "root",
		"--inventory", filepath.Join(t.TempDir(), "inv"), "web1")
	if err == nil {
		t.Fatal("provision failure should propagate")
	}
}

func TestConfigLoadErrorPropagates(t *testing.T) {
	// An explicit, non-existent --config must fail in PersistentPreRunE.
	err := runCmd(newApp(), "--config", filepath.Join(t.TempDir(), "missing.yaml"),
		"backup", "purge", "--dest", "local", "--base-dir", t.TempDir(), "--prefix", "p/")
	if err == nil {
		t.Fatal("missing --config should error")
	}
}

func TestLogFormatOverride(t *testing.T) {
	home := t.TempDir()
	_ = os.WriteFile(filepath.Join(home, "config.xml"), []byte("<x/>"), 0o644)
	// --log-format exercises the override branch in buildRoot's pre-run.
	if err := runCmd(newApp(), "--log-format", "json",
		"backup", "jenkins", "--home", home, "--dest", "local", "--base-dir", t.TempDir()); err != nil {
		t.Fatalf("log-format override run: %v", err)
	}
}

func TestBootstrapRunPubkeyFileError(t *testing.T) {
	a, _ := newTestApp()
	a.dialer = func(ssh.Target, ssh.DialOptions) (SSHSession, error) { return &fakeSession{}, nil }
	err := runCmd(a, "bootstrap", "run", "-u", "root",
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
	if err := runCmd(a, "bootstrap", "run", "-u", "deploy", "--ask-pass", "--inventory", inv, "web1"); err != nil {
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

func TestBuildDestinationLocalDefaultBase(t *testing.T) {
	a, _ := newTestApp()
	d, err := a.buildDestination(context.Background(), config.DestConfig{Kind: "local"}) // empty BaseDir -> "."
	if err != nil || d == nil {
		t.Fatalf("local default base: %v", err)
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
	if err := runCmd(a, "bootstrap", "known-hosts", "h"); err == nil {
		t.Fatal("expected error when known_hosts is unreadable")
	}
}

func TestListDatabasesStreamError(t *testing.T) {
	a, _ := newTestApp()
	noTools(a)
	a.runnerFactory = func() runner.Runner {
		return &fakeRunner{responder: func(string, []string) (string, error) { return "", errScan }}
	}
	err := runCmd(a, "backup", "mysql", "--host", "h", "--all-databases", "--dest", "local", "--base-dir", t.TempDir())
	if err == nil {
		t.Fatal("listDatabases error should propagate")
	}
}
