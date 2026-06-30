//go:build e2e

package e2e

import (
	"net"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	gossh "golang.org/x/crypto/ssh"

	"github.com/DigitalTolk/keel/internal/bootstrap"
	"github.com/DigitalTolk/keel/internal/ssh"
)

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
	addr := os.Getenv("KEEL_E2E_SSH_ADDR")
	if addr == "" {
		t.Skip("set KEEL_E2E_SSH_ADDR (host:port) to run the e2e SSH test")
	}
	user := env("KEEL_E2E_SSH_USER", "root")
	pass := env("KEEL_E2E_SSH_PASSWORD", "keelpass")

	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("bad KEEL_E2E_SSH_ADDR %q: %v", addr, err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("bad port in KEEL_E2E_SSH_ADDR %q: %v", addr, err)
	}

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
	client, err := ssh.Dial(ssh.Target{User: user, Host: host, Port: port},
		ssh.DialOptions{Password: pass, Timeout: 15 * time.Second})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer client.Close()

	var output []string
	prov := bootstrap.Provisioner{
		Exec:        client,
		Sudo:        bootstrap.SudoWrapperFor(user, pass),
		AdminUser:   "bofh",
		ConnectUser: user,
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
	if out, err := client.Exec("id -u bofh"); err != nil || strings.TrimSpace(out) == "" {
		t.Fatalf("admin user 'bofh' not created: out=%q err=%v", out, err)
	}
	if _, err := client.Exec("test -f " + bootstrap.SudoersPath); err != nil {
		t.Fatalf("sudoers drop-in %s missing: %v", bootstrap.SudoersPath, err)
	}
	if _, err := client.Exec("visudo -cf " + bootstrap.SudoersPath); err != nil {
		t.Fatalf("sudoers drop-in is not valid: %v", err)
	}
	count, err := client.Exec("grep -cF '" + e2ePubkey + "' /home/bofh/.ssh/authorized_keys")
	if err != nil {
		t.Fatalf("read authorized_keys: %v", err)
	}
	if strings.TrimSpace(count) != "1" {
		t.Fatalf("authorized_keys should contain the key exactly once, got %q", count)
	}
}
