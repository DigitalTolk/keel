package ssh

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	gossh "golang.org/x/crypto/ssh"
)

// writeKeyFile generates an ed25519 private key and writes it in PEM form.
func writeKeyFile(t *testing.T) string {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	block, err := gossh.MarshalPrivateKey(priv, "")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "id_ed25519")
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestDialWithKeyFile(t *testing.T) {
	addr, _ := startTestServer(t, echoHandler)
	tgt := dialTarget(t, addr, "bofh")
	key := writeKeyFile(t)

	client, err := Dial(tgt, DialOptions{KeyFiles: []string{key}, Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("Dial with key: %v", err)
	}
	defer client.Close()
	if out, err := client.Exec("whoami"); err != nil || out != "ran: whoami" {
		t.Fatalf("Exec over key auth: out=%q err=%v", out, err)
	}
}

func TestDialNoUsableCredentials(t *testing.T) {
	addr, _ := startTestServer(t, echoHandler)
	tgt := dialTarget(t, addr, "bofh")
	// No password and only an unreadable key file -> no auth methods.
	_, err := Dial(tgt, DialOptions{KeyFiles: []string{"/nonexistent/key"}, Timeout: 2 * time.Second})
	if err == nil {
		t.Fatal("expected error when no usable credentials are available")
	}
}

// startForwardingServer accepts password auth and tunnels direct-tcpip channels
// to their requested address, so it can act as a jump host.
func startForwardingServer(t *testing.T) (string, gossh.PublicKey) {
	t.Helper()
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	signer, _ := gossh.NewSignerFromSigner(priv)
	cfg := &gossh.ServerConfig{
		PasswordCallback: func(gossh.ConnMetadata, []byte) (*gossh.Permissions, error) {
			return &gossh.Permissions{}, nil
		},
	}
	cfg.AddHostKey(signer)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go serveForward(conn, cfg)
		}
	}()
	return ln.Addr().String(), signer.PublicKey()
}

func serveForward(conn net.Conn, cfg *gossh.ServerConfig) {
	sconn, chans, reqs, err := gossh.NewServerConn(conn, cfg)
	if err != nil {
		return
	}
	defer sconn.Close()
	go gossh.DiscardRequests(reqs)

	for newCh := range chans {
		if newCh.ChannelType() != "direct-tcpip" {
			_ = newCh.Reject(gossh.UnknownChannelType, "only direct-tcpip")
			continue
		}
		var payload struct {
			DestAddr string
			DestPort uint32
			SrcAddr  string
			SrcPort  uint32
		}
		if err := gossh.Unmarshal(newCh.ExtraData(), &payload); err != nil {
			_ = newCh.Reject(gossh.ConnectionFailed, "bad payload")
			continue
		}
		upstream, err := net.Dial("tcp", net.JoinHostPort(payload.DestAddr, strconv.Itoa(int(payload.DestPort))))
		if err != nil {
			_ = newCh.Reject(gossh.ConnectionFailed, err.Error())
			continue
		}
		ch, chReqs, err := newCh.Accept()
		if err != nil {
			upstream.Close()
			continue
		}
		go gossh.DiscardRequests(chReqs)
		go func() { _, _ = io.Copy(ch, upstream); ch.Close() }()
		go func() { _, _ = io.Copy(upstream, ch); upstream.Close() }()
	}
}

func TestDialThroughJumpHost(t *testing.T) {
	targetAddr, _ := startTestServer(t, echoHandler)
	jumpAddr, _ := startForwardingServer(t)

	jumpHost, jumpPortStr, _ := net.SplitHostPort(jumpAddr)
	jumpPort, _ := strconv.Atoi(jumpPortStr)
	target := dialTarget(t, targetAddr, "bofh")

	client, err := Dial(target, DialOptions{
		Password: "pw",
		JumpHost: "bofh@" + jumpHost + "#" + strconv.Itoa(jumpPort),
		Timeout:  5 * time.Second,
	})
	if err != nil {
		t.Fatalf("Dial through jump host: %v", err)
	}
	defer client.Close()

	out, err := client.Exec("hostname")
	if err != nil || out != "ran: hostname" {
		t.Fatalf("Exec via jump host: out=%q err=%v", out, err)
	}
}

func TestDialJumpHostBadSpec(t *testing.T) {
	targetAddr, _ := startTestServer(t, echoHandler)
	target := dialTarget(t, targetAddr, "bofh")
	// An empty-host jump spec must fail to parse.
	_, err := Dial(target, DialOptions{Password: "pw", JumpHost: "bofh@#22", Timeout: 2 * time.Second})
	if err == nil {
		t.Fatal("expected error for invalid jump host spec")
	}
}

func TestDialJumpHostUnreachable(t *testing.T) {
	targetAddr, _ := startTestServer(t, echoHandler)
	target := dialTarget(t, targetAddr, "bofh")
	// Jump host points at a closed port.
	_, err := Dial(target, DialOptions{Password: "pw", JumpHost: "bofh@127.0.0.1#1", Timeout: 1 * time.Second})
	if err == nil {
		t.Fatal("expected error dialing an unreachable jump host")
	}
}

func TestDialTunnelFailure(t *testing.T) {
	jumpAddr, _ := startForwardingServer(t)
	jh, jp, _ := net.SplitHostPort(jumpAddr)
	jpN, _ := strconv.Atoi(jp)
	// Target port is closed, so the tunnel through the jump host fails.
	target := Target{User: "bofh", Host: "127.0.0.1", Port: 1}
	_, err := Dial(target, DialOptions{
		Password: "pw",
		JumpHost: "bofh@" + jh + "#" + strconv.Itoa(jpN),
		Timeout:  2 * time.Second,
	})
	if err == nil {
		t.Fatal("expected tunnel failure to a closed target")
	}
}

func TestScanHostKeyConnectError(t *testing.T) {
	// Port 1 on localhost should refuse / time out quickly.
	if _, err := ScanHostKey("127.0.0.1", 1, 1*time.Second); err == nil {
		t.Fatal("expected connect error scanning an unreachable port")
	}
}

func TestDialUsesDefaultKeyFromHome(t *testing.T) {
	addr, _ := startTestServer(t, echoHandler)
	tgt := dialTarget(t, addr, "bofh")

	home := t.TempDir()
	t.Setenv("HOME", home)
	sshDir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		t.Fatal(err)
	}
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	block, _ := gossh.MarshalPrivateKey(priv, "")
	if err := os.WriteFile(filepath.Join(sshDir, "id_ed25519"), pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatal(err)
	}

	// No password and no explicit key files -> default key discovery.
	client, err := Dial(tgt, DialOptions{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("Dial with default key: %v", err)
	}
	defer client.Close()
	if _, err := client.Exec("id"); err != nil {
		t.Fatalf("Exec: %v", err)
	}
}

func TestDialConnectionRefused(t *testing.T) {
	tgt := Target{User: "bofh", Host: "127.0.0.1", Port: 1}
	if _, err := Dial(tgt, DialOptions{Password: "pw", Timeout: 1 * time.Second}); err == nil {
		t.Fatal("expected dial error to an unreachable port")
	}
}

// startStderrServer answers exec requests by writing to the channel's stderr
// stream and exiting non-zero, so Exec's stderr-in-error branch is exercised.
func startStderrServer(t *testing.T) string {
	t.Helper()
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	signer, _ := gossh.NewSignerFromSigner(priv)
	cfg := &gossh.ServerConfig{
		PasswordCallback: func(gossh.ConnMetadata, []byte) (*gossh.Permissions, error) {
			return &gossh.Permissions{}, nil
		},
	}
	cfg.AddHostKey(signer)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				sconn, chans, reqs, err := gossh.NewServerConn(conn, cfg)
				if err != nil {
					return
				}
				defer sconn.Close()
				go gossh.DiscardRequests(reqs)
				for newCh := range chans {
					if newCh.ChannelType() != "session" {
						_ = newCh.Reject(gossh.UnknownChannelType, "")
						continue
					}
					ch, chReqs, _ := newCh.Accept()
					go func() {
						defer ch.Close()
						for req := range chReqs {
							if req.WantReply {
								_ = req.Reply(req.Type == "exec", nil)
							}
							if req.Type == "exec" {
								_, _ = ch.Stderr().Write([]byte("boom on the server"))
								_, _ = ch.SendRequest("exit-status", false, gossh.Marshal(struct{ S uint32 }{2}))
								return
							}
						}
					}()
				}
			}()
		}
	}()
	return ln.Addr().String()
}

func TestExecStderrIncludedInError(t *testing.T) {
	addr := startStderrServer(t)
	tgt := dialTarget(t, addr, "bofh")
	client, err := Dial(tgt, DialOptions{Password: "pw", Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer client.Close()

	_, err = client.Exec("failing-cmd")
	if err == nil {
		t.Fatal("expected error for failing command")
	}
	if !strings.Contains(err.Error(), "boom on the server") {
		t.Errorf("error should include server stderr, got %q", err.Error())
	}
}

func TestUpsertKnownHostsLineWhitespaceFirstField(t *testing.T) {
	// A line whose first field is empty exercises firstField's empty branch.
	out := UpsertKnownHostsLine([]byte("real.host ssh-rsa KEEP\n"), "   ")
	if len(out) == 0 {
		t.Fatal("expected non-empty output")
	}
}
