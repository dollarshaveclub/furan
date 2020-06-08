package tagcheck

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"os/user"
	"path"
	"strings"
	"testing"

	"github.com/dollarshaveclub/furan/pkg/config"
)

const (
	testPrivateRepo = "quay.io/dollarshaveclub/acyl"
)

var furanTestTags = []string{"master", "latest"}
var testPrivateRepoTags = []string{"master", "latest", "global-limit"}

var testLoggerFunc = func(string, ...interface{}) {}

func testReadDockercfg() (*config.Dockerconfig, error) {
	var dockercfgraw string
	var dconfig config.Dockerconfig
	if os.Getenv("CIRCLECI_DOCKERCFG") == "" { // read user's dockercfg
		usr, err := user.Current()
		if err != nil {
			return nil, fmt.Errorf("error getting current user: %v", err)
		}
		dp := path.Join(usr.HomeDir, ".dockercfg")
		if _, err := os.Stat(dp); os.IsNotExist(err) {
			return nil, fmt.Errorf("dockercfg not found: %v", err)
		}
		dcfg, err := ioutil.ReadFile(dp)
		if err != nil {
			return nil, fmt.Errorf("error reading dockercfg: %v", err)
		}
		dockercfgraw = string(dcfg)
	} else {
		dockercfgraw = os.Getenv("CIRCLECI_DOCKERCFG")
	}
	err := json.Unmarshal([]byte(dockercfgraw), &dconfig.DockercfgContents)
	if err != nil {
		return nil, fmt.Errorf("error unmarshaling dockercfg: %v", err)
	}
	for k, v := range dconfig.DockercfgContents {
		if v.Auth != "" && v.Username == "" && v.Password == "" {
			// Auth is a base64-encoded string of the form USERNAME:PASSWORD
			ab, err := base64.StdEncoding.DecodeString(v.Auth)
			if err != nil {
				return nil, fmt.Errorf("dockercfg: couldn't decode auth string: %v: %v", k, err)
			}
			as := strings.Split(string(ab), ":")
			if len(as) != 2 {
				return nil, fmt.Errorf("dockercfg: malformed auth string: %v: %v: %v", k, v.Auth, string(ab))
			}
			v.Username = as[0]
			v.Password = as[1]
			v.Auth = ""
		}
		v.ServerAddress = k
		dconfig.DockercfgContents[k] = v
	}
	return &dconfig, nil
}

func TestTagCheckerBadRepo(t *testing.T) {
	tc := NewRegistryTagChecker(&config.Dockerconfig{}, testLoggerFunc)
	_, _, err := tc.AllTagsExist([]string{"foo", "bar"}, "thisisabadrepo")
	if err == nil {
		t.Fatalf("expected bad repo error")
	}
}

func TestTagCheckerBadTags(t *testing.T) {
	tc := NewRegistryTagChecker(&config.Dockerconfig{}, testLoggerFunc)
	_, _, err := tc.AllTagsExist([]string{}, "hub.docker.io/foo/bar")
	if err == nil {
		t.Fatalf("expected bad tags error")
	}
}

func TestTagCheckerUnsupportedRegistry(t *testing.T) {
	tc := NewRegistryTagChecker(&config.Dockerconfig{}, testLoggerFunc)
	ok, m, err := tc.AllTagsExist([]string{"foo", "bar"}, "acmecompany/repo")
	if err != nil {
		t.Fatalf("should have succeeded: %v", err)
	}
	if ok {
		t.Fatalf("should have returned false")
	}
	if len(m) != 2 {
		t.Fatalf("should have returned all tags as missing")
	}
}

func TestTagCheckerQuayAllTagsExist(t *testing.T) {
	tc := NewRegistryTagChecker(&config.Dockerconfig{}, testLoggerFunc)
	ok, missing, err := tc.AllTagsExist(furanTestTags, "quay.io/dollarshaveclub/furan")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatalf("should have succeeded")
	}
	if missing != nil && len(missing) != 0 {
		t.Fatalf("missing should be nil or empty: %v", missing)
	}
}

func TestTagCheckerQuayMissingTag(t *testing.T) {
	tc := NewRegistryTagChecker(&config.Dockerconfig{}, testLoggerFunc)
	ok, missing, err := tc.AllTagsExist(append(furanTestTags, "missingtag"), "quay.io/dollarshaveclub/furan")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatalf("should have failed")
	}
	if len(missing) != 1 {
		t.Fatalf("bad missing length: %v", missing)
	}
	if missing[0] != "missingtag" {
		t.Fatalf("bad missing tag: %v", missing[0])
	}
}

func TestTagCheckerQuayPrivateRepoAllExist(t *testing.T) {
	dcfg, err := testReadDockercfg()
	if err != nil {
		t.SkipNow()
		return
	}
	tc := NewRegistryTagChecker(dcfg, testLoggerFunc)
	ok, missing, err := tc.AllTagsExist(testPrivateRepoTags, testPrivateRepo)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatalf("should have succeeded: missing: %v", missing)
	}
	if missing != nil && len(missing) != 0 {
		t.Fatalf("missing should be nil or empty: %v", missing)
	}
}

func TestTagCheckerQuayPrivateRepoMissingTag(t *testing.T) {
	dcfg, err := testReadDockercfg()
	if err != nil {
		t.SkipNow()
		return
	}
	tc := NewRegistryTagChecker(dcfg, testLoggerFunc)
	ok, missing, err := tc.AllTagsExist(append(testPrivateRepoTags, "missingtag123456790"), testPrivateRepo)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatalf("should have returned false")
	}
	if len(missing) != 1 {
		t.Fatalf("should have returned one missing tag")
	}
	if missing[0] != "missingtag123456790" {
		t.Fatalf("bad missing tag: %v", missing[0])
	}
}
