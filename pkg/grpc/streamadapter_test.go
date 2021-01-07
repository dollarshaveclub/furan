package grpc

import (
	"context"
	"testing"
	"time"

	"github.com/gofrs/uuid"
	"github.com/google/go-cmp/cmp"

	"github.com/dollarshaveclub/furan/pkg/generated/furanrpc"
	"github.com/dollarshaveclub/furan/pkg/models"
)

func TestMonitorStreamAdapter_RecvMsg(t *testing.T) {
	tests := []struct {
		name    string
		ctxfunc func() context.Context
		m       interface{}
		wantErr bool
	}{
		{
			name: "success",
			m: &furanrpc.BuildEvent{
				BuildId:      uuid.Must(uuid.NewV4()).String(),
				Message:      "asdf",
				CurrentState: 1,
			},
			wantErr: false,
		},
		{
			name:    "bad type",
			m:       &time.Time{},
			wantErr: true,
		},
		{
			name: "cancelled context",
			ctxfunc: func() context.Context {
				ctx, cf := context.WithCancel(context.Background())
				cf()
				return ctx
			},
			m: &furanrpc.BuildEvent{
				BuildId:      uuid.Must(uuid.NewV4()).String(),
				Message:      "asdf",
				CurrentState: 1,
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var ctx context.Context
			if tt.ctxfunc != nil {
				ctx = tt.ctxfunc()
			}
			msa := NewMonitorStreamAdapter(ctx, 0)
			if tt.m != nil {
				go msa.SendMsg(tt.m)
			}
			err := msa.RecvMsg(tt.m)
			if (err != nil) != tt.wantErr {
				t.Errorf("RecvMsg() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestMonitorStreamAdapter_SendMsg(t *testing.T) {
	id := uuid.Must(uuid.NewV4())
	events := []*furanrpc.BuildEvent{
		&furanrpc.BuildEvent{
			BuildId:      id.String(),
			Message:      "asdf1",
			CurrentState: models.BuildStatusRunning.State(),
		},
		&furanrpc.BuildEvent{
			BuildId:      id.String(),
			Message:      "asdf2",
			CurrentState: models.BuildStatusRunning.State(),
		},
		&furanrpc.BuildEvent{
			BuildId:      id.String(),
			Message:      "asdf3",
			CurrentState: models.BuildStatusRunning.State(),
		},
		&furanrpc.BuildEvent{
			BuildId:      id.String(),
			Message:      "asdf4",
			CurrentState: models.BuildStatusRunning.State(),
		},
	}

	ctx, cf := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cf()

	msa := NewMonitorStreamAdapter(ctx, 0)

	go func() {
		for i := range events {
			err := msa.SendMsg(events[i])
			if err != nil {
				t.Errorf("error sending msg %d: %v", i, err)
			}
		}
	}()

	received := make([]*furanrpc.BuildEvent, len(events))
	for i := range received {
		e := &furanrpc.BuildEvent{}
		err := msa.RecvMsg(e)
		received[i] = e
		if err != nil {
			t.Errorf("error receiving msg %d: %v", i, err)
		}
	}

	if !cmp.Equal(events, received) {
		t.Errorf("bad received events: %v", received)
	}
}
