package ssh

import (
	"fmt"
	"net"
	"strconv"
	"time"

	gossh "golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// ScanHostKey connects to host:port, captures the server's host key during the
// SSH handshake (as ssh-keyscan does), and returns a known_hosts line for it.
//
// Authentication is never attempted: the host key is exchanged before auth, so
// we record it in the HostKeyCallback and abort. The returned line is suitable
// for appending to ~/.ssh/known_hosts.
func ScanHostKey(host string, port int, timeout time.Duration) (string, error) {
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	addr := net.JoinHostPort(host, strconv.Itoa(port))

	var captured gossh.PublicKey
	cfg := &gossh.ClientConfig{
		User:    "keel-keyscan",
		Auth:    []gossh.AuthMethod{},
		Timeout: timeout,
		HostKeyCallback: func(_ string, _ net.Addr, key gossh.PublicKey) error {
			captured = key
			return nil
		},
	}

	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return "", fmt.Errorf("connect %s: %w", addr, err)
	}
	defer conn.Close()

	// The handshake fails at authentication, but the host-key callback has
	// already fired by then, so a captured key means success.
	sshConn, _, _, err := gossh.NewClientConn(conn, addr, cfg)
	if sshConn != nil {
		sshConn.Close()
	}
	if captured == nil {
		return "", fmt.Errorf("scan %s: no host key received: %w", addr, err)
	}

	// knownhosts.Normalize renders "[host]:port" for non-standard ports.
	hostPattern := knownhosts.Normalize(addr)
	return knownhosts.Line([]string{hostPattern}, captured), nil
}
