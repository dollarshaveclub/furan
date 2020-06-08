package datalayer_test

import (
	"fmt"
	"log"
	"os"
	"testing"
	"time"

	"github.com/dollarshaveclub/go-lib/cassandra"
	"github.com/gocql/gocql"

	"github.com/dollarshaveclub/furan/pkg/config"
	"github.com/dollarshaveclub/furan/pkg/datalayer"
	"github.com/dollarshaveclub/furan/pkg/datalayer/testsuite"
	"github.com/dollarshaveclub/furan/pkg/db"
)

const (
	testKeyspace = "furan_test"
)

var sname = "test"

var tn = os.Getenv("SCYLLA_TEST_NODE")
var ts *gocql.Session
var dbConfig config.DBconfig

var testDBTimeout = 5 * time.Minute

func setupTestDB() {
	// create keyspace
	c := gocql.NewCluster(tn)
	c.ProtoVersion = 3
	c.NumConns = 20
	c.SocketKeepalive = testDBTimeout
	c.Timeout = testDBTimeout
	c.MaxWaitSchemaAgreement = testDBTimeout
	log.Printf("creating keyspace session...\n")
	s, err := c.CreateSession()
	if err != nil {
		log.Fatalf("error creating keyspace session: %v", err)
	}
	defer s.Close()
	log.Printf("creating keyspace...\n")
	err = s.Query(fmt.Sprintf("CREATE KEYSPACE IF NOT EXISTS %v WITH REPLICATION = {'class': 'SimpleStrategy', 'replication_factor': 1};", testKeyspace)).Exec()
	if err != nil {
		log.Fatalf("error creating keyspace: %v", err)
	}
	time.Sleep(1)

	// DB setup
	c.Keyspace = testKeyspace
	dbConfig.Cluster = c
	dbConfig.Nodes = []string{tn}
	dbConfig.Keyspace = testKeyspace
	log.Printf("creating types...\n")
	err = cassandra.CreateRequiredTypes(dbConfig.Cluster, db.RequiredUDTs)
	if err != nil {
		log.Fatalf("error creating UDTs: %v", err)
	}
	log.Printf("creating tables...\n")
	err = cassandra.CreateRequiredTables(dbConfig.Cluster, db.RequiredTables)
	if err != nil {
		log.Fatalf("error creating tables: %v", err)
	}
	ts, err = dbConfig.Cluster.CreateSession()
	if err != nil {
		log.Fatalf("error getting session: %v", err)
	}
}

func teardownTestDB() {
	q := fmt.Sprintf("DROP KEYSPACE %v;", testKeyspace)
	err := ts.Query(q).Exec()
	if err != nil {
		log.Fatalf("error dropping keyspace: %v", err)
	}
}

func TestScyllaDBSuite(t *testing.T) {
	if tn == "" {
		t.SkipNow()
	} else {
		setupTestDB()
		defer teardownTestDB()
	}
	testsuite.RunTests(t, func() datalayer.DataLayer {
		return datalayer.NewDBLayer(ts, sname)
	})
}
