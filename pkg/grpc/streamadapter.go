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
	// Ctx is used as the stream context. If cancelled, any blocking methods will return
	Ctx context.Context
	// CancelFunc cancels the embedded context
	CancelFunc context.CancelFunc
	// BufferedMsgs is the number of messages to buffer internally without blocking (default: 0)
	BufferedMsgs uint
	c            chan *furanrpc.BuildEvent
	grpc.ServerStream
	sync.RWMutex
}

var _ furanrpc.FuranExecutor_MonitorBuildServer = &MonitorStreamAdapter{}

func (msa *MonitorStreamAdapter) init() {
	msa.RLock()
	defer msa.RUnlock()

	if msa.c == nil {
		msa.RUnlock()
		msa.Lock()
		msa.c = make(chan *furanrpc.BuildEvent, msa.BufferedMsgs)
		msa.Unlock()
		msa.RLock()
	}

	if msa.Ctx == nil {
		msa.RUnlock()
		msa.Lock()
		msa.Ctx = context.Background()
		msa.Unlock()
		msa.RLock()
	}

	if msa.CancelFunc == nil {
		msa.RUnlock()
		msa.Lock()
		msa.Ctx, msa.CancelFunc = context.WithCancel(msa.Ctx)
		msa.Unlock()
		msa.RLock()
	}
}

func (msa *MonitorStreamAdapter) checkCtx() bool {
	select {
	case <-msa.Ctx.Done():
		return false
	default:
		return true
	}
}

func (msa *MonitorStreamAdapter) Context() context.Context {
	msa.init()
	msa.RLock()
	defer msa.RUnlock()
	ctx := msa.Ctx
	return ctx
}

func (msa *MonitorStreamAdapter) Send(e *furanrpc.BuildEvent) error {
	return msa.SendMsg(e)
}

// SendMsg sends m (which must be a *furanrpc.BuildEvent) to listeners
func (msa *MonitorStreamAdapter) SendMsg(m interface{}) error {
	msa.init()
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
	case <-msa.Ctx.Done():
		return io.EOF
	}
}

// RecvMsg blocks and listens for *furanrpc.BuildEvent messages and if one is received, assigns it to m and returns a nil error
func (msa *MonitorStreamAdapter) RecvMsg(m interface{}) error {
	msa.init()
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
	case <-msa.Ctx.Done():
		return io.EOF
	}
}
