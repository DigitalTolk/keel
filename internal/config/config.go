// Package config loads and merges keel configuration with the precedence
// flags > environment > config file > built-in defaults. Flags are applied by
// the cli layer; this package handles defaults, file, and environment.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"gopkg.in/yaml.v3"
)

// Config is the resolved keel configuration.
type Config struct {
	Log    LogConfig    `yaml:"log"`
	SSH    SSHConfig    `yaml:"ssh"`
	AWS    AWSConfig    `yaml:"aws"`
	Backup BackupConfig `yaml:"backup"`
}

// AWSConfig holds shared AWS settings.
type AWSConfig struct {
	Profile string `yaml:"profile"`
}

// BackupConfig holds named backup jobs runnable via `keel backup run <name>`.
type BackupConfig struct {
	Jobs map[string]JobConfig `yaml:"jobs"`
}

// JobConfig defines one named backup job.
type JobConfig struct {
	Type       string          `yaml:"type"` // mysql|jenkins
	FilePrefix string          `yaml:"file_prefix"`
	MySQL      MySQLJob        `yaml:"mysql"`
	Jenkins    JenkinsJob      `yaml:"jenkins"`
	Dest       DestConfig      `yaml:"dest"`
	Retention  RetentionConfig `yaml:"retention"`
}

// MySQLJob holds connection details for a mysql backup job. The password is
// referenced indirectly, never inlined.
type MySQLJob struct {
	Host         string `yaml:"host"`
	Port         int    `yaml:"port"`
	User         string `yaml:"user"`
	DB           string `yaml:"db"`
	AllDatabases bool   `yaml:"all_databases"`
	PasswordEnv  string `yaml:"password_env"`
	PasswordFile string `yaml:"password_file"`
}

// JenkinsJob holds settings for a jenkins home backup.
type JenkinsJob struct {
	Home     string   `yaml:"home"`
	Excludes []string `yaml:"excludes"`
}

// DestConfig describes where a backup is stored.
type DestConfig struct {
	Kind     string `yaml:"kind"` // local|s3|b2
	Bucket   string `yaml:"bucket"`
	Prefix   string `yaml:"prefix"`
	BaseDir  string `yaml:"base_dir"`
	Region   string `yaml:"region"`
	Endpoint string `yaml:"endpoint"`
	KMSKey   string `yaml:"kms_key"`
	Profile  string `yaml:"profile"`
}

// RetentionConfig controls how many backups to keep.
type RetentionConfig struct {
	Keep int `yaml:"keep"`
}

// LogConfig controls console output formatting.
type LogConfig struct {
	Format string `yaml:"format"` // text|json
}

// SSHConfig holds defaults for remote connections, preserving the legacy
// SSH_USER / SSH_PORT / SSH_JUMP_HOST environment contract.
type SSHConfig struct {
	User     string `yaml:"user"`
	Port     int    `yaml:"port"`
	JumpHost string `yaml:"jump_host"`
}

// Default returns the built-in defaults, matching the original scripts
// (ssh user "bofh", port 22).
func Default() Config {
	return Config{
		Log: LogConfig{Format: "text"},
		SSH: SSHConfig{User: "bofh", Port: 22},
	}
}

// ApplyFile overlays YAML onto the receiver. Keys absent from the YAML leave
// the existing values untouched.
func (c *Config) ApplyFile(data []byte) error {
	if err := yaml.Unmarshal(data, c); err != nil {
		return fmt.Errorf("parse config: %w", err)
	}
	return nil
}

// ApplyEnv overlays environment variables, using getenv for lookups (injected
// for testability). Empty or invalid values are ignored so they never clobber
// a configured value.
func (c *Config) ApplyEnv(getenv func(string) string) {
	if v := getenv("SSH_USER"); v != "" {
		c.SSH.User = v
	}
	if v := getenv("SSH_PORT"); v != "" {
		if port, err := strconv.Atoi(v); err == nil {
			c.SSH.Port = port
		}
	}
	if v := getenv("SSH_JUMP_HOST"); v != "" {
		c.SSH.JumpHost = v
	}
	if v := getenv("KEEL_LOG_FORMAT"); v != "" {
		c.Log.Format = v
	}
}

// Load resolves configuration from the default layers: defaults, then the
// first config file found (or the explicit path), then the environment.
// An explicit path that does not exist is an error; auto-discovered paths
// that are missing are skipped.
func Load(explicitPath string) (Config, error) {
	c := Default()

	path, mustExist := explicitPath, explicitPath != ""
	if path == "" {
		path = discover()
	}
	if path != "" {
		data, err := os.ReadFile(path)
		switch {
		case err == nil:
			if err := c.ApplyFile(data); err != nil {
				return c, err
			}
		case mustExist:
			return c, fmt.Errorf("read config %q: %w", path, err)
		}
	}

	c.ApplyEnv(os.Getenv)
	return c, nil
}

// discover returns the first existing config file among the standard
// locations, or "" if none exist.
func discover() string {
	candidates := []string{"keel.yaml"}
	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates, filepath.Join(home, ".config", "keel", "config.yaml"))
	}
	candidates = append(candidates, "/etc/keel/config.yaml")
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}
