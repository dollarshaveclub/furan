package tracing

import (
	"gopkg.in/DataDog/dd-trace-go.v1/ddtrace/tracer"
)

const (
	datalayerOperation = "datalayer.operation"
)

// StartChildSpan is just a wrapper for starting a child span given a parent
// span.
func StartChildSpan(operationName string, parentSpan tracer.Span) tracer.Span {
	return tracer.StartSpan(operationName, tracer.ChildOf(parentSpan.Context()))
}
