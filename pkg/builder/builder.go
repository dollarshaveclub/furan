// +build linux darwin freebsd netbsd openbsd

package builder

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/gofrs/uuid"

	"github.com/dollarshaveclub/furan/pkg/datalayer"
	"github.com/dollarshaveclub/furan/pkg/models"
	"github.com/mitchellh/go-ps"
)

// Manager is an object that performs high-level management of image builds
type Manager struct {
	// BRunner is needed for Run()
	BRunner models.Builder
	// JRunner is needed for Start()
	JRunner models.JobRunner
	// TCheck is needed for Run()
	TCheck models.TagChecker
	// FetcherFactory is needed for Run()
	FetcherFactory func(token string) models.CodeFetcher
	// GitHubTokenKey is needed for Run()
	GitHubTokenKey [32]byte
	// DL is needed for all methods
	DL datalayer.DataLayer
}

// Start starts a single build using JobRunner and waits for the build to begin running, or returns error
func (m *Manager) Start(ctx context.Context, opts models.BuildOpts) error {
	if m.JRunner == nil {
		return fmt.Errorf("JRunner is required: %#v", m)
	}
	if m.DL == nil {
		return fmt.Errorf("DL is required")
	}
	b, err := m.DL.GetBuildByID(ctx, opts.BuildID)
	if err != nil {
		return fmt.Errorf("error getting build: %w", err)
	}
	j, err := m.JRunner.Run(b)
	if err != nil {
		return fmt.Errorf("error running build job: %w", err)
	}

	start := time.Now().UTC()
	t := time.NewTicker(20 * time.Second)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("context cancelled")
		case <-t.C:
			m.DL.AddEvent(ctx, b.ID, fmt.Sprintf("waiting for build handoff... (%v elapsed)", time.Since(start)))
			continue
		case err := <-j.Error():
			return fmt.Errorf("job error: %w", err)
		case <-j.Running():
			return nil
		}
	}
}

type atomicBool struct {
	sync.Mutex
	b bool
}

func (ab *atomicBool) set(val bool) {
	ab.Lock()
	ab.b = val
	ab.Unlock()
}

func (ab *atomicBool) get() bool {
	var out bool
	ab.Lock()
	out = ab.b
	ab.Unlock()
	return out
}

// getContextSubdir gets the single subdirectory under path which contains the context, or error
func getContextSubdir(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("error opening path: %w", err)
	}
	defer f.Close()
	fi, err := f.Readdir(-1)
	if err != nil {
		return "", fmt.Errorf("error reading path directory entries: %w", err)
	}
	if len(fi) != 1 {
		return "", fmt.Errorf("expected exactly one subdirectory, got %v", len(fi))
	}
	return fi[0].Name(), nil
}

// Run synchronously executes a build using BuildRunner
func (m *Manager) Run(ctx context.Context, buildID uuid.UUID) (err error) {
	if m.BRunner == nil || m.TCheck == nil || m.FetcherFactory == nil || m.DL == nil {
		return fmt.Errorf("missing dependencies: %#v", m)
	}

	// ensure that the buildkit sidecar container exits so the job completes
	defer func() {
		if err2 := m.killbuildkitd(); err2 != nil {
			m.DL.AddEvent(context.Background(), buildID, err2.Error())
		}
	}()

	b, err := m.DL.GetBuildByID(ctx, buildID)
	if err != nil {
		return fmt.Errorf("error getting build: %w", err)
	}

	if b.Status != models.BuildStatusNotStarted {
		return fmt.Errorf("build already attempted or has unexpected status (%v), wanted NotStarted, aborting", b.Status)
	}

	opts := b.BuildOptions
	opts.BuildID = b.ID

	ctx2, cf := context.WithCancel(ctx)
	defer cf()

	// handle signals
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		select {
		case sig := <-sigs:
			m.DL.AddEvent(context.Background(), b.ID, fmt.Sprintf("signal received: %v; aborting build", sig))
			cf()
		case <-ctx2.Done():
		}
	}()

	// This marks build handoff to this process
	// From this point forward, this process is responsible for updating build in the DB
	if err := m.DL.SetBuildAsRunning(ctx2, b.ID); err != nil {
		return fmt.Errorf("error setting build status to running: %w", err)
	}

	cancelled := &atomicBool{}
	go func() {
		if err := m.DL.ListenForCancellation(ctx2, b.ID); err == nil {
			cf()
			cancelled.set(true)
		}
	}()

	// ensure the build is always marked as completed
	defer func() {
		if err != nil {
			status := models.BuildStatusFailure
			msg := fmt.Sprintf("error running build: %v", err)
			if cancelled.get() {
				status = models.BuildStatusCancelled
				msg = "build was cancelled"
			}
			ctx := context.Background() // existing context may be cancelled
			m.DL.AddEvent(ctx, b.ID, msg)
			time.Sleep(100 * time.Millisecond) // brief pause to try to ensure the last error message gets through to any listeners
			m.DL.SetBuildAsCompleted(ctx, b.ID, status)
		}
	}()

	tkn, err := b.GetGitHubCredential(m.GitHubTokenKey)
	if err != nil {
		return fmt.Errorf("error decrypting github token: %w", err)
	}

	fetcher := m.FetcherFactory(tkn)

	csha, err := fetcher.GetCommitSHA(ctx2, b.GitHubRepo, b.GitHubRef)
	if err != nil {
		return fmt.Errorf("error getting commit sha: %w", err)
	}

	opts.CommitSHA = csha

	if b.Request.SkipIfExists {
		allexist := true
		tags := b.Tags
		if b.Request.Build.TagWithCommitSha {
			tags = append(tags, opts.CommitSHA)
		}
		for _, irepo := range b.ImageRepos {
			exists, _, err := m.TCheck.AllTagsExist(tags, irepo)
			if err != nil {
				return fmt.Errorf("error checking for tags: %v: %w", irepo, err)
			}
			allexist = allexist && exists
			if !allexist {
				break
			}
		}
		if allexist {
			m.DL.AddEvent(ctx2, b.ID, fmt.Sprintf("all tags exist in all image repos: %v; skipping", tags))
			// set build as skipped
			if err := m.DL.SetBuildAsCompleted(ctx2, b.ID, models.BuildStatusSkipped); err != nil {
				return fmt.Errorf("error setting build as skipped: %w", err)
			}
			return nil
		}
	}

	tdir, err := ioutil.TempDir("", "furan-build-context-*")
	if err != nil {
		return fmt.Errorf("error creating temp dir for build context: %w", err)
	}
	defer os.RemoveAll(tdir)
	err = fetcher.Fetch(ctx2, b.GitHubRepo, b.GitHubRef, tdir)
	if err != nil {
		return fmt.Errorf("error fetching repo: %w", err)
	}

	subdir, err := getContextSubdir(tdir)
	if err != nil {
		return fmt.Errorf("error getting context subdirectory: %w", err)
	}

	opts.ContextPath = filepath.Join(tdir, subdir)
	opts.RelativeDockerfilePath = filepath.Join(opts.ContextPath, b.BuildOptions.RelativeDockerfilePath)

	err = m.BRunner.Build(ctx2, opts)
	if err != nil {
		return fmt.Errorf("error executing build: %w", err)
	}
	if err := m.DL.SetBuildAsCompleted(ctx2, b.ID, models.BuildStatusSuccess); err != nil {
		return fmt.Errorf("error setting build as completed: %w", err)
	}
	return nil
}

// Send SIGTERM to the buildkitd sidecar container
// This requires shareProcessNamespace=true on the build job
func (m *Manager) killbuildkitd() error {
	psl, err := ps.Processes()
	if err != nil {
		return fmt.Errorf("error listing processes: %w", err)
	}
	for _, p := range psl {
		// if not using rootless buildkit, change this to "buildkitd"
		if p.Executable() == "rootlesskit" {
			err = syscall.Kill(p.Pid(), syscall.SIGTERM)
			if err != nil {
				return fmt.Errorf("error sending SIGTERM to buildkitd: %w", err)
			}
			return nil
		}
	}
	return fmt.Errorf("buildkitd process not found")
}
