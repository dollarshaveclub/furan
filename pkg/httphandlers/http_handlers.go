package httphandlers

import (
	"fmt"
	"io"
	"net/http"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"

	"github.com/dollarshaveclub/furan/pkg/generated/furanrpc"
	fgrpc "github.com/dollarshaveclub/furan/pkg/grpc"
	"github.com/golang/protobuf/jsonpb"
	"github.com/golang/protobuf/proto"
	"github.com/gorilla/mux"
	"golang.org/x/net/context"
)

// HTTPAdapter holds the handlers for the HTTP endpoints
type HTTPAdapter struct {
	gr  *fgrpc.GrpcServer
	pbm jsonpb.Marshaler
}

func NewHTTPAdapter(gr *fgrpc.GrpcServer) *HTTPAdapter {
	return &HTTPAdapter{
		gr: gr,
	}
}

// any null values (omitted) will be deserialized as nil, replace with empty structs
func (ha *HTTPAdapter) unmarshalRequest(r io.Reader) (*furanrpc.BuildRequest, error) {
	req := furanrpc.BuildRequest{}
	err := jsonpb.Unmarshal(r, &req)
	if err != nil {
		return nil, err
	}
	if req.Build == nil {
		req.Build = &furanrpc.BuildDefinition{}
	}
	if req.Push == nil {
		req.Push = &furanrpc.PushDefinition{
			Registry: &furanrpc.PushRegistryDefinition{},
			S3:       &furanrpc.PushS3Definition{},
		}
	}
	if req.Push.Registry == nil {
		req.Push.Registry = &furanrpc.PushRegistryDefinition{}
	}
	if req.Push.S3 == nil {
		req.Push.S3 = &furanrpc.PushS3Definition{}
	}
	return &req, nil
}

func (ha *HTTPAdapter) handleRPCError(w http.ResponseWriter, err error) {
	code := grpc.Code(err)
	switch code {
	case codes.InvalidArgument:
		ha.badRequestError(w, err)
	case codes.Internal:
		ha.internalError(w, err)
	default:
		ha.internalError(w, err)
	}
}

// REST interface handlers (proxy to gRPC handlers)
func (ha *HTTPAdapter) BuildRequestHandler(w http.ResponseWriter, r *http.Request) {
	req, err := ha.unmarshalRequest(r.Body)
	if err != nil {
		ha.badRequestError(w, err)
		return
	}
	resp, err := ha.gr.StartBuild(context.Background(), req)
	if err != nil {
		ha.handleRPCError(w, err)
		return
	}
	ha.httpSuccess(w, resp)
}

func (ha *HTTPAdapter) BuildStatusHandler(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	req := furanrpc.BuildStatusRequest{
		BuildId: id,
	}
	resp, err := ha.gr.GetBuildStatus(context.Background(), &req)
	if err != nil {
		ha.handleRPCError(w, err)
		return
	}
	ha.httpSuccess(w, resp)
}

func (ha *HTTPAdapter) BuildCancelHandler(w http.ResponseWriter, r *http.Request) {
	var req furanrpc.BuildCancelRequest
	err := jsonpb.Unmarshal(r.Body, &req)
	if err != nil {
		ha.badRequestError(w, err)
		return
	}
	resp, err := ha.gr.CancelBuild(context.Background(), &req)
	if err != nil {
		ha.handleRPCError(w, err)
		return
	}
	ha.httpSuccess(w, resp)
}

func (ha *HTTPAdapter) httpSuccess(w http.ResponseWriter, resp proto.Message) {
	js, err := ha.pbm.MarshalToString(resp)
	if err != nil {
		ha.internalError(w, err)
		return
	}
	w.Header().Add("Content-Type", "application/json")
	w.Write([]byte(js))
}

func (ha *HTTPAdapter) badRequestError(w http.ResponseWriter, err error) {
	ha.httpError(w, http.StatusBadRequest, err)
}

func (ha *HTTPAdapter) internalError(w http.ResponseWriter, err error) {
	ha.httpError(w, http.StatusInternalServerError, err)
}

func (ha *HTTPAdapter) httpError(w http.ResponseWriter, code int, err error) {
	w.Header().Add("Content-Type", "application/json")
	w.WriteHeader(code)
	w.Write([]byte(fmt.Sprintf(`{"error_details":"%v"}`, err)))
}

func (ha *HTTPAdapter) HealthHandler(w http.ResponseWriter, r *http.Request) {
	if cap(ha.gr.WorkerChan()) > 0 {
		w.WriteHeader(http.StatusOK)
	} else {
		w.WriteHeader(http.StatusTooManyRequests)
	}
}
