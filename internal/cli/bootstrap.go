package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/DigitalTolk/keel/internal/bootstrap"
	"github.com/DigitalTolk/keel/internal/ssh"
)

func newKnownHostsCmd(a *app) *cobra.Command {
	var port int
	cmd := &cobra.Command{
		Use:   "known-hosts HOST [HOST...]",
		Short: "Scan host keys and add them to ~/.ssh/known_hosts",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if port == 0 {
				port = a.cfg.SSH.Port
			}
			return runKnownHosts(a, args, port)
		},
	}
	cmd.Flags().IntVarP(&port, "port", "p", 0, "ssh port (default from config)")
	return cmd
}

func runKnownHosts(a *app, hosts []string, port int) error {
	path, err := knownHostsPath()
	if err != nil {
		return err
	}

	for _, host := range hosts {
		a.log.Info("scanning host keys", "host", host, "port", port)
		line, err := a.scanHostKey(host, port, 30*time.Second)
		if err != nil {
			return fmt.Errorf("scan %s: %w", host, err)
		}

		existing, err := os.ReadFile(path)
		if err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("read %s: %w", path, err)
		}
		updated := ssh.UpsertKnownHostsLine(existing, line)
		if err := writeFileAtomic(path, updated, 0o600); err != nil {
			return fmt.Errorf("write %s: %w", path, err)
		}
		a.log.Info("added to known_hosts", "host", host)
	}
	return nil
}

func newBootstrapCmd(a *app) *cobra.Command {
	var (
		user          string
		port          int
		jump          string
		askPass       bool
		identities    []string
		pubkeys       []string
		pubkeyFile    string
		inventoryPath string
		adminUser     string
	)
	cmd := &cobra.Command{
		Use:   "bootstrap HOST [HOST...]",
		Short: "Bootstrap hosts for Ansible: create the admin user, seed sudoers + ssh keys, write an inventory",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if user == "" {
				user = a.cfg.SSH.User
			}
			if port == 0 {
				port = a.cfg.SSH.Port
			}
			if jump == "" {
				jump = a.cfg.SSH.JumpHost
			}

			var password string
			switch {
			case askPass:
				pw, err := a.readPassword(fmt.Sprintf("ssh/sudo password for %q (hidden): ", user))
				if err != nil {
					return err
				}
				password = pw
			case os.Getenv("KEEL_SSH_PASSWORD") != "":
				password = os.Getenv("KEEL_SSH_PASSWORD")
			}

			keys, err := collectPubkeys(pubkeys, pubkeyFile)
			if err != nil {
				return err
			}

			return runBootstrap(a, bootstrapParams{
				hosts:         args,
				user:          user,
				port:          port,
				jump:          jump,
				password:      password,
				identities:    identities,
				keys:          keys,
				adminUser:     adminUser,
				inventoryPath: inventoryPath,
			})
		},
	}
	cmd.Flags().StringVarP(&user, "user", "u", "", "initial ssh user (default from config: bofh)")
	cmd.Flags().IntVarP(&port, "port", "p", 0, "ssh port (default from config: 22)")
	cmd.Flags().StringVarP(&jump, "jump", "J", "", "jump host (user@host#port)")
	cmd.Flags().BoolVar(&askPass, "ask-pass", false, "prompt for ssh/sudo password (hidden); KEEL_SSH_PASSWORD env is used otherwise")
	cmd.Flags().StringArrayVarP(&identities, "identity", "i", nil, "private key file for ssh auth (repeatable)")
	cmd.Flags().StringArrayVar(&pubkeys, "pubkey", nil, "authorized public key to install (repeatable)")
	cmd.Flags().StringVar(&pubkeyFile, "pubkey-file", "", "file containing public keys, one per line")
	cmd.Flags().StringVar(&inventoryPath, "inventory", "inventory", "path to write the Ansible inventory")
	cmd.Flags().StringVar(&adminUser, "admin-user", bootstrap.DefaultAdminUser, "privileged user to create")
	return cmd
}

type bootstrapParams struct {
	hosts         []string
	user          string
	port          int
	jump          string
	password      string
	identities    []string
	keys          []string
	adminUser     string
	inventoryPath string
}

func runBootstrap(a *app, p bootstrapParams) error {
	var inventory []string

	for _, host := range p.hosts {
		a.log.Info("bootstrapping host", "host", host)

		target := ssh.Target{User: p.user, Host: host, Port: p.port}
		client, err := a.dialer(target, ssh.DialOptions{
			Password: p.password,
			KeyFiles: p.identities,
			JumpHost: p.jump,
			Timeout:  30 * time.Second,
		})
		if err != nil {
			return fmt.Errorf("connect %s: %w", host, err)
		}

		prov := bootstrap.Provisioner{
			Exec:        client,
			Sudo:        bootstrap.SudoWrapperFor(p.user, p.password),
			AdminUser:   p.adminUser,
			ConnectUser: p.user,
		}
		if err := prov.Run(p.keys); err != nil {
			client.Close()
			return fmt.Errorf("provision %s: %w", host, err)
		}
		client.Close()

		// After bootstrap, Ansible connects as the admin user (which now holds
		// the seeded keys and passwordless sudo).
		inventory = append(inventory, bootstrap.InventoryHost{
			Host: host,
			Port: p.port,
			User: p.adminUser,
		}.Line())
		a.log.Info("bootstrapped host", "host", host)
	}

	content := strings.Join(inventory, "\n") + "\n"
	if err := writeFileAtomic(p.inventoryPath, []byte(content), 0o644); err != nil {
		return fmt.Errorf("write inventory: %w", err)
	}
	a.log.Info("wrote inventory", "path", p.inventoryPath)
	return nil
}

func collectPubkeys(flags []string, file string) ([]string, error) {
	keys := append([]string{}, flags...)
	if file != "" {
		data, err := os.ReadFile(file)
		if err != nil {
			return nil, fmt.Errorf("read pubkey file: %w", err)
		}
		for _, line := range strings.Split(string(data), "\n") {
			if s := strings.TrimSpace(line); s != "" {
				keys = append(keys, s)
			}
		}
	}
	return keys, nil
}

func promptPassword(prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt)
	b, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", fmt.Errorf("read password: %w", err)
	}
	return string(b), nil
}

func knownHostsPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".ssh", "known_hosts"), nil
}

func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, mode); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
