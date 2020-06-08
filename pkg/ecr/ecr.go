package ecr

import (
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	awsecr "github.com/aws/aws-sdk-go/service/ecr"
	"github.com/aws/aws-sdk-go/service/ecr/ecriface"
	ecrapi "github.com/awslabs/amazon-ecr-credential-helper/ecr-login/api"
)

// RegistryManager manages interaction with ECR-backed image repositories
type RegistryManager struct {
	AccessKeyID, SecretAccessKey string // AWS credentials scoped to ECR only
	ECRAuthClientFactoryFunc     func(s *session.Session, cfg *aws.Config) ecrapi.Client
	ECRClientFactoryFunc         func(s *session.Session) ecriface.ECRAPI
}

// GetDockerAuthConfig gets docker engine auth for a repository server URL ([ecr server url]/[repo]:[tag]) and returns the username and password, or error
func (r RegistryManager) GetDockerAuthConfig(serverURL string) (string, string, error) {
	// modified copypasta from https://github.com/awslabs/amazon-ecr-credential-helper/blob/master/ecr-login/ecr.go
	registry, err := ecrapi.ExtractRegistry(serverURL)
	if err != nil {
		return "", "", fmt.Errorf("error parsing server URL: %v", err)
	}
	cfg := &aws.Config{
		Region:      &registry.Region,
		Credentials: credentials.NewStaticCredentials(r.AccessKeyID, r.SecretAccessKey, ""),
	}
	sess, err := session.NewSession(cfg)
	if err != nil {
		return "", "", fmt.Errorf("error getting aws session: %v", err)
	}
	if r.ECRAuthClientFactoryFunc == nil {
		r.ECRAuthClientFactoryFunc = func(s *session.Session, cfg *aws.Config) ecrapi.Client {
			return ecrapi.DefaultClientFactory{}.NewClient(sess, cfg)
		}
	}
	client := r.ECRAuthClientFactoryFunc(sess, cfg)

	auth, err := client.GetCredentials(serverURL)
	if err != nil {
		return "", "", fmt.Errorf("error getting ECR credentials for repo: %v: %v", serverURL, err)
	}
	return auth.Username, auth.Password, nil
}

// IsECR returns whether repo ([owner/url]/[name]) is an ECR image repository
func (r RegistryManager) IsECR(repo string) bool {
	serverURL := strings.Split(repo, "/")[0]
	_, err := ecrapi.ExtractRegistry(serverURL)
	return err == nil
}

// AllTagsExist is API compatible with tagcheck and returns whether all tags exist in repo ([ecr server url]/[repo name]), and returns missing tags (if any)
func (r RegistryManager) AllTagsExist(tags []string, repo string) (bool, []string, error) {
	rs := strings.Split(repo, "/")
	if len(rs) != 2 {
		return false, nil, fmt.Errorf("unexpected repo format or bad repo: %v (expected: [ecr url]/[reponame]:[tag])", repo)
	}
	if strings.Contains(rs[1], ":") {
		return false, nil, fmt.Errorf("repo contains unexpected tag: %v", repo)
	}
	serverURL := rs[0]
	reponame := rs[1]
	registry, err := ecrapi.ExtractRegistry(serverURL)
	if err != nil {
		return false, nil, fmt.Errorf("error parsing server URL: %v", err)
	}
	sess, err := session.NewSession(&aws.Config{
		Region:      &registry.Region,
		Credentials: credentials.NewStaticCredentials(r.AccessKeyID, r.SecretAccessKey, ""),
	})
	if err != nil {
		return false, nil, fmt.Errorf("error getting aws session: %v", err)
	}
	ins := make([]*awsecr.DescribeImagesInput, len(tags))
	missing := make(map[string]struct{}, len(tags))
	for i, t := range tags {
		missing[t] = struct{}{}
		in := &awsecr.DescribeImagesInput{
			RepositoryName: aws.String(reponame),
			ImageIds: []*awsecr.ImageIdentifier{
				&awsecr.ImageIdentifier{ImageTag: aws.String(t)},
			},
		}
		ins[i] = in
	}
	if r.ECRClientFactoryFunc == nil {
		r.ECRClientFactoryFunc = func(sess *session.Session) ecriface.ECRAPI {
			return awsecr.New(sess)
		}
	}
	ecrsvc := r.ECRClientFactoryFunc(sess)
	// iterate through tags, each one that's found is removed from missing
	for _, in := range ins {
		err = ecrsvc.DescribeImagesPages(in, func(out *awsecr.DescribeImagesOutput, b bool) bool {
			for _, id := range out.ImageDetails {
				for _, it := range id.ImageTags {
					if it != nil {
						delete(missing, *it)
					}
				}
			}
			return true
		})
		if err != nil {
			awsErr, ok := err.(awserr.Error)
			if ok && awsErr.Code() == awsecr.ErrCodeImageNotFoundException {
				continue
			}
			return false, nil, fmt.Errorf("error describing image: %v", err)
		}
	}
	mt := make([]string, len(missing))
	i := 0
	for t := range missing {
		mt[i] = t
		i++
	}
	return len(missing) == 0, mt, nil
}
