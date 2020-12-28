package auth

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	awssession "github.com/aws/aws-sdk-go/aws/session"
	ecrapi "github.com/awslabs/amazon-ecr-credential-helper/ecr-login/api"
	"github.com/moby/buildkit/session"
	bkauth "github.com/moby/buildkit/session/auth"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Provider struct {
	QuayIOToken                  string
	AccessKeyID, SecretAccessKey string // AWS credentials scoped to ECR only
	ECRAuthClientFactoryFunc     func(s *awssession.Session, cfg *aws.Config) ecrapi.Client
}

var _ session.Attachable = &Provider{}

func New(quaytkn, awskeyid, awskey string) session.Attachable {
	return &Provider{
		QuayIOToken:     quaytkn,
		AccessKeyID:     awskeyid,
		SecretAccessKey: awskey,
	}
}

func (p *Provider) Register(s *grpc.Server) {
	bkauth.RegisterAuthServer(s, p)
}

// stub methods to satisfy the AuthServer interface
func (p *Provider) FetchToken(context.Context, *bkauth.FetchTokenRequest) (*bkauth.FetchTokenResponse, error) {
	return &bkauth.FetchTokenResponse{}, nil
}
func (p *Provider) GetTokenAuthority(context.Context, *bkauth.GetTokenAuthorityRequest) (*bkauth.GetTokenAuthorityResponse, error) {
	return &bkauth.GetTokenAuthorityResponse{}, nil
}
func (p *Provider) VerifyTokenAuthority(context.Context, *bkauth.VerifyTokenAuthorityRequest) (*bkauth.VerifyTokenAuthorityResponse, error) {
	return &bkauth.VerifyTokenAuthorityResponse{}, nil
}

func (p *Provider) Credentials(ctx context.Context, req *bkauth.CredentialsRequest) (*bkauth.CredentialsResponse, error) {
	if req == nil {
		return nil, status.Errorf(codes.InvalidArgument, "request is nil")
	}
	var username, secret string
	switch {
	case isECR(req.Host):
		u, s, err := p.GetECRAuth(req.Host)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "error getting ECR auth: %v", err)
		}
		username = u
		secret = s
	case isQuay(req.Host):
		username = "$oauthtoken"
		secret = p.QuayIOToken
	case strings.HasSuffix(req.Host, ".docker.io"): // allow anonymous pulls from Docker Hub (with empty creds)
		break
	default:
		return nil, status.Errorf(codes.InvalidArgument, "unsupported registry host: %v", req.Host)
	}
	return &bkauth.CredentialsResponse{
		Username: username,
		Secret:   secret,
	}, nil
}

// isECR returns whether host is an ECR image repository
func isECR(host string) bool {
	_, err := ecrapi.ExtractRegistry(host)
	return err == nil
}

// isQuay returns whether host is quay.io
func isQuay(host string) bool {
	return host == "quay.io"
}

func (p *Provider) GetECRAuth(serverURL string) (string, string, error) {
	// modified copypasta from https://github.com/awslabs/amazon-ecr-credential-helper/blob/master/ecr-login/ecr.go
	registry, err := ecrapi.ExtractRegistry(serverURL)
	if err != nil {
		return "", "", fmt.Errorf("error parsing server URL: %v", err)
	}
	cfg := &aws.Config{
		Region:      &registry.Region,
		Credentials: credentials.NewStaticCredentials(p.AccessKeyID, p.SecretAccessKey, ""),
	}
	sess, err := awssession.NewSession(cfg)
	if err != nil {
		return "", "", fmt.Errorf("error getting aws session: %v", err)
	}
	if p.ECRAuthClientFactoryFunc == nil {
		p.ECRAuthClientFactoryFunc = func(s *awssession.Session, cfg *aws.Config) ecrapi.Client {
			return ecrapi.DefaultClientFactory{}.NewClient(sess, cfg)
		}
	}
	client := p.ECRAuthClientFactoryFunc(sess, cfg)

	auth, err := client.GetCredentials(serverURL)
	if err != nil {
		return "", "", fmt.Errorf("error getting ECR credentials for repo: %v: %v", serverURL, err)
	}
	return auth.Username, auth.Password, nil
}
