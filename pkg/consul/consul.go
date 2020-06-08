package consul

import (
	"fmt"
	"time"

	"github.com/dollarshaveclub/furan/pkg/config"
	"github.com/gocql/gocql"
	consul "github.com/hashicorp/consul/api"
)

// KeyValueOrchestrator describes an object that can orchestrate with a K/V store
type KeyValueOrchestrator interface {
	SetBuildRunning(id gocql.UUID) error
	DeleteBuildRunning(id gocql.UUID) error
	SetBuildCancelled(id gocql.UUID) error
	DeleteBuildCancelled(id gocql.UUID) error
	CheckIfBuildRunning(id gocql.UUID) (bool, error)
	WatchIfBuildStopsRunning(id gocql.UUID, timeout time.Duration) (bool, error)
	WatchIfBuildIsCancelled(id gocql.UUID, timeout time.Duration) (bool, error)
}

// ConsulKVOrchestrator is an object that orchestrates via Consul
type ConsulKVOrchestrator struct {
	cio ConsulKV
	cfg *config.Consulconfig
}

// consulKV describes an object that does low-level interaction with Consul
type ConsulKV interface {
	Put(p *consul.KVPair, q *consul.WriteOptions) (*consul.WriteMeta, error)
	Keys(prefix, separator string, q *consul.QueryOptions) ([]string, *consul.QueryMeta, error)
	Get(key string, q *consul.QueryOptions) (*consul.KVPair, *consul.QueryMeta, error)
	Delete(key string, w *consul.WriteOptions) (*consul.WriteMeta, error)
}

// NewConsulKVOrchestrator returns a new ConsulKVOrchestrator
func NewConsulKVOrchestrator(cfg *config.Consulconfig) (*ConsulKVOrchestrator, error) {
	clientcfg := &consul.Config{
		Scheme:  "http",
		Address: cfg.Addr,
	}
	client, err := consul.NewClient(clientcfg)
	if err != nil {
		return nil, fmt.Errorf("error getting Consul client: %v", err)
	}
	return newConsulKVOrchestrator(client.KV(), cfg)
}

func newConsulKVOrchestrator(ckv ConsulKV, cfg *config.Consulconfig) (*ConsulKVOrchestrator, error) {
	if ckv == nil {
		return nil, fmt.Errorf("ConsulKV is required")
	}
	return &ConsulKVOrchestrator{
		cio: ckv,
		cfg: cfg,
	}, nil
}

func (cko *ConsulKVOrchestrator) setBuildKey(pfx, id string) error {
	kp := &consul.KVPair{
		Key: cko.cfg.KVPrefix + "/" + pfx + "/" + id,
	}
	_, err := cko.cio.Put(kp, nil)
	if err != nil {
		return fmt.Errorf("error setting build to %v: %v", pfx, err)
	}
	return nil
}

func (cko *ConsulKVOrchestrator) deleteBuildKey(pfx, id string) error {
	key := cko.cfg.KVPrefix + "/" + pfx + "/" + id
	_, err := cko.cio.Delete(key, nil)
	if err != nil {
		return fmt.Errorf("error deleting build key in %v: %v", pfx, err)
	}
	return nil
}

// SetBuildRunning sets a build ID to running in Consul
func (cko *ConsulKVOrchestrator) SetBuildRunning(id gocql.UUID) error {
	return cko.setBuildKey("running", id.String())
}

// DeleteBuildRunning deletes a build ID from running in Consul
func (cko *ConsulKVOrchestrator) DeleteBuildRunning(id gocql.UUID) error {
	return cko.deleteBuildKey("running", id.String())
}

// SetBuildCancelled sets a build ID to cancelled in Consul
func (cko *ConsulKVOrchestrator) SetBuildCancelled(id gocql.UUID) error {
	return cko.setBuildKey("cancelled", id.String())
}

// DeleteBuildCancelled deletes a build ID from cancelled in Consul
func (cko *ConsulKVOrchestrator) DeleteBuildCancelled(id gocql.UUID) error {
	return cko.deleteBuildKey("cancelled", id.String())
}

// CheckIfBuildRunning checks if build is listed as running in Consul
func (cko *ConsulKVOrchestrator) CheckIfBuildRunning(id gocql.UUID) (bool, error) {
	key := cko.cfg.KVPrefix + "/running/" + id.String()
	kvp, _, err := cko.cio.Get(key, nil)
	if err != nil {
		return false, fmt.Errorf("error checking if build running: %v", err)
	}
	return kvp != nil, nil
}

// watchPrefix blocks watching a prefix for changes. checkKeys determines whether the desired change happened (key there or not there). checkKeys should return true if watched change is observed.
func (cko *ConsulKVOrchestrator) watchPrefix(pfx string, timeout time.Duration, checkKeys func([]string) bool) (bool, error) {
	start := time.Now().UTC()

	// get start index
	var index uint64
	keys, qm, err := cko.cio.Keys(pfx, "", nil)
	if err != nil {
		return false, fmt.Errorf("error listing keys: %v: %v", pfx, err)
	}
	index = qm.LastIndex

	// initial check of key
	if checkKeys(keys) {
		return true, nil
	}

	// long poll query
	qo := &consul.QueryOptions{}
	for {
		elapsed := time.Now().UTC().Sub(start)
		if elapsed >= timeout {
			return false, nil
		}
		timeleft := timeout - elapsed
		qo.WaitIndex = index
		qo.WaitTime = timeleft
		keys, qm, err = cko.cio.Keys(pfx, "", qo)
		if err != nil {
			return false, fmt.Errorf("error listing keys: %v: %v", pfx, err)
		}
		if checkKeys(keys) {
			return true, nil
		}
		index = qm.LastIndex
	}
}

// WatchIfBuildStopsRunning blocks waiting for build to drop off running in Consul.
// false is returned if timeout occurs prior to build dropping off
func (cko *ConsulKVOrchestrator) WatchIfBuildStopsRunning(id gocql.UUID, timeout time.Duration) (bool, error) {
	pfx := cko.cfg.KVPrefix + "/running/"
	target := pfx + id.String()

	keyMissing := func(keys []string) bool {
		for _, k := range keys {
			if k == target {
				return false
			}
		}
		return true
	}

	return cko.watchPrefix(pfx, timeout, keyMissing)
}

// WatchIfBuildIsCancelled blocks waiting for build to appear in cancelled in Consul.
// false is returned if timeout occurs prior to build appearing
func (cko *ConsulKVOrchestrator) WatchIfBuildIsCancelled(id gocql.UUID, timeout time.Duration) (bool, error) {
	pfx := cko.cfg.KVPrefix + "/cancelled/"
	target := pfx + id.String()

	keyExists := func(keys []string) bool {
		for _, k := range keys {
			if k == target {
				return true
			}
		}
		return false
	}

	return cko.watchPrefix(pfx, timeout, keyExists)
}
