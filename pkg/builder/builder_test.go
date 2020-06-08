package builder

import (
	"context"
	"fmt"
	"io/ioutil"
	"log"
	"strings"
	"testing"

	//"gopkg.in/DataDog/dd-trace-go.v1/ddtrace/tracer"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	awsecr "github.com/aws/aws-sdk-go/service/ecr"
	"github.com/aws/aws-sdk-go/service/ecr/ecriface"
	ecrapi "github.com/awslabs/amazon-ecr-credential-helper/ecr-login/api"
	dtypes "github.com/docker/engine-api/types"
	"github.com/dollarshaveclub/furan/pkg/buildcontext"
	"github.com/dollarshaveclub/furan/pkg/ecr"
	"github.com/dollarshaveclub/furan/pkg/generated/furanrpc"
	"github.com/dollarshaveclub/furan/pkg/mocks"
	"github.com/gocql/gocql"
	"github.com/golang/mock/gomock"
	"gopkg.in/DataDog/dd-trace-go.v1/ddtrace/tracer"
)

var testLogger = log.New(ioutil.Discard, "", log.LstdFlags)
var testDockerCfg = map[string]dtypes.AuthConfig{}
var testS3ErrorLogcfg = S3ErrorLogConfig{}
var testSpan tracer.Span

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
	rm := ecr.RegistryManager{
		ECRAuthClientFactoryFunc: func(s *session.Session, cfg *aws.Config) ecrapi.Client {
			return &ecr.FakeECRAuthClient{
				GetCredsFunc: func(serverURL string) (*ecrapi.Auth, error) {
					return &ecrapi.Auth{
						Username: "foo",
						Password: "bar",
					}, nil
				},
			}
		},
		ECRClientFactoryFunc: func(s *session.Session) ecriface.ECRAPI {
			return &ecr.FakeECRClient{
				DescribeImagesPagesFunc: func(input *awsecr.DescribeImagesInput, fn func(*awsecr.DescribeImagesOutput, bool) bool) error {
					if len(input.ImageIds) != 1 {
						return fmt.Errorf("expected 1 image id: %v", len(input.ImageIds))
					}
					fn(&awsecr.DescribeImagesOutput{
						ImageDetails: []*awsecr.ImageDetail{
							&awsecr.ImageDetail{
								RepositoryName: input.RepositoryName,
								ImageTags: []*string{
									input.ImageIds[0].ImageTag,
								},
							},
						},
					}, true)
					return nil
				},
			}
		},
	}
	ibp, err := NewImageBuilder(deps.mebp, deps.mdl, deps.mcf, deps.mibc, deps.mmc, deps.mosm, deps.mis, deps.mitc, testDockerCfg, testS3ErrorLogcfg, testLogger)
	if err != nil {
		t.Fatalf("error getting ImageBuilder: %v", err)
	}
	ibp.rm = rm
	testSpan, _ = tracer.SpanFromContext(context.Background())
	return ibp, &deps, ctrl
}

func TestImageBuildTagCheckRegistrySkip(t *testing.T) {
	ibp, deps, ctrl := getTestImageBuildPusher(t)
	defer ctrl.Finish()

	id, _ := gocql.RandomUUID()
	ctx := buildcontext.NewBuildIDContext(context.Background(), id, testSpan)

	deps.mdl.EXPECT().SetBuildTimeMetric(gomock.Any(), id, gomock.Any()).Times(1)
	deps.mcf.EXPECT().GetCommitSHA(gomock.Any(), "dollarshaveclub", "furan", "master").Return("asdf1234", nil).Times(1)
	deps.mitc.EXPECT().AllTagsExist([]string{"master"}, "quay.io/dollarshaveclub/furan").Times(1).Return(true, nil, nil)
	deps.mebp.EXPECT().PublishEvent(gomock.Any()).AnyTimes()

	req := &furanrpc.BuildRequest{
		SkipIfExists: true,
		Build: &furanrpc.BuildDefinition{
			GithubRepo: "dollarshaveclub/furan",
			Ref:        "master",
			Tags:       []string{"master"},
		},
		Push: &furanrpc.PushDefinition{
			Registry: &furanrpc.PushRegistryDefinition{
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
	deps.mebp.EXPECT().PublishEvent(gomock.Any()).AnyTimes()

	req := &furanrpc.BuildRequest{
		SkipIfExists: true,
		Build: &furanrpc.BuildDefinition{
			GithubRepo: "dollarshaveclub/furan",
			Ref:        "master",
			Tags:       []string{"master"},
		},
		Push: &furanrpc.PushDefinition{
			Registry: &furanrpc.PushRegistryDefinition{},
			S3: &furanrpc.PushS3Definition{
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
