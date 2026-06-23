// Package bootstrap reproduces the server-preparation logic of know-hosts.sh:
// adding hosts to known_hosts, creating the privileged "bofh" user with a
// passwordless sudoers drop-in, seeding authorized_keys, and emitting an
// Ansible inventory.
package bootstrap

import "fmt"

// DefaultPythonInterpreter matches the ${PYTHON_BIN:-/usr/bin/python3} default
// in the original script.
const DefaultPythonInterpreter = "/usr/bin/python3"

// SudoersPath is the drop-in file written on each host.
const SudoersPath = "/etc/sudoers.d/100-no-pass-users"

// DefaultAdminUser is the privileged user created during bootstrap.
const DefaultAdminUser = "bofh"

// InventoryHost renders a single Ansible inventory line for a host.
type InventoryHost struct {
	Host              string
	Port              int
	User              string
	Password          string // when set, adds ansible_password / ansible_become_password
	PythonInterpreter string // defaults to DefaultPythonInterpreter
}

// Line renders the inventory entry, matching the format written to
// inventory-temp by know-hosts.sh.
func (h InventoryHost) Line() string {
	python := h.PythonInterpreter
	if python == "" {
		python = DefaultPythonInterpreter
	}
	if h.Password != "" {
		return fmt.Sprintf(
			"[%s]:%d ansible_user=%s ansible_password=%s ansible_become_password=%s ansible_python_interpreter=%s",
			h.Host, h.Port, h.User, h.Password, h.Password, python,
		)
	}
	return fmt.Sprintf(
		"[%s]:%d ansible_user=%s ansible_python_interpreter=%s",
		h.Host, h.Port, h.User, python,
	)
}

// SudoersContent returns the contents of the sudoers drop-in granting the
// given user passwordless sudo and disabling requiretty.
func SudoersContent(user string) string {
	return fmt.Sprintf("%s ALL=(ALL) NOPASSWD:ALL\nDefaults:%s !requiretty\n", user, user)
}
