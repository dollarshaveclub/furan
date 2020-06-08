package squasher

import (
	"archive/tar"
	"context"
	"fmt"
	"io"
	"io/ioutil"

	docker "github.com/docker/engine-api/client"
)

func deserializedtardata() []TarEntry {
	te1 := TarEntry{
		Header: &tar.Header{Name: "a"},
	}
	te2 := TarEntry{
		Header: &tar.Header{Name: "b"},
	}
	te3 := TarEntry{
		Header: &tar.Header{Name: "c"},
	}
	return []TarEntry{te1, te2, te3}
}

// func TestSquasherDeserializedTarAppend(t *testing.T) {
// 	d := NewDeserializedTar()
// 	for _, te := range deserializedtardata() {
// 		nte := te
// 		err := d.Append(&nte)
// 		if err != nil {
// 			t.Fatalf("error appending %v: %v", te.Header.Name, err)
// 		}
// 	}
// 	if len(d.Content) != 3 {
// 		t.Fatalf("bad length for content: %v", len(d.Content))
// 	}
// 	if len(d.Order) != 3 {
// 		t.Fatalf("bad length for order: %v", len(d.Order))
// 	}
// 	if d.Content["b"].Header.Name != "b" {
// 		t.Fatalf("bad name for b: %v (expected b)", d.Content["b"].Header.Name)
// 	}
// 	if d.Content["b"].Index != uint64(1) {
// 		t.Fatalf("bad index for b: %v (expected 1)", d.Content["b"].Index)
// 	}
// }
//
// func TestSquasherDeserializedTarRemove(t *testing.T) {
// 	d := NewDeserializedTar()
// 	for _, te := range deserializedtardata() {
// 		nte := te
// 		err := d.Append(&nte)
// 		if err != nil {
// 			t.Fatalf("error appending %v: %v", te.Header.Name, err)
// 		}
// 	}
// 	err := d.Remove("b")
// 	if err != nil {
// 		t.Fatalf("error removing item: %v", err)
// 	}
// 	err = d.Remove("doesnotexist")
// 	if err == nil {
// 		t.Fatalf("should have failed (doesnotexist)")
// 	}
// 	if d.Content["c"].Index != uint64(1) {
// 		t.Fatalf("bad index for c: %v (expected 1)", d.Content["c"].Index)
// 	}
// 	if len(d.Content) != 2 {
// 		t.Fatalf("bad length for content: %v", len(d.Content))
// 	}
// 	if len(d.Order) != 2 {
// 		t.Fatalf("bad length for order: %v", len(d.Order))
// 	}
// 	if d.Order[1] != "c" {
// 		t.Fatalf("bad last item: %v (expected c)", d.Order[1])
// 	}
// }

func dockerImageLoad(input io.Reader) error {
	dc, err := docker.NewEnvClient()
	if err != nil {
		return fmt.Errorf("error creating Docker client: %v", err)
	}
	resp, err := dc.ImageLoad(context.Background(), input, true)
	if err != nil {
		return fmt.Errorf("error loading image: %v", err)
	}
	defer resp.Body.Close()
	_, err = ioutil.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("error reading docker response: %v", err)
	}
	return nil
}

// func TestSquasherFunctional(t *testing.T) {
// 	logger := log.New(os.Stderr, "", log.LstdFlags)
// 	sqsh := NewDockerImageSquasher(logger)
// 	f, err := os.Open("./testdata/python-3.tar.gz")
// 	if err != nil {
// 		t.Fatalf("error opening image: %v", err)
// 	}
// 	defer f.Close()
// 	r, err := gzip.NewReader(f)
// 	if err != nil {
// 		t.Fatalf("error creating gzip reader: %v", err)
// 	}
// 	output := bytes.Buffer{}
// 	info, err := sqsh.Squash(context.Background(), r, &output)
// 	if err != nil {
// 		t.Fatalf("error squashing: %v", err)
// 	}
// 	t.Logf("squasher: layers removed: %v; whiteouts: %v; files removed: %v; input bytes: %v, output bytes: %v; size diff: %v; size pct diff: %v", info.LayersRemoved, len(info.FilesRemoved), info.FilesRemovedCount, info.InputBytes, info.OutputBytes, info.SizeDifference, info.SizePctDifference)
// 	err = dockerImageLoad(&output)
// 	if err != nil {
// 		t.Fatalf("error loading squashed image: %v", err)
// 	}
// }
