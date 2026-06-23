package bootstrap

import (
	"encoding/base64"
	"fmt"
	"strings"
	"testing"
)

// fakeExec records commands and returns programmable responses.
type fakeExec struct {
	cmds    []string
	respond func(cmd string) (string, error)
}

func (f *fakeExec) Exec(cmd string) (string, error) {
	f.cmds = append(f.cmds, cmd)
	if f.respond != nil {
		return f.respond(cmd)
	}
	return "", nil
}

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

func TestSudoWrapperRootRunsDirectly(t *testing.T) {
	w := SudoWrapperFor("root", "")
	got := w("apt-get update")
	if strings.Contains(got, "sudo") {
		t.Errorf("root wrapper should not use sudo, got %q", got)
	}
	if !strings.Contains(got, "base64 -d | bash") {
		t.Errorf("root wrapper should pipe decoded command to bash, got %q", got)
	}
}

func TestSudoWrapperPasswordlessUsesSudo(t *testing.T) {
	w := SudoWrapperFor("deploy", "")
	got := w("apt-get update")
	if !strings.Contains(got, "sudo bash") {
		t.Errorf("passwordless wrapper should use sudo bash, got %q", got)
	}
}

func TestSudoWrapperWithPasswordUsesSudoS(t *testing.T) {
	w := SudoWrapperFor("deploy", "hunter2")
	got := w("apt-get update")
	if !strings.Contains(got, "sudo -S") {
		t.Errorf("password wrapper should use sudo -S, got %q", got)
	}
	if !strings.Contains(got, "hunter2") {
		t.Errorf("password wrapper should supply the password, got %q", got)
	}
}

func TestProvisionCreatesAdminUserWhenMissing(t *testing.T) {
	exec := &fakeExec{respond: func(cmd string) (string, error) {
		if strings.Contains(cmd, "id -u bofh") {
			return "", fmt.Errorf("no such user") // user missing
		}
		return "", nil
	}}
	p := Provisioner{Exec: exec, Sudo: func(c string) string { return "sudo " + c }, AdminUser: "bofh", ConnectUser: "root"}

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
	p := Provisioner{Exec: exec, Sudo: func(c string) string { return "sudo " + c }, AdminUser: "bofh", ConnectUser: "root"}

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
