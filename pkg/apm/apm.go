package apm

import (
	"context"

	"gopkg.in/DataDog/dd-trace-go.v1/ddtrace/tracer"
)

const (
	spanCtxKey = "dd-furan2-span"
)

// NewAPMContext returns a new context derived from parent storing span
func NewAPMContext(parent context.Context, span tracer.Span) context.Context {
	return context.WithValue(parent, spanCtxKey, span)
}

// StartChildSpan begins a new child span from the root span stored in ctx and returns a function that finishes the child span
// If no root span is found in ctx this is a no-op
func StartChildSpan(ctx context.Context, name string, tags map[string]string) func(err error) {
	s, ok := ctx.Value(spanCtxKey).(tracer.Span)
	if !ok || s == nil {
		return func(_ error) {}
	}
	child := tracer.StartSpan(name, tracer.ChildOf(s.Context()))
	for k, v := range tags {
		child.SetTag(k, v)
	}
	return func(err error) {
		child.Finish(tracer.WithError(err))
	}
}
