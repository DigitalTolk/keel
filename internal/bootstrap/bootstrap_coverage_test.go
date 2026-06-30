package bootstrap

import (
	"fmt"
	"io"
	"slices"
	"strings"
	"testing"
)

// streamExec implements both Executor and StreamExecutor, so the Provisioner
// streams side-effect commands through ExecStream when OnOutput is set.
type streamExec struct {
	cmds   []string
	output string
}

func (s *streamExec) Exec(cmd string) (string, error) {
	s.cmds = append(s.cmds, cmd)
	if strings.Contains(cmd, "id -u") {
		return "1000", nil // user exists
	}
	return "", nil
}

func (s *streamExec) ExecStream(cmd string, w io.Writer) error {
	s.cmds = append(s.cmds, cmd)
	_, _ = io.WriteString(w, s.output)
	return nil
}

// TestProvisionStreamsOutput verifies that, given a StreamExecutor and OnOutput,
// the Provisioner emits each line of host output — including a trailing partial
// line with no newline (exercising lineWriter.flush).
func TestProvisionStreamsOutput(t *testing.T) {
	se := &streamExec{output: "Reading package lists...\nBuilding dependency tree\nDone"}
	var out []string
	p := Provisioner{
		Exec: se, Sudo: func(c string) string { return c }, AdminUser: "bofh", ConnectUser: "root",
		OnOutput: func(l string) { out = append(out, l) },
	}
	if err := p.Run([]string{"ssh-ed25519 K a@b"}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, want := range []string{"Reading package lists...", "Building dependency tree", "Done"} {
		if !slices.Contains(out, want) {
			t.Errorf("streamed output missing %q: %v", want, out)
		}
	}
}

// TestProvisionOnStepCallback verifies the OnStep hook is invoked with a label
// before each provisioning step.
func TestProvisionOnStepCallback(t *testing.T) {
	exec := &fakeExec{respond: func(cmd string) (string, error) {
		if strings.Contains(cmd, "id -u bofh") {
			return "1000", nil
		}
		return "", nil
	}}
	var steps []string
	p := Provisioner{
		Exec: exec, Sudo: func(c string) string { return c }, AdminUser: "bofh", ConnectUser: "root",
		OnStep: func(s string) { steps = append(steps, s) },
	}
	if err := p.Run([]string{"ssh-ed25519 K a@b"}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	joined := strings.Join(steps, "|")
	for _, want := range []string{"base packages", "admin user bofh", "sudoers", "authorized keys"} {
		if !strings.Contains(joined, want) {
			t.Errorf("step labels missing %q: %v", want, steps)
		}
	}
}

// TestProvisionSudoNilLeavesCommandsUnwrapped verifies that when Sudo is nil the
// sudo() helper returns the command unchanged (identity branch), so the raw
// commands are issued to the Executor without any sudo prefix.
func TestProvisionSudoNilLeavesCommandsUnwrapped(t *testing.T) {
	exec := &fakeExec{respond: func(cmd string) (string, error) {
		if strings.Contains(cmd, "id -u bofh") {
			return "1000", nil // user exists, so no useradd
		}
		return "", nil
	}}
	p := Provisioner{Exec: exec, Sudo: nil, AdminUser: "bofh", ConnectUser: "root"}

	if err := p.Run([]string{"ssh-ed25519 KEY a@b"}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// With Sudo==nil the apt command must be issued verbatim with no wrapping.
	wantApt := AptInstallCommand(BasePackages)
	found := false
	for _, c := range exec.cmds {
		if c == wantApt {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected raw apt command %q to be issued unwrapped; commands: %v", wantApt, exec.cmds)
	}
	for _, c := range exec.cmds {
		if strings.HasPrefix(c, "sudo ") {
			t.Errorf("no command should be sudo-wrapped when Sudo is nil; got %q", c)
		}
	}
}

// TestProvisionDefaultsAdminUserWhenEmpty verifies that an empty AdminUser falls
// back to DefaultAdminUser.
func TestProvisionDefaultsAdminUserWhenEmpty(t *testing.T) {
	exec := &fakeExec{respond: func(cmd string) (string, error) {
		if strings.Contains(cmd, "id -u "+DefaultAdminUser) {
			return "1000", nil
		}
		return "", nil
	}}
	p := Provisioner{Exec: exec, Sudo: func(c string) string { return c }, ConnectUser: "root"}

	if err := p.Run(nil); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !exec.issued("/home/" + DefaultAdminUser + "/.ssh") {
		t.Errorf("expected ssh dir for default admin %q; commands: %v", DefaultAdminUser, exec.cmds)
	}
}

// TestProvisionSkipsUseraddWhenConnectedAsAdmin verifies the ConnectUser==admin
// branch: no `id -u` probe and no useradd is attempted.
func TestProvisionSkipsUseraddWhenConnectedAsAdmin(t *testing.T) {
	exec := &fakeExec{}
	p := Provisioner{Exec: exec, Sudo: func(c string) string { return "sudo " + c }, AdminUser: "bofh", ConnectUser: "bofh"}

	if err := p.Run([]string{"ssh-ed25519 KEY a@b"}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if exec.issued("id -u bofh") {
		t.Errorf("should not probe user existence when connected as admin; commands: %v", exec.cmds)
	}
	if exec.issued("useradd") {
		t.Errorf("should not run useradd when connected as admin; commands: %v", exec.cmds)
	}
	if !exec.issued("authorized_keys") {
		t.Errorf("should still seed authorized_keys; commands: %v", exec.cmds)
	}
}

// TestProvisionSkipsBlankAuthorizedKeys verifies that empty and whitespace-only
// keys are skipped while non-empty keys are appended.
func TestProvisionSkipsBlankAuthorizedKeys(t *testing.T) {
	exec := &fakeExec{respond: func(cmd string) (string, error) {
		if strings.Contains(cmd, "id -u bofh") {
			return "1000", nil
		}
		return "", nil
	}}
	p := Provisioner{Exec: exec, Sudo: func(c string) string { return c }, AdminUser: "bofh", ConnectUser: "root"}

	keys := []string{"", "   ", "ssh-ed25519 REALKEY a@b", "\t\n"}
	if err := p.Run(keys); err != nil {
		t.Fatalf("Run: %v", err)
	}

	appendCount := 0
	for _, c := range exec.cmds {
		if strings.Contains(c, "authorized_keys") {
			appendCount++
		}
	}
	// EnsureSSHDirCommand does not touch authorized_keys, so every match is an
	// append. Only the single real key should produce one.
	if appendCount != 1 {
		t.Errorf("expected exactly 1 authorized_keys append for 1 real key, got %d; commands: %v", appendCount, exec.cmds)
	}
	encodedReal := base64Key("ssh-ed25519 REALKEY a@b")
	if !exec.issued(encodedReal) {
		t.Errorf("expected the real key to be appended (encoded %q); commands: %v", encodedReal, exec.cmds)
	}
}

// base64Key is a tiny helper mirroring the encoding used in
// AppendAuthorizedKeyCommand so the test can assert the real key's presence.
func base64Key(key string) string {
	return AppendAuthorizedKeyCommandEncodedMarker(key)
}

// AppendAuthorizedKeyCommandEncodedMarker extracts the base64 marker by building
// the command and is intentionally derived from production output to stay in
// sync with AppendAuthorizedKeyCommand.
func AppendAuthorizedKeyCommandEncodedMarker(key string) string {
	cmd := AppendAuthorizedKeyCommand("bofh", key)
	// The encoded key appears between "echo " and " | base64 -d) && grep".
	const prefix = "echo "
	idx := strings.Index(cmd, prefix)
	rest := cmd[idx+len(prefix):]
	end := strings.Index(rest, " | base64 -d)")
	return rest[:end]
}

// failOn returns a fakeExec that fails when the command contains substr.
func failOn(substr string, userExists bool) *fakeExec {
	return &fakeExec{respond: func(cmd string) (string, error) {
		if strings.Contains(cmd, "id -u ") {
			if userExists {
				return "1000", nil
			}
			return "", fmt.Errorf("no such user")
		}
		if strings.Contains(cmd, substr) {
			return "", fmt.Errorf("boom: %s", substr)
		}
		return "", nil
	}}
}

// TestProvisionErrorPropagation verifies that a failure at each provisioning
// step is wrapped and returned, halting the run.
func TestProvisionErrorPropagation(t *testing.T) {
	cases := []struct {
		name       string
		failSubstr string
		userExists bool
		wantWrap   string
	}{
		{name: "apt", failSubstr: "apt-get", userExists: true, wantWrap: "install base packages"},
		{name: "useradd", failSubstr: "useradd", userExists: false, wantWrap: "create admin user bofh"},
		{name: "sudoers", failSubstr: "visudo", userExists: true, wantWrap: "write sudoers"},
		{name: "ssh-dir", failSubstr: "install -d", userExists: true, wantWrap: "create ssh dir"},
		{name: "key-append", failSubstr: "authorized_keys", userExists: true, wantWrap: "append authorized key"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			exec := failOn(tc.failSubstr, tc.userExists)
			p := Provisioner{Exec: exec, Sudo: func(c string) string { return c }, AdminUser: "bofh", ConnectUser: "root"}

			err := p.Run([]string{"ssh-ed25519 KEY a@b"})
			if err == nil {
				t.Fatalf("expected error when %s fails", tc.name)
			}
			if !strings.Contains(err.Error(), tc.wantWrap) {
				t.Errorf("error %q should mention %q", err.Error(), tc.wantWrap)
			}
			if !strings.Contains(err.Error(), "boom") {
				t.Errorf("error %q should wrap the underlying executor error", err.Error())
			}
		})
	}
}
