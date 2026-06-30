// Package cli wires the keel command tree.
package cli

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/DigitalTolk/keel/internal/config"
	"github.com/DigitalTolk/keel/internal/ssh"
	"github.com/DigitalTolk/keel/internal/version"
)

// SSHSession is the subset of *ssh.Client that the bootstrap commands need.
// Defining it here lets tests substitute a fake instead of a live connection.
type SSHSession interface {
	Exec(cmd string) (string, error)
	Close() error
}

// app holds shared state resolved once in PersistentPreRunE and used by
// subcommands. The function fields are seams: they default to the real
// implementations and are swapped for fakes in tests.
type app struct {
	cfg     config.Config
	log     *slog.Logger
	verbose bool

	dialer       func(ssh.Target, ssh.DialOptions) (SSHSession, error)
	scanHostKey  func(host string, port int, timeout time.Duration) (string, error)
	readPassword func(prompt string) (string, error)
}

// newApp builds an app wired to the real implementations.
func newApp() *app {
	return &app{
		dialer: func(t ssh.Target, o ssh.DialOptions) (SSHSession, error) {
			return ssh.Dial(t, o)
		},
		scanHostKey:  ssh.ScanHostKey,
		readPassword: promptPassword,
	}
}

// newLogger builds an slog logger writing to w. format "json" selects the
// structured JSON handler; anything else uses the human-friendly text handler.
func newLogger(format string, w io.Writer) *slog.Logger {
	var h slog.Handler
	if format == "json" {
		h = slog.NewJSONHandler(w, nil)
	} else {
		h = slog.NewTextHandler(w, nil)
	}
	return slog.New(h)
}

func newRootCmd() *cobra.Command { return buildRoot(newApp()) }

// buildRoot assembles the command tree around a given app. Tests pass an app
// with fake seams and a pre-set logger/config; in that case config loading and
// logger construction are skipped.
func buildRoot(a *app) *cobra.Command {
	var (
		cfgPath   string
		logFormat string
		verbose   bool
	)

	root := &cobra.Command{
		Use:           "keel",
		Short:         "Bootstrap fresh servers for management with Ansible",
		Long:          "keel prepares a fresh machine for Ansible: it scans host keys, creates the admin user with a passwordless sudoers drop-in, seeds SSH keys, and writes an inventory.",
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       version.Version,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			a.verbose = verbose
			if a.log != nil {
				return nil // pre-configured (tests)
			}
			cfg, err := config.Load(cfgPath)
			if err != nil {
				return err
			}
			if logFormat != "" {
				cfg.Log.Format = logFormat
			}
			a.cfg = cfg
			a.log = newLogger(cfg.Log.Format, os.Stderr)
			return nil
		},
	}

	root.SetVersionTemplate(version.String() + "\n")
	root.PersistentFlags().StringVar(&cfgPath, "config", "", "path to config file")
	root.PersistentFlags().StringVar(&logFormat, "log-format", "", "log format: text|json")
	root.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "verbose error output")

	root.AddCommand(newBootstrapCmd(a))
	root.AddCommand(newKnownHostsCmd(a))

	return root
}

// Execute runs the root command and returns a process exit code.
func Execute() int {
	root := newRootCmd()
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "keel: "+err.Error())
		return 1
	}
	return 0
}
