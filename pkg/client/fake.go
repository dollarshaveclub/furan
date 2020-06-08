package rpcclient

import (
	"context"
	"errors"
	"fmt"
	"io"
)

// FakeFuranClient is fake client for testing
type FakeFuranClient struct {
	bf FakeClientBuilderFunc
}

// FakeClientBuilderFunc is a function that produces BuildEvents that are passed to the out channel. Return an error to abort the build. or io.EOF to gracefully signal the build is finished successfully.
type FakeClientBuilderFunc func() (*BuildEvent, error)

// NewFakeFuranClient returns a FakeFuranClient using bf and vf
func NewFakeFuranClient(bf FakeClientBuilderFunc) (*FakeFuranClient, error) {
	if bf == nil {
		return nil, errors.New("bf is nil")
	}
	return &FakeFuranClient{bf: bf}, nil
}

// Build performs a fake build, writing fake build events using FakeClientBuilderFunc to out.
func (ffc *FakeFuranClient) Build(ctx context.Context, out chan *BuildEvent, req *BuildRequest) (string, error) {
	fc := FuranClient{}
	if err := fc.validateBuildRequest(req); err != nil {
		return "", fmt.Errorf("request failed validation: %v", err)
	}
	for {
		select {
		case <-ctx.Done():
			return "", errors.New("build was cancelled")
		default:
		}
		event, err := ffc.bf()
		if err != nil {
			if err == io.EOF {
				break
			}
			return "", fmt.Errorf("build failed: %v", err)
		}
		out <- event
	}
	return "fakebuildid", nil
}
