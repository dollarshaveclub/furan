package builder

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"strings"
	"time"

	"gopkg.in/DataDog/dd-trace-go.v1/ddtrace/tracer"

	pkgerror "github.com/pkg/errors"
	"golang.org/x/net/context"

	dtypes "github.com/docker/engine-api/types"
	"github.com/dollarshaveclub/furan/pkg/buildcontext"
	"github.com/dollarshaveclub/furan/pkg/datalayer"
	"github.com/dollarshaveclub/furan/pkg/ecr"
	"github.com/dollarshaveclub/furan/pkg/errors"
	"github.com/dollarshaveclub/furan/pkg/generated/furanrpc"
	githubfetch "github.com/dollarshaveclub/furan/pkg/github_fetch"
	"github.com/dollarshaveclub/furan/pkg/kafka"
	"github.com/dollarshaveclub/furan/pkg/metrics"
	"github.com/dollarshaveclub/furan/pkg/s3"
	"github.com/dollarshaveclub/furan/pkg/squasher"
	"github.com/dollarshaveclub/furan/pkg/tagcheck"
	"github.com/gocql/gocql"
)

// ErrBuildNotNecessary indicates that the build was skipped due to not being necessary
var ErrBuildNotNecessary = fmt.Errorf("build not necessary: tags or object exist")

//go:generate stringer -type=actionType
type actionType int

// Docker action types
const (
	Build actionType = iota // Build is an image build
	Push                    // Push is a registry push
)

const (
	legalDockerTagChars = "abcdefghijklmnopqrtsuvwxyz-_.ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
)

func buildEventTypeFromActionType(atype actionType) furanrpc.BuildEvent_EventType {
	switch atype {
	case Build:
		return furanrpc.BuildEvent_DOCKER_BUILD_STREAM
	case Push:
		return furanrpc.BuildEvent_DOCKER_PUSH_STREAM
	}
	return -1 // unreachable
}

// RepoBuildData contains data about a GitHub repo necessary to do a Docker build
type RepoBuildData struct {
	DockerfilePath string
	Context        io.Reader
	Tags           []string //{name}:{tag}
}

// ImageBuildClient describes a client that can manipulate images
// Add whatever Docker API methods we care about here
type ImageBuildClient interface {
	ImageBuild(context.Context, io.Reader, dtypes.ImageBuildOptions) (dtypes.ImageBuildResponse, error)
	ImageInspectWithRaw(context.Context, string) (dtypes.ImageInspect, []byte, error)
	ImageRemove(context.Context, string, dtypes.ImageRemoveOptions) ([]dtypes.ImageDelete, error)
	ImagePush(context.Context, string, dtypes.ImagePushOptions) (io.ReadCloser, error)
	ImageSave(context.Context, []string) (io.ReadCloser, error)
	ImageList(context.Context, dtypes.ImageListOptions) ([]dtypes.Image, error)
}

// ImageBuildPusher describes an object that can build and push container images
type ImageBuildPusher interface {
	Build(context.Context, *furanrpc.BuildRequest, gocql.UUID) (string, error)
	CleanImage(context.Context, string) error
	PushBuildToRegistry(context.Context, *furanrpc.BuildRequest) error
	PushBuildToS3(context.Context, string, *furanrpc.BuildRequest) error
}

type S3ErrorLogConfig struct {
	PushToS3          bool
	PresignTTLMinutes uint
	Region            string
	Bucket            string
}

// ImageBuilder is an object that builds and pushes images
type ImageBuilder struct {
	c             ImageBuildClient
	gf            githubfetch.CodeFetcher
	ep            kafka.EventBusProducer
	dl            datalayer.DataLayer
	mc            metrics.MetricsCollector
	is            squasher.ImageSquasher
	osm           s3.ObjectStorageManager
	itc           tagcheck.ImageTagChecker
	rm            ecr.RegistryManager
	useECR        bool
	ecrServerURLs []string
	dockercfg     map[string]dtypes.AuthConfig
	s3errorcfg    S3ErrorLogConfig
	logger        *log.Logger
}

// NewImageBuilder returns a new ImageBuilder
func NewImageBuilder(eventbus kafka.EventBusProducer, datalayer datalayer.DataLayer, gf githubfetch.CodeFetcher, dc ImageBuildClient, mc metrics.MetricsCollector, osm s3.ObjectStorageManager, is squasher.ImageSquasher, itc tagcheck.ImageTagChecker, dcfg map[string]dtypes.AuthConfig, s3errorcfg S3ErrorLogConfig, logger *log.Logger) (*ImageBuilder, error) {
	return &ImageBuilder{
		gf:         gf,
		c:          dc,
		ep:         eventbus,
		dl:         datalayer,
		mc:         mc,
		osm:        osm,
		itc:        itc,
		is:         is,
		dockercfg:  dcfg,
		s3errorcfg: s3errorcfg,
		logger:     logger,
	}, nil
}

// SetECRConfig sets the AWS credentials for optional ECR support and the list of ECR server hosts to authorize for base images
func (ib *ImageBuilder) SetECRConfig(accessKeyID, secretAccessKeyID string, ecrServerHosts []string) {
	ib.useECR = true
	ib.ecrServerURLs = []string{}
	if len(ecrServerHosts) > 0 {
		ib.ecrServerURLs = ecrServerHosts
	}
	ib.rm.AccessKeyID = accessKeyID
	ib.rm.SecretAccessKey = secretAccessKeyID
}

func (ib *ImageBuilder) addECRtoAuth(repo string) map[string]dtypes.AuthConfig {
	if ib.useECR {
		dcfg := ib.dockercfg
		for _, su := range ib.ecrServerURLs {
			username, pwd, err := ib.rm.GetDockerAuthConfig(su)
			if err != nil {
				// log error and skip
				ib.logger.Printf("addECRtoAuth: error getting docker auth for ecrServerURL: %v: %v", su, err)
				continue
			}
			dcfg[su] = dtypes.AuthConfig{Username: username, Password: pwd}
		}
		if ib.rm.IsECR(repo) {
			surl := strings.Split(repo, "/")[0]
			username, pwd, err := ib.rm.GetDockerAuthConfig(surl)
			if err != nil {
				// if we can't get the auth just return the existing docker auths and let it fail later
				ib.logger.Printf("addECRtoAuth: error getting docker auth for %v: %v", repo, err)
				return dcfg
			}
			dcfg[surl] = dtypes.AuthConfig{Username: username, Password: pwd}
		}
		return dcfg
	}
	return ib.dockercfg
}

func (ib *ImageBuilder) logf(ctx context.Context, msg string, params ...interface{}) {
	id, ok := buildcontext.BuildIDFromContext(ctx)
	if !ok {
		ib.logger.Printf("build id missing from context")
		return
	}
	msg = fmt.Sprintf("%v: %v", id.String(), msg)
	ib.logger.Printf(msg, params...)
	go func() {
		event, err := ib.getBuildEvent(ctx, furanrpc.BuildEvent_LOG, furanrpc.BuildEventError_NO_ERROR, fmt.Sprintf(msg, params...), false)
		if err != nil {
			ib.logger.Printf("error building event object: %v", err)
			return
		}
		err = ib.ep.PublishEvent(event)
		if err != nil {
			ib.logger.Printf("error pushing event to bus: %v", err)
		}
	}()
}

func (ib *ImageBuilder) getBuildEvent(ctx context.Context, etype furanrpc.BuildEvent_EventType, errtype furanrpc.BuildEventError_ErrorType, msg string, finished bool) (*furanrpc.BuildEvent, error) {
	var event *furanrpc.BuildEvent
	id, ok := buildcontext.BuildIDFromContext(ctx)
	if !ok {
		return event, fmt.Errorf("build id missing from context")
	}
	event = &furanrpc.BuildEvent{
		EventError: &furanrpc.BuildEventError{
			ErrorType: errtype,
			IsError:   errtype != furanrpc.BuildEventError_NO_ERROR,
		},
		BuildId:       id.String(),
		EventType:     etype,
		BuildFinished: finished,
		Message:       msg,
	}
	return event, nil
}

func (ib *ImageBuilder) event(bevent *furanrpc.BuildEvent) {
	ib.logger.Printf("event: %v", bevent.Message)
	go func() {
		err := ib.ep.PublishEvent(bevent)
		if err != nil {
			ib.logger.Printf("error pushing event to bus: %v", err)
		}
	}()
}

func (ib ImageBuilder) filterTagName(tag string) string {
	mf := func(r rune) rune {
		if strings.Contains(legalDockerTagChars, string(r)) {
			return r
		}
		return rune(-1)
	}
	return strings.Map(mf, tag)
}

func (ib ImageBuilder) validateTag(tag string) bool {
	switch {
	case strings.HasPrefix(tag, "."):
		return false
	case strings.HasPrefix(tag, "-"):
		return false
	case len(tag) > 128:
		return false
	}
	return true
}

// Returns full docker name:tag strings from the supplied repo/tags
func (ib *ImageBuilder) getFullImageNames(ctx context.Context, req *furanrpc.BuildRequest) ([]string, error) {
	var bname string
	names := []string{}
	if req.Push.Registry.Repo != "" {
		bname = req.Push.Registry.Repo
	} else {
		bname = req.Build.GithubRepo
	}
	for _, t := range req.Build.Tags {
		ft := ib.filterTagName(t)
		if ft != t {
			// if any illegal chars were filtered out, the image will be tagged differently from
			// what the user expects, so return error instead
			return names, fmt.Errorf("tag contains illegal characters: %v", t)
		}
		if !ib.validateTag(ft) {
			return names, fmt.Errorf("invalid tag: %v", ft)
		}
		names = append(names, fmt.Sprintf("%v:%v", bname, ft))
	}
	if req.Build.TagWithCommitSha {
		csha, err := ib.getCommitSHA(ctx, req.Build.GithubRepo, req.Build.Ref)
		if err != nil {
			return names, fmt.Errorf("error getting commit sha: %v", err)
		}
		if !ib.validateTag(csha) {
			return names, fmt.Errorf("invalid commit sha tag: %v", csha)
		}
		names = append(names, fmt.Sprintf("%v:%v", bname, csha))
	}
	return names, nil
}

// tagCheck checks the existence of tags in the registry or the S3 object
// returns true if build/push should be performed
func (ib *ImageBuilder) tagCheck(ctx context.Context, req *furanrpc.BuildRequest) (bool, error) {
	var err error
	if !req.SkipIfExists {
		return true, nil
	}
	tagCheckSpan, ctx := tracer.StartSpanFromContext(ctx, "image_builder.tag_check")
	defer func() {
		tagCheckSpan.Finish(tracer.WithError(err))
	}()
	csha, err := ib.getCommitSHA(ctx, req.Build.GithubRepo, req.Build.Ref)
	if err != nil {
		return false, fmt.Errorf("error getting latest commit SHA: %v", err)
	}
	s3p := req.Push.Registry.Repo == ""
	if s3p {
		desc := s3.ImageDescription{
			GitHubRepo: req.Build.GithubRepo,
			CommitSHA:  csha,
		}
		opts := s3.S3Options{
			Region:    req.Push.S3.Region,
			Bucket:    req.Push.S3.Bucket,
			KeyPrefix: req.Push.S3.KeyPrefix,
		}
		ok, err := ib.osm.Exists(desc, &opts)
		if err != nil {
			return false, fmt.Errorf("error checking if S3 object exists: %v", err)
		}
		return !ok, nil
	}
	tags := make([]string, len(req.Build.Tags))
	copy(tags, req.Build.Tags)
	if req.Build.TagWithCommitSha {
		tags = append(tags, csha)
	}
	var ok bool
	if ib.useECR && ib.rm.IsECR(req.Push.Registry.Repo) {
		ok, _, err = ib.rm.AllTagsExist(tags, req.Push.Registry.Repo)
	} else {
		ok, _, err = ib.itc.AllTagsExist(tags, req.Push.Registry.Repo)
	}
	if err != nil {
		return false, fmt.Errorf("error checking if tags exist: %v", err)
	}
	return !ok, nil
}

// Build builds an image accourding to the request
func (ib *ImageBuilder) Build(ctx context.Context, req *furanrpc.BuildRequest, id gocql.UUID) (_ string, err error) {
	ib.logf(ctx, "starting build")
	parentSpan, _ := tracer.SpanFromContext(ctx)
	err = ib.dl.SetBuildTimeMetric(ctx, id, "docker_build_started")
	if err != nil {
		return "", fmt.Errorf("error setting build time metric in DB: %v", err)
	}
	rl := strings.Split(req.Build.GithubRepo, "/")
	if len(rl) != 2 {
		return "", fmt.Errorf("malformed github repo: %v", req.Build.GithubRepo)
	}
	ok, err := ib.tagCheck(ctx, req)
	if err != nil {
		return "", fmt.Errorf("error checking if tags/object exist: %v", err)
	}
	if !ok {
		return "", ErrBuildNotNecessary
	}
	owner := rl[0]
	repo := rl[1]
	if buildcontext.IsCancelled(ctx.Done()) {
		return "", errors.UserError(fmt.Sprintf("build was cancelled: %v", ctx.Err()))
	}
	ib.logf(ctx, "fetching github repo: %v", req.Build.GithubRepo)
	contents, err := ib.gf.Get(parentSpan, owner, repo, req.Build.Ref)
	if err != nil {
		return "", fmt.Errorf("error fetching repo: %v", err)
	}
	var dp string
	if req.Build.DockerfilePath == "" {
		dp = "Dockerfile"
	} else {
		dp = req.Build.DockerfilePath
	}
	inames, err := ib.getFullImageNames(ctx, req)
	if err != nil {
		return "", fmt.Errorf("error getting full image names: %v", err)
	}
	ib.logf(ctx, "tags: %v", inames)
	rbi := &RepoBuildData{
		DockerfilePath: dp,
		Context:        contents,
		Tags:           inames,
	}
	return ib.dobuild(ctx, req, rbi)
}

func (ib *ImageBuilder) saveOutput(ctx context.Context, action actionType, events []furanrpc.BuildEvent) error {
	if buildcontext.IsCancelled(ctx.Done()) {
		return errors.UserError(fmt.Sprintf("build was cancelled: %v", ctx.Err()))
	}
	id, ok := buildcontext.BuildIDFromContext(ctx)
	if !ok {
		return fmt.Errorf("build id missing from context")
	}
	var column string
	switch action {
	case Build:
		column = "build_output"
	case Push:
		column = "push_output"
	default:
		return fmt.Errorf("unknown action: %v", action)
	}
	return ib.dl.SaveBuildOutput(ctx, id, events, column)
}

// saveEventLogToS3 writes a stream of events to S3 and returns the S3 HTTP URL
func (ib *ImageBuilder) saveEventLogToS3(ctx context.Context, repo string, ref string, action actionType, events []furanrpc.BuildEvent) (string, error) {
	id, ok := buildcontext.BuildIDFromContext(ctx)
	if !ok {
		return "", fmt.Errorf("build id missing from context")
	}
	csha, err := ib.getCommitSHA(ctx, repo, ref)
	if err != nil {
		return "", err
	}
	idesc := s3.ImageDescription{
		GitHubRepo: repo,
		CommitSHA:  csha,
	}
	b := bytes.NewBuffer([]byte{})
	for _, e := range events {
		b.Write([]byte(e.Message + "\n"))
	}
	now := time.Now().UTC()
	s3opts := &s3.S3Options{
		Region:            ib.s3errorcfg.Region,
		Bucket:            ib.s3errorcfg.Bucket,
		KeyPrefix:         now.Round(time.Hour).Format(time.RFC3339) + "/",
		PresignTTLMinutes: ib.s3errorcfg.PresignTTLMinutes,
	}
	key := fmt.Sprintf("%v-%v-error.txt", id.String(), action.String())
	return ib.osm.WriteFile(key, idesc, "text/plain", b, s3opts)
}

const (
	buildSuccessEventPrefix        = "Successfully built "
	malformedDockerfileEventPrefix = "Error response from daemon: Dockerfile parse error"
)

// doBuild executes the archive file GET and triggers the Docker build
func (ib *ImageBuilder) dobuild(ctx context.Context, req *furanrpc.BuildRequest, rbi *RepoBuildData) (_ string, err error) {
	var imageid string
	id, ok := buildcontext.BuildIDFromContext(ctx)
	if !ok {
		return imageid, fmt.Errorf("build id missing from context")
	}
	opts := dtypes.ImageBuildOptions{
		Tags:        rbi.Tags,
		Remove:      true,
		ForceRemove: true,
		PullParent:  true,
		Dockerfile:  rbi.DockerfilePath,
		// if req.Push.Registry.Repo is empty this will return static auth configs only (with ECR if configured)
		AuthConfigs: ib.addECRtoAuth(req.GetPush().GetRegistry().GetRepo()),
		NoCache:     true,
		BuildArgs:   req.Build.Args,
	}
	buildSpan, ctx := tracer.StartSpanFromContext(ctx, "image_builder.dobuild")
	defer func() {
		buildSpan.Finish(tracer.WithError(err))
	}()
	ibr, err := ib.c.ImageBuild(ctx, rbi.Context, opts)
	if err != nil {
		if buildcontext.IsCancelled(ctx.Done()) {
			return imageid, errors.UserError(fmt.Sprintf("build was cancelled: %v", ctx.Err()))
		}
		dockerBuildStartError := pkgerror.Wrapf(err, "error starting build: ")
		if strings.HasPrefix(err.Error(), malformedDockerfileEventPrefix) {
			dockerBuildStartError = errors.UserError(fmt.Sprintf("malformed dockerfile: %v", err))
		}
		return imageid, dockerBuildStartError
	}
	output, err := ib.monitorDockerAction(ctx, ibr.Body, Build)
	err2 := ib.saveOutput(ctx, Build, output) // we want to save output even if error
	if err != nil {
		var s3LogURL string
		var saveLogError error
		if buildcontext.IsCancelled(ctx.Done()) {
			return imageid, errors.UserError(fmt.Sprintf("build was cancelled: %v", ctx.Err()))
		}
		le := output[len(output)-1]
		dockerBuildError := fmt.Errorf("build failed: %v, (%v) ", err, le.Message)
		if le.EventError.ErrorType == furanrpc.BuildEventError_FATAL && ib.s3errorcfg.PushToS3 {
			ib.logf(ctx, "pushing failed build log to S3: %v", id.String())
			s3LogURL, saveLogError = ib.saveEventLogToS3(ctx, req.Build.GithubRepo, req.Build.Ref, Build, output)
			if saveLogError != nil {
				ib.logf(ctx, "error saving build events to S3: %v", saveLogError)
			}
			dockerBuildError = fmt.Errorf("build failed: %v, link to logs: %v", err, s3LogURL)
			ib.logf(ctx, "build failed: log saved to: %v", s3LogURL)
		}
		var errorEvent dockerStreamEvent
		if err := json.Unmarshal([]byte(le.Message), &errorEvent); err != nil {
			ib.logger.Printf("error unmarshaling final build event: %v", err)
		}
		if errorEvent.ErrorDetail.Code != 0 {
			dockerBuildError = errors.UserError(fmt.Sprintf("last event message: %v, link to logs: %v", le.Message, s3LogURL))
		}
		return imageid, dockerBuildError
	}
	if err2 != nil {
		return imageid, fmt.Errorf("error saving action output: %v", err2)
	}
	walkback := len(rbi.Tags) + 1 // newer Docker engine versions have a message for each tag after the success message
	var dse dockerStreamEvent
	for i := 1; i <= walkback; i++ { // Parse final stream event to find image ID
		fes := output[len(output)-i].Message
		err = json.Unmarshal([]byte(fes), &dse)
		if err != nil {
			return imageid, fmt.Errorf("error marshaling final message to determine image id: %v", err)
		}
		if strings.HasPrefix(dse.Stream, buildSuccessEventPrefix) {
			imageid = strings.TrimRight(dse.Stream[len(buildSuccessEventPrefix):len(dse.Stream)], "\n")
			ib.logf(ctx, "built image ID %v", imageid)
			err = ib.dl.SetBuildTimeMetric(ctx, id, "docker_build_completed")
			if err != nil {
				return imageid, err
			}
			return imageid, ib.writeDockerImageSizeMetrics(ctx, imageid, req.Build.GithubRepo, req.Build.Ref)
		}
	}
	return imageid, fmt.Errorf("could not determine image id from final event: %v", dse.Stream)
}

func (ib *ImageBuilder) writeDockerImageSizeMetrics(ctx context.Context, imageid string, repo string, ref string) error {
	if buildcontext.IsCancelled(ctx.Done()) {
		return errors.UserError(fmt.Sprintf("build was cancelled: %v", ctx.Err()))
	}
	id, ok := buildcontext.BuildIDFromContext(ctx)
	if !ok {
		return fmt.Errorf("build id missing from context")
	}
	res, _, err := ib.c.ImageInspectWithRaw(ctx, imageid)
	if err != nil {
		return err
	}
	err = ib.mc.ImageSize(res.Size, res.VirtualSize, repo, ref)
	if err != nil {
		ib.logger.Printf("error pushing image size metrics: %v", err)
	}
	return ib.dl.SetDockerImageSizesMetric(ctx, id, res.Size, res.VirtualSize)
}

// Models for the JSON objects the Docker API returns
// This is a combination of all fields we may be interested in
// Each Docker API endpoint returns a different response schema :-\
type dockerStreamEvent struct {
	Stream         string                 `json:"stream,omitempty"`
	Status         string                 `json:"status,omitempty"`
	ProgressDetail map[string]interface{} `json:"progressDetail,omitempty"`
	Progress       string                 `json:"progress,omitempty"`
	ID             string                 `json:"id,omitempty"`
	Aux            map[string]interface{} `json:"aux,omitempty"`
	Error          string                 `json:"error,omitempty"`
	ErrorDetail    dockerErrorDetail      `json:"errorDetail,omitempty"`
}

type dockerErrorDetail struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// monitorDockerAction reads the Docker API response stream
func (ib *ImageBuilder) monitorDockerAction(ctx context.Context, rc io.ReadCloser, atype actionType) ([]furanrpc.BuildEvent, error) {
	rdr := bufio.NewReader(rc)
	output := []furanrpc.BuildEvent{}
	var bevent *furanrpc.BuildEvent
	for {
		line, err := rdr.ReadBytes('\n')
		if buildcontext.IsCancelled(ctx.Done()) {
			return output, errors.UserError(fmt.Sprintf("action was cancelled: %v", ctx.Err()))
		}
		if err != nil {
			if err == io.EOF {
				return output, nil
			}
			return output, fmt.Errorf("error reading event stream: %v", err)
		}
		var event dockerStreamEvent
		err = json.Unmarshal(line, &event)
		if err != nil {
			return output, fmt.Errorf("error unmarshaling event: %v (event: %v)", err, string(line))
		}
		var errtype furanrpc.BuildEventError_ErrorType
		if event.Error != "" {
			errtype = furanrpc.BuildEventError_FATAL
		} else {
			errtype = furanrpc.BuildEventError_NO_ERROR
		}
		bevent, err = ib.getBuildEvent(ctx, buildEventTypeFromActionType(atype), errtype, string(line), false)
		if err != nil {
			return output, err
		}
		output = append(output, *bevent)
		if errtype == furanrpc.BuildEventError_FATAL {
			// do not push final event (leave to upstream error handler)
			return output, fmt.Errorf("fatal error performing %v action: %v", atype.String(), string(line))
		}
		ib.event(bevent)
	}
}

// CleanImage cleans up the built image after it's been pushed
func (ib *ImageBuilder) CleanImage(ctx context.Context, imageid string) (err error) {
	if buildcontext.IsCancelled(ctx.Done()) {
		return errors.UserError(fmt.Sprintf("clean was cancelled: %v", ctx.Err()))
	}
	id, ok := buildcontext.BuildIDFromContext(ctx)
	if !ok {
		return fmt.Errorf("build id missing from context")
	}
	cleanSpan, ctx := tracer.StartSpanFromContext(ctx, "image_builder.clean")
	defer func() {
		cleanSpan.Finish(tracer.WithError(err))
	}()
	ib.logf(ctx, "cleaning up images")
	err = ib.dl.SetBuildTimeMetric(ctx, id, "clean_started")
	if err != nil {
		return err
	}
	opts := dtypes.ImageRemoveOptions{
		Force:         true,
		PruneChildren: true,
	}
	resp, err := ib.c.ImageRemove(ctx, imageid, opts)
	for _, r := range resp {
		ib.logf(ctx, "ImageDelete: %v", r)
	}
	if err != nil {
		return err
	}
	return ib.dl.SetBuildTimeMetric(ctx, id, "clean_completed")
}

// PushBuildToRegistry pushes the already built image and all associated tags to the
// configured remote Docker registry. Caller must ensure the image has already
// been built successfully
func (ib *ImageBuilder) PushBuildToRegistry(ctx context.Context, req *furanrpc.BuildRequest) (err error) {
	if buildcontext.IsCancelled(ctx.Done()) {
		return errors.UserError(fmt.Sprintf("push was cancelled: %v", ctx.Err()))
	}
	id, ok := buildcontext.BuildIDFromContext(ctx)
	if !ok {
		return fmt.Errorf("build id missing from context")
	}
	ib.logf(ctx, "pushing")
	err = ib.dl.SetBuildTimeMetric(ctx, id, "push_started")
	if err != nil {
		return err
	}
	repo := req.Push.Registry.Repo
	if repo == "" {
		return fmt.Errorf("PushBuildToRegistry called but repo is empty")
	}
	rsl := strings.Split(repo, "/")
	var registry string
	dcfg := ib.dockercfg
	switch {
	case ib.useECR && ib.rm.IsECR(repo):
		// ECR repo format: "123456789.dkr.ecr.us-west-2.amazonaws.com/widgets" (one slash, ECR hostname)
		registry = rsl[0]
		dcfg = ib.addECRtoAuth(repo)
	case len(rsl) == 2:
		// Docker Hub repo format: "acme/widgets" (one slash, org name [no host])
		registry = "https://index.docker.io/v2/"
	case len(rsl) == 3:
		// Private registry (quay) format: "quay.io/acme/widgets" (two slashes, hostname and org)
		registry = rsl[0]
	default:
		return fmt.Errorf("cannot determine base registry URL from %v", repo)
	}
	var auth string
	if val, ok := dcfg[registry]; ok {
		j, err := json.Marshal(&val)
		if err != nil {
			return fmt.Errorf("error marshaling auth: %v", err)
		}
		auth = base64.StdEncoding.EncodeToString(j)
	} else {
		return fmt.Errorf("auth not found in dockercfg for %v", registry)
	}
	opts := dtypes.ImagePushOptions{
		All:          true,
		RegistryAuth: auth,
	}
	var output []furanrpc.BuildEvent
	inames, err := ib.getFullImageNames(ctx, req)
	if err != nil {
		return err
	}
	pushSpan, ctx := tracer.StartSpanFromContext(ctx, "image_builder.push")
	defer func() {
		if err != nil {
			pushSpan.Finish(tracer.WithError(err))
		}
	}()
	for _, name := range inames {
		ipr, err := ib.c.ImagePush(ctx, name, opts)
		if buildcontext.IsCancelled(ctx.Done()) {
			return errors.UserError(fmt.Sprintf("push was cancelled: %v", ctx.Err()))
		}
		if err != nil {
			return fmt.Errorf("error initiating registry push: %v", err)
		}
		o, err := ib.monitorDockerAction(ctx, ipr, Push)
		output = append(output, o...)
		if err != nil {
			return err
		}
	}
	pushSpan.Finish()
	err = ib.dl.SetBuildTimeMetric(ctx, id, "push_completed")
	if err != nil {
		return err
	}
	ib.logf(ctx, "push completed")
	return ib.saveOutput(ctx, Push, output)
}

func (ib *ImageBuilder) getCommitSHA(ctx context.Context, repo, ref string) (string, error) {
	rl := strings.Split(repo, "/")
	if len(rl) != 2 {
		return "", fmt.Errorf("malformed GitHub repo: %v", repo)
	}
	parentSpan, _ := tracer.SpanFromContext(ctx)
	csha, err := ib.gf.GetCommitSHA(parentSpan, rl[0], rl[1], ref)
	if err != nil {
		return "", fmt.Errorf("error getting commit SHA: %v", err)
	}
	return csha, nil
}

// PushBuildToS3 exports and uploads the already built image to the configured S3 bucket/key
func (ib *ImageBuilder) PushBuildToS3(ctx context.Context, imageid string, req *furanrpc.BuildRequest) (err error) {
	if buildcontext.IsCancelled(ctx.Done()) {
		return errors.UserError(fmt.Sprintf("push was cancelled: %v", ctx.Err()))
	}
	csha, err := ib.getCommitSHA(ctx, req.Build.GithubRepo, req.Build.Ref)
	if err != nil {
		return err
	}
	pushSpan, _ := tracer.StartSpanFromContext(ctx, "image_builder.push")
	defer func() {
		pushSpan.Finish(tracer.WithError(err))
	}()
	info, _, err := ib.c.ImageInspectWithRaw(ctx, imageid)
	if err != nil {
		return err
	}
	if len(info.RepoTags) == 0 {
		return fmt.Errorf("no tags found for image")
	}
	r, err := ib.c.ImageSave(ctx, info.RepoTags)
	if err != nil {
		return fmt.Errorf("error saving image: %v", err)
	}
	defer r.Close()
	ib.logf(ctx, "squashing and pushing to S3: %v: %v/%v%v/%v.tar.gz", req.Push.S3.Region, req.Push.S3.Bucket, req.Push.S3.KeyPrefix, req.Build.GithubRepo, csha)
	done := make(chan error)
	pr, pw := io.Pipe()
	go func() {
		var err error
		var si *squasher.SquashInfo
		defer pw.CloseWithError(err)
		si, err = ib.is.Squash(ctx, r, pw)
		if err != nil {
			done <- fmt.Errorf("error squashing image: %v", err)
			return
		}
		ib.mc.Size("image.squashed.size_difference_bytes", req.Build.GithubRepo, req.Build.Ref, nil, si.SizeDifference)
		ib.mc.Float("image.squashed.size_difference_pct", req.Build.GithubRepo, req.Build.Ref, nil, si.SizePctDifference)
		ib.mc.Size("image.squashed.files_removed", req.Build.GithubRepo, req.Build.Ref, nil, int64(si.FilesRemovedCount))
		ib.mc.Size("image.squashed.layers_removed", req.Build.GithubRepo, req.Build.Ref, nil, int64(si.LayersRemoved))
		done <- nil
	}()
	go func() {
		idesc := s3.ImageDescription{
			GitHubRepo: req.Build.GithubRepo,
			CommitSHA:  csha,
		}
		opts := &s3.S3Options{
			Region:    req.Push.S3.Region,
			Bucket:    req.Push.S3.Bucket,
			KeyPrefix: req.Push.S3.KeyPrefix,
		}
		done <- ib.osm.Push(idesc, pr, opts)
	}()
	var failed bool
	errstrs := []string{}
	for i := 0; i < 2; i++ {
		err = <-done
		if err != nil {
			failed = true
			errstrs = append(errstrs, err.Error())
		}
	}
	if failed {
		return fmt.Errorf("squash/push failed: %v", strings.Join(errstrs, ", "))
	}
	return nil
}
