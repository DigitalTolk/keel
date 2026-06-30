// Package bootstrap reproduces the server-preparation logic of know-hosts.sh:
// adding hosts to known_hosts, creating the privileged "bofh" user with a
// passwordless sudoers drop-in, and seeding authorized_keys.
package bootstrap

import "fmt"

// SudoersPath is the drop-in file written on each host.
const SudoersPath = "/etc/sudoers.d/100-no-pass-users"

// DefaultAdminUser is the privileged user created during bootstrap.
const DefaultAdminUser = "bofh"

// SudoersContent returns the contents of the sudoers drop-in granting the
// given user passwordless sudo and disabling requiretty.
func SudoersContent(user string) string {
	return fmt.Sprintf("%s ALL=(ALL) NOPASSWD:ALL\nDefaults:%s !requiretty\n", user, user)
}
