package githubfetch

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io/ioutil"
	"testing"
)

func TestGitHubFetchTarPrefixStripper(t *testing.T) {
	tarballBuf := &bytes.Buffer{}
	gzipWriter := gzip.NewWriter(tarballBuf)
	tarWriter := tar.NewWriter(gzipWriter)

	if err := tarWriter.WriteHeader(&tar.Header{
		Name: "prefix/file1",
		Size: 9,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := tarWriter.Write([]byte("contents1")); err != nil {
		t.Fatal(err)
	}

	if err := tarWriter.WriteHeader(&tar.Header{
		Name: "prefix/file2",
		Size: 9,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := tarWriter.Write([]byte("contents2")); err != nil {
		t.Fatal(err)
	}

	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}

	reader := newTarPrefixStripper(ioutil.NopCloser(tarballBuf), []string{})
	tarReader := tar.NewReader(reader)

	header1, err := tarReader.Next()
	if err != nil {
		t.Fatal(err)
	}
	if header1.Name != "file1" {
		t.Fatalf("invalid file name found in tar header")
	}
	contents1, err := ioutil.ReadAll(tarReader)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents1) != "contents1" {
		t.Fatalf("invalid file contents")
	}

	header2, err := tarReader.Next()
	if err != nil {
		t.Fatal(err)
	}
	if header2.Name != "file2" {
		t.Fatalf("invalid file name found in tar header")
	}
	contents2, err := ioutil.ReadAll(tarReader)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents2) != "contents2" {
		t.Fatalf("invalid file contents")
	}
}
