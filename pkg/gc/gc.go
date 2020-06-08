package gc

import (
	"log"
	"os"
	"syscall"

	dtypes "github.com/docker/engine-api/types"
	"github.com/docker/engine-api/types/filters"
	"github.com/dollarshaveclub/furan/pkg/builder"
	"github.com/dollarshaveclub/furan/pkg/metrics"
	"github.com/dustin/go-humanize"
	"golang.org/x/net/context"
)

const (
	emptyTagString = "<none>:<none>"
)

type GCCleaner interface {
	GC()
}

type DockerImageGC struct {
	log        *log.Logger
	dc         builder.ImageBuildClient
	mc         metrics.MetricsCollector
	dockerPath string
}

func NewDockerImageGC(log *log.Logger, dc builder.ImageBuildClient, mc metrics.MetricsCollector, dockerPath string) *DockerImageGC {
	return &DockerImageGC{
		log:        log,
		dc:         dc,
		mc:         mc,
		dockerPath: dockerPath,
	}
}

func (dgc *DockerImageGC) reportDiskMetrics() error {
	if _, err := os.Stat(dgc.dockerPath); err != nil {
		if os.IsNotExist(err) {
			dgc.log.Printf("gc: %v: not found, not calculating free space", dgc.dockerPath)
			return nil
		}
		return err
	}
	fs := syscall.Statfs_t{}
	err := syscall.Statfs(dgc.dockerPath, &fs)
	if err != nil {
		return err
	}
	freeBytes := fs.Bfree * uint64(fs.Bsize) // bytes
	freeFileNodes := fs.Ffree

	dgc.log.Printf("gc: %v free space: %v", dgc.dockerPath, humanize.Bytes(freeBytes))
	dgc.log.Printf("gc: %v free file nodes: %v", dgc.dockerPath, freeFileNodes)
	dgc.mc.DiskFree(freeBytes)
	dgc.mc.FileNodesFree(freeFileNodes)

	return nil
}

func (dgc *DockerImageGC) cleanUntaggedImages() error {
	ctx := context.Background()
	f, err := filters.ParseFlag("dangling=true", filters.NewArgs())
	if err != nil {
		return err
	}
	ops := dtypes.ImageListOptions{
		All:     true,
		Filters: f,
	}
	il, err := dgc.dc.ImageList(ctx, ops)
	if err != nil {
		return err
	}
	ropts := dtypes.ImageRemoveOptions{
		Force:         true,
		PruneChildren: true,
	}
	i := 0
	isz := int64(0)
	for _, img := range il {
		if len(img.RepoTags) == 1 && img.RepoTags[0] == emptyTagString { // redundant, only untagged images should be returned
			dgc.log.Printf("gc: removing untagged image: %v (%v)", img.ID, humanize.Bytes(uint64(img.Size)))
			_, err = dgc.dc.ImageRemove(ctx, img.ID, ropts)
			if err != nil {
				return err
			}
			dgc.mc.GCUntaggedImageRemoved()
			i++
			isz += img.Size
		}
	}
	dgc.log.Printf("gc: removed %v untagged images (%v total)", i, humanize.Bytes(uint64(isz)))
	dgc.mc.GCBytesReclaimed(uint64(isz))
	return nil
}

func (dgc *DockerImageGC) GC() {
	err := dgc.cleanUntaggedImages()
	if err != nil {
		dgc.log.Printf("error cleaning untagged images: %v", err)
		dgc.mc.GCFailure()
	}
	err = dgc.reportDiskMetrics()
	if err != nil {
		dgc.log.Printf("error calculating free disk space: %v", err)
		dgc.mc.GCFailure()
	}
}
