package builder

import (
	"context"
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gofrs/uuid"
	"gopkg.in/DataDog/dd-trace-go.v1/ddtrace/tracer"

	"gopkg.in/DataDog/dd-trace-go.v1/ddtrace/mocktracer"

	"github.com/dollarshaveclub/furan/pkg/buildkit"
	"github.com/dollarshaveclub/furan/pkg/datalayer"
	"github.com/dollarshaveclub/furan/pkg/generated/furanrpc"
	"github.com/dollarshaveclub/furan/pkg/github"
	"github.com/dollarshaveclub/furan/pkg/jobrunner"
	"github.com/dollarshaveclub/furan/pkg/models"
	"github.com/dollarshaveclub/furan/pkg/tagcheck"
)

func TestManager_Start(t *testing.T) {
	type fields struct {
		BRunner        models.Builder
		TCheck         models.TagChecker
		FetcherFactory func(token string) models.CodeFetcher
	}
	type args struct {
		opts models.BuildOpts
	}
	tests := []struct {
		name                          string
		fields                        fields
		args                          args
		ctxCancel, joberr, jobrunning bool
		wantErr                       bool
	}{
		{
			name: "success",
			fields: fields{
				BRunner: &buildkit.BuildSolver{},
				TCheck:  &tagcheck.Checker{},
				FetcherFactory: func(token string) models.CodeFetcher {
					return &github.FakeFetcher{}
				},
			},
			args: args{
				opts: models.BuildOpts{},
			},
			jobrunning: true,
		},
		{
			name: "job error",
			fields: fields{
				BRunner: &buildkit.BuildSolver{},
				TCheck:  &tagcheck.Checker{},
				FetcherFactory: func(token string) models.CodeFetcher {
					return &github.FakeFetcher{}
				},
			},
			args: args{
				opts: models.BuildOpts{},
			},
			joberr:  true,
			wantErr: true,
		},
		{
			name: "context cancelled",
			fields: fields{
				BRunner: &buildkit.BuildSolver{},
				TCheck:  &tagcheck.Checker{},
				FetcherFactory: func(token string) models.CodeFetcher {
					return &github.FakeFetcher{}
				},
			},
			args: args{
				opts: models.BuildOpts{},
			},
			ctxCancel: true,
			wantErr:   true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !tt.ctxCancel && !tt.joberr && !tt.jobrunning {
				t.Fatalf("at least one outcome is required for test")
			}
			ctx := context.Background()
			dl := &datalayer.FakeDataLayer{}
			id, _ := dl.CreateBuild(ctx, models.Build{
				GitHubRepo: "acme/foo",
				GitHubRef:  "master",
				ImageRepos: []string{"acme/foo"},
			})
			tt.args.opts.BuildID = id
			fjr := &jobrunner.FakeJobRunner{
				RunFunc: func(build models.Build) (models.Job, error) {
					fj := jobrunner.NewFakeJob(nil)
					if tt.jobrunning {
						go func() {
							time.Sleep(10 * time.Millisecond)
							close(fj.RunningChan)
						}()
					}
					if tt.joberr {
						go func() {
							time.Sleep(10 * time.Millisecond)
							fj.ErrorChan <- fmt.Errorf("error in job")
						}()
					}
					return fj, nil
				},
			}
			m := &Manager{
				JRunner:        fjr,
				BRunner:        tt.fields.BRunner,
				TCheck:         tt.fields.TCheck,
				FetcherFactory: tt.fields.FetcherFactory,
				DL:             dl,
			}
			if tt.ctxCancel {
				ctx2, cf := context.WithCancel(ctx)
				go func() { time.Sleep(10 * time.Millisecond); cf() }()
				ctx = ctx2
			}
			if err := m.Start(ctx, tt.args.opts); (err != nil) != tt.wantErr {
				t.Errorf("Start() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestManager_Run(t *testing.T) {
	tests := []struct {
		name             string
		decrypterr       bool
		commitSHA        string
		commitSHAerr     error
		allTagsExistFunc func(tags []string, repo string) (bool, []string, error)
		imagerepos       []string
		fetcherr         error
		cancel           bool
		builderr         error
		contexterr       bool
		wantErr          bool
		verifyfunc       func(dl datalayer.DataLayer, id uuid.UUID) error
		verifytracerf    func(tracer mocktracer.Tracer) error
	}{
		{
			name:      "success",
			commitSHA: "asdf",
			allTagsExistFunc: func(tags []string, repo string) (bool, []string, error) {
				return false, tags, nil
			},
			verifyfunc: func(dl datalayer.DataLayer, id uuid.UUID) error {
				b, _ := dl.GetBuildByID(context.Background(), id)
				if b.Status != models.BuildStatusSuccess {
					return fmt.Errorf("bad status: %v (wanted Success)", b.Status)
				}
				return nil
			},
		},
		{
			name:      "traced",
			commitSHA: "asdf",
			allTagsExistFunc: func(tags []string, repo string) (bool, []string, error) {
				return false, tags, nil
			},
			verifyfunc: func(dl datalayer.DataLayer, id uuid.UUID) error {
				b, _ := dl.GetBuildByID(context.Background(), id)
				if b.Status != models.BuildStatusSuccess {
					return fmt.Errorf("bad status: %v (wanted Success)", b.Status)
				}
				return nil
			},
			verifytracerf: func(tracer mocktracer.Tracer) error {
				spans := tracer.FinishedSpans()
				if len(spans) != 2 {
					return fmt.Errorf("wanted 2 spans: %v", spans)
				}
				names := make(map[string]struct{}, 2)
				for _, s := range spans {
					names[s.OperationName()] = struct{}{}
				}
				if _, ok := names["traced"]; !ok {
					return fmt.Errorf("root span missing")
				}
				if _, ok := names["syncbuild"]; !ok {
					return fmt.Errorf("child span missing")
				}
				return nil
			},
		},
		{
			name:      "cancelled",
			commitSHA: "asdf",
			allTagsExistFunc: func(tags []string, repo string) (bool, []string, error) {
				return false, tags, nil
			},
			cancel:  true,
			wantErr: true,
			verifyfunc: func(dl datalayer.DataLayer, id uuid.UUID) error {
				b, _ := dl.GetBuildByID(context.Background(), id)
				if b.Status != models.BuildStatusCancelled {
					return fmt.Errorf("bad status: %v (wanted Cancelled)", b.Status)
				}
				return nil
			},
		},
		{
			name:      "skipped",
			commitSHA: "asdf",
			allTagsExistFunc: func(tags []string, repo string) (bool, []string, error) {
				return true, []string{}, nil
			},
			verifyfunc: func(dl datalayer.DataLayer, id uuid.UUID) error {
				b, _ := dl.GetBuildByID(context.Background(), id)
				if b.Status != models.BuildStatusSkipped {
					return fmt.Errorf("bad status: %v (wanted Skipped)", b.Status)
				}
				return nil
			},
		},
		{
			name:       "token decrypt error",
			decrypterr: true,
			commitSHA:  "asdf",
			wantErr:    true,
			verifyfunc: func(dl datalayer.DataLayer, id uuid.UUID) error {
				b, _ := dl.GetBuildByID(context.Background(), id)
				if b.Status != models.BuildStatusFailure {
					return fmt.Errorf("bad status: %v (wanted BuildStatusFailure)", b.Status)
				}
				return nil
			},
		},
		{
			name:         "commit sha error",
			commitSHAerr: fmt.Errorf("something happened"),
			wantErr:      true,
			verifyfunc: func(dl datalayer.DataLayer, id uuid.UUID) error {
				b, _ := dl.GetBuildByID(context.Background(), id)
				if b.Status != models.BuildStatusFailure {
					return fmt.Errorf("bad status: %v (wanted BuildStatusFailure)", b.Status)
				}
				return nil
			},
		},
		{
			name:      "fetch error",
			commitSHA: "asdf",
			allTagsExistFunc: func(tags []string, repo string) (bool, []string, error) {
				return false, tags, nil
			},
			fetcherr: fmt.Errorf("something happened"),
			wantErr:  true,
			verifyfunc: func(dl datalayer.DataLayer, id uuid.UUID) error {
				b, _ := dl.GetBuildByID(context.Background(), id)
				if b.Status != models.BuildStatusFailure {
					return fmt.Errorf("bad status: %v (wanted BuildStatusFailure)", b.Status)
				}
				return nil
			},
		},
		{
			name:      "bad context directory",
			commitSHA: "asdf",
			allTagsExistFunc: func(tags []string, repo string) (bool, []string, error) {
				return false, tags, nil
			},
			contexterr: true,
			wantErr:    true,
			verifyfunc: func(dl datalayer.DataLayer, id uuid.UUID) error {
				b, _ := dl.GetBuildByID(context.Background(), id)
				if b.Status != models.BuildStatusFailure {
					return fmt.Errorf("bad status: %v (wanted BuildStatusFailure)", b.Status)
				}
				return nil
			},
		},
		{
			name:      "build error",
			commitSHA: "asdf",
			allTagsExistFunc: func(tags []string, repo string) (bool, []string, error) {
				return false, tags, nil
			},
			builderr: fmt.Errorf("something happened"),
			wantErr:  true,
			verifyfunc: func(dl datalayer.DataLayer, id uuid.UUID) error {
				b, _ := dl.GetBuildByID(context.Background(), id)
				if b.Status != models.BuildStatusFailure {
					return fmt.Errorf("bad status: %v (wanted Failure)", b.Status)
				}
				return nil
			},
		},
		{
			name:      "tagcheck error",
			commitSHA: "asdf",
			allTagsExistFunc: func(tags []string, repo string) (bool, []string, error) {
				return false, tags, fmt.Errorf("something happened")
			},
			wantErr: true,
			verifyfunc: func(dl datalayer.DataLayer, id uuid.UUID) error {
				b, _ := dl.GetBuildByID(context.Background(), id)
				if b.Status != models.BuildStatusFailure {
					return fmt.Errorf("bad status: %v (wanted BuildStatusFailure)", b.Status)
				}
				return nil
			},
		},
		{
			name:      "multi repo tagcheck",
			commitSHA: "asdf",
			allTagsExistFunc: func(tags []string, repo string) (bool, []string, error) {
				if repo == "acme/foo" {
					return true, []string{}, nil
				}
				return false, tags, nil
			},
			imagerepos: []string{"acme/foo", "acme/bar"},
			verifyfunc: func(dl datalayer.DataLayer, id uuid.UUID) error {
				b, _ := dl.GetBuildByID(context.Background(), id)
				if b.Status != models.BuildStatusSuccess {
					return fmt.Errorf("bad status: %v (wanted Success)", b.Status)
				}
				return nil
			},
		},
		{
			name:      "multi repo tagcheck - skip build",
			commitSHA: "asdf",
			allTagsExistFunc: func(tags []string, repo string) (bool, []string, error) {
				return true, []string{}, nil
			},
			imagerepos: []string{"acme/foo", "acme/bar"},
			verifyfunc: func(dl datalayer.DataLayer, id uuid.UUID) error {
				b, _ := dl.GetBuildByID(context.Background(), id)
				if b.Status != models.BuildStatusSkipped {
					return fmt.Errorf("bad status: %v (wanted Skipped)", b.Status)
				}
				return nil
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			dl := &datalayer.FakeDataLayer{}
			if tt.imagerepos == nil {
				tt.imagerepos = []string{"acme/foo"}
			}
			b := models.Build{
				Status:     models.BuildStatusNotStarted,
				GitHubRepo: "acme/foo",
				GitHubRef:  "bar",
				ImageRepos: tt.imagerepos,
				Request: furanrpc.BuildRequest{
					SkipIfExists: true,
					Build:        &furanrpc.BuildDefinition{},
				},
			}

			ghtoken := "asdf1234"

			keybs := make([]byte, 32)
			if n, err := rand.Read(keybs); err != nil || n != len(keybs) {
				t.Errorf("error getting random key: %v", err)
			}
			var key [32]byte
			copy(key[:], keybs)
			if err := b.EncryptAndSetGitHubCredential([]byte(ghtoken), key); err != nil {
				t.Errorf("error encrypting token: %v", err)
			}

			id, _ := dl.CreateBuild(ctx, b)

			ff := func(token string) models.CodeFetcher {
				if token != ghtoken {
					t.Errorf("bad github token: %v (wanted %v)", token, ghtoken)
				}
				return &github.FakeFetcher{
					GetCommitSHAFunc: func(ctx context.Context, repo string, ref string) (string, error) {
						if repo != b.GitHubRepo {
							t.Errorf("bad repo: %v (wanted %v)", repo, b.GitHubRepo)
						}
						if ref != b.GitHubRef {
							t.Errorf("bad ref: %v (wanted %v)", ref, b.GitHubRef)
						}
						return tt.commitSHA, tt.commitSHAerr
					},
					FetchFunc: func(ctx context.Context, repo string, ref string, destinationPath string) error {
						if repo != b.GitHubRepo {
							t.Errorf("bad repo: %v (wanted %v)", repo, b.GitHubRepo)
						}
						if ref != b.GitHubRef {
							t.Errorf("bad ref: %v (wanted %v)", ref, b.GitHubRef)
						}
						if !tt.contexterr && tt.fetcherr == nil {
							p := filepath.Join(destinationPath, "foobar-1234")
							os.Mkdir(p, os.ModePerm)
						}
						return tt.fetcherr
					},
				}
			}

			buildEntered := make(chan struct{})

			fr := &buildkit.FakeBuilder{
				BuildFunc: func(ctx context.Context, opts models.BuildOpts) error {
					close(buildEntered)
					if opts.CommitSHA != tt.commitSHA {
						t.Errorf("bad commit sha in opts: %v (wanted %v)", opts.CommitSHA, tt.commitSHA)
					}
					if opts.BuildID != id {
						t.Errorf("bad build id in opts: %v (wanted %v)", opts.BuildID, id)
					}
					if tt.cancel {
						<-ctx.Done()
						return fmt.Errorf("context was cancelled")
					}
					return tt.builderr
				},
			}

			m := &Manager{
				DL:             dl,
				BRunner:        fr,
				TCheck:         &tagcheck.FakeChecker{AllTagsExistFunc: tt.allTagsExistFunc},
				FetcherFactory: ff,
				GitHubTokenKey: key,
				JRunner:        &jobrunner.FakeJobRunner{},
			}

			if tt.decrypterr {
				m.GitHubTokenKey = [32]byte{}
			}

			var mt mocktracer.Tracer
			var span tracer.Span
			if tt.verifytracerf != nil {
				mt = mocktracer.Start()
				defer mt.Stop()
				span, ctx = tracer.StartSpanFromContext(ctx, tt.name)
			}

			if tt.cancel {
				go func() {
					<-buildEntered
					// make sure we have at least one listener waiting for cancellation
					for {
						if dl.CancellationListeners() > 0 {
							break
						}
						time.Sleep(1 * time.Millisecond)
					}
					dl.CancelBuild(ctx, id)
				}()
			}

			if err := m.Run(ctx, id); (err != nil) != tt.wantErr {
				t.Errorf("Run() error = %v, wantErr %v", err, tt.wantErr)
			}

			if tt.verifyfunc != nil {
				if err := tt.verifyfunc(dl, id); err != nil {
					t.Errorf("verify failed: %v", err)
				}
			}

			if tt.verifytracerf != nil {
				span.Finish()
				if err := tt.verifytracerf(mt); err != nil {
					t.Errorf("tracer verify failed: %v", err)
				}
			}
		})
	}
}
