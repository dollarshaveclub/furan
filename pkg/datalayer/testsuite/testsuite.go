package testsuite

import (
	"context"
	"testing"
	"time"

	"github.com/gofrs/uuid"

	"github.com/dollarshaveclub/furan/pkg/datalayer"
	"github.com/dollarshaveclub/furan/pkg/generated/furanrpc"
	"github.com/dollarshaveclub/furan/pkg/models"
)

// DLFactoryFunc is a function that returns an empty DataLayer
type DLFactoryFunc func(t *testing.T) datalayer.DataLayer

// RunTests runs all tests against the DataLayer implementation returned by dlfunc
func RunTests(t *testing.T, dlfunc DLFactoryFunc) {
	if dlfunc == nil {
		t.Fatalf("dlfunc cannot be nil")
	}
	tests := []struct {
		name  string
		tfunc func(*testing.T, datalayer.DataLayer)
	}{
		{
			"create build",
			testDBCreateBuild,
		},
		{
			"delete build",
			testDBDeleteBuild,
		},
		{
			"get build by ID",
			testDBGetBuildByID,
		},
		{
			"set build completed timestamp",
			testDBSetBuildCompletedTimestamp,
		},
		{
			"set build status",
			testDBSetBuildStatus,
		},
		{
			"listen for and add events",
			testDBListenAndAddEvents,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.tfunc == nil {
				t.Skip("test func is nil")
			}
			tt.tfunc(t, dlfunc(t))
		})
	}
}

var tb = models.Build{
	ID:           uuid.Must(uuid.NewV4()),
	Created:      time.Now().UTC(),
	GitHubRepo:   "foobar/baz",
	GitHubRef:    "master",
	ImageRepo:    "quay.io/foobar/baz",
	Tags:         []string{"master"},
	CommitSHATag: true,
	Request: furanrpc.BuildRequest{
		Build: &furanrpc.BuildDefinition{
			GithubRepo:       "foobar/baz",
			Ref:              "master",
			Tags:             []string{"master"},
			TagWithCommitSha: true,
		},
		Push: &furanrpc.PushDefinition{
			Registry: &furanrpc.PushRegistryDefinition{},
		},
	},
	Status: 1,
	Events: []string{},
}

func testDBCreateBuild(t *testing.T, dl datalayer.DataLayer) {
	id, err := dl.CreateBuild(context.Background(), tb)
	if err != nil {
		t.Fatalf("error creating build: %v", err)
	}
	err = dl.DeleteBuild(context.Background(), id)
	if err != nil {
		t.Fatalf("error deleting build: %v", err)
	}
}

func testDBDeleteBuild(t *testing.T, dl datalayer.DataLayer) {
	err := dl.DeleteBuild(context.Background(), tb.ID)
	if err != nil {
		t.Fatalf("error creating build: %v", err)
	}
}

func testDBGetBuildByID(t *testing.T, dl datalayer.DataLayer) {
	id, err := dl.CreateBuild(context.Background(), tb)
	if err != nil {
		t.Fatalf("error creating build: %v", err)
	}
	b, err := dl.GetBuildByID(context.Background(), id)
	if err != nil {
		t.Fatalf("error getting build by ID: %v", err)
	}
	if b.GitHubRepo != tb.GitHubRepo {
		t.Fatalf("incorrect github repo: %v (expected %v)", b.GitHubRepo, tb.GitHubRepo)
	}
	err = dl.DeleteBuild(context.Background(), id)
	if err != nil {
		t.Fatalf("error deleting build: %v", err)
	}
}

func testDBSetBuildCompletedTimestamp(t *testing.T, dl datalayer.DataLayer) {
	id, err := dl.CreateBuild(context.Background(), tb)
	if err != nil {
		t.Fatalf("error creating build: %v", err)
	}
	now := time.Now().UTC()
	err = dl.SetBuildCompletedTimestamp(context.Background(), id, now)
	if err != nil {
		t.Fatalf("error setting build completed timestamp: %v", err)
	}
	b, err := dl.GetBuildByID(context.Background(), id)
	if err != nil {
		t.Fatalf("error getting build by id: %v", err)
	}
	if b.Completed.UTC() != now {
		t.Fatalf("bad completed timestamp: %v (expected %v)", b.Completed.UTC(), now)
	}
	err = dl.DeleteBuild(context.Background(), id)
	if err != nil {
		t.Fatalf("error deleting build: %v", err)
	}
}

func testDBSetBuildStatus(t *testing.T, dl datalayer.DataLayer) {
	id, err := dl.CreateBuild(context.Background(), tb)
	if err != nil {
		t.Fatalf("error creating build: %v", err)
	}
	b, err := dl.GetBuildByID(context.Background(), id)
	if err != nil {
		t.Fatalf("error getting build by id (initial): %v", err)
	}
	if b.Status != models.BuildStatusNotStarted {
		t.Fatalf("bad initial status: %v (expected %v)", b.Status, models.BuildStatusNotStarted)
	}
	err = dl.SetBuildStatus(context.Background(), id, models.BuildStatusSuccess)
	if err != nil {
		t.Fatalf("error setting build state: %v", err)
	}
	b, err = dl.GetBuildByID(context.Background(), id)
	if err != nil {
		t.Fatalf("error getting build by id (after): %v", err)
	}
	if b.Status != models.BuildStatusSuccess {
		t.Fatalf("bad status: %v (expected %v)", b.Status, models.BuildStatusSuccess)
	}
	err = dl.DeleteBuild(context.Background(), id)
	if err != nil {
		t.Fatalf("error deleting build: %v", err)
	}
}

func testDBListenAndAddEvents(t *testing.T, dl datalayer.DataLayer) {
	id, err := dl.CreateBuild(context.Background(), tb)
	if err != nil {
		t.Fatalf("error creating build: %v", err)
	}
	if err := dl.ListenForBuildEvents(context.Background(), id, make(chan string)); err == nil {
		t.Fatalf("listen should have returned error for bad build status")
	}
	if err := dl.SetBuildStatus(context.Background(), id, models.BuildStatusBuilding); err != nil {
		t.Fatalf("error setting build status: %v", err)
	}
	ctx, cf := context.WithCancel(context.Background())
	defer cf()
	c := make(chan string)
	defer close(c)
	revents := []string{}
	go func() {
		// take any events received on c and append to revents
		for e := range c {
			revents = append(revents, e)
		}
	}()
	go func() {
		// ListenForBuildEvents blocks and will write any received events to c
		dl.ListenForBuildEvents(ctx, id, c)
	}()
	time.Sleep(10 * time.Millisecond) // allow above go routines to run
	// add some events
	if err := dl.AddEvent(ctx, id, "something happened"); err != nil {
		t.Fatalf("error adding event 1: %v", err)
	}
	if err := dl.AddEvent(ctx, id, "something else happened"); err != nil {
		t.Fatalf("error adding event 2: %v", err)
	}
	if err := dl.AddEvent(ctx, id, "ok done"); err != nil {
		t.Fatalf("error adding event 3: %v", err)
	}
	cf() // cancel context, aborting listener
	time.Sleep(10 * time.Millisecond)
	if i := len(revents); i != 3 {
		t.Fatalf("expected 3 events, got %v", i)
	}
}
