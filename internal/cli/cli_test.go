package cli

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DigitalTolk/keel/internal/config"
	"github.com/DigitalTolk/keel/internal/log"
	"github.com/DigitalTolk/keel/internal/runner"
	"github.com/DigitalTolk/keel/internal/ssh"
)

// --- fakes -------------------------------------------------------------------

type fakeRunner struct {
	cmds []string
	// responder returns the output for a given command name + args.
	responder func(name string, args []string) (string, error)
}

func (f *fakeRunner) Stream(ctx context.Context, w io.Writer, name string, args ...string) error {
	f.cmds = append(f.cmds, name+" "+strings.Join(args, " "))
	if f.responder != nil {
		out, err := f.responder(name, args)
		if err != nil {
			return err
		}
		_, _ = io.WriteString(w, out)
	}
	return nil
}

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
	a.log = log.New(
		log.WithWriters(&buf, &buf),
		log.WithColor(false),
		log.WithTimestamps(false),
		log.WithProgram("keel"),
	)
	a.now = func() time.Time { return time.Date(2026, 6, 19, 0, 0, 0, 0, time.UTC) }
	return a, &buf
}

func runCmd(a *app, args ...string) error {
	root := buildRoot(a)
	root.SetArgs(args)
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	return root.Execute()
}

// noTools makes a.requireTools a no-op so command happy-paths run in tests
// regardless of which external tools are installed.
func noTools(a *app) { a.requireTools = func(...string) error { return nil } }

// --- pure helpers ------------------------------------------------------------

func TestFirstNonEmpty(t *testing.T) {
	if got := firstNonEmpty("", "", "x", "y"); got != "x" {
		t.Errorf("firstNonEmpty = %q, want x", got)
	}
	if got := firstNonEmpty("", ""); got != "" {
		t.Errorf("firstNonEmpty(empties) = %q, want empty", got)
	}
}

func TestAtoiOr(t *testing.T) {
	if got := atoiOr("42", 7); got != 42 {
		t.Errorf("atoiOr(42) = %d", got)
	}
	if got := atoiOr("", 7); got != 7 {
		t.Errorf("atoiOr(empty) = %d, want default 7", got)
	}
	if got := atoiOr("nope", 7); got != 7 {
		t.Errorf("atoiOr(invalid) = %d, want default 7", got)
	}
}

func TestResolveMySQLPassword(t *testing.T) {
	t.Setenv("CUSTOM_PW", "fromcustom")
	if got := resolveMySQLPassword("CUSTOM_PW"); got != "fromcustom" {
		t.Errorf("password-env path = %q", got)
	}
	t.Setenv("MYSQL_PWD", "frompwd")
	if got := resolveMySQLPassword(""); got != "frompwd" {
		t.Errorf("MYSQL_PWD fallback = %q", got)
	}
}

func TestResolveJobPassword(t *testing.T) {
	t.Setenv("JOB_PW", "secret")
	if got, _ := resolveJobPassword(config.MySQLJob{PasswordEnv: "JOB_PW"}); got != "secret" {
		t.Errorf("env job password = %q", got)
	}

	dir := t.TempDir()
	pf := filepath.Join(dir, "pw")
	_ = os.WriteFile(pf, []byte("filepw\n"), 0o600)
	if got, _ := resolveJobPassword(config.MySQLJob{PasswordFile: pf}); got != "filepw" {
		t.Errorf("file job password = %q (should be trimmed)", got)
	}

	if _, err := resolveJobPassword(config.MySQLJob{PasswordFile: filepath.Join(dir, "missing")}); err == nil {
		t.Error("missing password file should error")
	}

	if got, _ := resolveJobPassword(config.MySQLJob{}); got != "" {
		t.Errorf("no password source = %q, want empty", got)
	}
}

func TestDestFlagsToConfig(t *testing.T) {
	df := destFlags{kind: "s3", bucket: "b", prefix: "p/", region: "r"}
	dc := df.toConfig()
	if dc.Kind != "s3" || dc.Bucket != "b" || dc.Prefix != "p/" || dc.Region != "r" {
		t.Errorf("toConfig wrong: %+v", dc)
	}
}

// --- buildDestination --------------------------------------------------------

func TestBuildDestinationLocal(t *testing.T) {
	a, _ := newTestApp()
	d, err := a.buildDestination(context.Background(), config.DestConfig{Kind: "local", BaseDir: t.TempDir()})
	if err != nil || d == nil {
		t.Fatalf("local dest: %v", err)
	}
}

func TestBuildDestinationUnknownKind(t *testing.T) {
	a, _ := newTestApp()
	if _, err := a.buildDestination(context.Background(), config.DestConfig{Kind: "ftp"}); err == nil {
		t.Fatal("unknown dest kind should error")
	}
}

func TestBuildDestinationS3(t *testing.T) {
	a, _ := newTestApp()
	d, err := a.buildDestination(context.Background(), config.DestConfig{Kind: "s3", Bucket: "b", Region: "us-east-1"})
	if err != nil || d == nil {
		t.Fatalf("s3 dest construct: %v", err)
	}
}

func TestBuildDestinationB2MapsEnvCreds(t *testing.T) {
	t.Setenv("B2_APPLICATION_KEY_ID", "kid")
	t.Setenv("B2_APPLICATION_KEY", "kkey")
	a, _ := newTestApp()
	d, err := a.buildDestination(context.Background(), config.DestConfig{Kind: "b2", Bucket: "b", Endpoint: "https://example", Region: "r"})
	if err != nil || d == nil {
		t.Fatalf("b2 dest construct: %v", err)
	}
}

// --- backup jenkins (no external tools) --------------------------------------

func TestBackupJenkinsLocal(t *testing.T) {
	a, _ := newTestApp()
	home := t.TempDir()
	_ = os.WriteFile(filepath.Join(home, "config.xml"), []byte("<jenkins/>"), 0o644)
	base := t.TempDir()

	err := runCmd(a, "backup", "jenkins", "--home", home, "--dest", "local", "--base-dir", base, "--keep", "3")
	if err != nil {
		t.Fatalf("backup jenkins: %v", err)
	}
	if _, err := os.Stat(filepath.Join(base, "jenkins-2026-06-19.tar.gz")); err != nil {
		t.Fatalf("expected jenkins archive: %v", err)
	}
}

// --- backup purge (no external tools) ----------------------------------------

func TestBackupPurgeLocal(t *testing.T) {
	a, _ := newTestApp()
	base := t.TempDir()
	// seed 3 files under prefix p/
	for _, n := range []string{"a", "b", "c"} {
		p := filepath.Join(base, "p", n+".tar.gz")
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		_ = os.WriteFile(p, []byte("x"), 0o644)
	}
	if err := runCmd(a, "backup", "purge", "--dest", "local", "--base-dir", base, "--prefix", "p/", "--keep", "1"); err != nil {
		t.Fatalf("backup purge: %v", err)
	}
	left, _ := filepath.Glob(filepath.Join(base, "p", "*.tar.gz"))
	if len(left) != 1 {
		t.Fatalf("purge kept %d files, want 1", len(left))
	}
}

// --- backup mysql (fake runner) ----------------------------------------------

func TestBackupMySQLLocalWithFakeRunner(t *testing.T) {
	a, _ := newTestApp()
	noTools(a)
	a.runnerFactory = func() runner.Runner {
		return &fakeRunner{responder: func(name string, args []string) (string, error) {
			return "-- DUMP\nINSERT INTO t VALUES (1);\n", nil
		}}
	}
	base := t.TempDir()
	err := runCmd(a, "backup", "mysql", "--host", "db", "--db", "app",
		"--dest", "local", "--base-dir", base, "--file-prefix", "app", "--keep", "3")
	if err != nil {
		t.Fatalf("backup mysql: %v", err)
	}
	if _, err := os.Stat(filepath.Join(base, "app-2026-06-19.tar.gz")); err != nil {
		t.Fatalf("expected mysql archive: %v", err)
	}
}

func TestBackupMySQLRequiresHost(t *testing.T) {
	a, _ := newTestApp()
	noTools(a)
	a.runnerFactory = func() runner.Runner { return &fakeRunner{} }
	err := runCmd(a, "backup", "mysql", "--db", "app", "--dest", "local", "--base-dir", t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "host") {
		t.Fatalf("expected host-required error, got %v", err)
	}
}

func TestBackupMySQLAllDatabases(t *testing.T) {
	a, _ := newTestApp()
	noTools(a)
	a.runnerFactory = func() runner.Runner {
		return &fakeRunner{responder: func(name string, args []string) (string, error) {
			if name == "mysql" {
				return "app\nextradb\nmysql\nsys\n", nil // includes system DBs to skip
			}
			return "-- DUMP\n", nil
		}}
	}
	base := t.TempDir()
	err := runCmd(a, "backup", "mysql", "--host", "db", "--all-databases", "--dest", "local", "--base-dir", base)
	if err != nil {
		t.Fatalf("backup mysql --all-databases: %v", err)
	}
	// app + extradb backed up; mysql/sys skipped.
	for _, db := range []string{"app", "extradb"} {
		if _, err := os.Stat(filepath.Join(base, db, db+"-2026-06-19.tar.gz")); err != nil {
			t.Errorf("expected archive for %s", db)
		}
	}
	if _, err := os.Stat(filepath.Join(base, "mysql")); !os.IsNotExist(err) {
		t.Error("system db 'mysql' should have been skipped")
	}
}

// --- backup run (config jobs) ------------------------------------------------

func TestBackupRunJenkinsJob(t *testing.T) {
	a, _ := newTestApp()
	home := t.TempDir()
	_ = os.WriteFile(filepath.Join(home, "config.xml"), []byte("<x/>"), 0o644)
	base := t.TempDir()
	a.cfg.Backup.Jobs = map[string]config.JobConfig{
		"ci": {
			Type:      "jenkins",
			Jenkins:   config.JenkinsJob{Home: home},
			Dest:      config.DestConfig{Kind: "local", BaseDir: base, Prefix: "jenkins/"},
			Retention: config.RetentionConfig{Keep: 2},
		},
	}
	if err := runCmd(a, "backup", "run", "ci"); err != nil {
		t.Fatalf("backup run ci: %v", err)
	}
	if _, err := os.Stat(filepath.Join(base, "jenkins", "jenkins-2026-06-19.tar.gz")); err != nil {
		t.Fatalf("expected jenkins job archive: %v", err)
	}
}

func TestBackupRunUnknownJob(t *testing.T) {
	a, _ := newTestApp()
	if err := runCmd(a, "backup", "run", "nope"); err == nil {
		t.Fatal("unknown job should error")
	}
}

// --- bootstrap known-hosts (fake scanner) ------------------------------------

func TestBootstrapKnownHosts(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	a, _ := newTestApp()
	a.scanHostKey = func(host string, port int, _ time.Duration) (string, error) {
		return "[" + host + "]:22 ssh-ed25519 SCANNEDKEY", nil
	}
	if err := runCmd(a, "bootstrap", "known-hosts", "-p", "22", "web1"); err != nil {
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
	err := runCmd(a, "bootstrap", "run", "-u", "root",
		"--pubkey", "ssh-ed25519 KEY user@host", "--inventory", inv, "web1")
	if err != nil {
		t.Fatalf("bootstrap run: %v", err)
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
	err := runCmd(a, "bootstrap", "run", "-u", "root", "--inventory", filepath.Join(t.TempDir(), "inv"), "web1")
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
