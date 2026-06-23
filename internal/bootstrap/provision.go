package bootstrap

import (
	"encoding/base64"
	"fmt"
	"strings"
)

// BasePackages are installed on every bootstrapped host, matching the set the
// original know-hosts.sh installed (sudo, python3, python3-apt, acl).
var BasePackages = []string{"sudo", "python3", "python3-apt", "acl"}

// Executor runs a command on a remote host and returns its stdout. The
// ssh.Client satisfies this; tests use a fake.
type Executor interface {
	Exec(cmd string) (string, error)
}

// SudoWrapper elevates a command to root. Implementations: identity (already
// root), "sudo …" (passwordless), or "echo PASS | sudo -S …" (with password).
type SudoWrapper func(cmd string) string

// AptInstallCommand updates the package cache and installs pkgs without any
// interactive prompts.
func AptInstallCommand(pkgs []string) string {
	return fmt.Sprintf(
		"apt-get -qq update && DEBIAN_FRONTEND=noninteractive apt-get -y -qq install %s",
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
	encoded := base64.StdEncoding.EncodeToString([]byte(SudoersContent(user)))
	return fmt.Sprintf(
		"tmp=$(mktemp) && echo %s | base64 -d > \"$tmp\" && visudo -cf \"$tmp\" && install -m 0440 \"$tmp\" %s && rm -f \"$tmp\"",
		encoded, SudoersPath,
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
	encoded := base64.StdEncoding.EncodeToString([]byte(key))
	path := fmt.Sprintf("/home/%s/.ssh/authorized_keys", user)
	return fmt.Sprintf(
		"touch %s && k=$(echo %s | base64 -d) && grep -qxF \"$k\" %s || echo \"$k\" >> %s; "+
			"chown %s:%s %s && chmod 0640 %s",
		path, encoded, path, path, user, user, path, path,
	)
}

// SudoWrapperFor builds a SudoWrapper appropriate to the connecting user and
// whether a password is available. The command is base64-encoded for transport
// (avoiding quoting issues), then decoded and run remotely:
//
//   - root:         echo <b64> | base64 -d | bash
//   - passwordless: echo <b64> | base64 -d | sudo bash
//   - with password: echo '<pass>' | sudo -S -p ” bash -c "$(echo <b64> | base64 -d)"
func SudoWrapperFor(connectUser, password string) SudoWrapper {
	return func(cmd string) string {
		b64 := base64.StdEncoding.EncodeToString([]byte(cmd))
		switch {
		case connectUser == "root":
			return fmt.Sprintf("echo %s | base64 -d | bash", b64)
		case password != "":
			return fmt.Sprintf("echo '%s' | sudo -S -p '' bash -c \"$(echo %s | base64 -d)\"", password, b64)
		default:
			return fmt.Sprintf("echo %s | base64 -d | sudo bash", b64)
		}
	}
}

// Provisioner performs the remote provisioning steps over an Executor.
type Provisioner struct {
	Exec        Executor
	Sudo        SudoWrapper
	AdminUser   string // privileged user to ensure (default "bofh")
	ConnectUser string // the user we connected as
}

func (p Provisioner) sudo(cmd string) string {
	if p.Sudo == nil {
		return cmd
	}
	return p.Sudo(cmd)
}

func (p Provisioner) run(cmd string) error {
	_, err := p.Exec.Exec(cmd)
	return err
}

// Run executes the full bootstrap: install base packages, ensure the admin
// user exists, install the sudoers drop-in, and seed authorized_keys.
func (p Provisioner) Run(authorizedKeys []string) error {
	admin := p.AdminUser
	if admin == "" {
		admin = DefaultAdminUser
	}

	if err := p.run(p.sudo(AptInstallCommand(BasePackages))); err != nil {
		return fmt.Errorf("install base packages: %w", err)
	}

	// Create the admin user only if it does not already exist and we are not
	// already connected as it (mirrors the script's guard).
	if p.ConnectUser != admin {
		if _, err := p.Exec.Exec(fmt.Sprintf("id -u %s", admin)); err != nil {
			if err := p.run(p.sudo(UseraddCommand(admin))); err != nil {
				return fmt.Errorf("create admin user %s: %w", admin, err)
			}
		}
	}

	if err := p.run(p.sudo(WriteSudoersCommand(admin))); err != nil {
		return fmt.Errorf("write sudoers: %w", err)
	}

	if err := p.run(p.sudo(EnsureSSHDirCommand(admin))); err != nil {
		return fmt.Errorf("create ssh dir: %w", err)
	}
	for _, key := range authorizedKeys {
		if strings.TrimSpace(key) == "" {
			continue
		}
		if err := p.run(p.sudo(AppendAuthorizedKeyCommand(admin, key))); err != nil {
			return fmt.Errorf("append authorized key: %w", err)
		}
	}
	return nil
}
