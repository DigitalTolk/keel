package ssh

import (
	"bytes"
	"fmt"
	"os"
	"time"

	gossh "golang.org/x/crypto/ssh"
)

// DialOptions controls how Dial authenticates and connects.
//
// If Password is set, password authentication is used (replacing the sshpass
// dependency). Otherwise KeyFiles (or, if empty, the user's default key at
// ~/.ssh/id_ed25519 / id_rsa) are used. JumpHost, when set, is dialed first and
// the final connection is tunneled through it (the -J equivalent).
type DialOptions struct {
	Password        string
	KeyFiles        []string
	JumpHost        string
	Timeout         time.Duration
	HostKeyCallback gossh.HostKeyCallback // defaults to insecure (matches StrictHostKeyChecking=no)
}

// Client is a connected SSH session factory.
type Client struct {
	conn *gossh.Client
	jump *gossh.Client // non-nil when tunneling through a jump host
}

func (o DialOptions) authMethods() ([]gossh.AuthMethod, error) {
	if o.Password != "" {
		return []gossh.AuthMethod{gossh.Password(o.Password)}, nil
	}

	keyFiles := o.KeyFiles
	if len(keyFiles) == 0 {
		home, err := os.UserHomeDir()
		if err == nil {
			for _, name := range []string{"id_ed25519", "id_rsa"} {
				keyFiles = append(keyFiles, home+"/.ssh/"+name)
			}
		}
	}

	var signers []gossh.Signer
	for _, kf := range keyFiles {
		data, err := os.ReadFile(kf)
		if err != nil {
			continue
		}
		signer, err := gossh.ParsePrivateKey(data)
		if err != nil {
			continue
		}
		signers = append(signers, signer)
	}
	if len(signers) == 0 {
		return nil, fmt.Errorf("no usable ssh credentials (set a password or provide a key file)")
	}
	return []gossh.AuthMethod{gossh.PublicKeys(signers...)}, nil
}

func (o DialOptions) clientConfig(user string) (*gossh.ClientConfig, error) {
	auth, err := o.authMethods()
	if err != nil {
		return nil, err
	}
	hkcb := o.HostKeyCallback
	if hkcb == nil {
		hkcb = gossh.InsecureIgnoreHostKey()
	}
	timeout := o.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	return &gossh.ClientConfig{
		User:            user,
		Auth:            auth,
		HostKeyCallback: hkcb,
		Timeout:         timeout,
	}, nil
}

// Dial connects to the target, optionally through a jump host.
func Dial(t Target, opts DialOptions) (*Client, error) {
	cfg, err := opts.clientConfig(t.User)
	if err != nil {
		return nil, err
	}

	if opts.JumpHost == "" {
		conn, err := gossh.Dial("tcp", t.Addr(), cfg)
		if err != nil {
			return nil, fmt.Errorf("dial %s: %w", t.Addr(), err)
		}
		return &Client{conn: conn}, nil
	}

	jt, err := ParseTarget(opts.JumpHost)
	if err != nil {
		return nil, fmt.Errorf("jump host: %w", err)
	}
	jumpCfg, err := opts.clientConfig(jt.User)
	if err != nil {
		return nil, err
	}
	jumpConn, err := gossh.Dial("tcp", jt.Addr(), jumpCfg)
	if err != nil {
		return nil, fmt.Errorf("dial jump host %s: %w", jt.Addr(), err)
	}
	netConn, err := jumpConn.Dial("tcp", t.Addr())
	if err != nil {
		jumpConn.Close()
		return nil, fmt.Errorf("tunnel to %s via jump host: %w", t.Addr(), err)
	}
	ncc, chans, reqs, err := gossh.NewClientConn(netConn, t.Addr(), cfg)
	if err != nil {
		jumpConn.Close()
		return nil, fmt.Errorf("handshake with %s via jump host: %w", t.Addr(), err)
	}
	return &Client{conn: gossh.NewClient(ncc, chans, reqs), jump: jumpConn}, nil
}

// Exec runs a single command and returns its combined trimmed stdout. A
// non-zero exit status is returned as an error including any stderr output.
func (c *Client) Exec(cmd string) (string, error) {
	session, err := c.conn.NewSession()
	if err != nil {
		return "", fmt.Errorf("new session: %w", err)
	}
	defer session.Close()

	var stdout, stderr bytes.Buffer
	session.Stdout = &stdout
	session.Stderr = &stderr

	if err := session.Run(cmd); err != nil {
		return "", fmt.Errorf("command %q failed: %w: %s", cmd, err, trimNL(stderr.String()))
	}
	return trimNL(stdout.String()), nil
}

// Close releases the connection and any jump-host connection.
func (c *Client) Close() error {
	var err error
	if c.conn != nil {
		err = c.conn.Close()
	}
	if c.jump != nil {
		_ = c.jump.Close()
	}
	return err
}

func trimNL(s string) string {
	return string(bytes.TrimRight([]byte(s), "\r\n"))
}
