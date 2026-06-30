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
	Log LogConfig `yaml:"log"`
	SSH SSHConfig `yaml:"ssh"`
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
