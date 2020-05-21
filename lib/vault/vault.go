package vault

import (
	"io/ioutil"
	"log"
	"os"

	"github.com/dollarshaveclub/pvc"
	"github.com/pkg/errors"

	"github.com/dollarshaveclub/furan/lib/config"
)

func getVaultClient(vaultConfig *config.Vaultconfig) (*pvc.SecretsClient, error) {
	var opts []pvc.SecretsClientOption
	if vaultConfig.Addr == "" {
		return nil, errors.New("vault addr missing")
	}
	switch {
	case vaultConfig.TokenAuth:
		if vaultConfig.Token == "" {
			return nil, errors.New("token auth specified but token is empty")
		}
		opts = []pvc.SecretsClientOption{pvc.WithVaultBackend(), pvc.WithVaultAuthentication(pvc.Token), pvc.WithVaultToken(vaultConfig.Token)}
	case vaultConfig.AppID != "" && vaultConfig.UserIDPath != "":
		opts = []pvc.SecretsClientOption{pvc.WithVaultBackend(), pvc.WithVaultAuthentication(pvc.AppID), pvc.WithVaultAppID(vaultConfig.AppID), pvc.WithVaultUserIDPath(vaultConfig.UserIDPath)}
	case vaultConfig.K8sJWTPath != "" && vaultConfig.K8sRole != "":
		jwt, err := ioutil.ReadFile(vaultConfig.K8sJWTPath)
		if err != nil {
			return nil, errors.Wrap(err, "error reading JWT file")
		}
		opts = []pvc.SecretsClientOption{pvc.WithVaultBackend(), pvc.WithVaultAuthentication(pvc.K8s), pvc.WithVaultK8sAuth(string(jwt), vaultConfig.K8sRole), pvc.WithVaultK8sAuthPath(vaultConfig.K8sAuthPath)}
	default:
		return nil, errors.New("no authentication method was provided")
	}
	opts = append(opts, pvc.WithVaultHost(vaultConfig.Addr))
	opts = append(opts, pvc.WithVaultAuthRetries(5))
	opts = append(opts, pvc.WithVaultAuthRetryDelay(2))
	opts = append(opts, pvc.WithMapping(vaultConfig.VaultPathPrefix+"/{{ .ID }}"))
	return pvc.NewSecretsClient(opts...)
}

// SetupVault does generic Vault setup (all subcommands)
func SetupVault(vaultConfig *config.Vaultconfig, awsConfig *config.AWSConfig, dockerConfig *config.Dockerconfig, gitConfig *config.Gitconfig, serverConfig *config.Serverconfig, awscredsprefix string) {
	sc, err := getVaultClient(vaultConfig)
	if err != nil {
		log.Fatalf("Error creating Vault client: %v", err)
	}
	ght, err := sc.Get(gitConfig.TokenVaultPath)
	if err != nil {
		log.Fatalf("Error getting GitHub token: %v", err)
	}
	dcc, err := sc.Get(dockerConfig.DockercfgVaultPath)
	if err != nil {
		log.Fatalf("Error getting dockercfg: %v", err)
	}
	gitConfig.Token = string(ght)
	dockerConfig.DockercfgRaw = string(dcc)
	ak, err := sc.Get(awscredsprefix + "/access_key_id")
	if err != nil {
		log.Fatalf("Error getting AWS access key ID: %v", err)
	}
	sk, err := sc.Get(awscredsprefix + "/secret_access_key")
	if err != nil {
		log.Fatalf("Error getting AWS secret access key: %v", err)
	}
	awsConfig.AccessKeyID = string(ak)
	awsConfig.SecretAccessKey = string(sk)
}

func GetSumoURL(vaultConfig *config.Vaultconfig, serverConfig *config.Serverconfig) {
	sc, err := getVaultClient(vaultConfig)
	if err != nil {
		log.Fatalf("Error creating Vault client: %v", err)
	}
	scu, err := sc.Get(serverConfig.VaultSumoURLPath)
	if err != nil {
		log.Fatalf("Error getting SumoLogic collector URL: %v", err)
	}
	serverConfig.SumoURL = string(scu)
}

// WriteTLSCert fetches the TLS cert and key data from vault and writes to temporary files, the paths of which are returned
func WriteTLSCert(vaultConfig *config.Vaultconfig, serverConfig *config.Serverconfig) (string, string) {
	sc, err := getVaultClient(vaultConfig)
	if err != nil {
		log.Fatalf("Error creating Vault client: %v", err)
	}
	cert, err := sc.Get(serverConfig.VaultTLSCertPath)
	if err != nil {
		log.Fatalf("Error getting TLS certificate: %v", err)
	}
	key, err := sc.Get(serverConfig.VaultTLSKeyPath)
	if err != nil {
		log.Fatalf("Error getting TLS key: %v", err)
	}
	cf, err := ioutil.TempFile("", "tls-cert")
	if err != nil {
		log.Fatalf("Error creating TLS certificate temp file: %v", err)
	}
	defer cf.Close()
	_, err = cf.Write(cert)
	if err != nil {
		log.Fatalf("Error writing TLS certificate temp file: %v", err)
	}
	kf, err := ioutil.TempFile("", "tls-key")
	if err != nil {
		log.Fatalf("Error creating TLS key temp file: %v", err)
	}
	defer kf.Close()
	_, err = kf.Write(key)
	if err != nil {
		log.Fatalf("Error writing TLS key temp file: %v", err)
	}
	return cf.Name(), kf.Name()
}

// RmTempFiles cleans up temp files
func RmTempFiles(f1 string, f2 string) {
	for _, v := range []string{f1, f2} {
		err := os.Remove(v)
		if err != nil {
			log.Printf("Error removing file: %v", v)
		}
	}
}
