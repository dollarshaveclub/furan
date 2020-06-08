package streamadapter

/*
Adapter to allow us to directly call a server stream gRPC method and get
back the events

Stream Interface:

// Context returns the context for this stream.
Context() context.Context
// SendMsg blocks until it sends m, the stream is done or the stream
// breaks.
// On error, it aborts the stream and returns an RPC status on client
// side. On server side, it simply returns the error to the caller.
// SendMsg is called by generated code. Also Users can call SendMsg
// directly when it is really needed in their use cases.
SendMsg(m interface{}) error
// RecvMsg blocks until it receives a message or the stream is
// done. On client side, it returns io.EOF when the stream is done. On
// any other error, it aborts the stream and returns an RPC status. On
// server side, it simply returns the error to the caller.
RecvMsg(m interface{}) error

Server Stream:

// SendHeader sends the header metadata. It should not be called
// after SendProto. It fails if called multiple times or if
// called after SendProto.
SendHeader(metadata.MD) error
// SetTrailer sets the trailer metadata which will be sent with the
// RPC status.
SetTrailer(metadata.MD)

FuranExecutor_MonitorBuildServer:

Send(*BuildEvent) error
*/

import (
	"io"
	"strings"

	"github.com/dollarshaveclub/furan/pkg/generated/furanrpc"
	"golang.org/x/net/context"
	"google.golang.org/grpc/metadata"
)

type LocalServerStream struct {
	ctx context.Context
	out io.Writer
}

func NewLocalServerStream(ctx context.Context, out io.Writer) *LocalServerStream {
	return &LocalServerStream{
		ctx: ctx,
		out: out,
	}
}

func (lss *LocalServerStream) Context() context.Context {
	return lss.ctx
}

func (lss *LocalServerStream) Send(event *furanrpc.BuildEvent) error {
	if !strings.HasSuffix(event.Message, "\n") {
		event.Message += "\n"
	}
	lss.out.Write([]byte(event.Message))
	return nil
}

// unused
func (lss *LocalServerStream) SendMsg(m interface{}) error {
	return nil
}

// unused
func (lss *LocalServerStream) RecvMsg(m interface{}) error {
	return nil
}

// unused
func (lss *LocalServerStream) SendHeader(metadata.MD) error {
	return nil
}

// unused
func (lss *LocalServerStream) SetHeader(metadata.MD) error {
	return nil
}

// unused
func (lss *LocalServerStream) SetTrailer(metadata.MD) {
}
