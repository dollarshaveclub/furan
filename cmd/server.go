package cmd

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"

	// Import pprof handlers into http.DefaultServeMux
	_ "net/http/pprof"
	"os"
	"time"

	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/dollarshaveclub/furan/pkg/builder"
	"github.com/dollarshaveclub/furan/pkg/config"
	"github.com/dollarshaveclub/furan/pkg/datalayer"
	"github.com/dollarshaveclub/furan/pkg/generated/furanrpc"
	"github.com/dollarshaveclub/furan/pkg/github"
	"github.com/dollarshaveclub/furan/pkg/grpc"
	"github.com/dollarshaveclub/furan/pkg/jobrunner"
	"github.com/dollarshaveclub/furan/pkg/models"
)

var serverConfig config.ServerConfig

var serverCmd = &cobra.Command{
	Use:   "server",
	Short: "Run Furan server",
	Long:  `Furan API server (see docs)`,
	PreRun: func(cmd *cobra.Command, args []string) {
		gitHubSecrets()
		awsSecrets()
		quaySecrets()
		dbSecrets()
	},
	Run: server,
}

var defaultcachetype, tracesvcname string
var defaultcachemaxmode bool
var tlscert, tlskey string
var jcinterval, jcage time.Duration
var builderimg string

type maxResourceLimits struct {
	CPUReq, MemReq, CPULim, MemLim string
}

func (mr maxResourceLimits) process() (*grpc.MaxResourceLimits, error) {
	out := &grpc.MaxResourceLimits{}
	q, err := resource.ParseQuantity(mr.CPUReq)
	if err != nil {
		return nil, fmt.Errorf("max cpu req is invalid: %w", err)
	}
	out.MaxCPUReq = q
	q, err = resource.ParseQuantity(mr.MemReq)
	if err != nil {
		return nil, fmt.Errorf("max mem req is invalid: %w", err)
	}
	out.MaxMemReq = q
	q, err = resource.ParseQuantity(mr.CPULim)
	if err != nil {
		return nil, fmt.Errorf("max cpu limit is invalid: %w", err)
	}
	out.MaxCPULim = q
	q, err = resource.ParseQuantity(mr.MemLim)
	if err != nil {
		return nil, fmt.Errorf("max cpu limit is invalid: %w", err)
	}
	out.MaxMemLim = q
	return out, nil
}

var maxResources maxResourceLimits

// serverAndRunnerFlags adds flags shared by the "server" and "runbuild" commands
func serverAndRunnerFlags(cmd *cobra.Command) {
	// Secrets
	cmd.PersistentFlags().StringVar(&vaultConfig.Addr, "vault-addr", os.Getenv("VAULT_ADDR"), "Vault URL (if using Vault secret backend)")
	cmd.PersistentFlags().StringVar(&vaultConfig.Token, "vault-token", os.Getenv("VAULT_TOKEN"), "Vault token (if using token auth & Vault secret backend)")
	cmd.PersistentFlags().BoolVar(&vaultConfig.TokenAuth, "vault-token-auth", false, "Use Vault token-based auth (if using Vault secret backend)")
	cmd.PersistentFlags().BoolVar(&vaultConfig.K8sAuth, "vault-k8s-auth", false, "Use Vault k8s auth (if using Vault secret backend)")
	cmd.PersistentFlags().StringVar(&vaultConfig.K8sJWTPath, "vault-k8s-jwt-path", "/var/run/secrets/kubernetes.io/serviceaccount/token", "Vault k8s JWT file path (if using k8s auth & Vault secret backend)")
	cmd.PersistentFlags().StringVar(&vaultConfig.K8sRole, "vault-k8s-role", "", "Vault k8s role (if using k8s auth & Vault secret backend)")
	cmd.PersistentFlags().StringVar(&vaultConfig.K8sAuthPath, "vault-k8s-auth-path", "kubernetes", "Vault k8s auth path (if using k8s auth & Vault secret backend)")
	cmd.PersistentFlags().StringVar(&secretsbackend, "secrets-backend", "vault", "Secret backend (one of: vault,env,json,filetree)")
	cmd.PersistentFlags().StringVar(&sf.JSONFile, "secrets-json-file", "secrets.json", "Secret JSON file path (if using json backend)")
	cmd.PersistentFlags().StringVar(&sf.FileTreeRoot, "secrets-filetree-root", "/vault/secrets/", "Secrets filetree root path (if using filetree backend)")
	cmd.PersistentFlags().StringVar(&sf.Mapping, "secrets-mapping", "{{.ID}}", "Secrets mapping template string (required)")

	// AWS S3 Cache
	cmd.PersistentFlags().StringVar(&awsConfig.Region, "aws-region", "us-west-2", "AWS region")
	cmd.PersistentFlags().StringVar(&awsConfig.CacheBucket, "s3-cache-bucket", "", "AWS S3 cache bucket")
	cmd.PersistentFlags().StringVar(&awsConfig.CacheKeyPrefix, "s3-cache-key-pfx", "", "AWS S3 cache key prefix")

	// ECR
	cmd.PersistentFlags().BoolVar(&awsConfig.EnableECR, "ecr", false, "Enable AWS ECR support")
	cmd.PersistentFlags().StringSliceVar(&awsConfig.ECRRegistryHosts, "ecr-registry-hosts", []string{}, "ECR registry hosts (ex: 123456789.dkr.ecr.us-west-2.amazonaws.com) to authorize for base images")
}

func init() {
	serverAndRunnerFlags(serverCmd)
	serverCmd.PersistentFlags().StringVar(&serverConfig.HTTPSAddr, "https-addr", "0.0.0.0:4001", "REST HTTPS listen address")
	serverCmd.PersistentFlags().StringVar(&serverConfig.GRPCAddr, "grpc-addr", "0.0.0.0:4000", "gRPC listen address")
	serverCmd.PersistentFlags().StringVar(&tracesvcname, "trace-svc", "furan2", "APM trace service name (optional)")

	// Builder image
	serverCmd.PersistentFlags().StringVar(&builderimg, "builder-image", "", "Buildkit builder container image override (optional)")

	// Job cleanup
	serverCmd.PersistentFlags().DurationVar(&jcinterval, "job-cleanup-interval", 30*time.Minute, "Build job cleanup check interval")
	serverCmd.PersistentFlags().DurationVar(&jcage, "job-cleanup-min-age", 48*time.Hour, "Minimum age for build jobs to be eligible for cleanup (deletion)")

	// TLS cert
	serverCmd.PersistentFlags().StringVar(&tlscert, "tls-cert-file", "", "Path to TLS certificate (optional, overrides secrets provider)")
	serverCmd.PersistentFlags().StringVar(&tlskey, "tls-key-file", "", "Path to TLS key (optional, overrides secrets provider)")

	// Default cache options
	serverCmd.PersistentFlags().StringVar(&defaultcachetype, "default-cache-type", "disabled", "Default cache type (if not specified in build request): s3, inline, disabled")
	serverCmd.PersistentFlags().BoolVar(&defaultcachemaxmode, "default-cache-max-mode", false, "Cache max mode by default")

	// Max resource requests/limits allowed
	serverCmd.PersistentFlags().StringVar(&maxResources.CPUReq, "max-cpu-request", "2", "Maximum build CPU request allowed (if empty, forbid API clients from setting this value at all)")
	serverCmd.PersistentFlags().StringVar(&maxResources.MemReq, "max-mem-request", "16G", "Maximum build memory request allowed  (if empty, forbid API clients from setting this value at all)")
	serverCmd.PersistentFlags().StringVar(&maxResources.CPULim, "max-cpu-limit", "4", "Maximum build CPU limit allowed (if empty, forbid API clients from setting this value at all)")
	serverCmd.PersistentFlags().StringVar(&maxResources.MemLim, "max-mem-limit", "64G", "Maximum build memory limit allowed (if empty, forbid API clients from setting this value at all)")

	RootCmd.AddCommand(serverCmd)
}

func cachedefaults() furanrpc.BuildCacheOpts {
	out := furanrpc.BuildCacheOpts{}
	switch defaultcachetype {
	case "s3":
		out.Type = furanrpc.BuildCacheOpts_S3
	case "inline":
		out.Type = furanrpc.BuildCacheOpts_INLINE
	case "disabled":
		out.Type = furanrpc.BuildCacheOpts_DISABLED
	default:
		log.Printf("unknown cache type: %v; ignoring", defaultcachetype)
		// leave as unset/unknown
	}
	out.MaxMode = defaultcachemaxmode
	return out
}

func server(cmd *cobra.Command, args []string) {
	dl, err := datalayer.NewPostgresDBLayer(dbConfig.PostgresURI)
	if err != nil {
		clierr("error configuring database: %v", err)
	}

	ctx := context.Background()

	if builderimg != "" {
		jobrunner.BuildKitImage = builderimg
	}

	jr, err := jobrunner.NewInClusterRunner(dl)
	if err != nil {
		clierr("error setting up in-cluster job runner: %v", err)
	}
	jr.JobFunc = jobrunner.FuranJobFunc
	jr.LogFunc = log.Printf
	if err := jr.StartCleanup(ctx, jcinterval, jcage, jobrunner.JobLabel); err != nil {
		clierr("error starting job cleanup: %v", err)
	}

	bm := &builder.Manager{
		JRunner: jr,
		DL:      dl,
	}

	var cert tls.Certificate
	if tlskey != "" && tlscert != "" {
		c, err := tls.LoadX509KeyPair(tlscert, tlskey)
		if err != nil {
			clierr("error loading tls cert/key: %v", err)
		}
		cert = c
	}

	mr, err := maxResources.process()
	if err != nil {
		clierr("error in max resource limits: %v", err)
	}

	gopts := grpc.Options{
		TraceSvcName:            tracesvcname,
		CredentialDecryptionKey: dbConfig.CredEncKeyArray,
		Cache:                   cachedefaults(),
		TLSCertificate:          cert,
		LogFunc:                 log.Printf,
		MaxResources:            *mr,
	}

	grs := grpc.Server{
		DL:        dl,
		BM:        bm,
		CFFactory: func(tkn string) models.CodeFetcher { return github.NewGitHubFetcher(tkn) },
		Opts:      gopts,
	}

	log.Println(grs.Listen(serverConfig.GRPCAddr))

}
