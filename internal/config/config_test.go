package config

import "testing"

func noEnv(string) string { return "" }

func envFrom(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestDefaults(t *testing.T) {
	c := Default()
	if c.SSH.User != "bofh" {
		t.Errorf("default ssh user = %q, want bofh", c.SSH.User)
	}
	if c.SSH.Port != 22 {
		t.Errorf("default ssh port = %d, want 22", c.SSH.Port)
	}
	if c.Log.Format != "text" {
		t.Errorf("default log format = %q, want text", c.Log.Format)
	}
}

func TestApplyFileOverridesDefaults(t *testing.T) {
	c := Default()
	yaml := []byte("ssh:\n  user: deploy\n  port: 2200\nlog:\n  format: json\n")
	if err := c.ApplyFile(yaml); err != nil {
		t.Fatalf("ApplyFile: %v", err)
	}
	if c.SSH.User != "deploy" {
		t.Errorf("ssh user = %q, want deploy", c.SSH.User)
	}
	if c.SSH.Port != 2200 {
		t.Errorf("ssh port = %d, want 2200", c.SSH.Port)
	}
	if c.Log.Format != "json" {
		t.Errorf("log format = %q, want json", c.Log.Format)
	}
}

func TestApplyFileLeavesUnsetKeysUntouched(t *testing.T) {
	c := Default()
	yaml := []byte("ssh:\n  port: 2200\n") // only port set
	if err := c.ApplyFile(yaml); err != nil {
		t.Fatalf("ApplyFile: %v", err)
	}
	if c.SSH.User != "bofh" {
		t.Errorf("ssh user = %q, want unchanged default bofh", c.SSH.User)
	}
	if c.SSH.Port != 2200 {
		t.Errorf("ssh port = %d, want 2200", c.SSH.Port)
	}
}

func TestApplyEnvOverridesFile(t *testing.T) {
	c := Default()
	_ = c.ApplyFile([]byte("ssh:\n  port: 2200\n"))
	c.ApplyEnv(envFrom(map[string]string{
		"SSH_USER":      "root",
		"SSH_PORT":      "2222",
		"SSH_JUMP_HOST": "jump.internal",
	}))
	if c.SSH.User != "root" {
		t.Errorf("ssh user = %q, want root (env wins)", c.SSH.User)
	}
	if c.SSH.Port != 2222 {
		t.Errorf("ssh port = %d, want 2222 (env wins over file)", c.SSH.Port)
	}
	if c.SSH.JumpHost != "jump.internal" {
		t.Errorf("ssh jump host = %q, want jump.internal", c.SSH.JumpHost)
	}
}

func TestApplyEnvIgnoresEmptyAndInvalid(t *testing.T) {
	c := Default()
	_ = c.ApplyFile([]byte("ssh:\n  port: 2200\n"))
	c.ApplyEnv(noEnv) // all empty
	if c.SSH.Port != 2200 {
		t.Errorf("ssh port = %d, want 2200 (empty env must not clobber)", c.SSH.Port)
	}
	if c.SSH.User != "bofh" {
		t.Errorf("ssh user = %q, want bofh", c.SSH.User)
	}

	// invalid port is ignored, not fatal, leaving prior value
	c.ApplyEnv(envFrom(map[string]string{"SSH_PORT": "notaport"}))
	if c.SSH.Port != 2200 {
		t.Errorf("ssh port = %d, want 2200 (invalid env ignored)", c.SSH.Port)
	}
}
