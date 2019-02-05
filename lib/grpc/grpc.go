package grpc

import (
	"fmt"
	"io"
	"log"
	"net"
	"sync"
	"time"

	"github.com/dollarshaveclub/furan/generated/lib"
	"github.com/dollarshaveclub/furan/lib/buildcontext"
	"github.com/dollarshaveclub/furan/lib/builder"
	"github.com/dollarshaveclub/furan/lib/consul"
	"github.com/dollarshaveclub/furan/lib/datalayer"
	"github.com/dollarshaveclub/furan/lib/errors"
	"github.com/dollarshaveclub/furan/lib/kafka"
	"github.com/dollarshaveclub/furan/lib/metrics"
	grpctrace "gopkg.in/DataDog/dd-trace-go.v1/contrib/google.golang.org/grpc.v12"

	"github.com/gocql/gocql"

	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
)

const (
	gracefulStopTimeout = 20 * time.Minute
)

// activeBuildMap contains a map of build ID to CancelFunc to allow cancellation
type activeBuildMap struct {
	sync.Mutex
	m          map[gocql.UUID]context.CancelFunc
	loggerFunc func(string, ...interface{})
	wg         sync.WaitGroup
}

func (abm *activeBuildMap) AddBuild(id gocql.UUID, cf context.CancelFunc) {
	abm.Lock()
	defer abm.Unlock()
	abm.m[id] = cf
	abm.wg.Add(1)
}

func (abm *activeBuildMap) RemoveBuild(id gocql.UUID) {
	abm.Lock()
	defer abm.Unlock()
	if _, ok := abm.m[id]; ok {
		delete(abm.m, id)
	}
	abm.wg.Done()
}

func (abm *activeBuildMap) Wait() bool {
	c := make(chan struct{})
	go func() {
		defer close(c)
		abm.wg.Wait()
	}()
	select {
	case <-c:
		return true
	case <-time.After(gracefulStopTimeout):
		return false
	}
}

func (abm *activeBuildMap) Cancel(id gocql.UUID) error {
	abm.Lock()
	defer abm.Unlock()
	if cf, ok := abm.m[id]; ok {
		abm.loggerFunc("cancelling build %v", id.String())
		cf()
		delete(abm.m, id)
		return nil
	}
	return fmt.Errorf("id not found")
}

func (abm *activeBuildMap) CancelAll() {
	abm.Lock()
	defer abm.Unlock()
	for id, cf := range abm.m {
		abm.loggerFunc("cancelling build %v", id.String())
		cf()
		delete(abm.m, id)
	}
}

func newActiveBuildMap(logger *log.Logger) activeBuildMap {
	return activeBuildMap{
		m:          make(map[gocql.UUID]context.CancelFunc),
		loggerFunc: logger.Printf,
	}
}

// GrpcServer represents an object that responds to gRPC calls
type GrpcServer struct {
	ib          builder.ImageBuildPusher
	dl          datalayer.DataLayer
	ep          kafka.EventBusProducer
	ec          kafka.EventBusConsumer
	ls          io.Writer
	mc          metrics.MetricsCollector
	kvo         consul.KeyValueOrchestrator
	abm         activeBuildMap
	wcf         []context.CancelFunc // worker CancelFuncs
	wwg         *sync.WaitGroup      //async goroutines waitgroup
	logger      *log.Logger
	s           *grpc.Server
	workerChan  chan *workerRequest
	qsize       uint
	wcnt        uint
	serviceName string
}

type workerRequest struct {
	ctx context.Context
	req *lib.BuildRequest
}

// NewGRPCServer returns a new instance of the gRPC server
func NewGRPCServer(ib builder.ImageBuildPusher, dl datalayer.DataLayer, ep kafka.EventBusProducer, ec kafka.EventBusConsumer, mc metrics.MetricsCollector, kvo consul.KeyValueOrchestrator, queuesize uint, concurrency uint, logger *log.Logger, serviceName string) *GrpcServer {
	grs := &GrpcServer{
		ib:          ib,
		dl:          dl,
		ep:          ep,
		ec:          ec,
		mc:          mc,
		kvo:         kvo,
		abm:         newActiveBuildMap(logger),
		wcf:         []context.CancelFunc{},
		wwg:         &sync.WaitGroup{},
		logger:      logger,
		workerChan:  make(chan *workerRequest, queuesize),
		qsize:       queuesize,
		wcnt:        concurrency,
		serviceName: serviceName,
	}
	grs.runWorkers()
	return grs
}

func (gr *GrpcServer) runWorkers() {
	for i := 0; uint(i) < gr.wcnt; i++ {
		ctx, cf := context.WithCancel(context.Background())
		gr.wcf = append(gr.wcf, cf)
		gr.wwg.Add(1)
		go func() { defer gr.wwg.Done(); gr.buildWorker(ctx) }()
	}
	gr.logf("%v workers running (queue: %v)", gr.wcnt, gr.qsize)
}

// WorkerChan returns the worker chan
func (gr *GrpcServer) WorkerChan() chan *workerRequest {
	return gr.workerChan
}

func (gr *GrpcServer) buildWorker(ctx context.Context) {
	var wreq *workerRequest
	var err error
	var id gocql.UUID
	for {
		select {
		case wreq = <-gr.workerChan:
			if !buildcontext.IsCancelled(wreq.ctx.Done()) {
				gr.syncBuild(wreq.ctx, wreq.req)
			} else {
				id, _ = buildcontext.BuildIDFromContext(wreq.ctx)
				gr.logf("context is cancelled, skipping: %v", id)
				if err = gr.dl.DeleteBuild(id); err != nil {
					gr.logf("error deleting build: %v", err)
				}
			}
		case <-ctx.Done():
			gr.logf("worker: cancelled, exiting")
			return
		}
	}
}

func (gr *GrpcServer) cancelWorkers() {
	for _, cf := range gr.wcf {
		cf()
	}
}

// ListenRPC starts the RPC listener on addr:port
func (gr *GrpcServer) ListenRPC(addr string, port uint) error {
	addr = fmt.Sprintf("%v:%v", addr, port)
	l, err := net.Listen("tcp", addr)
	if err != nil {
		gr.logf("error starting gRPC listener: %v", err)
		return err
	}
	ui := grpctrace.UnaryServerInterceptor(grpctrace.WithServiceName(gr.serviceName))
	s := grpc.NewServer(grpc.UnaryInterceptor(ui))
	gr.s = s
	lib.RegisterFuranExecutorServer(s, gr)
	gr.logf("gRPC listening on: %v", addr)
	return s.Serve(l)
}

func (gr *GrpcServer) logf(msg string, params ...interface{}) {
	gr.logger.Printf(msg, params...)
}

func (gr *GrpcServer) durationFromStarted(ctx context.Context) (time.Duration, error) {
	started, ok := buildcontext.StartedFromContext(ctx)
	if !ok {
		return 0, fmt.Errorf("started missing from context")
	}
	return time.Now().UTC().Sub(started), nil
}

func (gr *GrpcServer) buildDuration(ctx context.Context, req *lib.BuildRequest) error {
	d, err := gr.durationFromStarted(ctx)
	if err != nil {
		return err
	}
	return gr.mc.Duration("build.duration", req.Build.GithubRepo, req.Build.Ref, nil, d.Seconds())
}

func (gr *GrpcServer) pushDuration(ctx context.Context, req *lib.BuildRequest, s3 bool) error {
	ps, ok := buildcontext.PushStartedFromContext(ctx)
	if !ok {
		return fmt.Errorf("push_started missing from context")
	}
	tags := []string{}
	if s3 {
		tags = append(tags, "s3")
	} else {
		tags = append(tags, "registry")
	}
	d := time.Now().UTC().Sub(ps).Seconds()
	return gr.mc.Duration("push.duration", req.Build.GithubRepo, req.Build.Ref, tags, d)
}

func (gr *GrpcServer) totalDuration(ctx context.Context, req *lib.BuildRequest) error {
	d, err := gr.durationFromStarted(ctx)
	if err != nil {
		return err
	}
	return gr.mc.Duration("total.duration", req.Build.GithubRepo, req.Build.Ref, nil, d.Seconds())
}

// monitorCancelled watches the KV store and if this build is requested to be cancelled it cancels the context
func (gr *GrpcServer) monitorCancelled(ctx context.Context, cf context.CancelFunc) {
	var err error
	var cancelled bool
	id, ok := buildcontext.BuildIDFromContext(ctx)
	if !ok {
		gr.logf("build id missing from context")
		return
	}
	start := time.Now().UTC()
	timeout := 2 * time.Hour
	for {
		if time.Now().UTC().Sub(start) > timeout {
			gr.logf("timed out")
			return
		}
		if buildcontext.IsCancelled(ctx.Done()) { // context cancelled externally (build finished)
			return
		}
		cancelled, err = gr.kvo.WatchIfBuildIsCancelled(id, 10*time.Second)
		if err != nil {
			gr.logf("error watching if build is cancelled: id: %v: %v", id.String(), err)
			return
		}
		if cancelled {
			cf()
			return
		}
	}
}

// Performs build synchronously
func (gr *GrpcServer) syncBuild(ctx context.Context, req *lib.BuildRequest) (outcome lib.BuildStatusResponse_BuildState) {
	var err error // so deferred finalize function has access to any error
	var failed bool
	var userError bool
	var cf context.CancelFunc
	ctx, cf = context.WithCancel(ctx)
	defer cf()
	id, ok := buildcontext.BuildIDFromContext(ctx)
	if !ok {
		gr.logf("build id missing from context")
		return
	}
	defer gr.abm.RemoveBuild(id)
	gr.logf("syncBuild started: %v", id.String())
	if err := gr.kvo.SetBuildRunning(id); err != nil {
		gr.logf("error setting build as running in KV: %v", err)
		gr.logf("build will not be cancellable!")
	}
	defer gr.kvo.DeleteBuildRunning(id)
	go gr.monitorCancelled(ctx, cf)
	// Finalize build and send event. Failures should set err and return the appropriate build state.
	defer func(id gocql.UUID) {
		failed = outcome == lib.BuildStatusResponse_BUILD_FAILURE || outcome == lib.BuildStatusResponse_PUSH_FAILURE
		flags := map[string]bool{
			"failed":   failed,
			"finished": true,
		}
		var eet lib.BuildEventError_ErrorType
		if failed {
			eet = lib.BuildEventError_FATAL
			gr.mc.BuildFailed(req.Build.GithubRepo, req.Build.Ref, userError)
		} else {
			eet = lib.BuildEventError_NO_ERROR
			gr.mc.BuildSucceeded(req.Build.GithubRepo, req.Build.Ref)
		}
		if err := gr.totalDuration(ctx, req); err != nil {
			gr.logger.Printf("error pushing total duration: %v", err)
		}
		var msg string
		if err != nil {
			msg = err.Error()
		} else {
			msg = "build finished"
		}
		gr.logf("syncBuild: finished: %v", msg)
		if err2 := gr.dl.SetBuildState(id, outcome); err2 != nil {
			gr.logf("failBuild: error setting build state: %v", err2)
		}
		if err2 := gr.dl.SetBuildFlags(id, flags); err2 != nil {
			gr.logf("failBuild: error setting build flags: %v", err2)
		}
		event := &lib.BuildEvent{
			EventError: &lib.BuildEventError{
				ErrorType: eet,
				IsError:   eet != lib.BuildEventError_NO_ERROR,
			},
			BuildId:       id.String(),
			BuildFinished: true,
			EventType:     lib.BuildEvent_LOG,
			Message:       msg,
		}
		if err2 := gr.ep.PublishEvent(event); err2 != nil {
			gr.logf("failBuild: error publishing event: %v", err2)
		}
		gr.logf("%v: %v: failed: %v", id.String(), msg, flags["failed"])
	}(id)
	err = gr.mc.BuildStarted(req.Build.GithubRepo, req.Build.Ref)
	if err != nil {
		gr.logger.Printf("error pushing BuildStarted metric: %v", err)
	}
	if buildcontext.IsCancelled(ctx.Done()) {
		err = fmt.Errorf("build was cancelled")
		return lib.BuildStatusResponse_BUILD_FAILURE
	}
	err = gr.dl.SetBuildState(id, lib.BuildStatusResponse_BUILDING)
	if err != nil {
		err = fmt.Errorf("error setting build state to building: %v", err)
		return lib.BuildStatusResponse_BUILD_FAILURE
	}
	imageid, err := gr.ib.Build(ctx, req, id)
	if err := gr.buildDuration(ctx, req); err != nil {
		gr.logger.Printf("error pushing build duration metric: %v", err)
	}
	if err != nil {
		if err == builder.ErrBuildNotNecessary {
			return lib.BuildStatusResponse_NOT_NECESSARY
		}
		userError = errors.IsUserError(err)
		err = fmt.Errorf("error performing build: %v", err)
		return lib.BuildStatusResponse_BUILD_FAILURE
	}
	if buildcontext.IsCancelled(ctx.Done()) {
		err = fmt.Errorf("build was cancelled")
		return lib.BuildStatusResponse_BUILD_FAILURE
	}
	err = gr.dl.SetBuildState(id, lib.BuildStatusResponse_PUSHING)
	if err != nil {
		err = fmt.Errorf("error setting build state to pushing: %v", err)
		return lib.BuildStatusResponse_BUILD_FAILURE
	}
	ctx = buildcontext.NewPushStartedContext(ctx)
	s3 := req.Push.Registry.Repo == ""
	if s3 {
		err = gr.ib.PushBuildToS3(ctx, imageid, req)
	} else {
		err = gr.ib.PushBuildToRegistry(ctx, req)
	}
	if err := gr.pushDuration(ctx, req, s3); err != nil {
		gr.logger.Printf("error pushing push duration metric: %v", err)
	}
	if err != nil {
		err = fmt.Errorf("error pushing: %v", err)
		return lib.BuildStatusResponse_PUSH_FAILURE
	}
	err = gr.ib.CleanImage(ctx, imageid)
	if err != nil { // Cleaning images is non-fatal because concurrent builds can cause failures here
		gr.logger.Printf("CleanImage error (non-fatal): %v", err)
	}
	err = gr.dl.SetBuildState(id, lib.BuildStatusResponse_SUCCESS)
	if err != nil {
		err = fmt.Errorf("error setting build state to success: %v", err)
		return lib.BuildStatusResponse_PUSH_FAILURE
	}
	err = gr.dl.SetBuildCompletedTimestamp(id)
	if err != nil {
		err = fmt.Errorf("error setting build completed timestamp: %v", err)
		return lib.BuildStatusResponse_PUSH_FAILURE
	}
	return lib.BuildStatusResponse_SUCCESS
}

// gRPC handlers
func (gr *GrpcServer) StartBuild(ctx context.Context, req *lib.BuildRequest) (_ *lib.BuildRequestResponse, err error) {
	resp := &lib.BuildRequestResponse{}
	if req.Push.Registry.Repo == "" {
		if req.Push.S3.Bucket == "" || req.Push.S3.KeyPrefix == "" || req.Push.S3.Region == "" {
			return nil, grpc.Errorf(codes.InvalidArgument, "must specify either registry repo or S3 region/bucket/key-prefix")
		}
	}
	id, err := gr.dl.CreateBuild(req)
	if err != nil {
		return nil, grpc.Errorf(codes.Internal, "error creating build in DB: %v", err)
	}
	var cf context.CancelFunc
	ctx, cf = context.WithCancel(buildcontext.NewBuildIDContext(context.Background(), id))
	wreq := workerRequest{
		ctx: ctx,
		req: req,
	}
	select {
	case gr.workerChan <- &wreq:
		gr.abm.AddBuild(id, cf)
		gr.logf("queueing build: %v", id.String())
		resp.BuildId = id.String()
		return resp, nil
	default:
		gr.logf("build id %v cannot run because queue is full", id.String())
		err = gr.dl.DeleteBuild(id)
		if err != nil {
			gr.logf("error deleting build from DB: %v", err)
		}
		return nil, grpc.Errorf(codes.ResourceExhausted, "build queue is full; try again later")
	}
}

func (gr *GrpcServer) GetBuildStatus(ctx context.Context, req *lib.BuildStatusRequest) (_ *lib.BuildStatusResponse, err error) {
	resp := &lib.BuildStatusResponse{}
	id, err := gocql.ParseUUID(req.BuildId)
	if err != nil {
		return nil, grpc.Errorf(codes.InvalidArgument, "bad id: %v", err)
	}
	resp, err = gr.dl.GetBuildByID(id)
	if err != nil {
		if err == gocql.ErrNotFound {
			return nil, grpc.Errorf(codes.InvalidArgument, "build not found")
		} else {
			return nil, grpc.Errorf(codes.Internal, "error getting build: %v", err)
		}
	}
	return resp, nil
}

// Reconstruct the stream of events for a build from the data layer
func (gr *GrpcServer) eventsFromDL(stream lib.FuranExecutor_MonitorBuildServer, id gocql.UUID) error {
	bo, err := gr.dl.GetBuildOutput(id, "build_output")
	if err != nil {
		return fmt.Errorf("error getting build output: %v", err)
	}
	po, err := gr.dl.GetBuildOutput(id, "push_output")
	if err != nil {
		return fmt.Errorf("error getting push output: %v", err)
	}
	events := append(bo, po...)
	for _, event := range events {
		err = stream.Send(&event)
		if err != nil {
			return grpc.Errorf(codes.Internal, "error sending event: %v", err)
		}
	}
	return nil
}

func (gr *GrpcServer) eventsFromEventBus(stream lib.FuranExecutor_MonitorBuildServer, id gocql.UUID) error {
	var err error
	output := make(chan *lib.BuildEvent)
	done := make(chan struct{})
	defer func() {
		select {
		case <-done:
			return
		default:
			close(done)
		}
	}()
	err = gr.ec.SubscribeToTopic(output, done, id)
	if err != nil {
		return err
	}
	var event *lib.BuildEvent
	for {
		event = <-output
		if event == nil {
			return nil
		}
		err = stream.Send(event)
		if err != nil {
			return err
		}
	}
}

// MonitorBuild streams events from a specified build
func (gr *GrpcServer) MonitorBuild(req *lib.BuildStatusRequest, stream lib.FuranExecutor_MonitorBuildServer) (err error) {
	id, err := gocql.ParseUUID(req.BuildId)
	if err != nil {
		return grpc.Errorf(codes.InvalidArgument, "bad build id: %v", err)
	}
	build, err := gr.dl.GetBuildByID(id)
	if err != nil {
		return grpc.Errorf(codes.Internal, "error getting build: %v", err)
	}
	if build.Finished { // No need to use Kafka, just stream events from the data layer
		return gr.eventsFromDL(stream, id)
	}
	return gr.eventsFromEventBus(stream, id)
}

// CancelBuild stops a currently-running build
func (gr *GrpcServer) CancelBuild(ctx context.Context, req *lib.BuildCancelRequest) (_ *lib.BuildCancelResponse, err error) {
	id, err := gocql.ParseUUID(req.BuildId)
	if err != nil {
		return nil, grpc.Errorf(codes.InvalidArgument, "invalid BuildId")
	}
	running, err := gr.kvo.CheckIfBuildRunning(id)
	if err != nil {
		return nil, grpc.Errorf(codes.Internal, "error checking if build is running: %v", err)
	}
	if !running {
		return nil, grpc.Errorf(codes.FailedPrecondition, "build not running")
	}
	err = gr.kvo.SetBuildCancelled(id)
	if err != nil {
		return nil, grpc.Errorf(codes.Internal, "error setting build as cancelled: %v", err)
	}
	defer gr.kvo.DeleteBuildCancelled(id)

	var gone bool
	for i := 0; i < 5; i++ {
		gone, err = gr.kvo.WatchIfBuildStopsRunning(id, 1*time.Minute)
		if err != nil {
			return nil, grpc.Errorf(codes.Internal, "error watching if build stops running: %v", err)
		}
		if gone {
			return &lib.BuildCancelResponse{BuildId: req.BuildId}, nil
		}
	}
	return nil, grpc.Errorf(codes.DeadlineExceeded, "timed out waiting for build to stop")
}

// Shutdown gracefully stops the GRPC server, signals all workers to stop, waits for builds to finish (with timeout) and then waits for goroutines to finish
func (gr *GrpcServer) Shutdown() {
	gr.s.GracefulStop() // stop GRPC server
	gr.cancelWorkers()  // signal all workers to stop processing jobs
	if !gr.abm.Wait() { // wait for builds to finish
		gr.logf("timeout waiting for builds to finish")
	}
	gr.wwg.Wait() // wait for workers to return
}
