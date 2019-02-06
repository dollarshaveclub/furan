package datalayer_test

import (
	"testing"

	"gopkg.in/DataDog/dd-trace-go.v1/ddtrace/tracer"

	pb "github.com/dollarshaveclub/furan/generated/lib"
	"github.com/dollarshaveclub/furan/lib/datalayer"
)

var testSpan = tracer.StartSpan("test")

var tbr = &pb.BuildRequest{
	Build: &pb.BuildDefinition{
		GithubRepo:       "foobar/baz",
		Ref:              "master",
		Tags:             []string{"master"},
		TagWithCommitSha: true,
	},
	Push: &pb.PushDefinition{
		Registry: &pb.PushRegistryDefinition{},
		S3: &pb.PushS3Definition{
			Bucket:    "asdf",
			Region:    "us-east-1",
			KeyPrefix: "qwerty",
		},
	},
}

func TestDBCreateBuild(t *testing.T) {
	dl := datalayer.NewDBLayer(ts)
	id, err := dl.CreateBuild(testSpan, tbr)
	if err != nil {
		t.Fatalf("error creating build: %v", err)
	}
	err = dl.DeleteBuild(testSpan, id)
	if err != nil {
		t.Fatalf("error deleting build: %v", err)
	}
}

func TestDBGetBuildByID(t *testing.T) {
	dl := datalayer.NewDBLayer(ts)
	id, err := dl.CreateBuild(testSpan, tbr)
	if err != nil {
		t.Fatalf("error creating build: %v", err)
	}
	bsr, err := dl.GetBuildByID(testSpan, id)
	if err != nil {
		t.Fatalf("error getting build by ID: %v", err)
	}
	if bsr.BuildId != id.String() {
		t.Fatalf("incorrect build id: %v (expected %v)", bsr.BuildId, id.String())
	}
	err = dl.DeleteBuild(testSpan, id)
	if err != nil {
		t.Fatalf("error deleting build: %v", err)
	}
}

func TestDBSetBuildFlags(t *testing.T) {
	dl := datalayer.NewDBLayer(ts)
	id, err := dl.CreateBuild(testSpan, tbr)
	if err != nil {
		t.Fatalf("error creating build: %v", err)
	}
	flags := map[string]bool{
		"finished":  true,
		"failed":    true,
		"cancelled": true,
	}
	err = dl.SetBuildFlags(testSpan, id, flags)
	if err != nil {
		t.Fatalf("error setting build flags: %v", err)
	}
	err = dl.DeleteBuild(testSpan, id)
	if err != nil {
		t.Fatalf("error deleting build: %v", err)
	}
}

func TestDBSetBuildCompletedTimestamp(t *testing.T) {
	dl := datalayer.NewDBLayer(ts)
	id, err := dl.CreateBuild(testSpan, tbr)
	if err != nil {
		t.Fatalf("error creating build: %v", err)
	}
	err = dl.SetBuildCompletedTimestamp(testSpan, id)
	if err != nil {
		t.Fatalf("error setting build completed timestamp: %v", err)
	}
	err = dl.DeleteBuild(testSpan, id)
	if err != nil {
		t.Fatalf("error deleting build: %v", err)
	}
}

func TestDBSetBuildState(t *testing.T) {
	dl := datalayer.NewDBLayer(ts)
	id, err := dl.CreateBuild(testSpan, tbr)
	if err != nil {
		t.Fatalf("error creating build: %v", err)
	}
	err = dl.SetBuildState(testSpan, id, pb.BuildStatusResponse_BUILDING)
	if err != nil {
		t.Fatalf("error setting build state: %v", err)
	}
	err = dl.DeleteBuild(testSpan, id)
	if err != nil {
		t.Fatalf("error deleting build: %v", err)
	}
}

func TestDBSetBuildTimeMetric(t *testing.T) {
	dl := datalayer.NewDBLayer(ts)
	id, err := dl.CreateBuild(testSpan, tbr)
	if err != nil {
		t.Fatalf("error creating build: %v", err)
	}
	for _, m := range []string{"docker_build_completed", "push_completed", "clean_completed"} {
		err = dl.SetBuildTimeMetric(testSpan, id, m)
		if err != nil {
			t.Fatalf("error setting build time metric: %v", err)
		}
	}
	err = dl.SetBuildTimeMetric(testSpan, id, "invalid_metric_name")
	if err == nil {
		t.Fatalf("invalid build metric should have failed")
	}
	err = dl.DeleteBuild(testSpan, id)
	if err != nil {
		t.Fatalf("error deleting build: %v", err)
	}
}

func TestDBSetDockerImageSizesMetric(t *testing.T) {
	dl := datalayer.NewDBLayer(ts)
	id, err := dl.CreateBuild(testSpan, tbr)
	if err != nil {
		t.Fatalf("error creating build: %v", err)
	}
	err = dl.SetDockerImageSizesMetric(testSpan, id, 10000, 999999)
	if err != nil {
		t.Fatalf("error setting docker image sizes metric: %v", err)
	}
	err = dl.DeleteBuild(testSpan, id)
	if err != nil {
		t.Fatalf("error deleting build: %v", err)
	}
}

func TestDBSaveBuildOutput(t *testing.T) {
	dl := datalayer.NewDBLayer(ts)
	id, err := dl.CreateBuild(testSpan, tbr)
	if err != nil {
		t.Fatalf("error creating build: %v", err)
	}
	events := []pb.BuildEvent{
		pb.BuildEvent{
			BuildId: id.String(),
			EventError: &pb.BuildEventError{
				ErrorType: pb.BuildEventError_NO_ERROR,
			},
			EventType: pb.BuildEvent_DOCKER_BUILD_STREAM,
			Message:   "something happened",
		},
	}
	err = dl.SaveBuildOutput(testSpan, id, events, "build_output")
	if err != nil {
		t.Fatalf("error setting build_output: %v", err)
	}
	err = dl.SaveBuildOutput(testSpan, id, events, "push_output")
	if err != nil {
		t.Fatalf("error setting push_output: %v", err)
	}
	err = dl.SaveBuildOutput(testSpan, id, events, "invalid_column")
	if err == nil {
		t.Fatalf("invalid column should have failed")
	}
	err = dl.DeleteBuild(testSpan, id)
	if err != nil {
		t.Fatalf("error deleting build: %v", err)
	}
}

func TestDBGetBuildOutput(t *testing.T) {
	dl := datalayer.NewDBLayer(ts)
	id, err := dl.CreateBuild(testSpan, tbr)
	if err != nil {
		t.Fatalf("error creating build: %v", err)
	}
	events := []pb.BuildEvent{
		pb.BuildEvent{
			BuildId: id.String(),
			EventError: &pb.BuildEventError{
				ErrorType: pb.BuildEventError_NO_ERROR,
			},
			EventType: pb.BuildEvent_DOCKER_BUILD_STREAM,
			Message:   "something happened",
		},
	}
	err = dl.SaveBuildOutput(testSpan, id, events, "build_output")
	if err != nil {
		t.Fatalf("error setting build_output: %v", err)
	}
	evl, err := dl.GetBuildOutput(testSpan, id, "build_output")
	if err != nil {
		t.Fatalf("error getting build output: %v", err)
	}
	if len(evl) != 1 {
		t.Fatalf("unexpected number of events (wanted 1): %v", len(evl))
	}
	if evl[0].BuildId != id.String() {
		t.Fatalf("bad build id: %v", evl[0].BuildId)
	}
	err = dl.DeleteBuild(testSpan, id)
	if err != nil {
		t.Fatalf("error deleting build: %v", err)
	}
}
