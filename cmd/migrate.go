package cmd

import (
	"fmt"
	"log"
	"path"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/spf13/cobra"
)

// migrateCmd represents the migrate command
var migrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "Run database migrations",
	Long:  `Execute database migrations (in migrations/ by default) against a running PostgreSQL instance`,
	Run:   domigrations,
}

var migrationsPath, postgresURI, migrationCommand string
var verboseMigrations bool

func init() {
	migrateCmd.Flags().StringVar(&migrationCommand, "cmd", "up", "Migration command (one of: up, down, version)")
	migrateCmd.Flags().StringVar(&migrationsPath, "migrations-path", "migrations", "Path to migrations files")
	migrateCmd.Flags().StringVar(&postgresURI, "postgres-uri", "", "PostgreSQL connection URL (ex: postgres://user:pwd@localhost:5432/furan?sslmode=enable)")
	migrateCmd.Flags().BoolVar(&verboseMigrations, "verbose", false, "verbose mode")
	RootCmd.AddCommand(migrateCmd)
}

type migrationLogger struct {
	LF func(msg string, args ...interface{})
}

func (ml *migrationLogger) Printf(format string, v ...interface{}) {
	if ml.LF != nil {
		ml.LF(format, v...)
	}
}

func (ml *migrationLogger) Verbose() bool {
	return verboseMigrations
}

func domigrations(cmd *cobra.Command, args []string) {
	m, err := migrate.New(
		path.Join("file://", migrationsPath),
		postgresURI)
	if err != nil {
		clierr("error creating migrations client: %v", err)
	}
	m.Log = &migrationLogger{LF: log.Printf}
	switch migrationCommand {
	case "up":
		if err := m.Up(); err != nil {
			clierr("error running up migrations: %v", err)
		}
	case "down":
		if err := m.Down(); err != nil {
			clierr("error running down migrations: %v", err)
		}
	case "version":
		v, dirty, err := m.Version()
		if err != nil {
			if err == migrate.ErrNilVersion {
				fmt.Println("no migrations applied yet")
				break
			}
			clierr("error checking migrations version: %v", err)
		}
		fmt.Printf("version: %v; dirty: %v", v, dirty)
	default:
		clierr("unknown/invalid command: %v", migrationCommand)
	}
}
