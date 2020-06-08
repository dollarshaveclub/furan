package ecr

import (
	"fmt"
	"testing"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/session"
	awsecr "github.com/aws/aws-sdk-go/service/ecr"
	"github.com/aws/aws-sdk-go/service/ecr/ecriface"
	ecrapi "github.com/awslabs/amazon-ecr-credential-helper/ecr-login/api"
)

func TestRegistryManagerZeroValue(t *testing.T) {
	// verify that the zero value is functional without panics
	rm := RegistryManager{}
	if !rm.IsECR("123456789.dkr.ecr.us-west-2.amazonaws.com/widgets") {
		t.Fatalf("IsECR: expected true")
	}
	_, _, err := rm.GetDockerAuthConfig("123456789.dkr.ecr.us-west-2.amazonaws.com/widgets")
	if err == nil {
		t.Fatalf("GetDockerAuthConfig: expected error")
	}
	_, _, err = rm.AllTagsExist([]string{"foo", "bar"}, "123456789.dkr.ecr.us-west-2.amazonaws.com/widgets")
	if err == nil {
		t.Fatalf("AllTagsExist: expected error")
	}
}

func TestRegistryManager_GetDockerAuthConfig(t *testing.T) {
	type fields struct {
		ECRAuthClientFactoryFunc func(s *session.Session, cfg *aws.Config) ecrapi.Client
	}
	type args struct {
		serverURL string
	}
	authfunc := func(s *session.Session, cfg *aws.Config) ecrapi.Client {
		return &FakeECRAuthClient{
			GetCredsFunc: func(serverURL string) (*ecrapi.Auth, error) {
				return &ecrapi.Auth{
					Username: "foo",
					Password: "bar",
				}, nil
			},
		}
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		want    string
		want1   string
		wantErr bool
	}{
		{
			name: "success",
			fields: fields{
				ECRAuthClientFactoryFunc: authfunc,
			},
			args: args{
				serverURL: "123456789.dkr.ecr.us-west-2.amazonaws.com",
			},
			want: "foo",
			want1: "bar",
		},
		{
			name: "bad ecr url",
			fields: fields{
				ECRAuthClientFactoryFunc: authfunc,
			},
			args: args{
				serverURL: "quay.io/acme/foobar",
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := RegistryManager{
				ECRAuthClientFactoryFunc: tt.fields.ECRAuthClientFactoryFunc,
			}
			got, got1, err := r.GetDockerAuthConfig(tt.args.serverURL)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetDockerAuthConfig() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("GetDockerAuthConfig() got = %v, want %v", got, tt.want)
			}
			if got1 != tt.want1 {
				t.Errorf("GetDockerAuthConfig() got1 = %v, want %v", got1, tt.want1)
			}
		})
	}
}

func TestRegistryManager_IsECR(t *testing.T) {
	type fields struct {
		AccessKeyID              string
		SecretAccessKey          string
		ECRAuthClientFactoryFunc func(s *session.Session, cfg *aws.Config) ecrapi.Client
		ecrClientFactoryFunc     func(s *session.Session) ecriface.ECRAPI
	}
	type args struct {
		repo string
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   bool
	}{
		{
			name: "ecr repo",
			args: args{repo: "123456789.dkr.ecr.us-west-2.amazonaws.com/widgets"},
			want: true,
		},
		{
			name: "ecr repo with tag",
			args: args{repo: "123456789.dkr.ecr.us-west-2.amazonaws.com/widgets:master"},
			want: true,
		},
		{
			name: "non-ecr repo (quay)",
			args: args{repo: "quay.io/acme/foobar"},
			want: false,
		},
		{
			name: "non-ecr repo (docker hub)",
			args: args{repo: "acme/foobar"},
			want: false,
		},
		{
			name: "non-ecr repo with tag (quay)",
			args: args{repo: "quay.io/acme/foobar:master"},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := RegistryManager{
				AccessKeyID:              tt.fields.AccessKeyID,
				SecretAccessKey:          tt.fields.SecretAccessKey,
				ECRAuthClientFactoryFunc: tt.fields.ECRAuthClientFactoryFunc,
				ECRClientFactoryFunc:     tt.fields.ecrClientFactoryFunc,
			}
			if got := r.IsECR(tt.args.repo); got != tt.want {
				t.Errorf("IsECR() = %v, want %v", got, tt.want)
			}
		})
	}
}

func sliceContentsEqual(s1, s2 []string) bool {
	if len(s1) != len(s2) {
		return false
	}
	m := make(map[string]struct{}, len(s1))
	for i := range s1 {
		m[s1[i]] = struct{}{}
	}
	for _, s := range s2 {
		if _, ok := m[s]; !ok {
			return false
		}
	}
	return true
}

func TestRegistryManager_AllTagsExist(t *testing.T) {
	type fields struct {
		ecrClientFactoryFunc     func(s *session.Session) ecriface.ECRAPI
	}
	type args struct {
		tags []string
		repo string
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		want    bool
		want1   []string
		wantErr bool
	}{
		{
			name: "all exist",
			fields: fields{
				ecrClientFactoryFunc: func(_ *session.Session) ecriface.ECRAPI {
					return &FakeECRClient{
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
			},
			args: args{
				tags: []string{"master", "release", "asdf"},
				repo: "123456789.dkr.ecr.us-west-2.amazonaws.com/widgets",
			},
			want: true,
			want1: []string{},
			wantErr: false,
		},
		{
			name: "missing tag",
			fields: fields{
				ecrClientFactoryFunc: func(_ *session.Session) ecriface.ECRAPI {
					return &FakeECRClient{
						DescribeImagesPagesFunc: func(input *awsecr.DescribeImagesInput, fn func(*awsecr.DescribeImagesOutput, bool) bool) error {
							if len(input.ImageIds) != 1 {
								return fmt.Errorf("expected 1 image id: %v", len(input.ImageIds))
							}
							if *input.ImageIds[0].ImageTag == "release" {
								return awserr.New(awsecr.ErrCodeImageNotFoundException, "some message", fmt.Errorf("some err"))
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
			},
			args: args{
				tags: []string{"master", "release", "asdf"},
				repo: "123456789.dkr.ecr.us-west-2.amazonaws.com/widgets",
			},
			want: false,
			want1: []string{"release"},
			wantErr: false,
		},
		{
			name: "non-ecr repo",
			args: args{
				tags: []string{"master", "release", "asdf"},
				repo: "quay.io/acme/widgets",
			},
			want: false,
			want1: []string{},
			wantErr: true,
		},
		{
			name: "malformed repo",
			args: args{
				tags: []string{"master", "release", "asdf"},
				repo: "somerandomthing",
			},
			want: false,
			want1: []string{},
			wantErr: true,
		},
		{
			name: "repo with tag",
			args: args{
				tags: []string{"master", "release", "asdf"},
				repo: "123456789.dkr.ecr.us-west-2.amazonaws.com/widgets:master",
			},
			want: false,
			want1: []string{},
			wantErr: true,
		},
		{
			name: "aws error",
			fields: fields{
				ecrClientFactoryFunc: func(_ *session.Session) ecriface.ECRAPI {
					return &FakeECRClient{
						DescribeImagesPagesFunc: func(input *awsecr.DescribeImagesInput, fn func(*awsecr.DescribeImagesOutput, bool) bool) error {
							if len(input.ImageIds) != 1 {
								return fmt.Errorf("expected 1 image id: %v", len(input.ImageIds))
							}
							if *input.ImageIds[0].ImageTag == "release" {
								return awserr.New(awsecr.ErrCodeServerException, "some message", fmt.Errorf("some err"))
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
			},
			args: args{
				tags: []string{"master", "release", "asdf"},
				repo: "123456789.dkr.ecr.us-west-2.amazonaws.com/widgets",
			},
			want: false,
			want1: []string{},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := RegistryManager{
				ECRClientFactoryFunc:     tt.fields.ecrClientFactoryFunc,
			}
			got, got1, err := r.AllTagsExist(tt.args.tags, tt.args.repo)
			if (err != nil) != tt.wantErr {
				t.Errorf("AllTagsExist() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("AllTagsExist() got = %v, want %v", got, tt.want)
			}
			if !sliceContentsEqual(got1, tt.want1) {
				t.Errorf("AllTagsExist() got1 = %v, want %v", got1, tt.want1)
			}
		})
	}
}