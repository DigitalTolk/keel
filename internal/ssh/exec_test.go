package ssh

import (
	"strings"
	"testing"
	"time"
)

func echoHandler(cmd, stdin string) (string, int) { return "ran: " + cmd, 0 }

func TestExecReturnsStdout(t *testing.T) {
	addr, _ := startTestServer(t, echoHandler)
	tgt := dialTarget(t, addr, "bofh")

	client, err := Dial(tgt, DialOptions{Password: "pw", Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer client.Close()

	out, err := client.Exec("id -u bofh", "")
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if out != "ran: id -u bofh" {
		t.Fatalf("Exec output = %q, want %q", out, "ran: id -u bofh")
	}
}

// TestExecWritesStdin verifies the input reaches the remote command's stdin,
// which is how a password gets to sudo or su without touching the command line.
func TestExecWritesStdin(t *testing.T) {
	addr, _ := startTestServer(t, func(cmd, stdin string) (string, int) { return "read: " + stdin, 0 })
	tgt := dialTarget(t, addr, "bofh")

	client, err := Dial(tgt, DialOptions{Password: "pw", Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer client.Close()

	out, err := client.Exec("sudo -S true", "s3cret\n")
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if out != "read: s3cret" {
		t.Fatalf("Exec output = %q, want the stdin echoed back", out)
	}
}

func TestExecNonZeroExitIsError(t *testing.T) {
	addr, _ := startTestServer(t, func(cmd, stdin string) (string, int) { return "boom", 3 })
	tgt := dialTarget(t, addr, "bofh")

	client, err := Dial(tgt, DialOptions{Password: "pw", Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer client.Close()

	if _, err := client.Exec("false", ""); err == nil {
		t.Fatal("Exec with non-zero exit: want error, got nil")
	}
}

// TestExecErrorWithoutStderrEndsCleanly verifies a failure with nothing on
// stderr reads as the exit status alone, with no dangling separator.
func TestExecErrorWithoutStderrEndsCleanly(t *testing.T) {
	addr, _ := startTestServer(t, func(cmd, stdin string) (string, int) { return "", 1 })
	tgt := dialTarget(t, addr, "bofh")

	client, err := Dial(tgt, DialOptions{Password: "pw", Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer client.Close()

	_, err = client.Exec("false", "")
	if err == nil {
		t.Fatal("Exec with non-zero exit: want error, got nil")
	}
	if msg := err.Error(); strings.HasSuffix(msg, ":") || strings.HasSuffix(msg, ": ") {
		t.Errorf("error %q should not end with a separator", msg)
	}
}

func TestDialWrongPasswordFails(t *testing.T) {
	addr, _ := startTestServer(t, echoHandler)
	tgt := dialTarget(t, addr, "bofh")

	if _, err := Dial(tgt, DialOptions{Password: "wrong", Timeout: 5 * time.Second}); err == nil {
		t.Fatal("Dial with wrong password: want error, got nil")
	}
}
