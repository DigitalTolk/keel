package bootstrap

import (
	"cmp"
	"encoding/base64"
	"fmt"
)

// Command is a remote command line and the input written to its stdin. A
// password travels in Stdin, so it never appears on a command line: not in the
// host's process list and not in an error message.
type Command struct {
	Line  string
	Stdin string
}

// Escalator turns a command into one that runs as root.
type Escalator func(cmd string) Command

// AsRoot runs commands for a connection that is already root. Every escalator
// sends the command base64-encoded and decodes it on the host, so quoting and
// shell metacharacters survive the trip.
func AsRoot(cmd string) Command {
	return Command{Line: fmt.Sprintf("echo %s | base64 -d | bash", encode(cmd))}
}

// ViaSudo runs commands through sudo. With a password, sudo reads it from
// stdin (-S, with an empty prompt), so the command itself cannot come through
// stdin and is decoded into bash -c instead.
func ViaSudo(password string) Escalator {
	return func(cmd string) Command {
		if password == "" {
			return Command{Line: fmt.Sprintf("echo %s | base64 -d | sudo bash", encode(cmd))}
		}
		return Command{
			Line:  fmt.Sprintf("sudo -S -p '' bash -c \"$(echo %s | base64 -d)\"", encode(cmd)),
			Stdin: password + "\n",
		}
	}
}

// ViaSu runs commands through su with root's password on stdin, for a user who
// may not use sudo. su prints its "Password:" prompt on stderr, so su's own
// stderr is discarded and the command gets the session's stderr back through
// fd 3. A login shell (-l) gives root's PATH, which has the sbin directories.
func ViaSu(password string) Escalator {
	return func(cmd string) Command {
		return Command{
			Line:  fmt.Sprintf("su -l -c 'exec 2>&3 3>&-; echo %s | base64 -d | bash' root 3>&2 2>/dev/null", encode(cmd)),
			Stdin: password + "\n",
		}
	}
}

// escalation works out how ConnectUser becomes root: directly when it is root,
// through sudo when the user may use it, and otherwise through su with root's
// password. Each way is tried with `true`, so nothing on the host has changed
// when none of them works.
func (p Provisioner) escalation() (Escalator, error) {
	if p.ConnectUser == "root" {
		return AsRoot, nil
	}
	p.step("checking root access")
	sudo := ViaSudo(p.Password)
	sudoErr := p.try(sudo)
	if sudoErr == nil {
		p.output("using sudo")
		return sudo, nil
	}
	password := cmp.Or(p.RootPassword, p.Password)
	if password == "" {
		return nil, fmt.Errorf("sudo: %v; su needs root's password", sudoErr)
	}
	su := ViaSu(password)
	if err := p.try(su); err != nil {
		return nil, fmt.Errorf("sudo: %v; su: %v", sudoErr, err)
	}
	p.output("using su")
	return su, nil
}

// try runs `true` as root through escalate.
func (p Provisioner) try(escalate Escalator) error {
	c := escalate("true")
	_, err := p.Exec.Exec(c.Line, c.Stdin)
	return err
}

func encode(cmd string) string {
	return base64.StdEncoding.EncodeToString([]byte(cmd))
}
