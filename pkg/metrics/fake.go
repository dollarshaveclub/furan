package metrics

type FakeCollector struct{}

func (fc *FakeCollector) Duration(string, string, string, []string, float64) error { return nil }
func (fc *FakeCollector) Size(string, string, string, []string, int64) error       { return nil }
func (fc *FakeCollector) Float(string, string, string, []string, float64) error    { return nil }
func (fc *FakeCollector) ImageSize(int64, int64, string, string) error             { return nil }
func (fc *FakeCollector) BuildStarted(string, string) error                        { return nil }
func (fc *FakeCollector) BuildFailed(string, string, bool) error                   { return nil }
func (fc *FakeCollector) BuildSucceeded(string, string) error                      { return nil }
func (fc *FakeCollector) TriggerCompleted(string, string, bool) error              { return nil }
func (fc *FakeCollector) KafkaProducerFailure() error                              { return nil }
func (fc *FakeCollector) KafkaConsumerFailure() error                              { return nil }
func (fc *FakeCollector) GCFailure() error                                         { return nil }
func (fc *FakeCollector) GCUntaggedImageRemoved() error                            { return nil }
func (fc *FakeCollector) GCBytesReclaimed(uint64) error                            { return nil }
func (fc *FakeCollector) DiskFree(uint64) error                                    { return nil }
func (fc *FakeCollector) FileNodesFree(uint64) error                               { return nil }

var _ MetricsCollector = &FakeCollector{}
