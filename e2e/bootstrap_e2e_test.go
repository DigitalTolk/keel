//go:build e2e

package e2e

import (
	"net"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	gossh "golang.org/x/crypto/ssh"

	"github.com/DigitalTolk/keel/internal/bootstrap"
	"github.com/DigitalTolk/keel/internal/ssh"
)

// The non-root users test/e2e/Dockerfile creates. Root's own password is
// KEEL_E2E_SSH_PASSWORD.
const (
	sudoUser     = "deploy"
	sudoPassword = "deploypass"
	suUser       = "customer"
	suPassword   = "customerpass"
)

// target returns the e2e server's address, skipping the test when it is unset.
func target(t *testing.T) (string, int) {
	t.Helper()
	addr := os.Getenv("KEEL_E2E_SSH_ADDR")
	if addr == "" {
		t.Skip("set KEEL_E2E_SSH_ADDR (host:port) to run the e2e SSH test")
	}
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("bad KEEL_E2E_SSH_ADDR %q: %v", addr, err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("bad port in KEEL_E2E_SSH_ADDR %q: %v", addr, err)
	}
	return host, port
}

// dial connects to the e2e server as user with a password.
func dial(t *testing.T, user, pass string) *ssh.Client {
	t.Helper()
	host, port := target(t)
	client, err := ssh.Dial(ssh.Target{User: user, Host: host, Port: port},
		ssh.DialOptions{Password: pass, Timeout: 15 * time.Second})
	if err != nil {
		t.Fatalf("Dial as %s: %v", user, err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// e2ePubkey is a syntactically valid ed25519 authorized_keys entry. It is never
// used to authenticate — the test only checks that bootstrap installs it.
const e2ePubkey = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIObe2etestkeyobe2etestkeyobe2etestkey00 e2e@keel"

// TestBootstrapAgainstRealSSHD provisions a real SSH server end-to-end: it scans
// the host key, dials in, runs the full Provisioner, and verifies on the box
// that the admin user, a valid sudoers drop-in, and authorized_keys all landed.
//
// Enable with KEEL_E2E_SSH_ADDR (host:port); see test/e2e/Dockerfile.
func TestBootstrapAgainstRealSSHD(t *testing.T) {
	host, port := target(t)
	user := env("KEEL_E2E_SSH_USER", "root")
	pass := env("KEEL_E2E_SSH_PASSWORD", "keelpass")

	// 1. Host-key scan returns a parseable known_hosts line for the real server
	//    (the key type depends on the server — ed25519, ecdsa, rsa, …).
	line, err := ssh.ScanHostKey(host, port, 10*time.Second)
	if err != nil {
		t.Fatalf("ScanHostKey: %v", err)
	}
	if _, _, _, _, _, err := gossh.ParseKnownHosts([]byte(line)); err != nil {
		t.Fatalf("scanned known_hosts line does not parse: %q: %v", line, err)
	}

	// 2. Dial in and run the full provisioning twice (to prove idempotency).
	client := dial(t, user, pass)

	var output []string
	prov := bootstrap.Provisioner{
		Exec:        client,
		AdminUser:   "bofh",
		ConnectUser: user,
		Password:    pass,
		OnOutput:    func(l string) { output = append(output, l) },
	}
	for i := 0; i < 2; i++ {
		if err := prov.Run([]string{e2ePubkey}); err != nil {
			t.Fatalf("provision run %d: %v", i+1, err)
		}
	}

	// The host's own command output must stream back (e.g. apt's progress),
	// confirming ExecStream + OnOutput surface real server output.
	if len(output) == 0 {
		t.Error("expected streamed host output, got none")
	}
	if !strings.Contains(strings.Join(output, "\n"), "package lists") {
		t.Logf("note: apt output did not contain the expected phrase; lines:\n%s", strings.Join(output, "\n"))
	}

	// 3. Verify the results on the real host.
	if out, err := client.Exec("id -u bofh", ""); err != nil || strings.TrimSpace(out) == "" {
		t.Fatalf("admin user 'bofh' not created: out=%q err=%v", out, err)
	}
	if _, err := client.Exec("test -f "+bootstrap.SudoersPath, ""); err != nil {
		t.Fatalf("sudoers drop-in %s missing: %v", bootstrap.SudoersPath, err)
	}
	if _, err := client.Exec("visudo -cf "+bootstrap.SudoersPath, ""); err != nil {
		t.Fatalf("sudoers drop-in is not valid: %v", err)
	}
	count, err := client.Exec("grep -cF '"+e2ePubkey+"' /home/bofh/.ssh/authorized_keys", "")
	if err != nil {
		t.Fatalf("read authorized_keys: %v", err)
	}
	if strings.TrimSpace(count) != "1" {
		t.Fatalf("authorized_keys should contain the key exactly once, got %q", count)
	}
}

// provisionAs runs the full provisioning as a non-root user, creating admin,
// and returns the streamed host output.
func provisionAs(t *testing.T, user, pass, rootPass, admin string) ([]string, error) {
	t.Helper()
	var output []string
	prov := bootstrap.Provisioner{
		Exec:         dial(t, user, pass),
		AdminUser:    admin,
		ConnectUser:  user,
		Password:     pass,
		RootPassword: rootPass,
		OnOutput:     func(l string) { output = append(output, l) },
	}
	return output, prov.Run([]string{e2ePubkey})
}

// assertAdminCreated checks, as root (the admin's home is not readable by
// other users), that admin now exists with the key installed exactly once.
func assertAdminCreated(t *testing.T, admin string) {
	t.Helper()
	client := dial(t, "root", env("KEEL_E2E_SSH_PASSWORD", "keelpass"))
	if out, err := client.Exec("id -u "+admin, ""); err != nil || strings.TrimSpace(out) == "" {
		t.Fatalf("admin user %q not created: out=%q err=%v", admin, out, err)
	}
	count, err := client.Exec("grep -cF '"+e2ePubkey+"' /home/"+admin+"/.ssh/authorized_keys", "")
	if err != nil || strings.TrimSpace(count) != "1" {
		t.Fatalf("authorized_keys for %s should contain the key once: count=%q err=%v", admin, count, err)
	}
}

// TestBootstrapThroughSudo provisions as a user who may sudo with its password,
// which reaches sudo on stdin.
func TestBootstrapThroughSudo(t *testing.T) {
	output, err := provisionAs(t, sudoUser, sudoPassword, "", "viasudo")
	if err != nil {
		t.Fatalf("provision as %s: %v", sudoUser, err)
	}
	if !slices.Contains(output, "using sudo") {
		t.Errorf("expected sudo to be used; output:\n%s", strings.Join(output, "\n"))
	}
	assertAdminCreated(t, "viasudo")
}

// TestBootstrapThroughSu provisions as a user who is not in the sudoers file:
// keel falls back to su with root's password, and su's password prompt stays
// out of the streamed output.
func TestBootstrapThroughSu(t *testing.T) {
	rootPass := env("KEEL_E2E_SSH_PASSWORD", "keelpass")
	output, err := provisionAs(t, suUser, suPassword, rootPass, "viasu")
	if err != nil {
		t.Fatalf("provision as %s: %v", suUser, err)
	}
	if !slices.Contains(output, "using su") {
		t.Errorf("expected su to be used; output:\n%s", strings.Join(output, "\n"))
	}
	for _, l := range output {
		if strings.Contains(l, "Password:") {
			t.Errorf("su's password prompt leaked into the output: %q", l)
		}
	}
	assertAdminCreated(t, "viasu")
}

// TestBootstrapWithoutRootAccessLeaksNoPassword runs the reported failure for
// real: neither sudo nor su works, and the error names why without repeating
// either password.
func TestBootstrapWithoutRootAccessLeaksNoPassword(t *testing.T) {
	const wrongRoot = "not-the-root-password"
	_, err := provisionAs(t, suUser, suPassword, wrongRoot, "never")
	if err == nil {
		t.Fatal("expected provisioning to fail with a wrong root password")
	}
	msg := err.Error()
	if !strings.Contains(msg, "no root access") || !strings.Contains(msg, "not in the sudoers file") {
		t.Errorf("error should say why there is no root access, got %q", msg)
	}
	for _, secret := range []string{suPassword, wrongRoot} {
		if strings.Contains(msg, secret) {
			t.Errorf("error leaks a password: %q", msg)
		}
	}
}
