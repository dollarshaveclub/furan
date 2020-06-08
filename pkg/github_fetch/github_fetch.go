package githubfetch

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/docker/docker/builder/dockerignore"
	"github.com/docker/docker/pkg/fileutils"

	"github.com/google/go-github/github"
	"golang.org/x/oauth2"
	"gopkg.in/DataDog/dd-trace-go.v1/ddtrace/tracer"
)

const (
	githubDownloadTimeoutSecs = 300
	dockerIgnorePath          = ".dockerignore"
)

// CodeFetcher represents an object capable of fetching code and returning a
// gzip-compressed tarball io.Reader
type CodeFetcher interface {
	GetCommitSHA(tracer.Span, string, string, string) (string, error)
	Get(tracer.Span, string, string, string) (io.Reader, error)
}

// GitHubFetcher represents a github data fetcher
type GitHubFetcher struct {
	c *github.Client
}

// NewGitHubFetcher returns a new github fetcher
func NewGitHubFetcher(token string) *GitHubFetcher {
	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
	tc := oauth2.NewClient(oauth2.NoContext, ts)
	gf := &GitHubFetcher{
		c: github.NewClient(tc),
	}
	return gf
}

// GetCommitSHA returns the commit SHA for a reference
func (gf *GitHubFetcher) GetCommitSHA(parentSpan tracer.Span, owner string, repo string, ref string) (csha string, err error) {
	span := tracer.StartSpan("github_fetcher.get_commit_sha", tracer.ChildOf(parentSpan.Context()))
	defer func() {
		span.Finish(tracer.WithError(err))
	}()
	ctx, cf := context.WithTimeout(context.Background(), githubDownloadTimeoutSecs*time.Second)
	defer cf()
	csha, _, err = gf.c.Repositories.GetCommitSHA1(ctx, owner, repo, ref, "")
	return csha, err
}

// Get fetches contents of GitHub repo and returns the processed contents as
// an in-memory io.Reader.
func (gf *GitHubFetcher) Get(parentSpan tracer.Span, owner string, repo string, ref string) (tarball io.Reader, err error) {
	span := tracer.StartSpan("github_fetcher.get", tracer.ChildOf(parentSpan.Context()))
	defer func() {
		span.Finish(tracer.WithError(err))
	}()
	opt := &github.RepositoryContentGetOptions{
		Ref: ref,
	}
	ctx, cf := context.WithTimeout(context.Background(), githubDownloadTimeoutSecs*time.Second)
	defer cf()

	excludes, err := gf.parseDockerIgnoreIfExists(ctx, owner, repo, opt)
	if err != nil {
		return nil, fmt.Errorf("error parsing %v file: %v", dockerIgnorePath, err)
	}
	url, resp, err := gf.c.Repositories.GetArchiveLink(ctx, owner, repo, github.Tarball, opt)
	if err != nil {
		return nil, fmt.Errorf("error getting archive link: %v", err)
	}
	if resp.StatusCode > 399 {
		return nil, fmt.Errorf("error status when getting archive link: %v", resp.Status)
	}
	if url == nil {
		return nil, fmt.Errorf("url is nil")
	}
	return gf.getArchive(url, excludes)
}

// parseDockerIgnoreIfExists will parse the docker ignore file if it exists in order to determine which patterns should be excluded.
// The excluded patterns are intended to be used with a pattern matcher.
func (gf *GitHubFetcher) parseDockerIgnoreIfExists(ctx context.Context, owner, repo string, opt *github.RepositoryContentGetOptions) ([]string, error) {
	fc, _, resp, err := gf.c.Repositories.GetContents(ctx, owner, repo, dockerIgnorePath, opt)
	if err != nil {
		if resp.StatusCode == 404 {
			// Not all repos will have dockerignore, just move along
			return []string{}, nil
		}
		return nil, fmt.Errorf("error getting .dockerignore: %v", err)
	}
	content, err := fc.GetContent()
	if err != nil {
		return nil, fmt.Errorf("error getting content from %v, %v", dockerIgnorePath, err)
	}
	excludes, err := dockerignore.ReadAll(strings.NewReader(content))
	if err != nil {
		return nil, fmt.Errorf("error parsing %v, %v", dockerIgnorePath, err)
	}
	return excludes, nil
}

func (gf *GitHubFetcher) getArchive(archiveURL *url.URL, excludes []string) (io.Reader, error) {
	hc := http.Client{
		Timeout: githubDownloadTimeoutSecs * time.Second,
	}
	hr, err := http.NewRequest("GET", archiveURL.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("error creating http request: %v", err)
	}
	resp, err := hc.Do(hr)
	if err != nil {
		return nil, fmt.Errorf("error performing archive http request: %v", err)
	}
	if resp == nil {
		return nil, fmt.Errorf("error getting archive: response is nil")
	}
	if resp.StatusCode > 299 {
		return nil, fmt.Errorf("archive http request failed: %v", resp.StatusCode)
	}
	return newTarPrefixStripper(resp.Body, excludes), nil
}

func (gf *GitHubFetcher) debugWriteTar(contents []byte) {
	f, err := ioutil.TempFile("", "output-tar")
	if err != nil {
		log.Printf("debug: error creating TempFile: %v", err)
	}
	defer f.Close()
	log.Printf("debug: saving tar output to %v", f.Name())
	_, err = f.Write(contents)
	if err != nil {
		log.Printf("debug: error writing tar output: %v", err)
	}
}

// tarPrefixStripper removes a random path that Github prefixes its
// archives with.
type tarPrefixStripper struct {
	tarball          io.ReadCloser
	pipeReader       *io.PipeReader
	pipeWriter       *io.PipeWriter
	strippingStarted bool
	excludes         []string
}

func newTarPrefixStripper(tarball io.ReadCloser, excludes []string) io.Reader {
	reader, writer := io.Pipe()
	return &tarPrefixStripper{
		tarball:    tarball,
		pipeReader: reader,
		pipeWriter: writer,
		excludes:   excludes,
	}
}

func (t *tarPrefixStripper) Read(p []byte) (n int, err error) {
	if !t.strippingStarted {
		go t.startStrippingPipe()
		t.strippingStarted = true
	}
	return t.pipeReader.Read(p)
}

func (t *tarPrefixStripper) shouldSkipDockerIgnoredFile(h *tar.Header) (bool, error) {
	match, err := fileutils.Matches(h.Name, t.excludes)
	if err != nil {
		return false, fmt.Errorf("error matching file name to docker ignored files: %v", h.Name)
	}
	return match, nil
}

func (t *tarPrefixStripper) processHeader(h *tar.Header) (bool, error) {
	// metadata file, ignore
	if h.Name == "pax_global_header" {
		return true, nil
	}
	if path.IsAbs(h.Name) {
		return true, fmt.Errorf("archive contains absolute path: %v", h.Name)
	}

	// top-level directory entry
	spath := strings.Split(h.Name, "/")
	if len(spath) == 2 && spath[1] == "" {
		return true, nil
	}
	h.Name = strings.Join(spath[1:len(spath)], "/")

	return t.shouldSkipDockerIgnoredFile(h)
}

func (t *tarPrefixStripper) startStrippingPipe() {
	gzr, err := gzip.NewReader(t.tarball)
	if err != nil {
		t.pipeWriter.CloseWithError(err)
		return
	}

	tarball := tar.NewReader(gzr)
	outTarball := tar.NewWriter(t.pipeWriter)

	closeFunc := func(e error) {
		outTarball.Close()
		t.pipeWriter.CloseWithError(e)
		t.tarball.Close()
	}

	for {
		header, err := tarball.Next()
		if err == io.EOF {
			closeFunc(nil)
			return
		}
		if err != nil {
			closeFunc(err)
			return
		}

		skip, err := t.processHeader(header)
		if err != nil {
			closeFunc(err)
			return
		}
		if skip {
			continue
		}

		if err := outTarball.WriteHeader(header); err != nil {
			closeFunc(err)
			return
		}
		if _, err := io.Copy(outTarball, tarball); err != nil {
			closeFunc(err)
			return
		}
		if err := outTarball.Flush(); err != nil {
			closeFunc(err)
			return
		}
	}
}
