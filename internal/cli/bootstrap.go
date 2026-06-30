package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/DigitalTolk/keel/internal/bootstrap"
	"github.com/DigitalTolk/keel/internal/config"
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
		user       string
		port       int
		jump       string
		askPass    bool
		identities []string
		pubkeys    []string
		pubkeyFile string
		adminUser  string
	)
	cmd := &cobra.Command{
		Use:   "bootstrap [HOST...]",
		Short: "Bootstrap hosts for Ansible: create the admin user, seed sudoers + ssh keys",
		Long: "Bootstrap one or more hosts for Ansible: install base packages, create the\n" +
			"admin user with passwordless sudo, and seed its authorized_keys.\n\n" +
			"In a terminal this opens a guided, full-screen form (pre-filled with any\n" +
			"HOST arguments and flags you pass) that also shows live progress. Piped or\n" +
			"non-interactive, it runs straight from the flags.",
		RunE: func(cmd *cobra.Command, args []string) error {
			p := bootstrapParams{
				hosts:      args,
				user:       user,
				port:       port,
				jump:       jump,
				identities: identities,
				adminUser:  adminUser,
			}

			// Seed keys from flags (used to pre-fill the form and to run directly).
			seedKeys := append([]string{}, pubkeys...)
			if pubkeyFile != "" {
				more, err := collectPubkeys(nil, pubkeyFile)
				if err != nil {
					if !a.interactive() {
						return err
					}
				} else {
					seedKeys = append(seedKeys, more...)
				}
			}
			p.keys = seedKeys

			if a.interactive() {
				// Pre-fill the form from ~/.ssh/config for the first host alias.
				if len(p.hosts) > 0 {
					hc := ssh.ResolveHost(p.hosts[0])
					if p.user == "" {
						p.user = hc.User
					}
					if p.port == 0 {
						p.port = hc.Port
					}
					if p.jump == "" {
						p.jump = hc.ProxyJump
					}
					p.identities = append(p.identities, hc.IdentityFiles...)
				}
				return a.tui(a, p)
			}

			// Non-interactive: defaults + ssh_config resolve per host at dial time
			// (see resolveTarget); here we only need the password and a host.
			switch {
			case askPass:
				pw, err := a.readPassword(fmt.Sprintf("ssh/sudo password for %q (hidden): ", firstNonEmpty(p.user, a.cfg.SSH.User)))
				if err != nil {
					return err
				}
				p.password = pw
			case os.Getenv("KEEL_SSH_PASSWORD") != "":
				p.password = os.Getenv("KEEL_SSH_PASSWORD")
			}
			if len(p.hosts) == 0 {
				return errors.New("no HOST given: pass HOST arguments, or run keel in a terminal for guided setup")
			}
			return runBootstrap(a, p)
		},
	}
	cmd.Flags().StringVarP(&user, "user", "u", "", "initial ssh user (default from config: bofh)")
	cmd.Flags().IntVarP(&port, "port", "p", 0, "ssh port (default from config: 22)")
	cmd.Flags().StringVarP(&jump, "jump", "J", "", "jump host (user@host#port)")
	cmd.Flags().BoolVar(&askPass, "ask-pass", false, "prompt for ssh/sudo password (hidden); KEEL_SSH_PASSWORD env is used otherwise")
	cmd.Flags().StringArrayVarP(&identities, "identity", "i", nil, "private key file for ssh auth (repeatable)")
	cmd.Flags().StringArrayVar(&pubkeys, "pubkey", nil, "authorized public key to install (repeatable)")
	cmd.Flags().StringVar(&pubkeyFile, "pubkey-file", "", "file containing public keys, one per line")
	cmd.Flags().StringVar(&adminUser, "admin-user", bootstrap.DefaultAdminUser, "privileged user to create")
	return cmd
}

type bootstrapParams struct {
	hosts      []string
	user       string
	port       int
	jump       string
	password   string
	identities []string
	keys       []string
	adminUser  string
}

// runBootstrap provisions each host directly (non-interactive path). The
// interactive path provisions inside the TUI instead (see tui.go).
func runBootstrap(a *app, p bootstrapParams) error {
	for _, host := range p.hosts {
		a.log.Info("bootstrapping host", "host", host)

		target, opts := a.resolveTarget(host, p)
		client, err := a.dialer(target, opts)
		if err != nil {
			return fmt.Errorf("connect %s: %w", host, err)
		}

		prov := bootstrap.Provisioner{
			Exec:        client,
			Sudo:        bootstrap.SudoWrapperFor(target.User, p.password),
			AdminUser:   p.adminUser,
			ConnectUser: target.User,
		}
		if err := prov.Run(p.keys); err != nil {
			client.Close()
			return fmt.Errorf("provision %s: %w", host, err)
		}
		client.Close()
		a.log.Info("bootstrapped host", "host", host)
	}
	a.log.Info("bootstrap complete", "hosts", len(p.hosts))
	return nil
}

// resolveTarget merges ~/.ssh/config for host with the explicit params and
// keel's config-file defaults, returning the effective dial Target and options.
// Precedence: explicit flag/TUI value > ssh_config > keel config/defaults.
func (a *app) resolveTarget(host string, p bootstrapParams) (ssh.Target, ssh.DialOptions) {
	hc := ssh.ResolveHost(host)
	hostName := host
	if hc.HostName != "" {
		hostName = hc.HostName
	}
	target := ssh.Target{
		User: firstNonEmpty(p.user, hc.User, a.cfg.SSH.User),
		Host: hostName,
		Port: firstPositive(p.port, hc.Port, a.cfg.SSH.Port, ssh.DefaultPort),
	}
	opts := ssh.DialOptions{
		Password: p.password,
		KeyFiles: append(append([]string{}, p.identities...), hc.IdentityFiles...),
		JumpHost: firstNonEmpty(p.jump, hc.ProxyJump, a.cfg.SSH.JumpHost),
		Timeout:  30 * time.Second,
	}
	return target, opts
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func firstPositive(vals ...int) int {
	for _, v := range vals {
		if v > 0 {
			return v
		}
	}
	return 0
}

// --- guided-setup field mapping (shared with the TUI) ------------------------

// bootstrapFields holds the string-typed values the TUI form binds to. It maps
// to a bootstrapParams via toParams once submitted.
type bootstrapFields struct {
	hosts      string
	user       string
	password   string // blank => SSH-key auth
	pubkeys    string // authorized keys to install, one per line
	port       string
	adminUser  string
	jump       string
	identities string
	pubkeyFile string
}

// newBootstrapFields seeds the form with any values already supplied on the
// command line, falling back to config defaults so the prompts are pre-filled.
func newBootstrapFields(p bootstrapParams, cfg config.Config) bootstrapFields {
	user := p.user
	if user == "" {
		user = cfg.SSH.User
	}
	port := p.port
	if port == 0 {
		port = cfg.SSH.Port
	}
	jump := p.jump
	if jump == "" {
		jump = cfg.SSH.JumpHost
	}
	admin := p.adminUser
	if admin == "" {
		admin = bootstrap.DefaultAdminUser
	}
	return bootstrapFields{
		hosts:      strings.Join(p.hosts, " "),
		user:       user,
		pubkeys:    strings.Join(p.keys, "\n"),
		port:       strconv.Itoa(port),
		adminUser:  admin,
		jump:       jump,
		identities: strings.Join(p.identities, " "),
	}
}

// toParams maps submitted form fields to a bootstrapParams, resolving the
// authorized keys (inline + file) along the way. A non-empty password selects
// password auth; otherwise key auth is used.
func (f bootstrapFields) toParams() (bootstrapParams, error) {
	hosts := splitList(f.hosts)
	if len(hosts) == 0 {
		return bootstrapParams{}, errors.New("at least one host is required")
	}
	port := ssh.DefaultPort
	if s := strings.TrimSpace(f.port); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil {
			return bootstrapParams{}, fmt.Errorf("invalid port %q", f.port)
		}
		port = n
	}
	admin := strings.TrimSpace(f.adminUser)
	if admin == "" {
		admin = bootstrap.DefaultAdminUser
	}

	var inline []string
	for _, line := range strings.Split(f.pubkeys, "\n") {
		if s := strings.TrimSpace(line); s != "" {
			inline = append(inline, s)
		}
	}
	keys, err := collectPubkeys(inline, strings.TrimSpace(f.pubkeyFile))
	if err != nil {
		return bootstrapParams{}, err
	}

	p := bootstrapParams{
		hosts:     hosts,
		user:      strings.TrimSpace(f.user),
		port:      port,
		jump:      strings.TrimSpace(f.jump),
		adminUser: admin,
		keys:      keys,
		password:  f.password,
	}
	if s := strings.TrimSpace(f.identities); s != "" {
		p.identities = splitList(s)
	}
	return p, nil
}

// splitList splits a comma/space/tab/newline-separated list, dropping empties.
func splitList(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\n' || r == '\r'
	})
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
