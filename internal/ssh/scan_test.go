package ssh

import (
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	gossh "golang.org/x/crypto/ssh"
)

func TestScanHostKeyReturnsKnownHostsLine(t *testing.T) {
	addr, hostKey := startTestServer(t, echoHandler)

	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("split addr: %v", err)
	}
	port, _ := strconv.Atoi(portStr)

	line, err := ScanHostKey(host, port, 5*time.Second)
	if err != nil {
		t.Fatalf("ScanHostKey: %v", err)
	}
	if !strings.Contains(line, hostKey.Type()) {
		t.Fatalf("line %q missing key type %q", line, hostKey.Type())
	}

	// The scanned line must parse back to the server's actual host key.
	_, _, parsedKey, _, _, err := gossh.ParseKnownHosts([]byte(line))
	if err != nil {
		t.Fatalf("ParseKnownHosts(%q): %v", line, err)
	}
	if string(parsedKey.Marshal()) != string(hostKey.Marshal()) {
		t.Fatal("scanned host key does not match server host key")
	}
}
