package buildcontext

import (
	"context"
	"time"

	"github.com/gocql/gocql"
	"gopkg.in/DataDog/dd-trace-go.v1/ddtrace/tracer"
)

type ctxKeyType string

var ctxIDKey ctxKeyType = "id"
var ctxStartedKey ctxKeyType = "started"
var ctxPushStartedkey ctxKeyType = "push_started"

// NewBuildIDContext returns a context with the current build ID and time started
// stored as values
func NewBuildIDContext(ctx context.Context, id gocql.UUID, span tracer.Span) context.Context {
	ctxWithSpan := tracer.ContextWithSpan(ctx, span)
	return context.WithValue(context.WithValue(ctxWithSpan, ctxIDKey, id), ctxStartedKey, time.Now().UTC())
}

// NewPushStartedContext returns a context with the push started timestamp stored
// as a value
func NewPushStartedContext(ctx context.Context) context.Context {
	return context.WithValue(ctx, ctxPushStartedkey, time.Now().UTC())
}

// BuildIDFromContext returns the ID stored in ctx, if any
func BuildIDFromContext(ctx context.Context) (gocql.UUID, bool) {
	id, ok := ctx.Value(ctxIDKey).(gocql.UUID)
	return id, ok
}

// StartedFromContext returns the time the build started stored in ctx, if any
func StartedFromContext(ctx context.Context) (time.Time, bool) {
	started, ok := ctx.Value(ctxStartedKey).(time.Time)
	return started, ok
}

// PushStartedFromContext returns the time the push started stored in ctx, if any
func PushStartedFromContext(ctx context.Context) (time.Time, bool) {
	ps, ok := ctx.Value(ctxPushStartedkey).(time.Time)
	return ps, ok
}

// IsCancelled checks if the provided done channel is closed
func IsCancelled(done <-chan struct{}) bool {
	select {
	case <-done:
		return true
	default:
		return false
	}
}
