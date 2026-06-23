// Package ssh provides native remote command execution, replacing the
// sshpass + base64 piping approach from libs/remote_exec.lib.sh.
package ssh

import (
	"fmt"
	"net"
	"strconv"
	"strings"
)

// DefaultPort is used when a target omits an explicit port.
const DefaultPort = 22

// Target identifies a remote SSH endpoint, parsed from the
// "user@host#port" form used throughout the original scripts.
type Target struct {
	User string
	Host string
	Port int
}

// Addr returns the host:port string suitable for net.Dial.
func (t Target) Addr() string {
	return net.JoinHostPort(t.Host, strconv.Itoa(t.Port))
}

// ParseTarget parses a "[user@]host[#port]" specification.
//
// Examples:
//
//	bofh@web1#2222 -> {User: bofh, Host: web1, Port: 2222}
//	bofh@web1      -> {User: bofh, Host: web1, Port: 22}
//	web1           -> {Host: web1, Port: 22}
func ParseTarget(s string) (Target, error) {
	if strings.TrimSpace(s) == "" {
		return Target{}, fmt.Errorf("empty target")
	}

	t := Target{Port: DefaultPort}

	conn := s
	if i := strings.LastIndex(s, "#"); i >= 0 {
		conn = s[:i]
		portStr := s[i+1:]
		port, err := strconv.Atoi(portStr)
		if err != nil {
			return Target{}, fmt.Errorf("invalid port %q: not a number", portStr)
		}
		if port < 1 || port > 65535 {
			return Target{}, fmt.Errorf("invalid port %d: out of range 1-65535", port)
		}
		t.Port = port
	}

	if i := strings.Index(conn, "@"); i >= 0 {
		t.User = conn[:i]
		t.Host = conn[i+1:]
	} else {
		t.Host = conn
	}

	if t.Host == "" {
		return Target{}, fmt.Errorf("empty host in target %q", s)
	}

	return t, nil
}
