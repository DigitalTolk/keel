package mysqladmin

import (
	"slices"
	"strings"
	"testing"
)

func TestShowTablesArgs(t *testing.T) {
	args := ShowTablesArgs("/tmp/my.cnf", "app")
	for _, want := range []string{"--defaults-extra-file=/tmp/my.cnf", "--batch", "--skip-column-names", "-e", "show tables"} {
		if !slices.Contains(args, want) {
			t.Errorf("ShowTablesArgs missing %q in %v", want, args)
		}
	}
	if args[len(args)-1] != "app" {
		t.Errorf("db must be the final arg, got %v", args)
	}
}

func TestAlterEngineStatementQuotesTable(t *testing.T) {
	got := AlterEngineStatement("user`s", "InnoDB")
	// Backticks inside the identifier must be doubled to stay safe.
	want := "ALTER TABLE `user``s` ENGINE = InnoDB"
	if got != want {
		t.Fatalf("AlterEngineStatement = %q, want %q", got, want)
	}
}

func TestAlterEngineArgs(t *testing.T) {
	args := AlterEngineArgs("/tmp/my.cnf", "app", "widgets", "InnoDB")
	if !slices.Contains(args, "--defaults-extra-file=/tmp/my.cnf") {
		t.Errorf("missing defaults-extra-file: %v", args)
	}
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "ALTER TABLE `widgets` ENGINE = InnoDB") {
		t.Errorf("missing alter statement: %v", args)
	}
	if args[len(args)-1] != "app" {
		t.Errorf("db must be the final arg, got %v", args)
	}
}
