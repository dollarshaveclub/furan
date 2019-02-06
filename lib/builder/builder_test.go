package builder

import (
	"context"
	"io/ioutil"
	"log"
	"strings"
	"testing"

	"gopkg.in/DataDog/dd-trace-go.v1/ddtrace/tracer"

	dtypes "github.com/docker/engine-api/types"
	"github.com/dollarshaveclub/furan/generated/lib"
	"github.com/dollarshaveclub/furan/lib/buildcontext"
	"github.com/dollarshaveclub/furan/lib/mocks"
	"github.com/gocql/gocql"
	"github.com/golang/mock/gomock"
)

var testLogger = log.New(ioutil.Discard, "", log.LstdFlags)
var testDockerCfg = map[string]dtypes.AuthConfig{}
var testS3ErrorLogcfg = S3ErrorLogConfig{}
testSpan, _ := tracer.SpanFromContext(context.Background())

type imageBuildPusherDeps struct {
	ctrl *gomock.Controller
	mdl  *mocks.MockDataLayer
	mcf  *mocks.MockCodeFetcher
	mebp *mocks.MockEventBusProducer
	mmc  *mocks.MockMetricsCollector
	mis  *mocks.MockImageSquasher
	mibc *mocks.MockImageBuildClient
	mitc *mocks.MockImageTagChecker
	mosm *mocks.MockObjectStorageManager
}

func getTestImageBuildPusher(t *testing.T) (ImageBuildPusher, *imageBuildPusherDeps, *gomock.Controller) {
	ctrl := gomock.NewController(t)
	deps := imageBuildPusherDeps{
		mdl:  mocks.NewMockDataLayer(ctrl),
		mcf:  mocks.NewMockCodeFetcher(ctrl),
		mebp: mocks.NewMockEventBusProducer(ctrl),
		mmc:  mocks.NewMockMetricsCollector(ctrl),
		mis:  mocks.NewMockImageSquasher(ctrl),
		mibc: mocks.NewMockImageBuildClient(ctrl),
		mitc: mocks.NewMockImageTagChecker(ctrl),
		mosm: mocks.NewMockObjectStorageManager(ctrl),
	}
	ibp, err := NewImageBuilder(deps.mebp, deps.mdl, deps.mcf, deps.mibc, deps.mmc, deps.mosm, deps.mis, deps.mitc, testDockerCfg, testS3ErrorLogcfg, testLogger)
	if err != nil {
		t.Fatalf("error getting ImageBuilder: %v", err)
	}
	return ibp, &deps, ctrl
}

func TestImageBuildTagCheckRegistrySkip(t *testing.T) {
	ibp, deps, ctrl := getTestImageBuildPusher(t)
	defer ctrl.Finish()

	id, _ := gocql.RandomUUID()
	ctx := buildcontext.NewBuildIDContext(context.Background(), id, testSpan))

	deps.mdl.EXPECT().SetBuildTimeMetric(gomock.Any(), id, gomock.Any()).Times(1)
	deps.mcf.EXPECT().GetCommitSHA(gomock.Any(), "dollarshaveclub", "furan", "master").Return("asdf1234", nil).Times(1)
	deps.mitc.EXPECT().AllTagsExist([]string{"master"}, "quay.io/dollarshaveclub/furan").Times(1).Return(true, nil, nil)
	deps.mebp.EXPECT().PublishEvent(gomock.Any()).AnyTimes()

	req := &lib.BuildRequest{
		SkipIfExists: true,
		Build: &lib.BuildDefinition{
			GithubRepo: "dollarshaveclub/furan",
			Ref:        "master",
			Tags:       []string{"master"},
		},
		Push: &lib.PushDefinition{
			Registry: &lib.PushRegistryDefinition{
				Repo: "quay.io/dollarshaveclub/furan",
			},
		},
	}

	_, err := ibp.Build(ctx, req, id)
	if err == nil {
		t.Fatalf("build should have been skipped")
	}
	if !strings.Contains(err.Error(), "build not necessary") {
		t.Fatalf("build error should have said not necessary")
	}
}

func TestImageBuildTagCheckS3Skip(t *testing.T) {
	ibp, deps, ctrl := getTestImageBuildPusher(t)
	defer ctrl.Finish()

	id, _ := gocql.RandomUUID()
	ctx := buildcontext.NewBuildIDContext(context.Background(), id, testSpan)

	deps.mdl.EXPECT().SetBuildTimeMetric(gomock.Any(), id, gomock.Any()).Times(1)
	deps.mcf.EXPECT().GetCommitSHA(gomock.Any(), "dollarshaveclub", "furan", "master").Return("asdf1234", nil).Times(1)
	deps.mosm.EXPECT().Exists(gomock.Any(), gomock.Any()).Times(1).Return(true, nil)

	req := &lib.BuildRequest{
		SkipIfExists: true,
		Build: &lib.BuildDefinition{
			GithubRepo: "dollarshaveclub/furan",
			Ref:        "master",
			Tags:       []string{"master"},
		},
		Push: &lib.PushDefinition{
			Registry: &lib.PushRegistryDefinition{},
			S3: &lib.PushS3Definition{
				Region:    "us-west-2",
				Bucket:    "foo",
				KeyPrefix: "bar",
			},
		},
	}

	_, err := ibp.Build(ctx, req, id)
	if err == nil {
		t.Fatalf("build should have been skipped")
	}
	if !strings.Contains(err.Error(), "build not necessary") {
		t.Fatalf("build error should have said not necessary")
	}
}
