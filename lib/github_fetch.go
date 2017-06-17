package lib

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"time"

	"github.com/google/go-github/github"
	"golang.org/x/oauth2"
)

const (
	githubDownloadTimeoutSecs = 300
)

// CodeFetcher represents an object capable of fetching code and returning a
// gzip-compressed tarball io.Reader
type CodeFetcher interface {
	GetCommitSHA(string, string, string) (string, error)
	Get(string, string, string) (io.ReadCloser, error)
}

// GitHubFetcher represents a github data fetcher
type GitHubFetcher struct {
	c            *github.Client
	diskCacheDir string
}

// NewGitHubFetcher returns a new github fetcher
func NewGitHubFetcher(token, diskCacheDir string) *GitHubFetcher {
	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
	tc := oauth2.NewClient(oauth2.NoContext, ts)
	gf := &GitHubFetcher{
		c:            github.NewClient(tc),
		diskCacheDir: diskCacheDir,
	}
	return gf
}

// GetCommitSHA returns the commit SHA for a reference
func (gf *GitHubFetcher) GetCommitSHA(owner string, repo string, ref string) (string, error) {
	csha, _, err := gf.c.Repositories.GetCommitSHA1(owner, repo, ref, "")
	return csha, err
}

// Get fetches contents of GitHub repo and returns the processed contents as
// an in-memory io.Reader.
func (gf *GitHubFetcher) Get(owner string, repo string, ref string) (tarball io.ReadCloser, err error) {
	opt := &github.RepositoryContentGetOptions{
		Ref: ref,
	}
	url, _, err := gf.c.Repositories.GetArchiveLink(owner, repo, github.Tarball, opt)
	if err != nil {
		return nil, err
	}
	return gf.getArchive(url)
}

func (gf *GitHubFetcher) getArchive(archiveURL *url.URL) (io.ReadCloser, error) {
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
	if resp.StatusCode > 299 {
		return nil, fmt.Errorf("archive http request failed: %v", resp.StatusCode)
	}
	return gf.stripTarPrefix(resp.Body)
}

func (gf *GitHubFetcher) debugWriteTar(contents []byte) {
	f, err := ioutil.TempFile("", "output-tar")
	defer f.Close()
	log.Printf("debug: saving tar output to %v", f.Name())
	_, err = f.Write(contents)
	if err != nil {
		log.Printf("debug: error writing tar output: %v", err)
	}
}

// Files within GitHub archives are prefixed with a random path, so we have to
// strip that prefix to get an archive suitable for a Docker build context.
// Note that we do not compress the output tar archive since we assume callers
// will be passing it to the Docker engine running on localhost
func (gf *GitHubFetcher) stripTarPrefix(input io.ReadCloser) (io.ReadCloser, error) {
	defer input.Close()
	gzr, err := gzip.NewReader(input)
	if err != nil {
		return nil, fmt.Errorf("error creating gzip reader: %v", err)
	}
	defer gzr.Close()
	output, err := newTempTarball(gf.diskCacheDir)
	if err != nil {
		return nil, err
	}
	intar := tar.NewReader(gzr)
	outtar := tar.NewWriter(output)
	defer outtar.Close()
	var contents []byte
	for {
		h, err := intar.Next()
		if err != nil {
			if err == io.EOF {
				// Close the tarball so that we can
				// complete writing to the file.
				outtar.Close()

				// Rewind the file so it can now be
				// read.
				if _, err := output.Seek(0, 0); err != nil {
					return nil, err
				}
				return output, nil
			}
			return nil, fmt.Errorf("error reading input tar entry: %v", err)
		}
		if h.Name == "pax_global_header" { // metadata file, ignore
			continue
		}
		if path.IsAbs(h.Name) {
			return nil, fmt.Errorf("archive contains absolute path: %v", h.Name)
		}
		spath := strings.Split(h.Name, "/")
		if len(spath) == 2 && spath[1] == "" { // top-level directory entry
			continue
		}
		h.Name = strings.Join(spath[1:len(spath)], "/")
		err = outtar.WriteHeader(h)
		if err != nil {
			return nil, fmt.Errorf("error writing output tar header: %v", err)
		}
		contents, err = ioutil.ReadAll(intar)
		if err != nil {
			return nil, fmt.Errorf("error reading input tar contents: %v", err)
		}
		if int64(len(contents)) != h.Size {
			log.Printf("%v: size mismatch: read %v (size: %v)", h.Name, len(contents), h.Size)
		}
		_, err = outtar.Write(contents)
		if err != nil {
			return nil, fmt.Errorf("error writing output tar contents: %v", err)
		}
	}
}

// tempTarball is a io.ReadCloser that's backed by disk. Upon closing,
// the underlying file is deleted.
type tempTarball struct {
	*os.File
}

func newTempTarball(diskCacheDir string) (*tempTarball, error) {
	f, err := ioutil.TempFile(diskCacheDir, "furan-tarball")
	if err != nil {
		return nil, err
	}
	return &tempTarball{File: f}, nil
}

func (t *tempTarball) Close() error {
	if err := t.File.Close(); err != nil {
		return err
	}
	return os.Remove(t.Name())

}
