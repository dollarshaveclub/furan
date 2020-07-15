package github

import (
	"archive/tar"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/google/go-github/github"
	"github.com/mholt/archiver/v3"
	"golang.org/x/oauth2"
)

// CodeFetcher represents an object capable of fetching code and returning a
// gzip-compressed tarball io.Reader
type CodeFetcher interface {
	GetCommitSHA(ctx context.Context, repo, ref string) (string, error)
	Fetch(ctx context.Context, repo, ref, destinationPath string) error
}

// GitHubFetcher represents a github data fetcher
type GitHubFetcher struct {
	c  *github.Client
	hc http.Client
}

var _ CodeFetcher = &GitHubFetcher{}

// NewGitHubFetcher returns a new github fetcher
func NewGitHubFetcher(token string) *GitHubFetcher {
	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
	tc := oauth2.NewClient(context.Background(), ts)
	gf := &GitHubFetcher{
		c: github.NewClient(tc),
	}
	return gf
}

// GetCommitSHA returns the commit SHA for a reference
func (gf *GitHubFetcher) GetCommitSHA(ctx context.Context, repo string, ref string) (csha string, err error) {
	rs := strings.SplitN(repo, "/", 2)
	csha, _, err = gf.c.Repositories.GetCommitSHA1(ctx, rs[0], rs[1], ref, "")
	return csha, err
}

func (gf *GitHubFetcher) Fetch(ctx context.Context, repo, ref, destinationPath string) error {
	rs := strings.SplitN(repo, "/", 2)
	opt := &github.RepositoryContentGetOptions{
		Ref: ref,
	}
	aurl, resp, err := gf.c.Repositories.GetArchiveLink(ctx, rs[0], rs[1], github.Tarball, opt)
	if err != nil {
		return fmt.Errorf("error getting archive link: %v", err)
	}
	if resp.StatusCode > 399 {
		return fmt.Errorf("error status when getting archive link: %v", resp.Status)
	}
	if aurl == nil {
		return fmt.Errorf("url is nil")
	}
	return gf.getAndExtractArchive(aurl, destinationPath)
}

func (gf *GitHubFetcher) getAndExtractArchive(archiveURL *url.URL, destination string) error {
	hr, err := http.NewRequest("GET", archiveURL.String(), nil)
	if err != nil {
		return fmt.Errorf("error creating http request: %v", err)
	}
	resp, err := gf.hc.Do(hr)
	if err != nil {
		return fmt.Errorf("error performing archive http request: %v", err)
	}
	if resp == nil {
		return fmt.Errorf("error getting archive: response is nil")
	}
	if resp.StatusCode > 299 {
		return fmt.Errorf("archive http request failed: %v", resp.StatusCode)
	}
	return gf.extractArchive(resp.Body, destination)
}

func (gf *GitHubFetcher) extractArchive(in io.ReadCloser, dest string) error {
	defer in.Close()
	tgz := archiver.DefaultTarGz
	if err := tgz.Open(in, 0); err != nil {
		return fmt.Errorf("error opening tar stream: %w", err)
	}
	defer tgz.Close()

	for {
		err := untarNext(dest, tgz)
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return fmt.Errorf("error reading file from tar stream: %w", err)
		}
	}
}

// Below is modified copypasta from https://github.com/mholt/archiver/ to allow streaming unarchiving
func untarNext(dest string, tgz *archiver.TarGz) error {
	f, err := tgz.Read()
	if err != nil {
		return err // don't wrap error; calling loop must break on io.EOF
	}
	header, ok := f.Header.(*tar.Header)
	if !ok {
		return fmt.Errorf("expected header to be *tar.Header but was %T", f.Header)
	}
	return untarFile(f, dest, header)
}

func untarFile(f archiver.File, destination string, hdr *tar.Header) error {
	to := filepath.Join(destination, hdr.Name)

	// do not overwrite existing files, if configured
	if !f.IsDir() && fileExists(to) {
		return fmt.Errorf("file already exists: %s", to)
	}

	switch hdr.Typeflag {
	case tar.TypeDir:
		return mkdir(to, f.Mode())
	case tar.TypeReg, tar.TypeRegA, tar.TypeChar, tar.TypeBlock, tar.TypeFifo, tar.TypeGNUSparse:
		return writeNewFile(to, f, f.Mode())
	case tar.TypeSymlink:
		return writeNewSymbolicLink(to, hdr.Linkname)
	case tar.TypeLink:
		return writeNewHardLink(to, filepath.Join(destination, hdr.Linkname))
	case tar.TypeXGlobalHeader:
		return nil // ignore the pax global header from git-generated tarballs
	default:
		return fmt.Errorf("%s: unknown type flag: %c", hdr.Name, hdr.Typeflag)
	}
}

func fileExists(name string) bool {
	_, err := os.Stat(name)
	return !os.IsNotExist(err)
}

func mkdir(dirPath string, dirMode os.FileMode) error {
	err := os.MkdirAll(dirPath, dirMode)
	if err != nil {
		return fmt.Errorf("%s: making directory: %w", dirPath, err)
	}
	return nil
}

func writeNewFile(fpath string, in io.Reader, fm os.FileMode) error {
	err := os.MkdirAll(filepath.Dir(fpath), 0755)
	if err != nil {
		return fmt.Errorf("%s: making directory for file: %w", fpath, err)
	}

	out, err := os.Create(fpath)
	if err != nil {
		return fmt.Errorf("%s: creating new file: %w", fpath, err)
	}
	defer out.Close()

	err = out.Chmod(fm)
	if err != nil && runtime.GOOS != "windows" {
		return fmt.Errorf("%s: changing file mode: %w", fpath, err)
	}

	_, err = io.Copy(out, in)
	if err != nil {
		return fmt.Errorf("%s: writing file: %w", fpath, err)
	}
	return nil
}

func writeNewSymbolicLink(fpath string, target string) error {
	err := os.MkdirAll(filepath.Dir(fpath), 0755)
	if err != nil {
		return fmt.Errorf("%s: making directory for file: %w", fpath, err)
	}

	_, err = os.Lstat(fpath)
	if err == nil {
		err = os.Remove(fpath)
		if err != nil {
			return fmt.Errorf("%s: failed to unlink: %w", fpath, err)
		}
	}

	err = os.Symlink(target, fpath)
	if err != nil {
		return fmt.Errorf("%s: making symbolic link for: %w", fpath, err)
	}
	return nil
}

func writeNewHardLink(fpath string, target string) error {
	err := os.MkdirAll(filepath.Dir(fpath), 0755)
	if err != nil {
		return fmt.Errorf("%s: making directory for file: %w", fpath, err)
	}

	_, err = os.Lstat(fpath)
	if err == nil {
		err = os.Remove(fpath)
		if err != nil {
			return fmt.Errorf("%s: failed to unlink: %w", fpath, err)
		}
	}

	err = os.Link(target, fpath)
	if err != nil {
		return fmt.Errorf("%s: making hard link for: %w", fpath, err)
	}
	return nil
}
