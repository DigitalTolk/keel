package cli

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/DigitalTolk/keel/internal/backup"
	awscloud "github.com/DigitalTolk/keel/internal/cloud/aws"
	"github.com/DigitalTolk/keel/internal/config"
	"github.com/DigitalTolk/keel/internal/runner"
)

func newBackupCmd(a *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "backup",
		Short: "Create and rotate database/file backups",
	}
	cmd.AddCommand(
		newBackupMySQLCmd(a),
		newBackupJenkinsCmd(a),
		newBackupRsyncCmd(a),
		newBackupSftpCmd(a),
		newBackupPurgeCmd(a),
		newBackupRunCmd(a),
	)
	return cmd
}

// runSyncThenArchive runs an external sync tool to populate workDir, then
// archives + uploads + rotates it via DirBackup. Shared by rsync and sftp.
func (a *app) runSyncThenArchive(ctx context.Context, tool string, args []string, workDir string, df destFlags, filePrefix string, keep int) error {
	if err := a.requireTools(tool); err != nil {
		return err
	}
	if err := os.MkdirAll(workDir, 0o750); err != nil {
		return fmt.Errorf("create work dir: %w", err)
	}
	a.log.Info(fmt.Sprintf("syncing into %s with %s", workDir, tool))
	if err := a.runnerFactory().Stream(ctx, io.Discard, tool, args...); err != nil {
		return err
	}
	dest, err := a.buildDestination(ctx, df.toConfig())
	if err != nil {
		return err
	}
	key, err := backup.DirBackup{
		Root: workDir, Dest: dest, KeyPrefix: df.prefix, FilePrefix: filePrefix, Keep: keep, Now: a.now,
	}.Run(ctx)
	if err != nil {
		return err
	}
	a.log.Success(fmt.Sprintf("stored %s", key))
	return nil
}

func newBackupRsyncCmd(a *app) *cobra.Command {
	var (
		df               destFlags
		host, user, path string
		workDir, keyFile string
		filePrefix       string
		port, keep       int
		sudo             bool
	)
	cmd := &cobra.Command{
		Use:   "rsync",
		Short: "rsync a remote directory over SSH, then archive it to a destination",
		RunE: func(cmd *cobra.Command, args []string) error {
			host = firstNonEmpty(host, os.Getenv("SOURCE_SSH_HOST"))
			user = firstNonEmpty(user, os.Getenv("SOURCE_SSH_USER"))
			path = firstNonEmpty(path, os.Getenv("SOURCE_SSH_PATH"))
			workDir = firstNonEmpty(workDir, os.Getenv("TARGET_PATH"))
			keyFile = firstNonEmpty(keyFile, os.Getenv("SOURCE_SSH_KEYFILE"))
			filePrefix = firstNonEmpty(filePrefix, os.Getenv("FILE_PREFIX"), "bak")
			if port == 0 {
				port = atoiOr(os.Getenv("SOURCE_SSH_PORT"), 22)
			}
			if !sudo && os.Getenv("RSYNC_SUDO") != "" {
				sudo = true
			}
			df.bucket = firstNonEmpty(df.bucket, os.Getenv("S3_BUCKET"), os.Getenv("B2_BUCKET"))
			df.prefix = firstNonEmpty(df.prefix, os.Getenv("S3_PREFIX"), os.Getenv("B2_FOLDER"))
			df.profile = firstNonEmpty(df.profile, os.Getenv("AWS_PROFILE"))

			if host == "" || user == "" || path == "" || workDir == "" {
				return fmt.Errorf("--source-host, --source-user, --source-path and --work-dir are required")
			}
			rargs := backup.RsyncArgs(backup.RsyncOptions{
				User: user, Host: host, Path: path, Dest: workDir, Port: port, KeyFile: keyFile, Sudo: sudo,
			})
			return a.runSyncThenArchive(cmd.Context(), "rsync", rargs, workDir, df, filePrefix, keep)
		},
	}
	df.register(cmd.Flags())
	cmd.Flags().StringVar(&host, "source-host", "", "remote host (SOURCE_SSH_HOST)")
	cmd.Flags().StringVar(&user, "source-user", "", "remote user (SOURCE_SSH_USER)")
	cmd.Flags().StringVar(&path, "source-path", "", "remote path (SOURCE_SSH_PATH)")
	cmd.Flags().IntVar(&port, "source-port", 0, "remote ssh port (SOURCE_SSH_PORT, default 22)")
	cmd.Flags().StringVarP(&keyFile, "identity", "i", "", "ssh identity file (SOURCE_SSH_KEYFILE)")
	cmd.Flags().BoolVar(&sudo, "sudo", false, "run the remote rsync via sudo (RSYNC_SUDO)")
	cmd.Flags().StringVar(&workDir, "work-dir", "", "local sync directory (TARGET_PATH)")
	cmd.Flags().StringVar(&filePrefix, "file-prefix", "", "archive filename prefix (default bak)")
	cmd.Flags().IntVar(&keep, "keep", 0, "backups to retain (0 = keep all)")
	return cmd
}

func newBackupSftpCmd(a *app) *cobra.Command {
	var (
		df               destFlags
		host, user, path string
		workDir, extra   string
		filePrefix       string
		port, parallel   int
		keep             int
	)
	cmd := &cobra.Command{
		Use:   "sftp",
		Short: "Mirror a remote directory over SFTP (lftp), then archive it to a destination",
		RunE: func(cmd *cobra.Command, args []string) error {
			host = firstNonEmpty(host, os.Getenv("SOURCE_SFTP_HOST"))
			user = firstNonEmpty(user, os.Getenv("SOURCE_SFTP_USER"))
			path = firstNonEmpty(path, os.Getenv("SOURCE_SFTP_PATH"))
			workDir = firstNonEmpty(workDir, os.Getenv("TARGET_PATH"))
			extra = firstNonEmpty(extra, os.Getenv("MIRROR_EXTRA_ARGS"))
			filePrefix = firstNonEmpty(filePrefix, os.Getenv("FILE_PREFIX"), "bak")
			if port == 0 {
				port = atoiOr(os.Getenv("SOURCE_SFTP_PORT"), 22)
			}
			if parallel == 0 {
				parallel = atoiOr(os.Getenv("MIRROR_PARALLEL"), 8)
			}
			df.bucket = firstNonEmpty(df.bucket, os.Getenv("S3_BUCKET"), os.Getenv("B2_BUCKET"))
			df.prefix = firstNonEmpty(df.prefix, os.Getenv("S3_PREFIX"), os.Getenv("B2_FOLDER"))
			df.profile = firstNonEmpty(df.profile, os.Getenv("AWS_PROFILE"))

			if host == "" || user == "" || path == "" || workDir == "" {
				return fmt.Errorf("--source-host, --source-user, --source-path and --work-dir are required")
			}
			largs := backup.LftpMirrorArgs(backup.SftpOptions{
				User: user, Host: host, Path: path, Dest: workDir, Port: port, Parallel: parallel, ExtraArgs: extra,
			})
			return a.runSyncThenArchive(cmd.Context(), "lftp", largs, workDir, df, filePrefix, keep)
		},
	}
	df.register(cmd.Flags())
	cmd.Flags().StringVar(&host, "source-host", "", "remote host (SOURCE_SFTP_HOST)")
	cmd.Flags().StringVar(&user, "source-user", "", "remote user (SOURCE_SFTP_USER)")
	cmd.Flags().StringVar(&path, "source-path", "", "remote path (SOURCE_SFTP_PATH)")
	cmd.Flags().IntVar(&port, "source-port", 0, "remote sftp port (SOURCE_SFTP_PORT, default 22)")
	cmd.Flags().IntVar(&parallel, "parallel", 0, "parallel transfers (MIRROR_PARALLEL, default 8)")
	cmd.Flags().StringVar(&extra, "extra", "", "extra lftp mirror args (MIRROR_EXTRA_ARGS)")
	cmd.Flags().StringVar(&workDir, "work-dir", "", "local sync directory (TARGET_PATH)")
	cmd.Flags().StringVar(&filePrefix, "file-prefix", "", "archive filename prefix (default bak)")
	cmd.Flags().IntVar(&keep, "keep", 0, "backups to retain (0 = keep all)")
	return cmd
}

// destFlags holds the destination-related flags shared by subcommands.
type destFlags struct {
	kind     string
	bucket   string
	prefix   string
	baseDir  string
	region   string
	endpoint string
	kmsKey   string
	profile  string
}

func (df *destFlags) register(f *pflag.FlagSet) {
	f.StringVar(&df.kind, "dest", "local", "destination: local|s3|b2")
	f.StringVar(&df.bucket, "bucket", "", "bucket (s3/b2)")
	f.StringVar(&df.prefix, "prefix", "", "key prefix / folder")
	f.StringVar(&df.baseDir, "base-dir", ".", "base directory (local dest)")
	f.StringVar(&df.region, "region", "", "region (s3/b2)")
	f.StringVar(&df.endpoint, "endpoint", "", "custom endpoint (b2/minio)")
	f.StringVar(&df.kmsKey, "kms-key", "", "SSE-KMS key id (s3)")
	f.StringVar(&df.profile, "profile", "", "aws profile")
}

func (df *destFlags) toConfig() config.DestConfig {
	return config.DestConfig{
		Kind: df.kind, Bucket: df.bucket, Prefix: df.prefix, BaseDir: df.baseDir,
		Region: df.region, Endpoint: df.endpoint, KMSKey: df.kmsKey, Profile: df.profile,
	}
}

// buildDestination constructs a backup.Destination from a DestConfig.
func (a *app) buildDestination(ctx context.Context, dc config.DestConfig) (backup.Destination, error) {
	switch dc.Kind {
	case "", "local":
		base := dc.BaseDir
		if base == "" {
			base = "."
		}
		return backup.NewLocalDestination(base), nil
	case "s3", "b2":
		profile := dc.Profile
		if profile == "" {
			profile = a.cfg.AWS.Profile
		}
		opts := awscloud.S3Options{
			Bucket: dc.Bucket, Region: dc.Region, Endpoint: dc.Endpoint,
			KMSKeyID: dc.KMSKey, Profile: profile,
			UsePathStyle: dc.Kind == "b2" || dc.Endpoint != "",
		}
		// Map the legacy B2 env contract onto S3-compatible static credentials.
		if dc.Kind == "b2" {
			if id := os.Getenv("B2_APPLICATION_KEY_ID"); id != "" {
				opts.AccessKeyID = id
				opts.SecretAccessKey = os.Getenv("B2_APPLICATION_KEY")
			}
		}
		return awscloud.NewS3Destination(ctx, opts)
	default:
		return nil, fmt.Errorf("unknown dest kind %q (want local|s3|b2)", dc.Kind)
	}
}

func newBackupMySQLCmd(a *app) *cobra.Command {
	var (
		df           destFlags
		host, user   string
		db, passEnv  string
		port, keep   int
		filePrefix   string
		allDatabases bool
	)
	cmd := &cobra.Command{
		Use:   "mysql",
		Short: "Dump a MySQL database (or all) to a destination, with rotation",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Legacy env fallbacks.
			host = firstNonEmpty(host, os.Getenv("MYSQL_HOST"))
			user = firstNonEmpty(user, os.Getenv("MYSQL_USER"))
			db = firstNonEmpty(db, os.Getenv("MYSQL_DB"))
			if port == 0 {
				port = atoiOr(os.Getenv("MYSQL_PORT"), 3306)
			}
			df.bucket = firstNonEmpty(df.bucket, os.Getenv("S3_BUCKET"), os.Getenv("B2_BUCKET"))
			df.prefix = firstNonEmpty(df.prefix, os.Getenv("S3_PREFIX"), os.Getenv("B2_FOLDER"))
			df.kmsKey = firstNonEmpty(df.kmsKey, os.Getenv("S3_KMS_KEY"))
			df.profile = firstNonEmpty(df.profile, os.Getenv("AWS_PROFILE"))
			filePrefix = firstNonEmpty(filePrefix, os.Getenv("FILE_PREFIX"), db)
			if keep == 0 {
				keep = atoiOr(os.Getenv("NUM_BACKUPS"), 7)
			}

			password := resolveMySQLPassword(passEnv)
			tools := []string{"mysqldump"}
			if allDatabases {
				tools = append(tools, "mysql") // listing tables needs the mysql client too
			}
			if err := a.requireTools(tools...); err != nil {
				return err
			}
			if host == "" {
				return fmt.Errorf("--host (or MYSQL_HOST) is required")
			}

			dest, err := a.buildDestination(cmd.Context(), df.toConfig())
			if err != nil {
				return err
			}

			dbs := []string{db}
			if allDatabases {
				dbs, err = listDatabases(cmd.Context(), a.runnerFactory(), host, port, user, password)
				if err != nil {
					return err
				}
			} else if db == "" {
				return fmt.Errorf("--db (or MYSQL_DB) is required unless --all-databases")
			}

			for _, name := range dbs {
				prefix := df.prefix
				fp := filePrefix
				if allDatabases {
					prefix = df.prefix + name + "/"
					fp = name
				}
				job := backup.MySQLBackup{
					Runner:     a.runnerFactory(),
					Dest:       dest,
					Config:     backup.MySQLConfig{Host: host, Port: port, User: user, Password: password, DB: name},
					KeyPrefix:  prefix,
					FilePrefix: fp,
					Keep:       keep,
					Now:        a.now,
				}
				a.log.Info(fmt.Sprintf("backing up database %q", name))
				key, err := job.Run(cmd.Context())
				if err != nil {
					return err
				}
				a.log.Success(fmt.Sprintf("stored %s", key))
			}
			return nil
		},
	}
	df.register(cmd.Flags())
	cmd.Flags().StringVar(&host, "host", "", "mysql host (MYSQL_HOST)")
	cmd.Flags().IntVar(&port, "port", 0, "mysql port (MYSQL_PORT, default 3306)")
	cmd.Flags().StringVar(&user, "user", "", "mysql user (MYSQL_USER)")
	cmd.Flags().StringVar(&db, "db", "", "database name (MYSQL_DB)")
	cmd.Flags().BoolVar(&allDatabases, "all-databases", false, "back up every non-system database")
	cmd.Flags().StringVar(&passEnv, "password-env", "", "env var holding the password (default MYSQL_PWD/MYSQL_PASS)")
	cmd.Flags().StringVar(&filePrefix, "file-prefix", "", "archive filename prefix (FILE_PREFIX, default db name)")
	cmd.Flags().IntVar(&keep, "keep", 0, "backups to retain (NUM_BACKUPS, default 7)")
	return cmd
}

func newBackupJenkinsCmd(a *app) *cobra.Command {
	var (
		df         destFlags
		home       string
		keep       int
		filePrefix string
		excludes   []string
	)
	cmd := &cobra.Command{
		Use:   "jenkins",
		Short: "Archive a Jenkins home directory to a destination, with rotation",
		RunE: func(cmd *cobra.Command, args []string) error {
			home = firstNonEmpty(home, os.Getenv("JENKINS_HOME"), "/var/lib/jenkins")
			df.bucket = firstNonEmpty(df.bucket, os.Getenv("S3_BUCKET"))
			df.prefix = firstNonEmpty(df.prefix, os.Getenv("S3_PREFIX"))
			df.kmsKey = firstNonEmpty(df.kmsKey, os.Getenv("S3_KMS_KEY"))
			df.profile = firstNonEmpty(df.profile, os.Getenv("AWS_PROFILE"))
			filePrefix = firstNonEmpty(filePrefix, os.Getenv("FILE_PREFIX"), "jenkins")
			if keep == 0 {
				keep = atoiOr(os.Getenv("NUM_BACKUPS"), 7)
			}
			if len(excludes) == 0 {
				excludes = []string{"cache/*", "nodes/*", "workspace/*", "logs/*", "org.jenkinsci.plugins.github.GitHubPlugin.cache/*"}
			}

			dest, err := a.buildDestination(cmd.Context(), df.toConfig())
			if err != nil {
				return err
			}
			job := backup.DirBackup{
				Root: home, Excludes: excludes, Dest: dest,
				KeyPrefix: df.prefix, FilePrefix: filePrefix, Keep: keep, Now: a.now,
			}
			a.log.Info(fmt.Sprintf("archiving %s", home))
			key, err := job.Run(cmd.Context())
			if err != nil {
				return err
			}
			a.log.Success(fmt.Sprintf("stored %s", key))
			return nil
		},
	}
	df.register(cmd.Flags())
	cmd.Flags().StringVar(&home, "home", "", "jenkins home (JENKINS_HOME, default /var/lib/jenkins)")
	cmd.Flags().StringVar(&filePrefix, "file-prefix", "", "archive filename prefix (default jenkins)")
	cmd.Flags().IntVar(&keep, "keep", 0, "backups to retain (default 7)")
	cmd.Flags().StringArrayVar(&excludes, "exclude", nil, "exclude glob (repeatable; defaults to jenkins cache/logs/workspace)")
	return cmd
}

func newBackupPurgeCmd(a *app) *cobra.Command {
	var (
		df   destFlags
		keep int
	)
	cmd := &cobra.Command{
		Use:   "purge",
		Short: "Delete old backups under a prefix, keeping the newest N",
		RunE: func(cmd *cobra.Command, args []string) error {
			df.bucket = firstNonEmpty(df.bucket, os.Getenv("S3_BUCKET"), os.Getenv("B2_BUCKET"))
			df.prefix = firstNonEmpty(df.prefix, os.Getenv("S3_PREFIX"), os.Getenv("B2_FOLDER"))
			df.profile = firstNonEmpty(df.profile, os.Getenv("AWS_PROFILE"))
			if keep == 0 {
				keep = atoiOr(os.Getenv("NUM_BACKUPS"), 7)
			}
			dest, err := a.buildDestination(cmd.Context(), df.toConfig())
			if err != nil {
				return err
			}
			deleted, err := backup.Purge(cmd.Context(), dest, df.prefix, keep)
			if err != nil {
				return err
			}
			for _, k := range deleted {
				a.log.Info(fmt.Sprintf("deleted %s", k))
			}
			a.log.Success(fmt.Sprintf("purge complete: %d removed, keeping newest %d", len(deleted), keep))
			return nil
		},
	}
	df.register(cmd.Flags())
	cmd.Flags().IntVar(&keep, "keep", 0, "backups to retain (NUM_BACKUPS, default 7)")
	return cmd
}

func newBackupRunCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "run JOB",
		Short: "Run a named backup job from the config file",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			job, ok := a.cfg.Backup.Jobs[name]
			if !ok {
				return fmt.Errorf("no backup job named %q in config", name)
			}
			dest, err := a.buildDestination(cmd.Context(), job.Dest)
			if err != nil {
				return err
			}
			a.log.Info(fmt.Sprintf("running backup job %q (%s)", name, job.Type))

			switch job.Type {
			case "mysql":
				password, err := resolveJobPassword(job.MySQL)
				if err != nil {
					return err
				}
				if err := a.requireTools("mysqldump"); err != nil {
					return err
				}
				b := backup.MySQLBackup{
					Runner:     a.runnerFactory(),
					Dest:       dest,
					Config:     backup.MySQLConfig{Host: job.MySQL.Host, Port: job.MySQL.Port, User: job.MySQL.User, Password: password, DB: job.MySQL.DB},
					KeyPrefix:  job.Dest.Prefix,
					FilePrefix: firstNonEmpty(job.FilePrefix, job.MySQL.DB),
					Keep:       job.Retention.Keep,
					Now:        a.now,
				}
				key, err := b.Run(cmd.Context())
				if err != nil {
					return err
				}
				a.log.Success(fmt.Sprintf("stored %s", key))
			case "jenkins":
				b := backup.DirBackup{
					Root: job.Jenkins.Home, Excludes: job.Jenkins.Excludes, Dest: dest,
					KeyPrefix: job.Dest.Prefix, FilePrefix: firstNonEmpty(job.FilePrefix, "jenkins"), Keep: job.Retention.Keep, Now: a.now,
				}
				key, err := b.Run(cmd.Context())
				if err != nil {
					return err
				}
				a.log.Success(fmt.Sprintf("stored %s", key))
			default:
				return fmt.Errorf("unsupported job type %q", job.Type)
			}
			return nil
		},
	}
}

// --- helpers -----------------------------------------------------------------

func resolveMySQLPassword(passEnv string) string {
	if passEnv != "" {
		return os.Getenv(passEnv)
	}
	return firstNonEmpty(os.Getenv("MYSQL_PWD"), os.Getenv("MYSQL_PASS"))
}

func resolveJobPassword(j config.MySQLJob) (string, error) {
	if j.PasswordEnv != "" {
		return os.Getenv(j.PasswordEnv), nil
	}
	if j.PasswordFile != "" {
		data, err := os.ReadFile(j.PasswordFile)
		if err != nil {
			return "", fmt.Errorf("read password file: %w", err)
		}
		return strings.TrimSpace(string(data)), nil
	}
	return "", nil
}

// writeMyCnf writes a 0600 my.cnf defaults-extra-file and returns its path plus
// a cleanup func, so the password is referenced by file rather than placed in
// the process argument list.
func writeMyCnf(cfg backup.MySQLConfig) (string, func(), error) {
	tmp, err := os.CreateTemp("", "keel-my-*.cnf")
	if err != nil {
		return "", nil, err
	}
	if _, err := tmp.WriteString(backup.RenderMyCnf(cfg)); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return "", nil, err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return "", nil, err
	}
	return tmp.Name(), func() { os.Remove(tmp.Name()) }, nil
}

func listDatabases(ctx context.Context, r runner.Runner, host string, port int, user, password string) ([]string, error) {
	cnf, cleanup, err := writeMyCnf(backup.MySQLConfig{Host: host, Port: port, User: user, Password: password})
	if err != nil {
		return nil, err
	}
	defer cleanup()

	var out bytes.Buffer
	if err := r.Stream(ctx, &out, "mysql", backup.ShowDatabasesArgs(cnf)...); err != nil {
		return nil, err
	}
	var dbs []string
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		name := strings.TrimSpace(line)
		if name != "" && !backup.ShouldSkipSystemDB(name) {
			dbs = append(dbs, name)
		}
	}
	return dbs, nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func atoiOr(s string, def int) int {
	if s == "" {
		return def
	}
	var n int
	if _, err := fmt.Sscanf(s, "%d", &n); err != nil {
		return def
	}
	return n
}
