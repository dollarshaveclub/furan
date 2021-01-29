package testsuite

import (
	"context"
	"fmt"
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
			"list builds",
			testDBListBuilds,
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
		{
			"cancellation and listen for cancellation",
			testDBCancelBuildAndListenForCancellation,
		},
		{
			"set build as running and listen for running",
			testDBSetBuildAsRunningAndListenForBuildRunning,
		},
		{
			"set build as completed and listen for completion",
			testDBSetBuildAsCompletedAndListenForBuildCompleted,
		},
		{
			"create API key",
			testDBCreateAPIKey,
		},
		{
			"get API key",
			testDBGetAPIKey,
		},
		{
			"delete API key",
			testDBDeleteAPIKey,
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
	ImageRepos:   []string{"quay.io/foobar/baz"},
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
			Registries: []*furanrpc.PushRegistryDefinition{
				&furanrpc.PushRegistryDefinition{
					Repo: "quay.io/foobar/baz",
				},
			},
		},
	},
	Status: 1,
	Events: []string{},
}

func testDBCreateBuild(t *testing.T, dl datalayer.DataLayer) {
	ctx := context.Background()
	id, err := dl.CreateBuild(ctx, tb)
	if err != nil {
		t.Fatalf("error creating build: %v", err)
	}
	b, _ := dl.GetBuildByID(ctx, id)
	if b.Status != models.BuildStatusNotStarted {
		t.Fatalf("bad status: %v", b.Status)
	}
	err = dl.DeleteBuild(ctx, id)
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
	if b.Request.GetBuild().GithubRepo != tb.Request.GetBuild().GithubRepo {
		t.Fatalf("unexpected req.build.github_repo: %v", b.Request.GetBuild().GithubRepo)
	}
	_, err = dl.GetBuildByID(context.Background(), uuid.Must(uuid.NewV4()))
	if err == nil {
		t.Fatalf("expected id missing error")
	}
	err = dl.DeleteBuild(context.Background(), id)
	if err != nil {
		t.Fatalf("error deleting build: %v", err)
	}
}

func testDBListBuilds(t *testing.T, dl datalayer.DataLayer) {
	ctx := context.Background()
	startedAfter := time.Now().UTC() // started timestamps are automatically set during creation
	id1, _ := dl.CreateBuild(ctx, models.Build{GitHubRepo: "foo/bar"})
	id2, _ := dl.CreateBuild(ctx, models.Build{GitHubRef: "asdf"})
	id3, _ := dl.CreateBuild(ctx, models.Build{ImageRepos: []string{"some/repo"}})
	id4, _ := dl.CreateBuild(ctx, models.Build{})
	dl.SetBuildStatus(ctx, id4, models.BuildStatusFailure)
	completedAfter := time.Date(2020, time.January, 4, 1, 1, 1, 1, time.UTC)
	completedBefore := time.Date(2020, time.January, 2, 1, 1, 1, 1, time.UTC)
	id5, _ := dl.CreateBuild(ctx, models.Build{})
	dl.SetBuildCompletedTimestamp(ctx, id5, completedAfter.Add(1*time.Hour))
	id6, _ := dl.CreateBuild(ctx, models.Build{})
	dl.SetBuildCompletedTimestamp(ctx, id6, completedBefore.Add(-1*time.Hour))
	startedBefore := time.Now().UTC().Add(1 * time.Hour)

	_, err := dl.ListBuilds(ctx, datalayer.ListBuildsOptions{})
	if err == nil {
		t.Errorf("empty list options should return error")
	}

	timestamp := startedAfter
	_, err = dl.ListBuilds(ctx, datalayer.ListBuildsOptions{
		StartedBefore: timestamp,
		StartedAfter:  timestamp,
	})
	if err == nil {
		t.Errorf("duplicate timestamps should return error")
	}

	builds, err := dl.ListBuilds(ctx, datalayer.ListBuildsOptions{WithGitHubRepo: "foo/bar"})
	if err != nil {
		t.Errorf("list builds by github repo failed: %v", err)
	}
	if len(builds) != 1 || builds[0].ID != id1 || builds[0].GitHubRepo != "foo/bar" {
		t.Errorf("bad result for listing by github repo: %+v", builds)
	}

	builds, err = dl.ListBuilds(ctx, datalayer.ListBuildsOptions{WithGitHubRef: "asdf"})
	if err != nil {
		t.Errorf("list builds by github ref failed: %v", err)
	}
	if len(builds) != 1 || builds[0].ID != id2 || builds[0].GitHubRef != "asdf" {
		t.Errorf("bad result for listing by github ref: %+v", builds)
	}

	builds, err = dl.ListBuilds(ctx, datalayer.ListBuildsOptions{WithImageRepo: "some/repo"})
	if err != nil {
		t.Errorf("list builds by image repo failed: %v", err)
	}
	if len(builds) != 1 || builds[0].ID != id3 || builds[0].ImageRepos[0] != "some/repo" {
		t.Errorf("bad result for listing by image repo: %+v", builds)
	}

	builds, err = dl.ListBuilds(ctx, datalayer.ListBuildsOptions{WithStatus: models.BuildStatusFailure})
	if err != nil {
		t.Errorf("list builds by status failed: %v", err)
	}
	if len(builds) != 1 || builds[0].ID != id4 || builds[0].Status != models.BuildStatusFailure {
		t.Errorf("bad result for listing by status: %+v", builds)
	}

	builds, err = dl.ListBuilds(ctx, datalayer.ListBuildsOptions{CompletedAfter: completedAfter})
	if err != nil {
		t.Errorf("list builds by completed after failed: %v", err)
	}
	if len(builds) != 1 || builds[0].ID != id5 || !(builds[0].Completed.After(completedAfter) || builds[0].Completed.Equal(completedAfter)) {
		t.Errorf("bad result for listing by completed after: %+v", builds)
	}

	builds, err = dl.ListBuilds(ctx, datalayer.ListBuildsOptions{CompletedBefore: completedBefore})
	if err != nil {
		t.Errorf("list builds by completed before failed: %v", err)
	}
	if len(builds) != 1 || builds[0].ID != id6 || !builds[0].Completed.Before(completedBefore) {
		t.Errorf("bad result for listing by completed before: %+v", builds)
	}

	builds, err = dl.ListBuilds(ctx, datalayer.ListBuildsOptions{StartedAfter: startedAfter})
	if err != nil {
		t.Errorf("list builds by started after failed: %v", err)
	}
	if len(builds) != 6 {
		t.Errorf("bad result for listing by started after: %+v", builds)
	}

	builds, err = dl.ListBuilds(ctx, datalayer.ListBuildsOptions{StartedBefore: startedBefore})
	if err != nil {
		t.Errorf("list builds by started before failed: %v", err)
	}
	if len(builds) != 6 {
		t.Errorf("bad result for listing by started before: %+v", builds)
	}

	// limit
	builds, err = dl.ListBuilds(ctx, datalayer.ListBuildsOptions{StartedBefore: startedBefore, Limit: 2})
	if err != nil {
		t.Errorf("list builds by started before with limit failed: %v", err)
	}
	if len(builds) != 2 {
		t.Errorf("bad result for listing by started before with limit: %+v", builds)
	}

	// impossible set of options should return no results
	timestamp = startedBefore
	builds, err = dl.ListBuilds(ctx, datalayer.ListBuildsOptions{
		StartedAfter:  timestamp,
		StartedBefore: timestamp.Add(-1 * time.Hour),
	})
	if err != nil {
		t.Errorf("impossible options failed: %v", err)
	}
	if len(builds) != 0 {
		t.Errorf("bad result for impossible options: %+v", builds)
	}

	// verify boolean ANDs
	id7, _ := dl.CreateBuild(ctx, models.Build{GitHubRepo: "foo/bar2", GitHubRef: "1234", ImageRepos: []string{"abc", "def"}})
	dl.SetBuildStatus(ctx, id7, models.BuildStatusRunning)
	builds, err = dl.ListBuilds(ctx, datalayer.ListBuildsOptions{
		StartedAfter:   startedAfter,
		WithGitHubRepo: "foo/bar2",
		WithGitHubRef:  "1234",
		WithImageRepo:  "def",
		WithStatus:     models.BuildStatusRunning,
	})
	if err != nil {
		t.Errorf("multiple options failed: %v", err)
	}
	if len(builds) != 1 || builds[0].ID != id7 {
		t.Errorf("bad result for AND check multiple options: %+v", builds)
	}

	// change one of the conditionals to not match anything
	builds, err = dl.ListBuilds(ctx, datalayer.ListBuildsOptions{
		StartedAfter:   startedBefore,
		WithGitHubRepo: "foo/bar2",
		WithGitHubRef:  "1234",
		WithImageRepo:  "invalid", // won't match
		WithStatus:     models.BuildStatusRunning,
	})
	if err != nil {
		t.Errorf("non-matching multiple options failed: %v", err)
	}
	if len(builds) != 0 {
		t.Errorf("non-matching multiple options should return no results: %+v", builds)
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
	if b.Completed.UTC().Truncate(time.Second) != now.Truncate(time.Second) {
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
	if err := dl.SetBuildStatus(context.Background(), id, models.BuildStatusFailure); err != nil {
		t.Fatalf("error setting build status 1: %v", err)
	}
	if err := dl.ListenForBuildEvents(context.Background(), id, make(chan string)); err == nil {
		t.Fatalf("listen should have returned error for bad build status")
	}
	if err := dl.SetBuildStatus(context.Background(), id, models.BuildStatusRunning); err != nil {
		t.Fatalf("error setting build status 2: %v", err)
	}
	ctx, cf := context.WithCancel(context.Background())
	defer cf()
	c := make(chan string)

	revents := make(chan string, 3)
	elisten := make(chan struct{})
	done := make(chan struct{})

	go func() {
		// take any events received on c and append to revents
		for i := 0; i < 3; i++ {
			if i == 0 {
				close(elisten)
			}
			revents <- <-c
		}
		close(done)
	}()

	listen := make(chan struct{}) // signals that we are listening

	go func() {
		<-elisten
		close(listen)
		// ListenForBuildEvents blocks and will write any received events to c
		dl.ListenForBuildEvents(ctx, id, c)
	}()

	<-listen // make sure we're listening
	<-elisten

	time.Sleep(100 * time.Millisecond) // we need this (unfortunately) to make the fake test reliable

	// add some events
	if err := dl.AddEvent(ctx, id, "something happened - 'embedded quoted string'"); err != nil {
		t.Fatalf("error adding event 1: %v", err)
	}
	if err := dl.AddEvent(ctx, id, "something else happened"); err != nil {
		t.Fatalf("error adding event 2: %v", err)
	}
	if err := dl.AddEvent(ctx, id, "ok done"); err != nil {
		t.Fatalf("error adding event 3: %v", err)
	}

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	select {
	case <-done:
		break
	case <-ticker.C:
		t.Fatalf("time out waiting for 3 events")
	}

	if i := len(revents); i != 3 {
		t.Fatalf("expected 3 events, got %v", i)
	}
}

func testDBCancelBuildAndListenForCancellation(t *testing.T, dl datalayer.DataLayer) {
	id, err := dl.CreateBuild(context.Background(), tb)
	if err != nil {
		t.Fatalf("error creating build: %v", err)
	}
	if err := dl.ListenForCancellation(context.Background(), id); err == nil {
		t.Fatalf("listen should have returned error for bad build status")
	}
	if err := dl.SetBuildStatus(context.Background(), id, models.BuildStatusCancelRequested); err != nil {
		t.Fatalf("error setting build status to cancelled: %v", err)
	}
	if err := dl.ListenForCancellation(context.Background(), id); err != nil {
		t.Fatalf("listen should have returned immediately since status is cancel requested")
	}
	if err := dl.SetBuildStatus(context.Background(), id, models.BuildStatusRunning); err != nil {
		t.Fatalf("error setting build status to running: %v", err)
	}
	ctx, cf := context.WithCancel(context.Background())
	defer cf()

	c := make(chan struct{})      // chan signals that ListenForCancellation returned
	listen := make(chan struct{}) // chan used to signal that we're listening

	go func() {
		close(listen)
		dl.ListenForCancellation(ctx, id)
		close(c)
	}()

	cxl := make(chan struct{}) // chan used to signal cancellation

	cancelled := make(chan struct{}) // chan signals that cancellation is completed
	go func() {
		defer close(cancelled)
		<-cxl
		if err := dl.CancelBuild(ctx, id); err != nil {
			fmt.Println(err.Error())
			t.Errorf("error cancelling build: %v", err)
		}
	}()

	<-listen // block until we're listening

	time.Sleep(100 * time.Millisecond) // we need this (unfortunately) to make the fake test reliable

	// build shouldn't be cancelled yet
	select {
	case <-c:
		t.Errorf("build shouldn't be cancelled but is")
	default:
	}

	close(cxl) // allow cancellation request to be sent

	time.Sleep(100 * time.Millisecond) // we need this (unfortunately) to make the fake test reliable

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	<-cancelled

	// build should be cancelled now
	select {
	case <-c:
		break
	case <-ticker.C:
		t.Errorf("timeout: build not cancelled yet but should be")
	}
}

func testDBSetBuildAsRunningAndListenForBuildRunning(t *testing.T, dl datalayer.DataLayer) {
	id, err := dl.CreateBuild(context.Background(), tb)
	if err != nil {
		t.Fatalf("error creating build: %v", err)
	}
	if err := dl.SetBuildStatus(context.Background(), id, models.BuildStatusSuccess); err != nil {
		t.Fatalf("error setting build status to success: %v", err)
	}
	if err := dl.ListenForBuildRunning(context.Background(), id); err == nil {
		t.Fatalf("listen should have returned error for bad build status")
	}
	if err := dl.SetBuildStatus(context.Background(), id, models.BuildStatusRunning); err != nil {
		t.Fatalf("error setting build status to running: %v", err)
	}
	if err := dl.ListenForBuildRunning(context.Background(), id); err != nil {
		t.Fatalf("listen should have returned immediately since status is running")
	}
	if err := dl.SetBuildStatus(context.Background(), id, models.BuildStatusNotStarted); err != nil {
		t.Fatalf("error setting build status to not started: %v", err)
	}

	ctx, cf := context.WithCancel(context.Background())
	defer cf()

	c := make(chan struct{})      // chan signals that ListenForBuildRunning returned
	listen := make(chan struct{}) // chan used to signal that we're listening

	go func() {
		close(listen)
		dl.ListenForBuildRunning(ctx, id)
		close(c)
	}()

	run := make(chan struct{}) // chan used to signal running

	running := make(chan struct{}) // chan signals that build was set as running
	go func() {
		defer close(running)
		<-run
		if err := dl.SetBuildAsRunning(ctx, id); err != nil {
			t.Errorf("error setting build as running: %v", err)
		}
	}()

	<-listen // block until we're listening

	// build shouldn't be running yet
	select {
	case <-c:
		t.Errorf("build shouldn't be running but is")
	default:
	}

	close(run) // allow build to be set as running

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	<-running

	// build should be running now
	select {
	case <-c:
		break
	case <-ticker.C:
		t.Errorf("timeout: build not running yet but should be")
	}
}

func testDBSetBuildAsCompletedAndListenForBuildCompleted(t *testing.T, dl datalayer.DataLayer) {
	id, err := dl.CreateBuild(context.Background(), tb)
	if err != nil {
		t.Fatalf("error creating build: %v", err)
	}

	if err := dl.SetBuildStatus(context.Background(), id, models.BuildStatusUnknown); err != nil {
		t.Fatalf("error setting build status to unknown: %v", err)
	}
	if _, err := dl.ListenForBuildCompleted(context.Background(), id); err == nil {
		t.Fatalf("listen should have returned error for unknown build status")
	}
	if err := dl.SetBuildStatus(context.Background(), id, models.BuildStatusSuccess); err != nil {
		t.Fatalf("error setting build status to success: %v", err)
	}
	if _, err := dl.ListenForBuildCompleted(context.Background(), id); err != nil {
		t.Fatalf("listen should have returned immediately since status is success")
	}
	if err := dl.SetBuildStatus(context.Background(), id, models.BuildStatusRunning); err != nil {
		t.Fatalf("error setting build status to running: %v", err)
	}

	ctx, cf := context.WithCancel(context.Background())
	defer cf()

	c := make(chan models.BuildStatus) // chan signals that build has completed
	defer close(c)
	listen := make(chan struct{}) // chan used to signal that we're listening

	go func() {
		close(listen)
		bs, _ := dl.ListenForBuildCompleted(ctx, id)
		c <- bs
	}()

	done := make(chan struct{}) // chan used to signal completion

	completed := make(chan struct{}) // chan signals that build was set as completed
	go func() {
		defer close(completed)
		<-done
		if err := dl.SetBuildAsCompleted(ctx, id, models.BuildStatusSuccess); err != nil {
			t.Errorf("error setting build as completed: %v", err)
		}
	}()

	<-listen // block until we're listening

	// build shouldn't be completed yet
	select {
	case <-c:
		t.Errorf("build shouldn't be completed but is")
	default:
	}

	close(done) // allow build to be set as running

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	<-completed

	// build should be completed now
	select {
	case s := <-c:
		if s != models.BuildStatusSuccess {
			t.Errorf("bad build status (wanted success): %v", s)
		}
		break
	case <-ticker.C:
		t.Errorf("timeout: build not completed yet but should be")
	}
}

func testDBCreateAPIKey(t *testing.T, dl datalayer.DataLayer) {
	ctx := context.Background()
	id, err := dl.CreateAPIKey(ctx, models.APIKey{ReadOnly: true})
	if err != nil {
		t.Fatalf("error creating api key: %v", err)
	}
	if id == uuid.Nil {
		t.Fatalf("zero value id")
	}
	ak, err := dl.GetAPIKey(ctx, id)
	if err != nil {
		t.Fatalf("error getting key: %v", err)
	}
	if !ak.ReadOnly {
		t.Fatalf("expected read only")
	}
	err = dl.DeleteAPIKey(ctx, id)
	if err != nil {
		t.Fatalf("error deleting api key: %v", err)
	}
}

func testDBGetAPIKey(t *testing.T, dl datalayer.DataLayer) {
	ctx := context.Background()
	id, err := dl.CreateAPIKey(ctx, models.APIKey{ReadOnly: true})
	if err != nil {
		t.Fatalf("error creating api key: %v", err)
	}
	ak, err := dl.GetAPIKey(ctx, id)
	if err != nil {
		t.Fatalf("error getting api key: %v", err)
	}
	if !ak.ReadOnly {
		t.Fatalf("expected read only")
	}
	_, err = dl.GetAPIKey(ctx, uuid.Must(uuid.NewV4()))
	if err == nil {
		t.Fatalf("expected id missing error")
	}
	err = dl.DeleteAPIKey(ctx, id)
	if err != nil {
		t.Fatalf("error deleting build: %v", err)
	}
}

func testDBDeleteAPIKey(t *testing.T, dl datalayer.DataLayer) {
	ctx := context.Background()
	id, err := dl.CreateAPIKey(ctx, models.APIKey{ReadOnly: true})
	if err != nil {
		t.Fatalf("error creating api key: %v", err)
	}
	err = dl.DeleteAPIKey(ctx, id)
	if err != nil {
		t.Fatalf("error deleting api key: %v", err)
	}
	err = dl.DeleteAPIKey(ctx, uuid.Must(uuid.NewV4()))
	// should succeed even if api key doesn't exist
	if err != nil {
		t.Fatalf("error deleting missing api key: %v", err)
	}
}
