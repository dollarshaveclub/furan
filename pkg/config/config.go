package config

/*
"secret" struct tags are secret ids for PVC auto-fill
*/

type VaultConfig struct {
	Addr        string
	Token       string
	TokenAuth   bool
	K8sAuth     bool
	K8sJWTPath  string
	K8sAuthPath string
	K8sRole     string
}

type GitHubConfig struct {
	Token string `secret:"github/token"`
}

type QuayConfig struct {
	Token string `secret:"quay/token"`
}

// AWSConfig contains all information needed to access AWS services
type AWSConfig struct {
	Region           string
	CacheBucket      string
	CacheKeyPrefix   string
	AccessKeyID      string `secret:"aws/access_key_id"`
	SecretAccessKey  string `secret:"aws/secret_access_key"`
	EnableECR        bool
	ECRRegistryHosts []string
}

type DBConfig struct {
	PostgresURI             string `secret:"db/uri"`
	CredentialEncryptionKey []byte `secret:"db/credential_encryption_key"`
	CredEncKeyArray         [32]byte
}

type APMConfig struct {
	Addr string
	App, Environment string
	APM, Profiling bool
}

type ServerConfig struct {
	HTTPSAddr string
	GRPCAddr  string
}
