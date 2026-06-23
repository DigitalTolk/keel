package ssh

import (
	"testing"
	"time"
)

func echoHandler(cmd string) (string, int) { return "ran: " + cmd, 0 }

func TestExecReturnsStdout(t *testing.T) {
	addr, _ := startTestServer(t, echoHandler)
	tgt := dialTarget(t, addr, "bofh")

	client, err := Dial(tgt, DialOptions{Password: "pw", Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer client.Close()

	out, err := client.Exec("id -u bofh")
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if out != "ran: id -u bofh" {
		t.Fatalf("Exec output = %q, want %q", out, "ran: id -u bofh")
	}
}

func TestExecNonZeroExitIsError(t *testing.T) {
	addr, _ := startTestServer(t, func(cmd string) (string, int) { return "boom", 3 })
	tgt := dialTarget(t, addr, "bofh")

	client, err := Dial(tgt, DialOptions{Password: "pw", Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer client.Close()

	if _, err := client.Exec("false"); err == nil {
		t.Fatal("Exec with non-zero exit: want error, got nil")
	}
}

func TestDialWrongPasswordFails(t *testing.T) {
	addr, _ := startTestServer(t, echoHandler)
	tgt := dialTarget(t, addr, "bofh")

	if _, err := Dial(tgt, DialOptions{Password: "wrong", Timeout: 5 * time.Second}); err == nil {
		t.Fatal("Dial with wrong password: want error, got nil")
	}
}
