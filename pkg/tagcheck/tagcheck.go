package tagcheck

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/AlternativaPlatform/docker-registry-client/registry"
	"github.com/dollarshaveclub/furan/pkg/config"
	"golang.org/x/sync/errgroup"
)

// ImageTagChecker describes an object that can see if a tag exists for an image in a registry
type ImageTagChecker interface {
	AllTagsExist(tags []string, repo string) (bool, []string, error)
}

// RegistryTagChecker is an object that can check a remote registry for a set of tags
type RegistryTagChecker struct {
	dockercfg  *config.Dockerconfig
	loggerFunc func(string, ...interface{})
}

// NewRegistryTagChecker returns a RegistryTagChecker using the specified dockercfg for authentication
func NewRegistryTagChecker(dockercfg *config.Dockerconfig, loggerFunc func(string, ...interface{})) *RegistryTagChecker {
	return &RegistryTagChecker{
		dockercfg:  dockercfg,
		loggerFunc: loggerFunc,
	}
}

type manifestError struct {
	Errors []struct {
		Code string `json:"code"`
	} `json:"errors"`
}

const tagMissingManifestErrorCode = "MANIFEST_UNKNOWN"

// AllTagsExist checks a remote registry to see if all tags exist for the given repository.
// It returns the missing tags, if any.
// Only quay.io is supported for now as each registry demonstrates different behavior when using the Docker Registry V2 API
func (rtc *RegistryTagChecker) AllTagsExist(tags []string, repo string) (bool, []string, error) {
	if len(tags) == 0 {
		return false, nil, fmt.Errorf("at least one tag is required")
	}
	rs := strings.Split(repo, "/")
	if len(rs) == 3 {
		if rs[0] != "quay.io" {
			rtc.loggerFunc("quay.io is the only supported registry for tag checking: %v", rs[0])
			return false, tags, nil
		}
	} else {
		if len(rs) == 2 {
			rtc.loggerFunc("quay.io is the only supported registry for tag checking: Docker Hub")
			return false, tags, nil
		}
		return false, nil, fmt.Errorf("bad format for repo: expected [host]/[namespace]/[repository] or [namespace]/[repository]: %v", repo)
	}
	hc := &http.Client{}
	domain := rs[0]
	url := "https://" + domain
	ac, ok := rtc.dockercfg.DockercfgContents[domain]
	if ok { // if missing, anonymous auth
		hc.Transport = registry.WrapTransport(http.DefaultTransport, url, ac.Username, ac.Password)
	}
	// reg.Ping() fails for quay.io, so we manually construct a registry client here
	reg := registry.Registry{
		URL:    url,
		Client: hc,
		Logf:   rtc.loggerFunc,
	}
	ctx, cf := context.WithTimeout(context.Background(), 1*time.Minute)
	defer cf()
	g, _ := errgroup.WithContext(ctx)
	missing := make([]string, len(tags))
	for i := range tags {
		t := tags[i]
		g.Go(func() error {
			m, err := reg.ManifestV2(fmt.Sprintf("%v/%v", rs[1], rs[2]), t)
			if err != nil {
				errmsg := err.Error()
				// authenticated, private repo requests return a 404 error like this...
				if strings.Contains(errmsg, "status=404") && strings.Contains(errmsg, `\"code\":\"MANIFEST_UNKNOWN\"`) {
					missing[i] = t
					return nil
				}
				return fmt.Errorf("error getting manifest: tag: %v: %v", t, err)
			}
			b, _ := m.MarshalJSON()
			me := manifestError{}
			if err := json.Unmarshal(b, &me); err != nil {
				return fmt.Errorf("error unmarshaling response: tag: %v: %v", t, err)
			}
			if len(me.Errors) > 0 {
				// unauthenticated, public repo requests return status 200 but with this error message as the body...
				if me.Errors[0].Code == tagMissingManifestErrorCode {
					missing[i] = t
				} else {
					return fmt.Errorf("unknown error: tag: %v: %v", t, string(b))
				}
			}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return false, nil, err
	}
	out := []string{}
	for _, m := range missing {
		if m != "" {
			out = append(out, m)
		}
	}
	return len(out) == 0, out, nil
}
