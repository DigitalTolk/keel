package backup

import (
	"slices"
	"strings"
	"testing"
)

func TestRenderMyCnfHasClientSection(t *testing.T) {
	got := RenderMyCnf(MySQLConfig{Host: "db1", Port: 3306, User: "backup", Password: "pw"})
	for _, want := range []string{"[client]", "host = db1", "user = backup", "password = pw", "port = 3306"} {
		if !strings.Contains(got, want) {
			t.Errorf("RenderMyCnf missing %q in:\n%s", want, got)
		}
	}
}

func TestMysqldumpArgsKeepPasswordOutOfArgv(t *testing.T) {
	args := MysqldumpArgs("/tmp/my.cnf", "app")
	joined := strings.Join(args, " ")
	if strings.Contains(joined, "secret") || strings.Contains(joined, "-p") {
		t.Errorf("password/-p must not appear in argv: %v", args)
	}
	if !slices.Contains(args, "--single-transaction") || !slices.Contains(args, "--quick") {
		t.Errorf("expected --single-transaction --quick, got %v", args)
	}
	if !slices.Contains(args, "--defaults-extra-file=/tmp/my.cnf") {
		t.Errorf("expected defaults-extra-file, got %v", args)
	}
	if args[len(args)-1] != "app" {
		t.Errorf("db name must be the final argument, got %v", args)
	}
}

func TestShouldSkipSystemDB(t *testing.T) {
	for _, db := range []string{"information_schema", "mysql", "performance_schema", "sys"} {
		if !ShouldSkipSystemDB(db) {
			t.Errorf("ShouldSkipSystemDB(%q) = false, want true", db)
		}
	}
	if ShouldSkipSystemDB("app") {
		t.Error("ShouldSkipSystemDB(app) = true, want false")
	}
}
