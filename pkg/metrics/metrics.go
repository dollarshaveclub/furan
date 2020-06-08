package metrics

import (
	"fmt"

	"github.com/DataDog/datadog-go/statsd"
)

const (
	namespace = "furan."
)

// MetricsCollector describes an object capabale of pushing metrics somewhere
type MetricsCollector interface {
	Duration(string, string, string, []string, float64) error
	Size(string, string, string, []string, int64) error
	Float(string, string, string, []string, float64) error
	ImageSize(int64, int64, string, string) error
	BuildStarted(string, string) error
	BuildFailed(string, string, bool) error
	BuildSucceeded(string, string) error
	TriggerCompleted(string, string, bool) error
	KafkaProducerFailure() error
	KafkaConsumerFailure() error
	GCFailure() error
	GCUntaggedImageRemoved() error
	GCBytesReclaimed(uint64) error
	DiskFree(uint64) error
	FileNodesFree(uint64) error
}

// DatadogCollector represents a collector that pushes metrics to Datadog
type DatadogCollector struct {
	c           *statsd.Client
	defaultTags []string
}

var _ MetricsCollector = &DatadogCollector{}

// NewDatadogCollector returns a DatadogCollector using dogstatsd at addr
func NewDatadogCollector(addr string, defaultTags []string) (*DatadogCollector, error) {
	c, err := statsd.New(addr)
	if err != nil {
		return nil, err
	}
	c.Namespace = namespace
	return &DatadogCollector{
		c:           c,
		defaultTags: defaultTags,
	}, nil
}

func (dc *DatadogCollector) tags(repo, ref string) []string {
	return append([]string{
		fmt.Sprintf("repo:%v", repo),
		fmt.Sprintf("ref:%v", ref),
	}, dc.defaultTags...)
}

func (dc *DatadogCollector) buildErrorTags(repo, ref string, userError bool) []string {
	errorTag := fmt.Sprintf("user_error:%v", userError)
	return append(dc.tags(repo, ref), errorTag)
}

// Duration pushes duration d (seconds) to the metric name to dogstatsd
func (dc *DatadogCollector) Duration(name string, repo string, ref string, tags []string, d float64) error {
	return dc.c.Histogram(name, d, append(dc.tags(repo, ref), tags...), 1)
}

// Size pushes sz (bytes) to the metric name to dogstatsd
func (dc *DatadogCollector) Size(name string, repo string, ref string, tags []string, sz int64) error {
	return dc.c.Histogram(name, float64(sz), append(dc.tags(repo, ref), tags...), 1)
}

// Float pushes val to the metric name to dogstatsd
func (dc *DatadogCollector) Float(name string, repo string, ref string, tags []string, val float64) error {
	return dc.c.Histogram(name, val, append(dc.tags(repo, ref), tags...), 1)
}

// ImageSize pushes sz (total size in bytes) and vxz (virtual size in bytes) to dogstatsd
func (dc *DatadogCollector) ImageSize(sz int64, vsz int64, repo string, ref string) error {
	tags := dc.tags(repo, ref)
	err := dc.c.Histogram("image.size_bytes", float64(sz), tags, 1)
	if err != nil {
		return err
	}
	return dc.c.Histogram("image.vsize_bytes", float64(vsz), tags, 1)
}

// BuildStarted increments the counter for each requested build
func (dc *DatadogCollector) BuildStarted(repo, ref string) error {
	return dc.c.Count("build.started", 1, dc.tags(repo, ref), 1)
}

// BuildFailed increments the counter for each build that fails
func (dc *DatadogCollector) BuildFailed(repo, ref string, userError bool) error {
	return dc.c.Count("build.failed", 1, dc.buildErrorTags(repo, ref, userError), 1)
}

// BuildSucceeded increments the counter for each build that succeeds
func (dc *DatadogCollector) BuildSucceeded(repo, ref string) error {
	return dc.c.Count("build.succeeded", 1, dc.tags(repo, ref), 1)
}

// KafkaProducerFailure increments the counter for a Kafka publish failure
func (dc *DatadogCollector) KafkaProducerFailure() error {
	return dc.c.Count("kafka.producer.failure", 1, nil, 1)
}

// KafkaConsumerFailure increments the counter for a Kafka read failure
func (dc *DatadogCollector) KafkaConsumerFailure() error {
	return dc.c.Count("kafka.consumer.failure", 1, nil, 1)
}

// GCFailure reports an occurence of GC failure
func (dc *DatadogCollector) GCFailure() error {
	return dc.c.Count("gc.failure", 1, nil, 1)
}

// GCUntaggedImageRemoved reports a single untagged image removal
func (dc *DatadogCollector) GCUntaggedImageRemoved() error {
	return dc.c.Count("gc.rm_untagged_image", 1, nil, 1)
}

// GCBytesReclaimed reports the total bytes reclaimed during a GC run
func (dc *DatadogCollector) GCBytesReclaimed(size uint64) error {
	return dc.c.Histogram("gc.bytes_reclaimed", float64(size), nil, 1)
}

// DiskFree reports the amount of disk space left on the Docker volume
func (dc *DatadogCollector) DiskFree(bytes uint64) error {
	return dc.c.Gauge("disk_bytes_free", float64(bytes), nil, 1)
}

// FileNodesFree reports the number of file nodes (inodes) left on the Docker volume
func (dc *DatadogCollector) FileNodesFree(nodes uint64) error {
	return dc.c.Gauge("file_nodes_free", float64(nodes), nil, 1)
}

// TriggerCompleted reports the completion of the "trigger" subcommand
func (dc *DatadogCollector) TriggerCompleted(repo, ref string, failed bool) error {
	failedTag := fmt.Sprintf("failed=%v", failed)
	return dc.c.Count("trigger.completed", 1, append(dc.tags(repo,ref), failedTag), 1)
}
