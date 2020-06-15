package datalayer

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/gofrs/uuid"

	"github.com/dollarshaveclub/furan/pkg/models"
)

type FakeDataLayer struct {
	mtx       sync.RWMutex
	d         map[uuid.UUID]*models.Build
	listeners map[uuid.UUID][]chan string
}

var _ DataLayer = &FakeDataLayer{}

func (fdl *FakeDataLayer) init() {
	if fdl.d == nil {
		fdl.mtx.Lock()
		fdl.d = make(map[uuid.UUID]*models.Build)
		fdl.mtx.Unlock()
	}
	if fdl.listeners == nil {
		fdl.mtx.Lock()
		fdl.listeners = make(map[uuid.UUID][]chan string)
		fdl.mtx.Unlock()
	}
}

func (fdl *FakeDataLayer) CreateBuild(ctx context.Context, b models.Build) (uuid.UUID, error) {
	fdl.init()
	fdl.mtx.Lock()
	defer fdl.mtx.Unlock()
	b.ID = uuid.Must(uuid.NewV4())
	fdl.d[b.ID] = &b
	return b.ID, nil
}
func (fdl *FakeDataLayer) GetBuildByID(ctx context.Context, id uuid.UUID) (models.Build, error) {
	fdl.init()
	fdl.mtx.RLock()
	defer fdl.mtx.RUnlock()
	bsr, ok := fdl.d[id]
	if !ok {
		return models.Build{}, fmt.Errorf("not found")
	}
	return *bsr, nil
}

func (fdl *FakeDataLayer) SetBuildCompletedTimestamp(ctx context.Context, id uuid.UUID, ts time.Time) error {
	fdl.init()
	fdl.mtx.Lock()
	defer fdl.mtx.Unlock()
	bsr, ok := fdl.d[id]
	if ok {
		bsr.Completed = ts
	}
	return nil
}

func (fdl *FakeDataLayer) SetBuildStatus(ctx context.Context, id uuid.UUID, s models.BuildStatus) error {
	fdl.init()
	fdl.mtx.Lock()
	defer fdl.mtx.Unlock()
	bsr, ok := fdl.d[id]
	if ok {
		bsr.Status = s
	}
	return nil
}

func (fdl *FakeDataLayer) DeleteBuild(ctx context.Context, id uuid.UUID) error {
	fdl.init()
	fdl.mtx.Lock()
	defer fdl.mtx.Unlock()
	delete(fdl.d, id)
	return nil
}

func (fdl *FakeDataLayer) ListenForBuildEvents(ctx context.Context, id uuid.UUID, c chan<- string) error {
	fdl.init()

	fdl.mtx.RLock()
	b, ok := fdl.d[id]
	if !ok {
		fdl.mtx.RUnlock()
		return fmt.Errorf("build not found")
	}
	fdl.mtx.RUnlock()

	if !b.CanAddEvent() {
		return fmt.Errorf("cannot add event to build with status %v", b.Status)
	}

	lc := make(chan string)

	fdl.mtx.Lock()
	fdl.listeners[id] = append(fdl.listeners[id], lc)
	i := len(fdl.listeners[id]) - 1 // index of listener
	fdl.mtx.Unlock()
	defer func() {
		// remove listener chan from listeners
		fdl.mtx.Lock()
		if i == 0 {
			delete(fdl.listeners, id)
			return
		}
		fdl.listeners[id] = append(fdl.listeners[id][:i], fdl.listeners[id][i+1:]...)
		fdl.mtx.Unlock()
		close(lc)
	}()

	for {
		select {
		case e := <-lc:
			c <- e
		case <-ctx.Done():
			return fmt.Errorf("context cancelled")
		}
	}
}

func (fdl *FakeDataLayer) AddEvent(ctx context.Context, id uuid.UUID, event string) error {
	fdl.init()

	fdl.mtx.Lock()
	fdl.d[id].Events = append(fdl.d[id].Events, event)
	fdl.mtx.Unlock()

	fdl.mtx.RLock()
	defer fdl.mtx.RUnlock()

	for _, c := range fdl.listeners[id] {
		c <- event
	}

	return nil
}
