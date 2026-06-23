package backup

import (
	"slices"
	"strings"
	"testing"
)

func TestRsyncArgsBasic(t *testing.T) {
	got := RsyncArgs(RsyncOptions{User: "u", Host: "h", Path: "/src", Dest: "/dst", Port: 22})
	want := []string{"-azq", "--delete", "-e", "ssh -p 22", "u@h:/src", "/dst"}
	if !slices.Equal(got, want) {
		t.Fatalf("RsyncArgs = %v, want %v", got, want)
	}
}

func TestRsyncArgsSudoAndKey(t *testing.T) {
	got := RsyncArgs(RsyncOptions{User: "u", Host: "h", Path: "/src", Dest: "/dst", Port: 2222, KeyFile: "/k", Sudo: true})
	want := []string{"-azq", "--delete", "--rsync-path=sudo rsync", "-e", "ssh -p 2222 -i /k", "u@h:/src", "/dst"}
	if !slices.Equal(got, want) {
		t.Fatalf("RsyncArgs = %v, want %v", got, want)
	}
}

func TestLftpMirrorArgs(t *testing.T) {
	got := LftpMirrorArgs(SftpOptions{User: "u", Host: "h", Path: "/src", Dest: "/dst", Port: 22, Parallel: 8})
	if len(got) != 2 || got[0] != "-c" {
		t.Fatalf("LftpMirrorArgs should be [-c <script>], got %v", got)
	}
	script := got[1]
	for _, want := range []string{
		"open -p 22 -u u,placeholder sftp://h",
		"mirror", "--only-newer", "--delete", "--parallel=8", "/src", "/dst",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("lftp script missing %q in:\n%s", want, script)
		}
	}
}

func TestLftpMirrorArgsDefaultParallelAndExtra(t *testing.T) {
	got := LftpMirrorArgs(SftpOptions{User: "u", Host: "h", Path: "/src", Dest: "/dst", Port: 22, ExtraArgs: "--no-perms"})
	script := got[1]
	if !strings.Contains(script, "--parallel=8") {
		t.Errorf("default parallel should be 8, got:\n%s", script)
	}
	if !strings.Contains(script, "--no-perms") {
		t.Errorf("extra args should be included, got:\n%s", script)
	}
}
