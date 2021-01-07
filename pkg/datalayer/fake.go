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
	mtx           sync.RWMutex
	d             map[uuid.UUID]*models.Build
	apikeys       map[uuid.UUID]*models.APIKey
	listeners     map[uuid.UUID][]chan string
	cxllisteners  map[uuid.UUID][]chan struct{}
	runlisteners  map[uuid.UUID][]chan struct{}
	donelisteners map[uuid.UUID][]chan models.BuildStatus
}

var _ DataLayer = &FakeDataLayer{}

func (fdl *FakeDataLayer) init() {
	if fdl.d == nil {
		fdl.mtx.Lock()
		fdl.d = make(map[uuid.UUID]*models.Build)
		fdl.mtx.Unlock()
	}
	if fdl.apikeys == nil {
		fdl.mtx.Lock()
		fdl.apikeys = make(map[uuid.UUID]*models.APIKey)
		fdl.mtx.Unlock()
	}
	if fdl.listeners == nil {
		fdl.mtx.Lock()
		fdl.listeners = make(map[uuid.UUID][]chan string)
		fdl.mtx.Unlock()
	}
	if fdl.cxllisteners == nil {
		fdl.mtx.Lock()
		fdl.cxllisteners = make(map[uuid.UUID][]chan struct{})
		fdl.mtx.Unlock()
	}
	if fdl.runlisteners == nil {
		fdl.mtx.Lock()
		fdl.runlisteners = make(map[uuid.UUID][]chan struct{})
		fdl.mtx.Unlock()
	}
	if fdl.donelisteners == nil {
		fdl.mtx.Lock()
		fdl.donelisteners = make(map[uuid.UUID][]chan models.BuildStatus)
		fdl.mtx.Unlock()
	}
}

func (fdl *FakeDataLayer) CreateBuild(ctx context.Context, b models.Build) (uuid.UUID, error) {
	fdl.init()
	fdl.mtx.Lock()
	defer fdl.mtx.Unlock()
	b.ID = uuid.Must(uuid.NewV4())
	b.Status = models.BuildStatusNotStarted
	b.Created = time.Now().UTC()
	fdl.d[b.ID] = &b
	return b.ID, nil
}
func (fdl *FakeDataLayer) GetBuildByID(ctx context.Context, id uuid.UUID) (models.Build, error) {
	fdl.init()
	fdl.mtx.RLock()
	defer fdl.mtx.RUnlock()
	bsr, ok := fdl.d[id]
	if !ok {
		return models.Build{}, ErrNotFound
	}
	return *bsr, nil
}

func (fdl *FakeDataLayer) ListBuilds(ctx context.Context, opts ListBuildsOptions) ([]models.Build, error) {
	fdl.init()
	fdl.mtx.RLock()
	defer fdl.mtx.RUnlock()
	zero := ListBuildsOptions{}
	if opts == zero {
		return nil, fmt.Errorf("at least one list option must be supplied")
	}
	// make sure a full table dump isn't getting requested
	if !opts.CompletedBefore.IsZero() && !opts.CompletedAfter.IsZero() {
		if opts.CompletedBefore.Equal(opts.CompletedAfter) {
			return nil, fmt.Errorf("CompletedBefore and CompletedAfter cannot be equal")
		}
	}
	if !opts.StartedBefore.IsZero() && !opts.StartedAfter.IsZero() {
		if opts.StartedBefore.Equal(opts.StartedAfter) {
			return nil, fmt.Errorf("StartedBefore and StartedAfter cannot be equal")
		}
	}
	filter := func(builds []models.Build, cf func(e models.Build) bool) []models.Build {
		pres := []models.Build{}
		for _, b := range builds {
			if cf(b) {
				pres = append(pres, b)
			}
		}
		return pres
	}
	out := make([]models.Build, len(fdl.d))
	i := 0
	for _, v := range fdl.d {
		out[i] = *v
		i++
	}
	if opts.WithGitHubRepo != "" {
		out = filter(out, func(e models.Build) bool {
			return e.GitHubRepo == opts.WithGitHubRepo
		})
	}
	if opts.WithGitHubRef != "" {
		out = filter(out, func(e models.Build) bool {
			return e.GitHubRef == opts.WithGitHubRef
		})
	}
	if opts.WithImageRepo != "" {
		out = filter(out, func(e models.Build) bool {
			for _, ir := range e.ImageRepos {
				if ir == opts.WithImageRepo {
					return true
				}
			}
			return false
		})
	}
	if opts.WithStatus != models.BuildStatusUnknown {
		out = filter(out, func(e models.Build) bool {
			return e.Status == opts.WithStatus
		})
	}
	if !opts.CompletedAfter.IsZero() {
		out = filter(out, func(e models.Build) bool {
			return e.Completed.After(opts.CompletedAfter) || e.Completed.Equal(opts.CompletedAfter)
		})
	}
	if !opts.StartedAfter.IsZero() {
		out = filter(out, func(e models.Build) bool {
			return e.Created.After(opts.StartedAfter) || e.Created.Equal(opts.StartedAfter)
		})
	}
	if !opts.CompletedBefore.IsZero() {
		out = filter(out, func(e models.Build) bool {
			if e.Completed.IsZero() {
				return false
			}
			return e.Completed.Before(opts.CompletedBefore)
		})
	}
	if !opts.StartedBefore.IsZero() {
		out = filter(out, func(e models.Build) bool {
			if e.Created.IsZero() {
				return false
			}
			return e.Created.Before(opts.StartedBefore)
		})
	}
	if opts.Limit > 0 && len(out) > int(opts.Limit) {
		return out[:opts.Limit], nil
	}
	return out, nil
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

	if !b.CanAddEvent() {
		fdl.mtx.RUnlock()
		return fmt.Errorf("cannot add event to build with status %v", b.Status)
	}
	fdl.mtx.RUnlock()

	lc := make(chan string)

	fdl.mtx.Lock()
	fdl.listeners[id] = append(fdl.listeners[id], lc)
	i := len(fdl.listeners[id]) - 1 // index of listener
	fdl.mtx.Unlock()
	defer func() {
		// remove listener chan from listeners
		fdl.mtx.Lock()
		i = len(fdl.listeners[id]) - 1
		if i < 0 {
			fdl.mtx.Unlock()
			return
		}
		if i == 0 {
			delete(fdl.listeners, id)
			fdl.mtx.Unlock()
			return
		}
		fdl.listeners[id] = append(fdl.listeners[id][:i], fdl.listeners[id][i+1:]...)
		fdl.mtx.Unlock()
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
	b, ok := fdl.d[id]
	if !ok {
		fdl.mtx.Unlock()
		return nil
	}
	b.Events = append(fdl.d[id].Events, event)
	fdl.mtx.Unlock()

	fdl.mtx.RLock()
	defer fdl.mtx.RUnlock()

	for _, c := range fdl.listeners[id] {
		c <- event
	}

	return nil
}

func (fdl *FakeDataLayer) CancelBuild(ctx context.Context, id uuid.UUID) error {
	fdl.init()

	fdl.mtx.Lock()
	b, ok := fdl.d[id]
	if ok && b != nil {
		b.Status = models.BuildStatusCancelRequested
	}
	fdl.mtx.Unlock()

	fdl.mtx.RLock()
	defer fdl.mtx.RUnlock()

	for _, c := range fdl.cxllisteners[id] {
		c <- struct{}{}
	}

	return nil
}

func (fdl *FakeDataLayer) CancellationListeners() uint {
	fdl.init()
	fdl.mtx.RLock()
	defer fdl.mtx.RUnlock()
	return uint(len(fdl.cxllisteners))
}

func (fdl *FakeDataLayer) ListenForCancellation(ctx context.Context, id uuid.UUID) error {
	fdl.init()

	fdl.mtx.RLock()
	b, ok := fdl.d[id]
	if !ok {
		fdl.mtx.RUnlock()
		return fmt.Errorf("build not found")
	}

	switch {
	case b.Running():
		break
	case b.Status == models.BuildStatusCancelRequested || b.Status == models.BuildStatusCancelled:
		fdl.mtx.RUnlock()
		return nil
	default:
		fdl.mtx.RUnlock()
		return fmt.Errorf("unexpected status for build (wanted Running or Cancelled): %v", b.Status.String())
	}
	fdl.mtx.RUnlock()

	lc := make(chan struct{})

	fdl.mtx.Lock()
	fdl.cxllisteners[id] = append(fdl.cxllisteners[id], lc)
	i := len(fdl.cxllisteners[id]) - 1 // index of listener
	fdl.mtx.Unlock()
	defer func() {
		// remove listener chan from cxllisteners
		fdl.mtx.Lock()
		i = len(fdl.cxllisteners[id]) - 1
		if i < 0 {
			fdl.mtx.Unlock()
			return
		}
		if i == 0 {
			delete(fdl.cxllisteners, id)
			fdl.mtx.Unlock()
			return
		}
		fdl.cxllisteners[id] = append(fdl.cxllisteners[id][:i], fdl.cxllisteners[id][i+1:]...)
		fdl.mtx.Unlock()
		close(lc)
	}()

	select {
	case <-lc:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("context cancelled")
	}
}

func (fdl *FakeDataLayer) SetBuildAsRunning(ctx context.Context, id uuid.UUID) error {
	fdl.init()

	fdl.mtx.Lock()
	b, ok := fdl.d[id]
	if ok && b != nil {
		b.Status = models.BuildStatusRunning
	}
	fdl.mtx.Unlock()

	fdl.mtx.RLock()
	defer fdl.mtx.RUnlock()

	for _, c := range fdl.runlisteners[id] {
		c <- struct{}{}
	}

	return nil
}

func (fdl *FakeDataLayer) ListenForBuildRunning(ctx context.Context, id uuid.UUID) error {
	fdl.init()

	fdl.mtx.RLock()
	b, ok := fdl.d[id]
	if !ok {
		fdl.mtx.RUnlock()
		return fmt.Errorf("build not found")
	}
	fdl.mtx.RUnlock()

	switch b.Status {
	case models.BuildStatusRunning:
		return nil
	case models.BuildStatusNotStarted:
		break
	default:
		return fmt.Errorf("unexpected build status (wanted Running or NotStarted): %v", b.Status.String())
	}

	lc := make(chan struct{})

	fdl.mtx.Lock()
	fdl.runlisteners[id] = append(fdl.runlisteners[id], lc)
	i := len(fdl.runlisteners[id]) - 1 // index of listener
	fdl.mtx.Unlock()
	defer func() {
		// remove listener chan from runlisteners
		fdl.mtx.Lock()
		i = len(fdl.runlisteners[id]) - 1
		if i < 0 {
			fdl.mtx.Unlock()
			return
		}
		if i == 0 {
			delete(fdl.runlisteners, id)
			fdl.mtx.Unlock()
			return
		}
		fdl.runlisteners[id] = append(fdl.runlisteners[id][:i], fdl.runlisteners[id][i+1:]...)
		fdl.mtx.Unlock()
		close(lc)
	}()

	select {
	case <-lc:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("context cancelled")
	}
}

func (fdl *FakeDataLayer) SetBuildAsCompleted(ctx context.Context, id uuid.UUID, status models.BuildStatus) error {
	fdl.init()

	fdl.mtx.Lock()
	b, ok := fdl.d[id]
	if ok && b != nil {
		b.Status = status
	}
	fdl.mtx.Unlock()

	fdl.mtx.RLock()
	defer fdl.mtx.RUnlock()

	for _, c := range fdl.donelisteners[id] {
		c <- status
	}

	return nil
}

func (fdl *FakeDataLayer) ListenForBuildCompleted(ctx context.Context, id uuid.UUID) (models.BuildStatus, error) {
	fdl.init()

	fdl.mtx.RLock()
	b, ok := fdl.d[id]
	if !ok {
		fdl.mtx.RUnlock()
		return 0, fmt.Errorf("build not found")
	}
	switch {
	case b.Status.TerminalState(): // if build is already finished, return status
		fdl.mtx.RUnlock()
		return b.Status, nil
	case b.Status == models.BuildStatusRunning || b.Status == models.BuildStatusNotStarted:
		break
	default:
		fdl.mtx.RUnlock()
		return b.Status, fmt.Errorf("unknown or invalid build status: %v", b.Status)
	}
	fdl.mtx.RUnlock()

	lc := make(chan models.BuildStatus)

	fdl.mtx.Lock()
	fdl.donelisteners[id] = append(fdl.donelisteners[id], lc)
	i := len(fdl.donelisteners[id]) - 1 // index of listener
	fdl.mtx.Unlock()
	defer func() {
		// remove listener chan from donelisteners
		fdl.mtx.Lock()
		i = len(fdl.donelisteners[id]) - 1
		if i < 0 {
			fdl.mtx.Unlock()
			return
		}
		if i == 0 {
			delete(fdl.donelisteners, id)
			fdl.mtx.Unlock()
			return
		}
		fdl.donelisteners[id] = append(fdl.donelisteners[id][:i], fdl.donelisteners[id][i+1:]...)
		fdl.mtx.Unlock()
		close(lc)
	}()

	select {
	case s := <-lc:
		return s, nil
	case <-ctx.Done():
		return 0, fmt.Errorf("context cancelled")
	}
}

func (fdl *FakeDataLayer) CreateAPIKey(ctx context.Context, ak models.APIKey) (uuid.UUID, error) {
	fdl.init()
	fdl.mtx.Lock()
	defer fdl.mtx.Unlock()
	ak.ID = uuid.Must(uuid.NewV4())
	fdl.apikeys[ak.ID] = &ak
	return ak.ID, nil
}

func (fdl *FakeDataLayer) GetAPIKey(ctx context.Context, id uuid.UUID) (models.APIKey, error) {
	fdl.init()
	fdl.mtx.RLock()
	defer fdl.mtx.RUnlock()
	apk, ok := fdl.apikeys[id]
	if !ok {
		return models.APIKey{}, ErrNotFound
	}
	return *apk, nil
}

func (fdl *FakeDataLayer) DeleteAPIKey(ctx context.Context, id uuid.UUID) error {
	fdl.init()
	fdl.mtx.Lock()
	defer fdl.mtx.Unlock()
	delete(fdl.apikeys, id)
	return nil
}
