package grpc

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"time"

	"github.com/gofrs/uuid"
	"github.com/golang/protobuf/proto"
	"github.com/jhump/protoreflect/grpcreflect"
	"google.golang.org/grpc/codes"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	grpctrace "gopkg.in/DataDog/dd-trace-go.v1/contrib/google.golang.org/grpc"

	"github.com/dollarshaveclub/furan/pkg/datalayer"
	"github.com/dollarshaveclub/furan/pkg/generated/furanrpc"
	"github.com/dollarshaveclub/furan/pkg/models"
)

type Options struct {
	// TraceSvcName is the service name for APM tracing (optional)
	TraceSvcName string
	// TLSCertificate is the TLS certificate used to secure the gRPC transport.
	TLSCertificate tls.Certificate
	// CredentialDecryptionKey is the secretbox key used to decrypt the build github credential
	CredentialDecryptionKey [32]byte
	Cache                   models.CacheOpts
	LogFunc                 func(msg string, args ...interface{})
	JobHandoffTimeout       time.Duration
}

// DefaultCacheOpts is the cache options that will be used if not overridden in Options
var DefaultCacheOpts = models.CacheOpts{
	Type:    models.S3CacheType,
	MaxMode: true,
}

// Server represents an object that responds to gRPC calls
type Server struct {
	DL datalayer.DataLayer
	BM models.BuildManager
	// CFFactory is a function that returns a CodeFetcher. Token should be considered optional.
	CFFactory func(token string) models.CodeFetcher
	Opts      Options
	s         *grpc.Server
	methods   map[string]bool // methods read-only flags (set once during Listen setup, read only from multiple goroutines afterward)
}

const serviceName = "furanrpc.FuranExecutor"

func methodsFromFuranService(s *grpc.Server) (map[string]bool, error) {
	sds, err := grpcreflect.LoadServiceDescriptors(s)
	if err != nil {
		return nil, fmt.Errorf("error loading grpc service descriptors: %w", err)
	}
	if len(sds) != 1 {
		return nil, fmt.Errorf("unexpected number of grpc services: %v (wanted 1)", len(sds))
	}
	svc, ok := sds[serviceName]
	if !ok {
		return nil, fmt.Errorf("missing service %v: %#v", serviceName, sds)
	}
	mthds := svc.GetMethods()
	methods := make(map[string]bool, len(mthds))
	sn := svc.GetFullyQualifiedName()
	for _, md := range mthds {
		ro, err := proto.GetExtension(md.GetOptions(), furanrpc.E_ReadOnly)
		if err != nil {
			return nil, fmt.Errorf("error getting read only method option: %w", err)
		}
		readonly, ok := ro.(*bool)
		if !ok {
			return nil, fmt.Errorf("unexpected type for read_only method option: %v: %T", md.GetName(), ro)
		}
		if readonly == nil {
			return nil, fmt.Errorf("read_only option is nil: %v", md.GetName())
		}
		mn := fmt.Sprintf("/%s/%s", sn, md.GetName())
		methods[mn] = *readonly
	}
	return methods, nil
}

// Listen starts the RPC listener on addr (host:port) and blocks until the server is signalled to stop.
// Always returns a non-nil error.
func (gr *Server) Listen(addr string) error {
	if gr.DL == nil {
		return fmt.Errorf("DataLayer is required")
	}
	if gr.CFFactory == nil {
		return fmt.Errorf("CFFactory is required")
	}
	l, err := net.Listen("tcp", addr)
	if err != nil {
		gr.logf("error starting gRPC listener: %v", err)
		return err
	}

	// APM tracing
	tri := grpctrace.UnaryServerInterceptor(grpctrace.WithServiceName(gr.Opts.TraceSvcName))
	stri := grpctrace.StreamServerInterceptor(grpctrace.WithServiceName(gr.Opts.TraceSvcName))

	// API key authentication
	// Unary
	uauthi := func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp interface{}, err error) {
		if gr.apiKeyAuth(ctx, info.FullMethod) {
			return handler(ctx, req)
		}
		return nil, status.Errorf(codes.Unauthenticated, "failed API key authentication")
	}
	// Stream
	sauthi := func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		if gr.apiKeyAuth(ss.Context(), info.FullMethod) {
			return handler(srv, ss)
		}
		return status.Errorf(codes.Unauthenticated, "failed API key authentication")
	}

	ops := []grpc.ServerOption{
		grpc.ChainStreamInterceptor(sauthi, stri),
		grpc.ChainUnaryInterceptor(uauthi, tri),
		grpc.Creds(credentials.NewServerTLSFromCert(&gr.Opts.TLSCertificate)),
	}

	s := grpc.NewServer(ops...)
	gr.s = s
	furanrpc.RegisterFuranExecutorServer(s, gr)

	// Load methods metadata for auth
	methods, err := methodsFromFuranService(s)
	if err != nil {
		return fmt.Errorf("error generating methods from service: %w", err)
	}
	gr.methods = methods

	gr.logf("gRPC listening on: %v", addr)
	return s.Serve(l)
}

// APIKeyLabel is the request metadata label used for the API key
const APIKeyLabel = "api_key"

func (gr *Server) apiKeyAuth(ctx context.Context, rpcname string) bool {
	if gr.methods == nil {
		gr.logf("BUG: methods map is nil")
		return false
	}
	ro, ok := gr.methods[rpcname]
	if !ok {
		gr.logf("method name not found in methods metadata: %v", rpcname)
		return false
	}
	md, ok := metadata.FromIncomingContext(ctx)
	if ok {
		apik := md.Get(APIKeyLabel)
		if len(apik) == 1 {
			ak, err := uuid.FromString(apik[0])
			if err != nil {
				gr.logf("invalid api key: %v", err)
				return false
			}
			akey, err := gr.DL.GetAPIKey(ctx, ak)
			if err != nil {
				if err == datalayer.ErrNotFound {
					gr.logf("unknown api key: %v", ak)
					return false
				}
				gr.logf("error getting api key from db: %v: %v", ak, err)
				return false
			}
			// is this API key marked as read-only and is the method a read-only method?
			if akey.ReadOnly && !ro {
				gr.logf("read-only API key but method requires greater permissions")
				return false
			}
			return true
		}
		gr.logf("multiple api keys present")
		return false
	}
	gr.logf("api key missing")
	return false
}

func (gr *Server) logf(msg string, args ...interface{}) {
	if gr.Opts.LogFunc != nil {
		gr.Opts.LogFunc(msg, args...)
	}
}

func (gr *Server) buildevent(id uuid.UUID, msg string, args ...interface{}) {
	if err := gr.DL.AddEvent(context.Background(), id, fmt.Sprintf(msg, args...)); err != nil {
		gr.logf("error adding event to build: %v: %v", id, err)
	}
}

// DefaultJobHandoffTimeout is the default maximum amount of time we will wait for job handoff to occur
// once the build job is running
var DefaultJobHandoffTimeout = 5 * time.Minute

// StartBuild creates a new asynchronous build job
func (gr *Server) StartBuild(ctx context.Context, req *furanrpc.BuildRequest) (*furanrpc.BuildRequestResponse, error) {
	if req.GetBuild().GithubRepo == "" {
		return nil, status.Errorf(codes.InvalidArgument, "github repo is required")
	}
	if req.GetBuild().Ref == "" {
		return nil, status.Errorf(codes.InvalidArgument, "github ref is required")
	}
	if _, err := gr.CFFactory(req.GetBuild().GithubCredential).GetCommitSHA(ctx, req.GetBuild().GithubRepo, req.GetBuild().Ref); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "error validating repo and ref: %v", err)
	}
	if len(req.GetPush().GetRegistries()) == 0 {
		return nil, status.Errorf(codes.InvalidArgument, "at least one image repo is required")
	}
	irepos := make([]string, len(req.GetPush().GetRegistries()))
	for i := range req.GetPush().GetRegistries() {
		irepos[i] = req.GetPush().GetRegistries()[i].Repo
	}
	cred := req.Build.GithubCredential
	req.Build.GithubCredential = ""
	b := models.Build{
		GitHubRepo:        req.GetBuild().GithubRepo,
		GitHubRef:         req.GetBuild().Ref,
		ImageRepos:        irepos,
		Tags:              req.GetBuild().GetTags(),
		CommitSHATag:      req.GetBuild().TagWithCommitSha,
		DisableBuildCache: req.GetBuild().DisableBuildCache,
		Request:           *req,
		Status:            models.BuildStatusNotStarted,
	}

	if cred == "" {
		return nil, status.Errorf(codes.InvalidArgument, "github credential is missing")
	}

	if err := b.EncryptAndSetGitHubCredential([]byte(cred), gr.Opts.CredentialDecryptionKey); err != nil {
		return nil, status.Errorf(codes.Internal, "error encrypting credential: %v", err)
	}

	dockerfile := "." // default relative Dockerfile path
	if req.GetBuild().DockerfilePath != "" {
		dockerfile = req.GetBuild().DockerfilePath
	}

	copts := gr.Opts.Cache
	if copts.Type == models.UnknownCacheType {
		copts = DefaultCacheOpts
	}
	if req.GetBuild().DisableBuildCache {
		copts.Type = models.DisabledCacheType
	}

	opts := models.BuildOpts{
		RelativeDockerfilePath: dockerfile,
		BuildArgs:              req.GetBuild().GetArgs(),
		Cache:                  copts,
	}

	b.BuildOptions = opts

	id, err := gr.DL.CreateBuild(ctx, b)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "error creating build in db: %v", err)
	}

	opts.BuildID = id

	// once everything is validated and we are ready to start job,
	// do the job creation and handoff asynchronously and return id to client
	go func() {
		jht := DefaultJobHandoffTimeout
		if gr.Opts.JobHandoffTimeout != 0 {
			jht = gr.Opts.JobHandoffTimeout
		}
		ctx2, cf := context.WithTimeout(context.Background(), jht)
		defer cf()
		created := time.Now().UTC()
		gr.buildevent(id, "creating build job")
		if err := gr.BM.Start(ctx2, opts); err != nil {
			gr.logf("error starting build job: %v", err)
			gr.buildevent(id, "error starting build job: %v", err)
			// new context because the error could be caused by context cancellation
			if err2 := gr.DL.SetBuildAsCompleted(context.Background(), id, models.BuildStatusFailure); err2 != nil {
				gr.logf("error setting build as failed: %v: %v", id, err2)
			}
			return
		}
		// "build handoff" means the build k8s job is running and the Furan container has started up and updated
		// the build to Running.
		// The Furan container in the k8s job is now responsible for updating this build.
		// Therefore, this goroutine can now exit
		gr.buildevent(id, "build is running (handoff took %v)", time.Now().UTC().Sub(created))
	}()

	return &furanrpc.BuildRequestResponse{
		BuildId: id.String(),
	}, nil
}

// GetBuildStatus returns the current status for a build
func (gr *Server) GetBuildStatus(ctx context.Context, req *furanrpc.BuildStatusRequest) (_ *furanrpc.BuildStatusResponse, err error) {
	id, err := uuid.FromString(req.BuildId)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid build id: %v", err)
	}
	b, err := gr.DL.GetBuildByID(ctx, id)
	if err != nil {
		if err == datalayer.ErrNotFound {
			return nil, status.Errorf(codes.InvalidArgument, "build not found")
		}
		return nil, status.Errorf(codes.Internal, "error getting build: %v", err)
	}
	var started, completed furanrpc.Timestamp
	started = models.RPCTimestampFromTime(b.Created)
	if !b.Completed.IsZero() {
		completed = models.RPCTimestampFromTime(b.Completed)
	}
	return &furanrpc.BuildStatusResponse{
		BuildId:      b.ID.String(),
		BuildRequest: &b.Request,
		State:        b.Status.State(),
		Started:      &started,
		Completed:    &completed,
	}, nil
}

// MonitorBuild streams events from a specified build until completion
func (gr *Server) MonitorBuild(req *furanrpc.BuildStatusRequest, stream furanrpc.FuranExecutor_MonitorBuildServer) error {
	id, err := uuid.FromString(req.BuildId)
	if err != nil {
		return status.Errorf(codes.InvalidArgument, "invalid build id: %v", err)
	}
	b, err := gr.DL.GetBuildByID(stream.Context(), id)
	if err != nil {
		if err == datalayer.ErrNotFound {
			return status.Errorf(codes.InvalidArgument, "build not found")
		}
		return status.Errorf(codes.Internal, "error getting build: %v", err)
	}

	// send any existing events on the stream
	for _, ev := range b.Events {
		if err := stream.Send(&furanrpc.BuildEvent{
			BuildId:      b.ID.String(),
			Message:      ev,
			CurrentState: b.Status.State(), // TODO: we should capture the current state at the time of the event in the database events column
		}); err != nil {
			return status.Errorf(codes.Internal, "error sending build event on RPC stream: %v", err)
		}
	}

	// if build is completed, close the stream
	if b.Status.TerminalState() {
		return nil
	}

	events := make(chan string)
	defer close(events)
	ctx, cf := context.WithCancel(stream.Context())
	defer cf()

	// this goroutine sends new build messages on the events channel
	// when the context is cancelled above via defer, this goroutine will exit
	go func() {
		err := gr.DL.ListenForBuildEvents(ctx, b.ID, events)
		if err != nil {
			select {
			case <-ctx.Done():
				return // if context is cancelled, don't log error
			default:
			}
			gr.logf("error listening for build events: %v", err)
		}
	}()

	// this goroutine reads from the events channel and sends on the grpc stream
	// when the events channel is closed above via defer, this goroutine will exit
	go func() {
		for event := range events {
			b, err := gr.DL.GetBuildByID(ctx, b.ID)
			if err != nil {
				select {
				case <-ctx.Done():
					return // if context is cancelled, don't log error
				default:
				}
				gr.logf("error getting build: %v", err)
				return
			}
			if err := stream.Send(&furanrpc.BuildEvent{
				BuildId:      b.ID.String(),
				Message:      event,
				CurrentState: b.Status.State(),
			}); err != nil {
				gr.logf("error sending build event on RPC stream: %v", err)
				return
			}
		}
	}()

	// this blocks until the build is completed
	_, err = gr.DL.ListenForBuildCompleted(ctx, b.ID)
	if err != nil {
		return status.Errorf(codes.Internal, "error listening for build completion: %v", err)
	}
	return nil
}

// CancelBuild requests cancellation for a currently-running build
func (gr *Server) CancelBuild(ctx context.Context, req *furanrpc.BuildCancelRequest) (_ *furanrpc.BuildCancelResponse, err error) {
	id, err := uuid.FromString(req.BuildId)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid build id: %v", err)
	}
	b, err := gr.DL.GetBuildByID(ctx, id)
	if err != nil {
		if err == datalayer.ErrNotFound {
			return nil, status.Errorf(codes.InvalidArgument, "build not found")
		}
		return nil, status.Errorf(codes.Internal, "error getting build: %v", err)
	}
	if b.Status.TerminalState() || !b.Running() {
		return nil, status.Errorf(codes.FailedPrecondition, "build state not compatible with cancellation: %v", b.Status)
	}
	if err := gr.DL.CancelBuild(ctx, b.ID); err != nil {
		return nil, status.Errorf(codes.Internal, "error cancelling build: %v", err)
	}
	return &furanrpc.BuildCancelResponse{
		BuildId: id.String(),
	}, nil
}

// Shutdown gracefully stops the  server
func (gr *Server) Shutdown() {
	gr.s.GracefulStop()
}
