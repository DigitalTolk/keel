package cli

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/DigitalTolk/keel/internal/backup"
	"github.com/DigitalTolk/keel/internal/mysqladmin"
)

func newMySQLCmd(a *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mysql",
		Short: "MySQL maintenance helpers",
	}
	cmd.AddCommand(newMySQLToInnoDBCmd(a))
	return cmd
}

func newMySQLToInnoDBCmd(a *app) *cobra.Command {
	var (
		host, user  string
		db, passEnv string
		port        int
	)
	cmd := &cobra.Command{
		Use:   "to-innodb",
		Short: "Convert all tables in a database to the InnoDB engine",
		RunE: func(cmd *cobra.Command, args []string) error {
			host = firstNonEmpty(host, os.Getenv("MYSQL_HOST"), "localhost")
			user = firstNonEmpty(user, os.Getenv("MYSQL_USER"))
			db = firstNonEmpty(db, os.Getenv("MYSQL_DB"))
			if port == 0 {
				port = atoiOr(os.Getenv("MYSQL_PORT"), 3306)
			}
			if db == "" {
				return fmt.Errorf("--db (or MYSQL_DB) is required")
			}
			if err := a.requireTools("mysql"); err != nil {
				return err
			}

			cnf, cleanup, err := writeMyCnf(backup.MySQLConfig{Host: host, Port: port, User: user, Password: resolveMySQLPassword(passEnv)})
			if err != nil {
				return err
			}
			defer cleanup()

			converted, err := a.convertToInnoDB(cmd.Context(), cnf, db)
			if err != nil {
				return err
			}
			a.log.Success(fmt.Sprintf("to-innodb complete: %d table(s) converted", converted))
			return nil
		},
	}
	cmd.Flags().StringVar(&host, "host", "", "mysql host (MYSQL_HOST, default localhost)")
	cmd.Flags().IntVar(&port, "port", 0, "mysql port (MYSQL_PORT, default 3306)")
	cmd.Flags().StringVar(&user, "user", "", "mysql user (MYSQL_USER)")
	cmd.Flags().StringVar(&db, "db", "", "database name (MYSQL_DB)")
	cmd.Flags().StringVar(&passEnv, "password-env", "", "env var holding the password (default MYSQL_PWD/MYSQL_PASS)")
	return cmd
}

// convertToInnoDB lists db's tables via the mysql client and converts each to
// InnoDB, returning the count converted. Separated from the command so it is
// testable with a fake runner.
func (a *app) convertToInnoDB(ctx context.Context, cnf, db string) (int, error) {
	r := a.runnerFactory()
	var out bytes.Buffer
	if err := r.Stream(ctx, &out, "mysql", mysqladmin.ShowTablesArgs(cnf, db)...); err != nil {
		return 0, err
	}

	converted := 0
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		table := strings.TrimSpace(line)
		if table == "" {
			continue
		}
		if err := r.Stream(ctx, io.Discard, "mysql", mysqladmin.AlterEngineArgs(cnf, db, table, "InnoDB")...); err != nil {
			return converted, err
		}
		a.log.Info(fmt.Sprintf("converted %s.%s", db, table))
		converted++
	}
	return converted, nil
}
