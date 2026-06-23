// Package mysqladmin provides MySQL administrative helpers, porting
// mysql-to-innodb.sh. Statements/args are built here (pure, testable); the
// caller runs them via the mysql client through internal/runner.
package mysqladmin

import (
	"fmt"
	"strings"
)

// ShowTablesArgs builds mysql client args to list a database's tables, one per
// line with no header (suitable for parsing).
func ShowTablesArgs(cnfPath, db string) []string {
	return []string{
		"--defaults-extra-file=" + cnfPath,
		"--batch",
		"--skip-column-names",
		"-e", "show tables",
		db,
	}
}

// AlterEngineStatement builds an ALTER TABLE … ENGINE = … statement, safely
// backtick-quoting the table identifier (internal backticks are doubled).
func AlterEngineStatement(table, engine string) string {
	quoted := "`" + strings.ReplaceAll(table, "`", "``") + "`"
	return fmt.Sprintf("ALTER TABLE %s ENGINE = %s", quoted, engine)
}

// AlterEngineArgs builds mysql client args to run AlterEngineStatement against
// db.
func AlterEngineArgs(cnfPath, db, table, engine string) []string {
	return []string{
		"--defaults-extra-file=" + cnfPath,
		"-e", AlterEngineStatement(table, engine),
		db,
	}
}
