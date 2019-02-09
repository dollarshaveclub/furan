package tracing

import (
	"context"

	"github.com/gocql/gocql"
	gocqltrace "gopkg.in/DataDog/dd-trace-go.v1/contrib/gocql/gocql"
	"gopkg.in/DataDog/dd-trace-go.v1/ddtrace/ext"
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

// GetTracedQuery will wrap a gocql Query and return a traced query with
// some additional context from the parent.
func GetTracedQuery(query *gocql.Query, parentSpan tracer.Span) *gocqltrace.Query {
	_, ctx := tracer.StartSpanFromContext(context.Background(), datalayerOperation,
		tracer.SpanType(ext.SpanTypeCassandra),
		tracer.ChildOf(parentSpan.Context()),
	)
	tracedQuery := gocqltrace.WrapQuery(query, gocqltrace.WithServiceName("furan-dqa.gocql")).WithContext(ctx)
	return tracedQuery
}
