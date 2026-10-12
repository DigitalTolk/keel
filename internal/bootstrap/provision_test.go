package bootstrap

import (
	"encoding/base64"
	"fmt"
	"slices"
	"strings"
	"testing"
)

// fakeExec records commands (and what was written to their stdin) and returns
// programmable responses.
type fakeExec struct {
	cmds    []string
	stdins  []string
	respond func(cmd string) (string, error)
}

func (f *fakeExec) Exec(cmd, stdin string) (string, error) {
	f.cmds = append(f.cmds, cmd)
	f.stdins = append(f.stdins, stdin)
	if f.respond != nil {
		return f.respond(cmd)
	}
	return "", nil
}

// plain is an Escalator that leaves commands as they are, so tests can match
// the raw provisioning commands.
func plain(cmd string) Command { return Command{Line: cmd} }

// sudoPrefix is an Escalator that marks commands as elevated in a readable way.
func sudoPrefix(cmd string) Command { return Command{Line: "sudo " + cmd} }

func (f *fakeExec) issued(substr string) bool {
	for _, c := range f.cmds {
		if strings.Contains(c, substr) {
			return true
		}
	}
	return false
}

func TestAptInstallCommandIsNonInteractive(t *testing.T) {
	got := AptInstallCommand([]string{"sudo", "python3"})
	for _, want := range []string{"DEBIAN_FRONTEND=noninteractive", "apt-get", "install", "sudo", "python3"} {
		if !strings.Contains(got, want) {
			t.Errorf("AptInstallCommand = %q, missing %q", got, want)
		}
	}
}

func TestUseraddCommandCreatesHomeAndShell(t *testing.T) {
	got := UseraddCommand("bofh")
	want := "useradd -d /home/bofh -m -s /bin/bash bofh"
	if got != want {
		t.Fatalf("UseraddCommand = %q, want %q", got, want)
	}
}

func TestWriteSudoersCommandValidatesWithVisudo(t *testing.T) {
	got := WriteSudoersCommand("bofh")
	encoded := base64.StdEncoding.EncodeToString([]byte(SudoersContent("bofh")))
	if !strings.Contains(got, encoded) {
		t.Errorf("WriteSudoersCommand should embed base64 sudoers content")
	}
	if !strings.Contains(got, "visudo -cf") {
		t.Errorf("WriteSudoersCommand should validate with visudo -cf, got %q", got)
	}
	if !strings.Contains(got, SudoersPath) {
		t.Errorf("WriteSudoersCommand should target %q", SudoersPath)
	}
}

func TestAppendAuthorizedKeyIsIdempotent(t *testing.T) {
	key := `ssh-ed25519 AAAA... user@host with "quoted" $comment`
	got := AppendAuthorizedKeyCommand("bofh", key)
	if !strings.Contains(got, "grep") || !strings.Contains(got, "authorized_keys") {
		t.Errorf("AppendAuthorizedKeyCommand should guard against duplicates, got %q", got)
	}
	// The key is base64-encoded for safe transport (comments may contain shell
	// metacharacters), so assert its exact bytes round-trip rather than appear raw.
	encoded := base64.StdEncoding.EncodeToString([]byte(key))
	if !strings.Contains(got, encoded) {
		t.Errorf("AppendAuthorizedKeyCommand should embed the base64-encoded key")
	}
}

// TestProvisionWritesEscalationInputToStdin verifies privileged commands are
// sent with the escalation's input, and the password never reaches a command
// line.
func TestProvisionWritesEscalationInputToStdin(t *testing.T) {
	const secret = "Edzfvbsvo4el!"
	exec := &fakeExec{respond: func(cmd string) (string, error) {
		if strings.Contains(cmd, "id -u bofh") {
			return "1000", nil
		}
		return "", nil
	}}
	p := Provisioner{Exec: exec, Escalate: ViaSudo(secret), AdminUser: "bofh", ConnectUser: "deploy"}

	if err := p.Run([]string{"ssh-ed25519 KEY a@b"}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	for i, cmd := range exec.cmds {
		if strings.Contains(cmd, secret) {
			t.Errorf("password leaked onto command line %q", cmd)
		}
		if strings.Contains(cmd, "sudo") && exec.stdins[i] != secret+"\n" {
			t.Errorf("privileged command %q sent stdin %q, want the password", cmd, exec.stdins[i])
		}
	}
	if !slices.Contains(exec.stdins, secret+"\n") {
		t.Errorf("no command received the password on stdin; commands: %v", exec.cmds)
	}
}

func TestProvisionCreatesAdminUserWhenMissing(t *testing.T) {
	exec := &fakeExec{respond: func(cmd string) (string, error) {
		if strings.Contains(cmd, "id -u bofh") {
			return "", fmt.Errorf("no such user") // user missing
		}
		return "", nil
	}}
	p := Provisioner{Exec: exec, Escalate: sudoPrefix, AdminUser: "bofh", ConnectUser: "root"}

	if err := p.Run([]string{"ssh-ed25519 KEY a@b"}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !exec.issued("useradd -d /home/bofh") {
		t.Errorf("expected useradd to be issued when bofh is missing; commands: %v", exec.cmds)
	}
}

func TestProvisionSkipsAdminUserWhenPresent(t *testing.T) {
	exec := &fakeExec{respond: func(cmd string) (string, error) {
		if strings.Contains(cmd, "id -u bofh") {
			return "1000", nil // user exists
		}
		return "", nil
	}}
	p := Provisioner{Exec: exec, Escalate: sudoPrefix, AdminUser: "bofh", ConnectUser: "root"}

	if err := p.Run([]string{"ssh-ed25519 KEY a@b"}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if exec.issued("useradd") {
		t.Errorf("expected no useradd when bofh exists; commands: %v", exec.cmds)
	}
	if !exec.issued("authorized_keys") {
		t.Errorf("expected authorized_keys to still be seeded; commands: %v", exec.cmds)
	}
}
