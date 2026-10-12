package bootstrap

import (
	"fmt"
	"io"
	"strings"
)

// BasePackages are installed on every bootstrapped host, matching the set the
// original know-hosts.sh installed (sudo, python3, python3-apt, acl).
var BasePackages = []string{"sudo", "python3", "python3-apt", "acl"}

// Executor runs a command on a remote host, with stdin as its standard input,
// and returns its stdout. The ssh.Client satisfies this; tests use a fake.
type Executor interface {
	Exec(cmd, stdin string) (string, error)
}

// StreamExecutor optionally streams a command's combined output line-by-line.
// ssh.Client implements it; when the Executor also implements it and OnOutput is
// set, side-effect commands stream their host output live. Fakes that don't
// implement it fall back to Exec.
type StreamExecutor interface {
	ExecStream(cmd, stdin string, w io.Writer) error
}

// lineWriter splits incoming bytes into lines and calls emit for each complete
// line, so streamed host output can be forwarded one line at a time.
type lineWriter struct {
	buf  strings.Builder
	emit func(string)
}

func (lw *lineWriter) Write(p []byte) (int, error) {
	for _, b := range p {
		switch b {
		case '\n':
			lw.emit(lw.buf.String())
			lw.buf.Reset()
		case '\r':
			// drop carriage returns
		default:
			lw.buf.WriteByte(b)
		}
	}
	return len(p), nil
}

func (lw *lineWriter) flush() {
	if lw.buf.Len() > 0 {
		lw.emit(lw.buf.String())
		lw.buf.Reset()
	}
}

// AptInstallCommand updates the package cache and installs pkgs without any
// interactive prompts. It uses -q (not -qq) so apt still reports what it is
// doing — that output is streamed into the provisioning log.
func AptInstallCommand(pkgs []string) string {
	return fmt.Sprintf(
		"apt-get -q update && DEBIAN_FRONTEND=noninteractive apt-get -y -q install %s",
		strings.Join(pkgs, " "),
	)
}

// UseraddCommand creates the admin user with a home directory and bash shell.
func UseraddCommand(user string) string {
	return fmt.Sprintf("useradd -d /home/%s -m -s /bin/bash %s", user, user)
}

// WriteSudoersCommand writes the passwordless sudoers drop-in atomically: it
// decodes the content from base64 (avoiding quoting issues over SSH), validates
// it with visudo, and only then installs it with mode 0440.
func WriteSudoersCommand(user string) string {
	return fmt.Sprintf(
		"tmp=$(mktemp) && echo %s | base64 -d > \"$tmp\" && visudo -cf \"$tmp\" && install -m 0440 \"$tmp\" %s && rm -f \"$tmp\"",
		encode(SudoersContent(user)), SudoersPath,
	)
}

// EnsureSSHDirCommand creates ~user/.ssh owned by the user with mode 0750.
func EnsureSSHDirCommand(user string) string {
	return fmt.Sprintf("install -d -o %s -g %s -m 0750 /home/%s/.ssh", user, user, user)
}

// AppendAuthorizedKeyCommand appends a public key to the user's
// authorized_keys only if not already present (idempotent), fixing ownership
// and mode.
func AppendAuthorizedKeyCommand(user, key string) string {
	path := fmt.Sprintf("/home/%s/.ssh/authorized_keys", user)
	return fmt.Sprintf(
		"touch %s && k=$(echo %s | base64 -d) && grep -qxF \"$k\" %s || echo \"$k\" >> %s; "+
			"chown %s:%s %s && chmod 0640 %s",
		path, encode(key), path, path, user, user, path, path,
	)
}

// Provisioner performs the remote provisioning steps over an Executor.
type Provisioner struct {
	Exec         Executor
	Escalate     Escalator    // runs a command as root; nil works it out on the host
	AdminUser    string       // privileged user to ensure (default "bofh")
	ConnectUser  string       // the user we connected as
	Password     string       // ConnectUser's password, for sudo
	RootPassword string       // root's password, for su; blank uses Password
	OnStep       func(string) // optional: called with a human-readable label before each step
	OnOutput     func(string) // optional: called with each line of live host output
}

func (p Provisioner) step(label string) {
	if p.OnStep != nil {
		p.OnStep(label)
	}
}

func (p Provisioner) output(line string) {
	if p.OnOutput != nil {
		p.OnOutput(line)
	}
}

func (p Provisioner) run(c Command) error {
	if se, ok := p.Exec.(StreamExecutor); ok && p.OnOutput != nil {
		lw := &lineWriter{emit: p.OnOutput}
		err := se.ExecStream(c.Line, c.Stdin, lw)
		lw.flush()
		return err
	}
	_, err := p.Exec.Exec(c.Line, c.Stdin)
	return err
}

// Run executes the full bootstrap: work out how to become root (unless
// Escalate is set), install base packages, ensure the admin user exists,
// install the sudoers drop-in, and seed authorized_keys.
func (p Provisioner) Run(authorizedKeys []string) error {
	admin := p.AdminUser
	if admin == "" {
		admin = DefaultAdminUser
	}

	if p.Escalate == nil {
		escalate, err := p.escalation()
		if err != nil {
			return fmt.Errorf("no root access: %w", err)
		}
		p.Escalate = escalate
	}

	p.step("installing base packages")
	if err := p.run(p.Escalate(AptInstallCommand(BasePackages))); err != nil {
		return fmt.Errorf("install base packages: %w", err)
	}

	// Create the admin user only if it does not already exist and we are not
	// already connected as it (mirrors the script's guard).
	if p.ConnectUser != admin {
		p.step("ensuring admin user " + admin)
		if _, err := p.Exec.Exec(fmt.Sprintf("id -u %s", admin), ""); err != nil {
			if err := p.run(p.Escalate(UseraddCommand(admin))); err != nil {
				return fmt.Errorf("create admin user %s: %w", admin, err)
			}
		}
	}

	p.step("writing sudoers drop-in")
	if err := p.run(p.Escalate(WriteSudoersCommand(admin))); err != nil {
		return fmt.Errorf("write sudoers: %w", err)
	}

	p.step("creating .ssh directory")
	if err := p.run(p.Escalate(EnsureSSHDirCommand(admin))); err != nil {
		return fmt.Errorf("create ssh dir: %w", err)
	}
	p.step("installing authorized keys")
	for _, key := range authorizedKeys {
		if strings.TrimSpace(key) == "" {
			continue
		}
		if err := p.run(p.Escalate(AppendAuthorizedKeyCommand(admin, key))); err != nil {
			return fmt.Errorf("append authorized key: %w", err)
		}
	}
	return nil
}
