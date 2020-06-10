package config

type GitConfig struct {
	Token string // GitHub token
}

// AWSConfig contains all information needed to access AWS services
type AWSConfig struct {
	AccessKeyID      string
	SecretAccessKey  string
	EnableECR        bool
	ECRRegistryHosts []string
}

type DBConfig struct {
	PostgresURI string
}

type ServerConfig struct {
	HTTPSPort       uint
	GRPCPort        uint
	PPROFPort       uint
	HTTPSAddr       string
	GRPCAddr        string
	HealthcheckAddr string
}
