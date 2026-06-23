package ssh

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"net"
	"testing"

	gossh "golang.org/x/crypto/ssh"
)

// dialTarget converts a "host:port" listen address into a Target using the
// "user@host#port" form that ParseTarget expects.
func dialTarget(t *testing.T, addr, user string) Target {
	t.Helper()
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("split addr %q: %v", addr, err)
	}
	tgt, err := ParseTarget(fmt.Sprintf("%s@%s#%s", user, host, port))
	if err != nil {
		t.Fatalf("parse target: %v", err)
	}
	return tgt
}

// execHandler runs a command on the test server and returns stdout + exit code.
type execHandler func(cmd string) (stdout string, exitCode int)

// startTestServer starts an in-process SSH server on a random localhost port.
// It accepts password auth for user "bofh" / password "pw" and dispatches
// "exec" requests to handler. It returns the listen address and host public key.
func startTestServer(t *testing.T, handler execHandler) (addr string, hostKey gossh.PublicKey) {
	t.Helper()

	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("gen host key: %v", err)
	}
	signer, err := gossh.NewSignerFromSigner(priv)
	if err != nil {
		t.Fatalf("signer: %v", err)
	}

	cfg := &gossh.ServerConfig{
		PasswordCallback: func(c gossh.ConnMetadata, pass []byte) (*gossh.Permissions, error) {
			if c.User() == "bofh" && string(pass) == "pw" {
				return &gossh.Permissions{}, nil
			}
			return nil, errors.New("auth failed")
		},
		// Accept any public key, so key-auth paths can be exercised.
		PublicKeyCallback: func(gossh.ConnMetadata, gossh.PublicKey) (*gossh.Permissions, error) {
			return &gossh.Permissions{}, nil
		},
	}
	cfg.AddHostKey(signer)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go serveConn(conn, cfg, handler)
		}
	}()

	return ln.Addr().String(), signer.PublicKey()
}

func serveConn(conn net.Conn, cfg *gossh.ServerConfig, handler execHandler) {
	sconn, chans, reqs, err := gossh.NewServerConn(conn, cfg)
	if err != nil {
		return
	}
	defer sconn.Close()
	go gossh.DiscardRequests(reqs)

	for newCh := range chans {
		if newCh.ChannelType() != "session" {
			_ = newCh.Reject(gossh.UnknownChannelType, "only session")
			continue
		}
		ch, chReqs, err := newCh.Accept()
		if err != nil {
			return
		}
		go func() {
			defer ch.Close()
			for req := range chReqs {
				if req.Type != "exec" {
					if req.WantReply {
						_ = req.Reply(false, nil)
					}
					continue
				}
				var payload struct{ Command string }
				_ = gossh.Unmarshal(req.Payload, &payload)
				if req.WantReply {
					_ = req.Reply(true, nil)
				}
				stdout, code := "", 0
				if handler != nil {
					stdout, code = handler(payload.Command)
				}
				_, _ = ch.Write([]byte(stdout))
				statusPayload := gossh.Marshal(struct{ Status uint32 }{uint32(code)})
				_, _ = ch.SendRequest("exit-status", false, statusPayload)
				return
			}
		}()
	}
}
