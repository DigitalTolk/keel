package backup

import (
	"fmt"
	"slices"
)

// MySQLConfig holds connection details for mysqldump.
type MySQLConfig struct {
	Host     string
	Port     int
	User     string
	Password string
	DB       string
}

// systemDatabases are skipped by --all-databases, matching backup-mysql-all.sh.
var systemDatabases = []string{"information_schema", "mysql", "performance_schema", "sys"}

// RenderMyCnf renders a my.cnf [client] section. It is written to a 0600 temp
// file and referenced via --defaults-extra-file so the password never appears
// in the process argument list.
func RenderMyCnf(c MySQLConfig) string {
	port := c.Port
	if port == 0 {
		port = 3306
	}
	return fmt.Sprintf("[client]\nhost = %s\nuser = %s\npassword = %s\nport = %d\n",
		c.Host, c.User, c.Password, port)
}

// MysqldumpArgs builds the mysqldump arguments for a single database. The
// connection (including password) comes from the defaults-extra-file at
// cnfPath; the database name is always the final argument.
func MysqldumpArgs(cnfPath, db string) []string {
	return []string{
		"--defaults-extra-file=" + cnfPath,
		"--single-transaction",
		"--quick",
		"--routines",
		"--triggers",
		"--default-character-set=utf8mb4",
		db,
	}
}

// ShowDatabasesArgs builds args for listing databases via the mysql client.
func ShowDatabasesArgs(cnfPath string) []string {
	return []string{"--defaults-extra-file=" + cnfPath, "-N", "-B", "-e", "SHOW DATABASES"}
}

// ShouldSkipSystemDB reports whether db is a MySQL system database excluded
// from --all-databases backups.
func ShouldSkipSystemDB(db string) bool {
	return slices.Contains(systemDatabases, db)
}
