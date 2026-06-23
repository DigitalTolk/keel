package config

import (
	"os"
	"path/filepath"
	"testing"
)

// TestApplyFileMalformedYAML exercises the error branch in ApplyFile when the
// YAML cannot be parsed.
func TestApplyFileMalformedYAML(t *testing.T) {
	c := Default()
	// A scalar where a mapping is expected, plus broken indentation, is not
	// valid YAML for the Config struct.
	bad := []byte("ssh: [this is not: valid: yaml: : :\n  - broken")
	err := c.ApplyFile(bad)
	if err == nil {
		t.Fatal("ApplyFile with malformed YAML = nil error, want error")
	}
	// Defaults must remain untouched after a failed parse path returns.
	if c.SSH.User != "bofh" {
		t.Errorf("ssh user = %q, want bofh after failed parse", c.SSH.User)
	}
}

// TestApplyEnvSSHPortInvalidIgnored verifies an unparseable SSH_PORT is
// silently ignored, leaving the existing port intact, while other env values
// still apply.
func TestApplyEnvSSHPortInvalidIgnored(t *testing.T) {
	c := Default() // port 22
	c.ApplyEnv(envFrom(map[string]string{
		"SSH_PORT": "abc", // invalid -> ignored
		"SSH_USER": "ops", // valid -> applied
	}))
	if c.SSH.Port != 22 {
		t.Errorf("ssh port = %d, want 22 (invalid SSH_PORT ignored)", c.SSH.Port)
	}
	if c.SSH.User != "ops" {
		t.Errorf("ssh user = %q, want ops", c.SSH.User)
	}
}

// TestApplyEnvLogFormat verifies the KEEL_LOG_FORMAT branch.
func TestApplyEnvLogFormat(t *testing.T) {
	c := Default() // format text
	c.ApplyEnv(envFrom(map[string]string{"KEEL_LOG_FORMAT": "json"}))
	if c.Log.Format != "json" {
		t.Errorf("log format = %q, want json", c.Log.Format)
	}
}

// TestLoadExplicitPath loads a config from an explicit, existing file path.
func TestLoadExplicitPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "custom.yaml")
	yaml := []byte("ssh:\n  user: deploy\n  port: 2200\nlog:\n  format: json\n")
	if err := os.WriteFile(path, yaml, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	// Clear env so it cannot influence the result.
	t.Setenv("SSH_USER", "")
	t.Setenv("SSH_PORT", "")
	t.Setenv("SSH_JUMP_HOST", "")
	t.Setenv("KEEL_LOG_FORMAT", "")

	c, err := Load(path)
	if err != nil {
		t.Fatalf("Load(%q): %v", path, err)
	}
	if c.SSH.User != "deploy" {
		t.Errorf("ssh user = %q, want deploy (from file)", c.SSH.User)
	}
	if c.SSH.Port != 2200 {
		t.Errorf("ssh port = %d, want 2200 (from file)", c.SSH.Port)
	}
	if c.Log.Format != "json" {
		t.Errorf("log format = %q, want json (from file)", c.Log.Format)
	}
}

// TestLoadExplicitPathEnvWins verifies env overlays the explicit file.
func TestLoadExplicitPathEnvWins(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "custom.yaml")
	if err := os.WriteFile(path, []byte("ssh:\n  port: 2200\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	t.Setenv("SSH_PORT", "2222")
	t.Setenv("SSH_USER", "root")
	t.Setenv("SSH_JUMP_HOST", "")
	t.Setenv("KEEL_LOG_FORMAT", "")

	c, err := Load(path)
	if err != nil {
		t.Fatalf("Load(%q): %v", path, err)
	}
	if c.SSH.Port != 2222 {
		t.Errorf("ssh port = %d, want 2222 (env wins over file)", c.SSH.Port)
	}
	if c.SSH.User != "root" {
		t.Errorf("ssh user = %q, want root (env)", c.SSH.User)
	}
}

// TestLoadExplicitPathMissing verifies that an explicit path that does not
// exist is an error.
func TestLoadExplicitPathMissing(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "nope.yaml")
	_, err := Load(missing)
	if err == nil {
		t.Fatalf("Load(%q) = nil error, want error for missing explicit file", missing)
	}
}

// TestLoadExplicitPathMalformed verifies that a malformed explicit file
// surfaces the ApplyFile error from Load.
func TestLoadExplicitPathMalformed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(path, []byte("ssh: [broken: : :\n - x"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	_, err := Load(path)
	if err == nil {
		t.Fatalf("Load(%q) = nil error, want parse error for malformed file", path)
	}
}

// TestLoadAutoDiscoverCwd verifies Load auto-discovers ./keel.yaml when no
// explicit path is given.
func TestLoadAutoDiscoverCwd(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	// Point HOME at an empty dir so the home candidate does not match.
	t.Setenv("HOME", t.TempDir())

	yaml := []byte("ssh:\n  user: discovered\n  port: 2300\n")
	if err := os.WriteFile(filepath.Join(dir, "keel.yaml"), yaml, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	t.Setenv("SSH_USER", "")
	t.Setenv("SSH_PORT", "")
	t.Setenv("SSH_JUMP_HOST", "")
	t.Setenv("KEEL_LOG_FORMAT", "")

	c, err := Load("")
	if err != nil {
		t.Fatalf("Load(\"\"): %v", err)
	}
	if c.SSH.User != "discovered" {
		t.Errorf("ssh user = %q, want discovered (auto-discovered cwd file)", c.SSH.User)
	}
	if c.SSH.Port != 2300 {
		t.Errorf("ssh port = %d, want 2300 (auto-discovered cwd file)", c.SSH.Port)
	}
}

// TestLoadAutoDiscoverHome verifies Load discovers ~/.config/keel/config.yaml
// when no cwd file exists.
func TestLoadAutoDiscoverHome(t *testing.T) {
	cwd := t.TempDir()
	t.Chdir(cwd) // no keel.yaml here

	home := t.TempDir()
	t.Setenv("HOME", home)
	cfgDir := filepath.Join(home, ".config", "keel")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	yaml := []byte("ssh:\n  user: homeuser\n")
	if err := os.WriteFile(filepath.Join(cfgDir, "config.yaml"), yaml, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	t.Setenv("SSH_USER", "")
	t.Setenv("SSH_PORT", "")
	t.Setenv("SSH_JUMP_HOST", "")
	t.Setenv("KEEL_LOG_FORMAT", "")

	c, err := Load("")
	if err != nil {
		t.Fatalf("Load(\"\"): %v", err)
	}
	if c.SSH.User != "homeuser" {
		t.Errorf("ssh user = %q, want homeuser (auto-discovered home file)", c.SSH.User)
	}
}

// TestLoadAutoDiscoverNone verifies Load returns defaults when no config file
// is found anywhere discoverable.
func TestLoadAutoDiscoverNone(t *testing.T) {
	cwd := t.TempDir()
	t.Chdir(cwd)                  // no keel.yaml
	t.Setenv("HOME", t.TempDir()) // empty home, no .config/keel/config.yaml

	t.Setenv("SSH_USER", "")
	t.Setenv("SSH_PORT", "")
	t.Setenv("SSH_JUMP_HOST", "")
	t.Setenv("KEEL_LOG_FORMAT", "")

	c, err := Load("")
	if err != nil {
		t.Fatalf("Load(\"\"): %v", err)
	}
	// Falls back to built-in defaults.
	if c.SSH.User != "bofh" {
		t.Errorf("ssh user = %q, want bofh (defaults, no file found)", c.SSH.User)
	}
	if c.SSH.Port != 22 {
		t.Errorf("ssh port = %d, want 22 (defaults, no file found)", c.SSH.Port)
	}
}

// TestDiscoverDirect drives discover() directly through cwd discovery so the
// happy path of returning a found candidate is exercised.
func TestDiscoverDirect(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("HOME", t.TempDir())
	if err := os.WriteFile(filepath.Join(dir, "keel.yaml"), []byte("log:\n  format: json\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	got := discover()
	if got != "keel.yaml" {
		t.Errorf("discover() = %q, want keel.yaml", got)
	}
}

// TestDiscoverNone verifies discover() returns "" when nothing exists.
func TestDiscoverNone(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("HOME", t.TempDir())
	if got := discover(); got != "" {
		t.Errorf("discover() = %q, want empty", got)
	}
}
