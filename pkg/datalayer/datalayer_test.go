package datalayer_test

import (
	"log"
	"os"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"

	"github.com/dollarshaveclub/furan/pkg/datalayer"
	"github.com/dollarshaveclub/furan/pkg/datalayer/testsuite"
)

var tdb = os.Getenv("FURAN_TEST_DB")

type migrationLogger struct {
	LF func(msg string, args ...interface{})
}

func (ml *migrationLogger) Printf(format string, v ...interface{}) {
	if ml.LF != nil {
		ml.LF(format, v...)
	}
}

func (ml *migrationLogger) Verbose() bool {
	return false
}

func migrator(t *testing.T) *migrate.Migrate {
	src := "file://../../migrations/"
	m, err := migrate.New(
		src,
		tdb)
	if err != nil {
		t.Fatalf("error creating migrations client: %v", err)
	}
	m.Log = &migrationLogger{LF: log.Printf}
	return m
}

func setupTestDB(t *testing.T) {
	m := migrator(t)
	if err := m.Up(); err != nil {
		t.Fatalf("error running up migrations: %v", err)
	}
}

func teardownTestDB(t *testing.T) {
	m := migrator(t)
	if err := m.Down(); err != nil {
		t.Fatalf("error running down migrations: %v", err)
	}
}

func TestPostgresDBSuite(t *testing.T) {
	if tdb == "" {
		t.SkipNow()
	} else {
		setupTestDB(t)
		defer teardownTestDB(t)
	}
	testsuite.RunTests(t, func(t *testing.T) datalayer.DataLayer {
		dl, err := datalayer.NewPostgresDBLayer(tdb)
		if err != nil {
			t.Skipf("error getting postgres datalayer: %v", err)
		}
		return dl
	})
}
