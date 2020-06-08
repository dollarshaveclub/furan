package pvc

import (
	"fmt"
	"os"
	"strings"
)

// Default mapping for this backend
const (
	DefaultEnvVarMapping = "SECRET_{{ .ID }}" // DefaultEnvVarMapping is uppercased after interpolation for convenience
)

type envVarBackendGetter struct {
	mapper SecretMapper
	config *envVarBackend
}

func newEnvVarBackendGetter(eb *envVarBackend) (*envVarBackendGetter, error) {
	if eb.mapping == "" {
		eb.mapping = DefaultEnvVarMapping
	}
	sm, err := newSecretMapper(eb.mapping)
	if err != nil {
		return nil, fmt.Errorf("error with mapping: %v", err)
	}
	return &envVarBackendGetter{
		mapper: sm,
		config: eb,
	}, nil
}

// sanitizeName replaces any illegal characters with underscores
func (ebg *envVarBackendGetter) sanitizeName(name string) string {
	name = strings.ToUpper(name)
	f := func(r rune) rune {
		i := int(r)
		switch {
		// rune is alphanumeric or an underscore
		case (i > 64 && i < 91) || (i > 47 && i < 58) || i == 95:
			return r
		default:
			return '_'
		}
	}
	return strings.Map(f, name)
}

func (ebg *envVarBackendGetter) Get(id string) ([]byte, error) {
	vname, err := ebg.mapper.MapSecret(id)
	if err != nil {
		return nil, fmt.Errorf("error mapping id to var name: %v", err)
	}
	vname = ebg.sanitizeName(vname)
	secret, exists := os.LookupEnv(vname)
	if !exists {
		return nil, fmt.Errorf("secret not found: %v", vname)
	}
	return []byte(secret), nil
}
