package grpc

import (
	"fmt"
	"net"
	"time"

	"github.com/dollarshaveclub/furan/pkg/datalayer"
	"github.com/dollarshaveclub/furan/pkg/generated/furanrpc"
	grpctrace "gopkg.in/DataDog/dd-trace-go.v1/contrib/google.golang.org/grpc.v12"

	"golang.org/x/net/context"
	"google.golang.org/grpc"
)

const (
	gracefulStopTimeout = 20 * time.Minute
)

// GrpcServer represents an object that responds to gRPC calls
type GrpcServer struct {
	dl          datalayer.DataLayer
	s           *grpc.Server
	qsize       uint
	wcnt        uint
	serviceName string
}

type workerRequest struct {
	ctx context.Context
	req *furanrpc.BuildRequest
}

// NewGRPCServer returns a new instance of the gRPC server
func NewGRPCServer(dl datalayer.DataLayer, queuesize uint, concurrency uint, serviceName string) *GrpcServer {
	grs := &GrpcServer{
		dl:          dl,
		qsize:       queuesize,
		wcnt:        concurrency,
		serviceName: serviceName,
	}
	return grs
}

// ListenRPC starts the RPC listener on addr:port
func (gr *GrpcServer) ListenRPC(addr string, port uint) error {
	addr = fmt.Sprintf("%v:%v", addr, port)
	l, err := net.Listen("tcp", addr)
	if err != nil {
		gr.logf("error starting gRPC listener: %v", err)
		return err
	}
	// Note (mk): We should consider upgrading our go grpc package so that we
	// can take advantage of stream interceptor
	ui := grpctrace.UnaryServerInterceptor(grpctrace.WithServiceName(gr.serviceName))
	s := grpc.NewServer(grpc.UnaryInterceptor(ui))
	gr.s = s
	furanrpc.RegisterFuranExecutorServer(s, gr)
	gr.logf("gRPC listening on: %v", addr)
	return s.Serve(l)
}

func (gr *GrpcServer) logf(msg string, params ...interface{}) {
}

// gRPC handlers
func (gr *GrpcServer) StartBuild(ctx context.Context, req *furanrpc.BuildRequest) (_ *furanrpc.BuildRequestResponse, err error) {
	return nil, nil
}

func (gr *GrpcServer) GetBuildStatus(ctx context.Context, req *furanrpc.BuildStatusRequest) (_ *furanrpc.BuildStatusResponse, err error) {
	return nil, nil
}

// MonitorBuild streams events from a specified build
func (gr *GrpcServer) MonitorBuild(req *furanrpc.BuildStatusRequest, stream furanrpc.FuranExecutor_MonitorBuildServer) (err error) {
	return nil
}

// CancelBuild stops a currently-running build
func (gr *GrpcServer) CancelBuild(ctx context.Context, req *furanrpc.BuildCancelRequest) (_ *furanrpc.BuildCancelResponse, err error) {
	return nil, nil
}

// Shutdown gracefully stops the GRPC server, signals all workers to stop, waits for builds to finish (with timeout) and then waits for goroutines to finish
func (gr *GrpcServer) Shutdown() {
	gr.s.GracefulStop() // stop GRPC server
}
