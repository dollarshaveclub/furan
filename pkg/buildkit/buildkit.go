package buildkit

import (
	"context"
	"io"
	"io/ioutil"
	"net"
	"os"
	"strings"
	"time"

	"github.com/gofrs/uuid"
	"github.com/moby/buildkit/client/llb"
	"github.com/moby/buildkit/session"
	"github.com/moby/buildkit/util/progress/progressui"

	"github.com/dollarshaveclub/furan/pkg/datalayer"
	"github.com/dollarshaveclub/furan/pkg/generated/furanrpc"
	"github.com/dollarshaveclub/furan/pkg/models"

	"fmt"

	bkclient "github.com/moby/buildkit/client"
)

type client interface {
	Solve(ctx context.Context, def *llb.Definition, opt bkclient.SolveOpt, statusChan chan *bkclient.SolveStatus) (*bkclient.SolveResponse, error)
}

var _ client = &bkclient.Client{}

type LogFunc func(msg string, args ...interface{})

type BuildSolver struct {
	dl               datalayer.DataLayer
	s3cf             models.CacheFetcher
	bc               client
	addr             string
	AuthProviderFunc func() []session.Attachable
	LogF             LogFunc
}

var _ models.Builder = &BuildSolver{}

func (bks *BuildSolver) log(msg string, args ...interface{}) {
	if bks.LogF != nil {
		bks.LogF(msg, args...)
	}
}

func NewBuildSolver(addr string, s3cf models.CacheFetcher, dl datalayer.DataLayer) (*BuildSolver, error) {
	if !strings.HasPrefix(addr, "unix://") {
		return nil, fmt.Errorf("addr must be a unix socket")
	}
	bc, err := bkclient.New(context.Background(), addr, bkclient.WithFailFast())
	if err != nil {
		return nil, fmt.Errorf("error getting buildkit client: %w", err)
	}
	return &BuildSolver{
		dl:   dl,
		s3cf: s3cf,
		bc:   bc,
		addr: addr,
	}, nil
}

func imageNames(imageRepos []string, tags []string) []string {
	out := make([]string, len(imageRepos)*len(tags))
	for i := range imageRepos {
		for j := range tags {
			out[(i*len(tags))+j] = imageRepos[i] + ":" + tags[j]
		}
	}
	return out
}

// loadCache fetches and configures this build cache (if requested & available) and sets sopts appropriately
// A cleanup function and the configured cache export path (after build) is returned
func (bks *BuildSolver) loadCache(ctx context.Context, opts models.BuildOpts, sopts *bkclient.SolveOpt) (func(), string, error) {
	b, err := bks.dl.GetBuildByID(ctx, opts.BuildID)
	if err != nil {
		return func() {}, "", fmt.Errorf("error getting build by id: %w", err)
	}

	switch opts.Cache.Type {
	case furanrpc.BuildCacheOpts_S3:
		cleanup := func() {}
		// create the cache export path first because even if cache fetch fails due to NotFound,
		// we still want the build to export the cache so we can save to S3 afterward
		exportpath, err := ioutil.TempDir("", "build-cache-export-*")
		if err != nil {
			return cleanup, "", fmt.Errorf("error creating cache export path: %w", err)
		}
		cleanup = func() { os.RemoveAll(exportpath) }
		mode := "min"
		if opts.Cache.MaxMode {
			mode = "max"
		}
		sopts.CacheExports = []bkclient.CacheOptionsEntry{
			bkclient.CacheOptionsEntry{
				Type: "local",
				Attrs: map[string]string{
					"dest": exportpath,
					"mode": mode,
				},
			},
		}
		path, err := bks.s3cf.Fetch(ctx, b)
		if err != nil {
			return cleanup, exportpath, fmt.Errorf("error fetching cache from s3: %w", err)
		}
		cleanup = func() { os.RemoveAll(exportpath); os.RemoveAll(path) }
		sopts.CacheImports = []bkclient.CacheOptionsEntry{
			bkclient.CacheOptionsEntry{
				Type: "local",
				Attrs: map[string]string{
					"src": path,
				},
			},
		}
		return cleanup, exportpath, nil
	case furanrpc.BuildCacheOpts_INLINE:
		sopts.CacheImports = make([]bkclient.CacheOptionsEntry, len(b.ImageRepos))
		for i := range b.ImageRepos {
			sopts.CacheImports[i] = bkclient.CacheOptionsEntry{
				Type: "registry",
				Attrs: map[string]string{
					"ref": b.ImageRepos[i],
				},
			}
		}
		sopts.CacheExports = []bkclient.CacheOptionsEntry{
			bkclient.CacheOptionsEntry{
				Type: "inline",
				// inline exporter supports min mode only
			},
		}
	}

	return func() {}, "", nil
}

// saveCache persists the exported build cache (if requested & available)
func (bks *BuildSolver) saveCache(ctx context.Context, opts models.BuildOpts, expath string) error {
	b, err := bks.dl.GetBuildByID(ctx, opts.BuildID)
	if err != nil {
		return fmt.Errorf("error getting build by id: %w", err)
	}

	// we only need to explicitly persist cache if using S3 (w/ local exporter)
	// expath will be the local filesystem cache export path configured and returned by loadCache()
	if opts.Cache.Type == furanrpc.BuildCacheOpts_S3 {
		if expath == "" {
			return fmt.Errorf("cache export path is empty")
		}
		if err := bks.s3cf.Save(ctx, b, expath); err != nil {
			return fmt.Errorf("error saving exported cache to S3: %w", err)
		}
	}

	return nil
}

func (bks *BuildSolver) genSolveOpt(b models.Build, opts models.BuildOpts) (bkclient.SolveOpt, error) {
	tags := b.Tags
	if b.CommitSHATag {
		tags = append(tags, opts.CommitSHA)
	}
	export := bkclient.ExportEntry{
		Type: "image",
		Attrs: map[string]string{
			"push": "true",
			"name": strings.Join(imageNames(b.ImageRepos, tags), ","),
		},
	}
	sopts := bkclient.SolveOpt{
		Exports:       []bkclient.ExportEntry{export},
		Frontend:      "dockerfile.v0",
		FrontendAttrs: map[string]string{},
		LocalDirs: map[string]string{
			"context":    opts.ContextPath,
			"dockerfile": opts.RelativeDockerfilePath,
		},
	}
	for k, v := range opts.BuildArgs {
		sopts.FrontendAttrs["build-arg:"+k] = v
	}
	if bks.AuthProviderFunc != nil {
		sopts.Session = bks.AuthProviderFunc()
	}
	return sopts, nil
}

var (
	SocketConnectTimeout    = 2 * time.Minute
	SocketConnectRetryDelay = 5 * time.Second
)

// verifyAddr ensures that the buildkit socket is connectable, returning an error if timeout is reached and
// the socket is still unavailable
func (bks *BuildSolver) VerifyAddr() error {
	deadline := time.Now().UTC().Add(SocketConnectTimeout)
	for {
		now := time.Now().UTC()
		if now.Equal(deadline) || now.After(deadline) {
			return fmt.Errorf("timeout waiting for buildkit socket (%v)", SocketConnectTimeout)
		}
		conn, err := net.Dial("unix", strings.TrimPrefix(bks.addr, "unix://"))
		if err != nil {
			time.Sleep(SocketConnectRetryDelay)
			continue
		}
		conn.Close()
		return nil
	}
}

// eventWriter is an io.Writer that sends writes as build events and pass-through writes to Sink if non-nil
type eventWriter struct {
	Ctx     context.Context
	Sink    io.Writer
	BuildID uuid.UUID
	DL      datalayer.DataLayer
}

// Write sends p as a build event for BuildID (converted to string and trimmed of all leading and trailing whitespace)
// If Sink is defined, p is written to it unchanged
// If p is empty or only whitespace, the write is ignored
func (ew *eventWriter) Write(p []byte) (n int, err error) {
	str := strings.TrimSpace(string(p))
	if str == "" {
		return len(p), nil
	}
	ew.DL.AddEvent(ew.Ctx, ew.BuildID, str)
	if ew.Sink != nil {
		return ew.Sink.Write(p)
	}
	return len(p), nil
}

var _ io.Writer = &eventWriter{}

var eventSink = os.Stderr

// Build performs the build defined by opts
func (bks *BuildSolver) Build(ctx context.Context, opts models.BuildOpts) error {
	b, err := bks.dl.GetBuildByID(ctx, opts.BuildID)
	if err != nil {
		return fmt.Errorf("error getting build: %v: %w", opts.BuildID, err)
	}
	sopts, err := bks.genSolveOpt(b, opts)
	if err != nil {
		return fmt.Errorf("error generating solve options: %w", err)
	}
	cleanup, expath, err := bks.loadCache(ctx, opts, &sopts)
	if err != nil {
		// non-fatal error if we can't load cache for some reason
		bks.dl.AddEvent(ctx, b.ID, fmt.Sprintf("warning: error loading cache: %v", err))
	}
	defer cleanup()

	c := make(chan *bkclient.SolveStatus)

	// this goroutine reads build messages and adds them as events
	// it returns when the context is cancelled
	go func() {
		ew := &eventWriter{
			Ctx:     ctx,
			Sink:    eventSink,
			BuildID: opts.BuildID,
			DL:      bks.dl,
		}
		// this is the same package used by the Docker CLI to display build status to the terminal
		// we're emulating the "plain" terminal output option, which gets written to build events and stderr
		// by eventWriter
		// Use a new context to allow it to read all values from c (prevents client hanging)
		err := progressui.DisplaySolveStatus(context.TODO(), "", nil, ew, c)
		if err != nil {
			select {
			case <-ctx.Done():
				return
			default:
			}
			bks.log("DisplaySolveStatus error: %v", err)
		}
	}()

	// make sure the buildkit socket is available
	if err := bks.VerifyAddr(); err != nil {
		return fmt.Errorf("error verifying that buildkit is available: %w", err)
	}

	resp, err := bks.bc.Solve(ctx, nil, sopts, c)
	if err != nil {
		return fmt.Errorf("error running solver: %w", err)
	}
	if err := bks.saveCache(ctx, opts, expath); err != nil {
		// non-fatal error if we can't save cache
		bks.dl.AddEvent(ctx, opts.BuildID, fmt.Sprintf("warning: error saving build cache: %v", err))
	}
	msg := fmt.Sprintf("solve success: %+v", resp.ExporterResponse)
	if err := bks.dl.AddEvent(ctx, opts.BuildID, msg); err != nil {
		bks.log("error adding success event: build: %v: %v", opts.BuildID, err)
	}
	return nil
}
