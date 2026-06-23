// Package cli wires the keel command tree.
package cli

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	awscloud "github.com/DigitalTolk/keel/internal/cloud/aws"
	"github.com/DigitalTolk/keel/internal/config"
	"github.com/DigitalTolk/keel/internal/log"
	"github.com/DigitalTolk/keel/internal/runner"
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
	log     *log.Logger
	verbose bool

	runnerFactory   func() runner.Runner
	requireTools    func(tools ...string) error
	dialer          func(ssh.Target, ssh.DialOptions) (SSHSession, error)
	scanHostKey     func(host string, port int, timeout time.Duration) (string, error)
	readPassword    func(prompt string) (string, error)
	fetchIP         func(ctx context.Context, url string) (string, error)
	sgClientFactory func(ctx context.Context, opts awscloud.SGOptions) (awscloud.SGClient, error)
	now             func() time.Time
}

// newApp builds an app wired to the real implementations.
func newApp() *app {
	return &app{
		runnerFactory: func() runner.Runner { return runner.Exec{} },
		requireTools:  runner.RequireTools,
		dialer: func(t ssh.Target, o ssh.DialOptions) (SSHSession, error) {
			return ssh.Dial(t, o)
		},
		scanHostKey:     ssh.ScanHostKey,
		readPassword:    promptPassword,
		fetchIP:         fetchPublicIP,
		sgClientFactory: awscloud.NewSecurityGroupClient,
		now:             func() time.Time { return time.Now().UTC() },
	}
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
		Short:         "Company-wide systems operations tool",
		Long:          "keel consolidates the keel server-bootstrap and operations scripts into one binary.",
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
			a.log = log.New()
			return nil
		},
	}

	root.SetVersionTemplate(version.String() + "\n")
	root.PersistentFlags().StringVar(&cfgPath, "config", "", "path to config file")
	root.PersistentFlags().StringVar(&logFormat, "log-format", "", "log format: text|json")
	root.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "verbose error output")

	root.AddCommand(newBootstrapCmd(a))
	root.AddCommand(newBackupCmd(a))
	root.AddCommand(newAWSCmd(a))
	root.AddCommand(newVboxCmd(a))
	root.AddCommand(newJenkinsCmd(a))
	root.AddCommand(newMySQLCmd(a))

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
