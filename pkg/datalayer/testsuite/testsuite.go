package testsuite

import (
	"context"
	"testing"

	"github.com/dollarshaveclub/furan/pkg/datalayer"
	"github.com/dollarshaveclub/furan/pkg/generated/furanrpc"
)

// DLFactoryFunc is a function that returns an empty DataLayer
type DLFactoryFunc func() datalayer.DataLayer

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
			"get build by ID",
			testDBGetBuildByID,
		},
		{
			"set build flags",
			testDBSetBuildFlags,
		},
		{
			"set build completed timestamp",
			testDBSetBuildCompletedTimestamp,
		},
		{
			"set build state",
			testDBSetBuildState,
		},
		{
			"set build time metric",
			testDBSetBuildTimeMetric,
		},
		{
			"set docker image sizes metric",
			testDBSetDockerImageSizesMetric,
		},
		{
			"save build output",
			testDBSaveBuildOutput,
		},
		{
			"get build output",
			testDBGetBuildOutput,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.tfunc(t, dlfunc())
		})
	}
}

var tbr = &furanrpc.BuildRequest{
	Build: &furanrpc.BuildDefinition{
		GithubRepo:       "foobar/baz",
		Ref:              "master",
		Tags:             []string{"master"},
		TagWithCommitSha: true,
	},
	Push: &furanrpc.PushDefinition{
		Registry: &furanrpc.PushRegistryDefinition{},
		S3: &furanrpc.PushS3Definition{
			Bucket:    "asdf",
			Region:    "us-east-1",
			KeyPrefix: "qwerty",
		},
	},
}

func testDBCreateBuild(t *testing.T, dl datalayer.DataLayer) {
	id, err := dl.CreateBuild(context.Background(), tbr)
	if err != nil {
		t.Fatalf("error creating build: %v", err)
	}
	err = dl.DeleteBuild(context.Background(), id)
	if err != nil {
		t.Fatalf("error deleting build: %v", err)
	}
}

func testDBGetBuildByID(t *testing.T, dl datalayer.DataLayer) {
	id, err := dl.CreateBuild(context.Background(), tbr)
	if err != nil {
		t.Fatalf("error creating build: %v", err)
	}
	bsr, err := dl.GetBuildByID(context.Background(), id)
	if err != nil {
		t.Fatalf("error getting build by ID: %v", err)
	}
	if bsr.BuildId != id.String() {
		t.Fatalf("incorrect build id: %v (expected %v)", bsr.BuildId, id.String())
	}
	err = dl.DeleteBuild(context.Background(), id)
	if err != nil {
		t.Fatalf("error deleting build: %v", err)
	}
}

func testDBSetBuildFlags(t *testing.T, dl datalayer.DataLayer) {
	id, err := dl.CreateBuild(context.Background(), tbr)
	if err != nil {
		t.Fatalf("error creating build: %v", err)
	}
	flags := map[string]bool{
		"finished":  true,
		"failed":    true,
		"cancelled": true,
	}
	err = dl.SetBuildFlags(context.Background(), id, flags)
	if err != nil {
		t.Fatalf("error setting build flags: %v", err)
	}
	err = dl.DeleteBuild(context.Background(), id)
	if err != nil {
		t.Fatalf("error deleting build: %v", err)
	}
}

func testDBSetBuildCompletedTimestamp(t *testing.T, dl datalayer.DataLayer) {
	id, err := dl.CreateBuild(context.Background(), tbr)
	if err != nil {
		t.Fatalf("error creating build: %v", err)
	}
	err = dl.SetBuildCompletedTimestamp(context.Background(), id)
	if err != nil {
		t.Fatalf("error setting build completed timestamp: %v", err)
	}
	err = dl.DeleteBuild(context.Background(), id)
	if err != nil {
		t.Fatalf("error deleting build: %v", err)
	}
}

func testDBSetBuildState(t *testing.T, dl datalayer.DataLayer) {
	id, err := dl.CreateBuild(context.Background(), tbr)
	if err != nil {
		t.Fatalf("error creating build: %v", err)
	}
	err = dl.SetBuildState(context.Background(), id, furanrpc.BuildStatusResponse_BUILDING)
	if err != nil {
		t.Fatalf("error setting build state: %v", err)
	}
	err = dl.DeleteBuild(context.Background(), id)
	if err != nil {
		t.Fatalf("error deleting build: %v", err)
	}
}

func testDBSetBuildTimeMetric(t *testing.T, dl datalayer.DataLayer) {
	id, err := dl.CreateBuild(context.Background(), tbr)
	if err != nil {
		t.Fatalf("error creating build: %v", err)
	}
	for _, m := range []string{"docker_build_completed", "push_completed", "clean_completed"} {
		err = dl.SetBuildTimeMetric(context.Background(), id, m)
		if err != nil {
			t.Fatalf("error setting build time metric: %v", err)
		}
	}
	err = dl.SetBuildTimeMetric(context.Background(), id, "invalid_metric_name")
	if err == nil {
		t.Fatalf("invalid build metric should have failed")
	}
	err = dl.DeleteBuild(context.Background(), id)
	if err != nil {
		t.Fatalf("error deleting build: %v", err)
	}
}

func testDBSetDockerImageSizesMetric(t *testing.T, dl datalayer.DataLayer) {
	id, err := dl.CreateBuild(context.Background(), tbr)
	if err != nil {
		t.Fatalf("error creating build: %v", err)
	}
	err = dl.SetDockerImageSizesMetric(context.Background(), id, 10000, 999999)
	if err != nil {
		t.Fatalf("error setting docker image sizes metric: %v", err)
	}
	err = dl.DeleteBuild(context.Background(), id)
	if err != nil {
		t.Fatalf("error deleting build: %v", err)
	}
}

func testDBSaveBuildOutput(t *testing.T, dl datalayer.DataLayer) {
	id, err := dl.CreateBuild(context.Background(), tbr)
	if err != nil {
		t.Fatalf("error creating build: %v", err)
	}
	events := []furanrpc.BuildEvent{
		furanrpc.BuildEvent{
			BuildId: id.String(),
			EventError: &furanrpc.BuildEventError{
				ErrorType: furanrpc.BuildEventError_NO_ERROR,
			},
			EventType: furanrpc.BuildEvent_DOCKER_BUILD_STREAM,
			Message:   "something happened",
		},
	}
	err = dl.SaveBuildOutput(context.Background(), id, events, "build_output")
	if err != nil {
		t.Fatalf("error setting build_output: %v", err)
	}
	err = dl.SaveBuildOutput(context.Background(), id, events, "push_output")
	if err != nil {
		t.Fatalf("error setting push_output: %v", err)
	}
	err = dl.SaveBuildOutput(context.Background(), id, events, "invalid_column")
	if err == nil {
		t.Fatalf("invalid column should have failed")
	}
	err = dl.DeleteBuild(context.Background(), id)
	if err != nil {
		t.Fatalf("error deleting build: %v", err)
	}
}

func testDBGetBuildOutput(t *testing.T, dl datalayer.DataLayer) {
	id, err := dl.CreateBuild(context.Background(), tbr)
	if err != nil {
		t.Fatalf("error creating build: %v", err)
	}
	events := []furanrpc.BuildEvent{
		furanrpc.BuildEvent{
			BuildId: id.String(),
			EventError: &furanrpc.BuildEventError{
				ErrorType: furanrpc.BuildEventError_NO_ERROR,
			},
			EventType: furanrpc.BuildEvent_DOCKER_BUILD_STREAM,
			Message:   "something happened",
		},
	}
	err = dl.SaveBuildOutput(context.Background(), id, events, "build_output")
	if err != nil {
		t.Fatalf("error setting build_output: %v", err)
	}
	evl, err := dl.GetBuildOutput(context.Background(), id, "build_output")
	if err != nil {
		t.Fatalf("error getting build output: %v", err)
	}
	if len(evl) != 1 {
		t.Fatalf("unexpected number of events (wanted 1): %v", len(evl))
	}
	if evl[0].BuildId != id.String() {
		t.Fatalf("bad build id: %v", evl[0].BuildId)
	}
	err = dl.DeleteBuild(context.Background(), id)
	if err != nil {
		t.Fatalf("error deleting build: %v", err)
	}
}
