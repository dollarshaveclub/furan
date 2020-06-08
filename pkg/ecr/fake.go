package ecr

import (
	awsecr "github.com/aws/aws-sdk-go/service/ecr"
	"github.com/aws/aws-sdk-go/service/ecr/ecriface"
	ecrapi "github.com/awslabs/amazon-ecr-credential-helper/ecr-login/api"
)

type FakeECRAuthClient struct {
	ecrapi.Client
	GetCredsFunc func(serverURL string) (*ecrapi.Auth, error)
}

func (eac FakeECRAuthClient)  GetCredentials(serverURL string) (*ecrapi.Auth, error) {
	if eac.GetCredsFunc != nil {
		return eac.GetCredsFunc(serverURL)
	}
	return &ecrapi.Auth{}, nil
}

type FakeECRClient struct {
	ecriface.ECRAPI
	DescribeImagesPagesFunc func(input *awsecr.DescribeImagesInput, fn func(*awsecr.DescribeImagesOutput, bool) bool) error
}

func (fec FakeECRClient) DescribeImagesPages(input *awsecr.DescribeImagesInput, fn func(*awsecr.DescribeImagesOutput, bool) bool) error {
	if fec.DescribeImagesPagesFunc != nil {
		return fec.DescribeImagesPagesFunc(input, fn)
	}
	return nil
}
