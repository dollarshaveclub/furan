package cmd

import (
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"os"
	"sync"
	"time"

	"github.com/ghodss/yaml"
	"github.com/gofrs/uuid"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/dollarshaveclub/furan/pkg/client"
	"github.com/dollarshaveclub/furan/pkg/datalayer"
	"github.com/dollarshaveclub/furan/pkg/generated/furanrpc"
	"github.com/dollarshaveclub/furan/pkg/models"
)

// integrationCmd represents the integration command
var integrationCmd = &cobra.Command{
	Use:   "integration",
	Short: "Run a set of integration tests",
	Long: `Run integration tests in a k8s cluster.

This command assumes that it is running in a Job pod in k8s. 
Furan server must also be running in the same cluster.

Pass a YAML file containing options for the integration test (see testdata/integration.json).`,
	RunE: integration,
}

var integrationTestsFile, furanaddr string
var furanServerConnectTimeout time.Duration

// K8sSecretNames defines the k8s secrets containing various secret values for the integration test
type K8sSecretNames struct {
	GitHubToken string `json:"github_token"`
}

type IntegrationTest struct {
	Name          string                   `json:"name"`
	Build         furanrpc.BuildDefinition `json:"build"`
	ImageRepos    []string                 `json:"image_repos"`
	SkipIfExists  bool                     `json:"skip_if_exists"`
	ExpectFailure bool                     `json:"expect_failure"`
	ExpectSkipped bool                     `json:"expect_skipped"`
	SecretNames   K8sSecretNames           `json:"secret_names"`
}

type IntegrationTests struct {
	Tests []IntegrationTest `json:"tests"`
}

func init() {
	integrationCmd.Flags().StringVar(&integrationTestsFile, "integration-tests-file", "/opt/testing/integration.yaml", "YAML integration tests file")
	integrationCmd.Flags().StringVar(&furanaddr, "furan-addr", "furan:4000", "furan server address")
	integrationCmd.Flags().DurationVar(&furanServerConnectTimeout, "furan-timeout", 2*time.Minute, "timeout for furan server to be up and ready")
	RootCmd.AddCommand(integrationCmd)
}

type buildMap struct {
	sync.RWMutex
	builds map[string]uuid.UUID
}

func integration(cmd *cobra.Command, args []string) error {
	f, err := os.Open(integrationTestsFile)
	if err != nil {
		return fmt.Errorf("error opening integration tests file: %w", err)
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return fmt.Errorf("error getting file info: %w", err)
	}

	if fi.Size() > 10_000_000 {
		return fmt.Errorf("integration test exceeds max size: %v", fi.Size())
	}

	td, err := ioutil.ReadAll(f)
	if err != nil {
		return fmt.Errorf("error reading file: %w", err)
	}

	var tests IntegrationTests
	if err := yaml.Unmarshal(td, &tests); err != nil {
		return fmt.Errorf("error decoding integration tests file: %w", err)
	}

	kc, err := NewInClusterK8sClient()
	if err != nil {
		return fmt.Errorf("error getting k8s client: %w", err)
	}

	if len(tests.Tests) == 0 {
		return fmt.Errorf("no tests defined")
	}

	ctx := context.Background()

	for i := range tests.Tests {
		sn := tests.Tests[i].SecretNames.GitHubToken
		s, err := kc.CoreV1().Secrets(os.Getenv("POD_NAMESPACE")).Get(ctx, sn, v1.GetOptions{})
		if err != nil {
			return fmt.Errorf("error getting secret: %v: %w", sn, err)
		}
		tests.Tests[i].Build.GithubCredential = string(s.Data["token"])
	}

	apikey, err := GetAPIKey()
	if err != nil {
		return fmt.Errorf("error getting api key: %w", err)
	}

	if err := WaitForFuranServer(furanaddr); err != nil {
		return fmt.Errorf("error verifying furan server is running: %w", err)
	}

	fc, err := client.New(client.Options{
		Address:               furanaddr,
		APIKey:                apikey.String(),
		TLSInsecureSkipVerify: true,
	})
	defer fc.Close()

	var eg errgroup.Group

	var bm buildMap
	bm.builds = make(map[string]uuid.UUID, len(tests.Tests))

	for i := range tests.Tests {
		t := tests.Tests[i]
		p := &furanrpc.PushDefinition{
			Registries: make([]*furanrpc.PushRegistryDefinition, len(t.ImageRepos)),
		}
		for i := range t.ImageRepos {
			p.Registries[i] = &furanrpc.PushRegistryDefinition{
				Repo: t.ImageRepos[i],
			}
		}
		n := i
		eg.Go(func() error {
			fmt.Printf("starting build: %v: %v\n", n, t.Name)
			bid, err := fc.StartBuild(ctx, furanrpc.BuildRequest{
				Build:        &t.Build,
				Push:         p,
				SkipIfExists: t.SkipIfExists,
			})
			if err != nil {
				return fmt.Errorf("error starting build: %v: %w", t.Name, err)
			}

			bm.Lock()
			bm.builds[t.Name] = bid
			bm.Unlock()

			fmt.Printf("build started: %v: %v: %v\n", n, t.Name, bid)
			return monitorIntegrationBuild(ctx, bid, t.ExpectFailure, t.ExpectSkipped, fc)
		})
	}

	if err := eg.Wait(); err != nil {
		return fmt.Errorf("failure: %w", err)
	}

	bm.Lock()
	defer bm.Unlock()
	for name, id := range bm.builds {
		bs, err := fc.GetBuildStatus(ctx, id)
		if err != nil {
			fmt.Printf("error getting build status: %v: %v: %v", id, name, err)
			continue
		}
		fmt.Printf("build status: %v: %v: %v\n", id, name, bs.State)
	}

	return nil
}

func monitorIntegrationBuild(ctx context.Context, bid uuid.UUID, fail, skip bool, fc *client.RemoteBuilder) error {
	s, err := fc.MonitorBuild(ctx, bid)
	if err != nil {
		return fmt.Errorf("error monitoring build %v: %w", bid, err)
	}
	for {
		ev, err := s.Recv()
		if err != nil {
			if err == io.EOF {
				break
			}
			return fmt.Errorf("error getting build event (%v): %w", bid, err)
		}
		var status string
		if ev.CurrentState != furanrpc.BuildState_RUNNING {
			status = fmt.Sprintf(" (status: %v)", ev.CurrentState)
		}
		log.Printf("%v: %v%v\n", bid, ev.Message, status)
	}
	bs, err := fc.GetBuildStatus(ctx, bid)
	if err != nil {
		return fmt.Errorf("error getting final build status: %w", err)
	}
	duration := " "
	if bs.Started != nil && bs.Completed != nil {
		d := models.TimeFromRPCTimestamp(*bs.Completed).Sub(models.TimeFromRPCTimestamp(*bs.Started))
		duration = fmt.Sprintf(" (duration: %v) ", d)
	}
	log.Printf("build completed: state: %v%v\n", bs.State, duration)
	switch bs.State {
	case furanrpc.BuildState_FAILURE:
		if !fail {
			return fmt.Errorf("unexpected build failure")
		}
	case furanrpc.BuildState_SUCCESS:
		if fail {
			return fmt.Errorf("wanted build failure but it succeeded instead")
		}
	case furanrpc.BuildState_SKIPPED:
		if !skip {
			return fmt.Errorf("unexpected build skipped")
		}
	default:
		return fmt.Errorf("unexpected final build status: %v", bs.State)
	}
	return nil
}

func NewInClusterK8sClient() (kubernetes.Interface, error) {
	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("error getting in-cluster config: %w", err)
	}
	kc, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("error getting client: %w", err)
	}
	return kc, nil
}

func GetAPIKey() (uuid.UUID, error) {
	db, err := datalayer.NewRawPGClient(os.Getenv("DB_URI"), 1)
	if err != nil {
		return uuid.Nil, fmt.Errorf("error getting postgres client: %w", err)
	}
	var key uuid.UUID
	err = db.QueryRow(context.Background(), `SELECT id FROM api_keys WHERE name = 'root' LIMIT 1;`).Scan(&key)
	return key, err
}

func WaitForFuranServer(addr string) error {
	deadline := time.Now().UTC().Add(furanServerConnectTimeout)
	for {
		now := time.Now().UTC()
		if now.Equal(deadline) || now.After(deadline) {
			return fmt.Errorf("timeout waiting for furan server (%v)", furanServerConnectTimeout)
		}
		conn, err := net.Dial("tcp", addr)
		if err != nil {
			log.Printf("error connecting to furan server: waiting & retrying...")
			time.Sleep(1 * time.Second)
			continue
		}
		conn.Close()
		return nil
	}
}
