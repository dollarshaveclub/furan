package datalayer

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/dollarshaveclub/furan/pkg/generated/furanrpc"
	"github.com/gocql/gocql"
)

type eventStreams struct {
	BuildEvents, PushEvents []furanrpc.BuildEvent
}

type FakeDataLayer struct {
	mtx sync.RWMutex
	d   map[gocql.UUID]*furanrpc.BuildStatusResponse
	bo  map[gocql.UUID]*eventStreams
}

var _ DataLayer = &FakeDataLayer{}

func (fdl *FakeDataLayer) init() {
	if fdl.d == nil {
		fdl.mtx.Lock()
		fdl.d = make(map[gocql.UUID]*furanrpc.BuildStatusResponse)
		fdl.mtx.Unlock()
	}
	if fdl.bo == nil {
		fdl.mtx.Lock()
		fdl.bo = make(map[gocql.UUID]*eventStreams)
		fdl.mtx.Unlock()
	}
}

func (fdl *FakeDataLayer) CreateBuild(ctx context.Context, req *furanrpc.BuildRequest) (gocql.UUID, error) {
	fdl.init()
	fdl.mtx.Lock()
	defer fdl.mtx.Unlock()
	id, _ := gocql.RandomUUID()
	fdl.d[id] = &furanrpc.BuildStatusResponse{
		BuildId:      id.String(),
		BuildRequest: req,
	}
	return id, nil
}
func (fdl *FakeDataLayer) GetBuildByID(ctx context.Context, id gocql.UUID) (*furanrpc.BuildStatusResponse, error) {
	fdl.init()
	fdl.mtx.RLock()
	defer fdl.mtx.RUnlock()
	bsr, ok := fdl.d[id]
	if !ok {
		return nil, fmt.Errorf("not found")
	}
	return bsr, nil
}
func (fdl *FakeDataLayer) SetBuildFlags(ctx context.Context, id gocql.UUID, flags map[string]bool) error {
	fdl.init()
	fdl.mtx.Lock()
	defer fdl.mtx.Unlock()
	bsr, ok := fdl.d[id]
	if ok {
		if finished, ok := flags["finished"]; ok {
			bsr.Finished = finished
		}
		if failed, ok := flags["failed"]; ok {
			bsr.Failed = failed
		}
		if cancelled, ok := flags["cancelled"]; ok {
			bsr.Cancelled = cancelled
		}
	}
	return nil
}

func (fdl *FakeDataLayer) SetBuildCompletedTimestamp(ctx context.Context, id gocql.UUID) error {
	fdl.init()
	fdl.mtx.Lock()
	defer fdl.mtx.Unlock()
	bsr, ok := fdl.d[id]
	if ok {
		bsr.Completed = time.Now().UTC().String()
	}
	return nil
}

func (fdl *FakeDataLayer) SetBuildState(ctx context.Context, id gocql.UUID, state furanrpc.BuildStatusResponse_BuildState) error {
	fdl.init()
	fdl.mtx.Lock()
	defer fdl.mtx.Unlock()
	bsr, ok := fdl.d[id]
	if ok {
		bsr.State = state
	}
	return nil
}

func (fdl *FakeDataLayer) DeleteBuild(ctx context.Context, id gocql.UUID) error {
	fdl.init()
	fdl.mtx.Lock()
	defer fdl.mtx.Unlock()
	delete(fdl.d, id)
	return nil
}
func (fdl *FakeDataLayer) SetBuildTimeMetric(ctx context.Context, id gocql.UUID, metric string) error {
	fdl.init()
	switch metric {
	case "docker_build_started":
		fallthrough
	case "docker_build_completed":
		fallthrough
	case "push_started":
		fallthrough
	case "push_completed":
		fallthrough
	case "clean_completed":
		return nil
	default:
		return fmt.Errorf("bad metric name: %v", metric)
	}
}

func (fdl *FakeDataLayer) SetDockerImageSizesMetric(context.Context, gocql.UUID, int64, int64) error {
	fdl.init()
	return nil
}

func (fdl *FakeDataLayer) SaveBuildOutput(ctx context.Context, id gocql.UUID, events []furanrpc.BuildEvent, column string) error {
	fdl.init()
	fdl.mtx.Lock()
	defer fdl.mtx.Unlock()
	es, ok := fdl.bo[id]
	if !ok {
		es = &eventStreams{}
		fdl.bo[id] = es
	}
	switch column {
	case "build_output":
		es.BuildEvents = events
	case "push_output":
		es.PushEvents = events
	default:
		return fmt.Errorf("bad column")
	}
	return nil
}

func (fdl *FakeDataLayer) GetBuildOutput(ctx context.Context, id gocql.UUID, column string) ([]furanrpc.BuildEvent, error) {
	fdl.init()
	fdl.mtx.RLock()
	defer fdl.mtx.RUnlock()
	es, ok := fdl.bo[id]
	if !ok {
		return nil, fmt.Errorf("not found")
	}
	switch column {
	case "build_output":
		return es.BuildEvents, nil
	case "push_output":
		return es.PushEvents, nil
	default:
		return nil, fmt.Errorf("bad column")
	}
}
