package consul

import (
	"testing"
	"time"

	"github.com/dollarshaveclub/furan/pkg/config"
	"github.com/dollarshaveclub/furan/pkg/mocks"
	"github.com/gocql/gocql"
	"github.com/golang/mock/gomock"
	consul "github.com/hashicorp/consul/api"
)

var testConsulconfig = &config.Consulconfig{
	KVPrefix: "furan",
}

func TestSetBuildRunning(t *testing.T) {
	ctrl := gomock.NewController(t)
	mconsul := mocks.NewMockConsulKV(ctrl)
	defer ctrl.Finish()
	id, _ := gocql.RandomUUID()
	mconsul.EXPECT().Put(gomock.Eq(&consul.KVPair{Key: testConsulconfig.KVPrefix + "/running/" + id.String()}), nil).Times(1)
	cvo, err := newConsulKVOrchestrator(mconsul, testConsulconfig)
	if err != nil {
		t.Fatalf("error creating consul orchestrator: %v", err)
	}
	err = cvo.SetBuildRunning(id)
	if err != nil {
		t.Fatalf("should have succeeded: %v", err)
	}
}

func TestCheckIfBuildRunning(t *testing.T) {
	ctrl := gomock.NewController(t)
	mconsul := mocks.NewMockConsulKV(ctrl)
	defer ctrl.Finish()
	id, _ := gocql.RandomUUID()
	key := testConsulconfig.KVPrefix + "/running/" + id.String()
	mconsul.EXPECT().Get(key, nil).Return(&consul.KVPair{Key: key}, nil, nil).Times(1)
	cvo, err := newConsulKVOrchestrator(mconsul, testConsulconfig)
	if err != nil {
		t.Fatalf("error creating consul orchestrator: %v", err)
	}
	running, err := cvo.CheckIfBuildRunning(id)
	if err != nil {
		t.Fatalf("should have succeeded: %v", err)
	}
	if !running {
		t.Fatalf("should have been true")
	}
}

func TestDeleteBuildRunning(t *testing.T) {
	ctrl := gomock.NewController(t)
	mconsul := mocks.NewMockConsulKV(ctrl)
	defer ctrl.Finish()
	id, _ := gocql.RandomUUID()
	mconsul.EXPECT().Delete(gomock.Eq(testConsulconfig.KVPrefix+"/running/"+id.String()), nil).Times(1)
	cvo, err := newConsulKVOrchestrator(mconsul, testConsulconfig)
	if err != nil {
		t.Fatalf("error creating consul orchestrator: %v", err)
	}
	err = cvo.DeleteBuildRunning(id)
	if err != nil {
		t.Fatalf("should have succeeded: %v", err)
	}
}

func TestSetBuildCancelled(t *testing.T) {
	ctrl := gomock.NewController(t)
	mconsul := mocks.NewMockConsulKV(ctrl)
	defer ctrl.Finish()
	id, _ := gocql.RandomUUID()
	mconsul.EXPECT().Put(gomock.Eq(&consul.KVPair{Key: testConsulconfig.KVPrefix + "/cancelled/" + id.String()}), nil).Times(1)
	cvo, err := newConsulKVOrchestrator(mconsul, testConsulconfig)
	if err != nil {
		t.Fatalf("error creating consul orchestrator: %v", err)
	}
	err = cvo.SetBuildCancelled(id)
	if err != nil {
		t.Fatalf("should have succeeded: %v", err)
	}
}

func TestDeleteBuildCancelled(t *testing.T) {
	ctrl := gomock.NewController(t)
	mconsul := mocks.NewMockConsulKV(ctrl)
	defer ctrl.Finish()
	id, _ := gocql.RandomUUID()
	mconsul.EXPECT().Delete(gomock.Eq(testConsulconfig.KVPrefix+"/cancelled/"+id.String()), nil).Times(1)
	cvo, err := newConsulKVOrchestrator(mconsul, testConsulconfig)
	if err != nil {
		t.Fatalf("error creating consul orchestrator: %v", err)
	}
	err = cvo.DeleteBuildCancelled(id)
	if err != nil {
		t.Fatalf("should have succeeded: %v", err)
	}
}

func TestWatchIfBuildStopsRunning(t *testing.T) {
	ctrl := gomock.NewController(t)
	mconsul := mocks.NewMockConsulKV(ctrl)
	defer ctrl.Finish()
	id, _ := gocql.RandomUUID()
	pfx := testConsulconfig.KVPrefix + "/running/"
	target := pfx + id.String()
	gomock.InOrder(
		mconsul.EXPECT().Keys(pfx, "", nil).Return([]string{target}, &consul.QueryMeta{}, nil),
		mconsul.EXPECT().Keys(pfx, "", gomock.Any()).Return([]string{}, &consul.QueryMeta{}, nil),
	)
	cvo, err := newConsulKVOrchestrator(mconsul, testConsulconfig)
	if err != nil {
		t.Fatalf("error creating consul orchestrator: %v", err)
	}
	yes, err := cvo.WatchIfBuildStopsRunning(id, 1*time.Second)
	if err != nil {
		t.Fatalf("should have succeeded")
	}
	if !yes {
		t.Fatalf("should have been true")
	}
}

func TestWatchIfBuildStopsRunningTimeout(t *testing.T) {
	ctrl := gomock.NewController(t)
	mconsul := mocks.NewMockConsulKV(ctrl)
	defer ctrl.Finish()
	id, _ := gocql.RandomUUID()
	pfx := testConsulconfig.KVPrefix + "/running/"
	target := pfx + id.String()
	mconsul.EXPECT().Keys(pfx, "", gomock.Any()).Return([]string{target}, &consul.QueryMeta{}, nil).AnyTimes()
	cvo, err := newConsulKVOrchestrator(mconsul, testConsulconfig)
	if err != nil {
		t.Fatalf("error creating consul orchestrator: %v", err)
	}
	yes, err := cvo.WatchIfBuildStopsRunning(id, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("should have succeeded")
	}
	if yes {
		t.Fatalf("should have timed out")
	}
}

func TestWatchIfBuildIsCancelled(t *testing.T) {
	ctrl := gomock.NewController(t)
	mconsul := mocks.NewMockConsulKV(ctrl)
	defer ctrl.Finish()
	id, _ := gocql.RandomUUID()
	pfx := testConsulconfig.KVPrefix + "/cancelled/"
	target := pfx + id.String()
	gomock.InOrder(
		mconsul.EXPECT().Keys(pfx, "", nil).Return([]string{}, &consul.QueryMeta{}, nil),
		mconsul.EXPECT().Keys(pfx, "", gomock.Any()).Return([]string{target}, &consul.QueryMeta{}, nil),
	)
	cvo, err := newConsulKVOrchestrator(mconsul, testConsulconfig)
	if err != nil {
		t.Fatalf("error creating consul orchestrator: %v", err)
	}
	yes, err := cvo.WatchIfBuildIsCancelled(id, 1*time.Second)
	if err != nil {
		t.Fatalf("should have succeeded")
	}
	if !yes {
		t.Fatalf("should have been true")
	}
}

func TestWatchIfBuildIsCancelledTimeout(t *testing.T) {
	ctrl := gomock.NewController(t)
	mconsul := mocks.NewMockConsulKV(ctrl)
	defer ctrl.Finish()
	id, _ := gocql.RandomUUID()
	pfx := testConsulconfig.KVPrefix + "/cancelled/"
	mconsul.EXPECT().Keys(pfx, "", gomock.Any()).Return([]string{}, &consul.QueryMeta{}, nil).AnyTimes()
	cvo, err := newConsulKVOrchestrator(mconsul, testConsulconfig)
	if err != nil {
		t.Fatalf("error creating consul orchestrator: %v", err)
	}
	yes, err := cvo.WatchIfBuildIsCancelled(id, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("should have succeeded")
	}
	if yes {
		t.Fatalf("should have timed out")
	}
}
