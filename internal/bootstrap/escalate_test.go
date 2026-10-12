package bootstrap

import (
	"encoding/base64"
	"errors"
	"slices"
	"strings"
	"testing"
)

// decoded returns the command an escalated line carries base64-encoded.
func decoded(t *testing.T, line string) string {
	t.Helper()
	for _, field := range strings.Fields(line) {
		if b, err := base64.StdEncoding.DecodeString(field); err == nil && len(field) > 8 {
			return string(b)
		}
	}
	t.Fatalf("no base64-encoded command in %q", line)
	return ""
}

func TestAsRootRunsDirectly(t *testing.T) {
	got := AsRoot("apt-get update")
	if strings.Contains(got.Line, "sudo") || strings.Contains(got.Line, "su ") {
		t.Errorf("root should not escalate, got %q", got.Line)
	}
	if !strings.Contains(got.Line, "base64 -d | bash") {
		t.Errorf("root should pipe the decoded command to bash, got %q", got.Line)
	}
	if decoded(t, got.Line) != "apt-get update" {
		t.Errorf("root line should carry the command, got %q", got.Line)
	}
	if got.Stdin != "" {
		t.Errorf("root needs no input, got %q", got.Stdin)
	}
}

func TestViaSudoWithoutPasswordUsesSudo(t *testing.T) {
	got := ViaSudo("")("apt-get update")
	if !strings.Contains(got.Line, "sudo bash") {
		t.Errorf("passwordless sudo should run sudo bash, got %q", got.Line)
	}
	if decoded(t, got.Line) != "apt-get update" {
		t.Errorf("sudo line should carry the command, got %q", got.Line)
	}
	if got.Stdin != "" {
		t.Errorf("passwordless sudo needs no input, got %q", got.Stdin)
	}
}

// TestViaSudoKeepsPasswordOffTheCommandLine reproduces the reported leak: the
// password used to be echoed into the command line, which the host's process
// list shows and a failure message repeated.
func TestViaSudoKeepsPasswordOffTheCommandLine(t *testing.T) {
	const secret = "Edzfvbsvo4el!"
	got := ViaSudo(secret)("apt-get update")
	if strings.Contains(got.Line, secret) {
		t.Fatalf("password must not be on the command line, got %q", got.Line)
	}
	if !strings.Contains(got.Line, "sudo -S") {
		t.Errorf("sudo should read the password from stdin (-S), got %q", got.Line)
	}
	if decoded(t, got.Line) != "apt-get update" {
		t.Errorf("sudo line should carry the command, got %q", got.Line)
	}
	if got.Stdin != secret+"\n" {
		t.Errorf("the password should be written to stdin, got %q", got.Stdin)
	}
}

func TestViaSuKeepsPasswordOffTheCommandLine(t *testing.T) {
	const secret = "r00t pass'word"
	got := ViaSu(secret)("apt-get update")
	if strings.Contains(got.Line, secret) {
		t.Fatalf("password must not be on the command line, got %q", got.Line)
	}
	if !strings.HasPrefix(got.Line, "su -l -c ") || !strings.Contains(got.Line, " root ") {
		t.Errorf("su should log in as root, got %q", got.Line)
	}
	if decoded(t, got.Line) != "apt-get update" {
		t.Errorf("su line should carry the command, got %q", got.Line)
	}
	if got.Stdin != secret+"\n" {
		t.Errorf("the password should be written to stdin, got %q", got.Stdin)
	}
}

// TestViaSuDropsItsPasswordPrompt verifies su's own stderr (where it prints its
// "Password:" prompt) is discarded while the command's stderr is kept, so the
// prompt never lands in front of the streamed output.
func TestViaSuDropsItsPasswordPrompt(t *testing.T) {
	got := ViaSu("pw")("apt-get update")
	if !strings.HasSuffix(got.Line, " 3>&2 2>/dev/null") {
		t.Errorf("su's stderr should be discarded with the command's moved to fd 3, got %q", got.Line)
	}
	if !strings.Contains(got.Line, "exec 2>&3 3>&-;") {
		t.Errorf("the command should get its stderr back from fd 3, got %q", got.Line)
	}
}

var errNotSudoer = errors.New("Process exited with status 1: customer is not in the sudoers file.")

// hostExec fakes a host where sudo and su each either work or fail with the
// given error, and the admin user already exists.
func hostExec(sudoErr, suErr error) *fakeExec {
	return &fakeExec{respond: func(cmd string) (string, error) {
		switch {
		case strings.HasPrefix(cmd, "id -u "):
			return "1000", nil
		case strings.Contains(cmd, "sudo"):
			return "", sudoErr
		case strings.HasPrefix(cmd, "su "):
			return "", suErr
		}
		return "", nil
	}}
}

// installedVia returns the line that ran the apt step, failing if none did.
func installedVia(t *testing.T, exec *fakeExec) (line, stdin string) {
	t.Helper()
	for i, cmd := range exec.cmds {
		if strings.Contains(cmd, encode(AptInstallCommand(BasePackages))) {
			return cmd, exec.stdins[i]
		}
	}
	t.Fatalf("base packages were never installed; commands: %v", exec.cmds)
	return "", ""
}

func TestProvisionAsRootNeedsNoAccessCheck(t *testing.T) {
	exec := hostExec(nil, nil)
	var steps []string
	p := Provisioner{Exec: exec, ConnectUser: "root", OnStep: func(s string) { steps = append(steps, s) }}

	if err := p.Run(nil); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if line, _ := installedVia(t, exec); line != AsRoot(AptInstallCommand(BasePackages)).Line {
		t.Errorf("root should run commands directly, got %q", line)
	}
	if slices.Contains(steps, "checking root access") {
		t.Errorf("root needs no access check, steps: %v", steps)
	}
}

func TestProvisionUsesSudoWhenAllowed(t *testing.T) {
	exec := hostExec(nil, nil)
	var steps, out []string
	p := Provisioner{
		Exec: exec, ConnectUser: "deploy", Password: "pw",
		OnStep:   func(s string) { steps = append(steps, s) },
		OnOutput: func(l string) { out = append(out, l) },
	}

	if err := p.Run(nil); err != nil {
		t.Fatalf("Run: %v", err)
	}
	line, stdin := installedVia(t, exec)
	if want := ViaSudo("pw")(AptInstallCommand(BasePackages)); line != want.Line || stdin != want.Stdin {
		t.Errorf("expected sudo with the password on stdin, got %q (stdin %q)", line, stdin)
	}
	if exec.issued("su -l") {
		t.Errorf("su should not be tried when sudo works; commands: %v", exec.cmds)
	}
	if !slices.Contains(steps, "checking root access") || !slices.Contains(out, "using sudo") {
		t.Errorf("the check and its outcome should be reported, steps %v, output %v", steps, out)
	}
}

// TestProvisionFallsBackToSuWhenNotASudoer covers the reported host: the user
// is not in the sudoers file, so root is reached through su, with the SSH
// password unless a root password is given.
func TestProvisionFallsBackToSuWhenNotASudoer(t *testing.T) {
	exec := hostExec(errNotSudoer, nil)
	var out []string
	p := Provisioner{
		Exec: exec, ConnectUser: "customer", Password: "pw",
		OnOutput: func(l string) { out = append(out, l) },
	}

	if err := p.Run(nil); err != nil {
		t.Fatalf("Run: %v", err)
	}
	line, stdin := installedVia(t, exec)
	if want := ViaSu("pw")(AptInstallCommand(BasePackages)); line != want.Line || stdin != want.Stdin {
		t.Errorf("expected su with the SSH password on stdin, got %q (stdin %q)", line, stdin)
	}
	if !slices.Contains(out, "using su") {
		t.Errorf("the fallback should be reported, output %v", out)
	}
}

func TestProvisionSuUsesTheRootPassword(t *testing.T) {
	exec := hostExec(errNotSudoer, nil)
	p := Provisioner{Exec: exec, ConnectUser: "customer", Password: "pw", RootPassword: "rootpw"}

	if err := p.Run(nil); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if _, stdin := installedVia(t, exec); stdin != "rootpw\n" {
		t.Errorf("su should be given the root password, got stdin %q", stdin)
	}
}

func TestProvisionWithoutRootAccessChangesNothing(t *testing.T) {
	exec := hostExec(errNotSudoer, errors.New("Process exited with status 1"))
	p := Provisioner{Exec: exec, ConnectUser: "customer", Password: "pw"}

	err := p.Run([]string{"ssh-ed25519 K a@b"})
	if err == nil {
		t.Fatal("expected an error when neither sudo nor su works")
	}
	for _, want := range []string{"no root access", "not in the sudoers file", "su:"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should mention %q", err, want)
		}
	}
	if len(exec.cmds) != 2 {
		t.Errorf("only the sudo and su checks should run, got: %v", exec.cmds)
	}
}

func TestProvisionWithoutAPasswordDoesNotTrySu(t *testing.T) {
	exec := hostExec(errNotSudoer, nil)
	p := Provisioner{Exec: exec, ConnectUser: "customer"}

	err := p.Run(nil)
	if err == nil || !strings.Contains(err.Error(), "su needs root's password") {
		t.Fatalf("expected an error saying su needs a password, got %v", err)
	}
	if exec.issued("su -l") {
		t.Errorf("su should not be tried without a password; commands: %v", exec.cmds)
	}
}
