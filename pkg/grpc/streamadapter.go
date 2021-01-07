package grpc

import (
	"context"
	"fmt"
	"io"
	"reflect"
	"sync"

	"google.golang.org/grpc"

	"github.com/dollarshaveclub/furan/pkg/generated/furanrpc"
)

// MonitorStreamAdapter allows the Monitor streaming RPC to be consumed by non-gRPC clients
type MonitorStreamAdapter struct {
	// ctx is used as the stream context. If cancelled, any blocking methods will return
	ctx context.Context
	// cancelFunc cancels the embedded context
	cancelFunc context.CancelFunc
	// bufferedMsgs is the number of messages to buffer internally without blocking (default: 0)
	bufferedMsgs uint
	c            chan *furanrpc.BuildEvent
	grpc.ServerStream
	sync.RWMutex
}

var _ furanrpc.FuranExecutor_MonitorBuildServer = &MonitorStreamAdapter{}

// NewMonitorStreamAdapter returns a MonitorStreamAdapter
func NewMonitorStreamAdapter(ctx context.Context, bufmsgs uint) *MonitorStreamAdapter {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cf := context.WithCancel(ctx)
	return &MonitorStreamAdapter{
		c:          make(chan *furanrpc.BuildEvent, bufmsgs),
		ctx:        ctx,
		cancelFunc: cf,
	}
}

func (msa *MonitorStreamAdapter) checkCtx() bool {
	select {
	case <-msa.ctx.Done():
		return false
	default:
		return true
	}
}

func (msa *MonitorStreamAdapter) Context() context.Context {
	msa.RLock()
	defer msa.RUnlock()
	ctx := msa.ctx
	return ctx
}

func (msa *MonitorStreamAdapter) Send(e *furanrpc.BuildEvent) error {
	return msa.SendMsg(e)
}

// SendMsg sends m (which must be a *furanrpc.BuildEvent) to listeners
func (msa *MonitorStreamAdapter) SendMsg(m interface{}) error {
	msa.RLock()
	defer msa.RUnlock()
	if !msa.checkCtx() {
		return fmt.Errorf("stream is closed")
	}
	if m == nil {
		return fmt.Errorf("msg is nil")
	}
	be, ok := m.(*furanrpc.BuildEvent)
	if !ok {
		return fmt.Errorf("unexpected msg type %T (expected BuildEvent)", m)
	}
	select {
	case msa.c <- be:
		return nil
	case <-msa.ctx.Done():
		return io.EOF
	}
}

// RecvMsg blocks and listens for *furanrpc.BuildEvent messages and if one is received, assigns it to m and returns a nil error
func (msa *MonitorStreamAdapter) RecvMsg(m interface{}) error {
	msa.RLock()
	defer msa.RUnlock()
	if !msa.checkCtx() {
		return fmt.Errorf("stream is closed")
	}
	if m == nil {
		return fmt.Errorf("msg is nil")
	}
	_, ok := m.(*furanrpc.BuildEvent)
	if !ok {
		return fmt.Errorf("unexpected msg type %T (expected BuildEvent)", m)
	}
	select {
	case received := <-msa.c:
		if received == nil {
			return io.EOF
		}
		// we already confirmed m is type *furanrpc.BuildEvent (pointer)
		// this re-assigns the internal pointer within the interface (m) to the
		// the value received on the channel
		val := reflect.ValueOf(m).Elem()
		recvd := reflect.Indirect(reflect.ValueOf(received))
		if !val.IsValid() || !recvd.IsValid() {
			return fmt.Errorf("m (%#v) or received value (%#v) is not valid", val, recvd)
		}
		if !val.Type().AssignableTo(recvd.Type()) {
			return fmt.Errorf("can not assign received (%T) to m (%T)", received, m)
		}
		val.Set(recvd)
		return nil
	case <-msa.ctx.Done():
		return io.EOF
	}
}
