package config

import (
	"bufio"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidate(t *testing.T) {
	t.Parallel()

	valid := Config{
		Environment:     EnvironmentProduction,
		ServiceName:     "test-service",
		HTTPAddress:     ":8080",
		DatabaseURL:     "postgres://user:pass@localhost:5432/service?sslmode=disable",
		AuthMode:        AuthModeOIDC,
		OIDCIssuerURL:   "https://identity.example.com",
		OIDCAudience:    "service-api",
		ShutdownTimeout: 10 * time.Second,
	}

	tests := map[string]struct {
		change func(*Config)
	}{
		"invalid environment": {change: func(c *Config) { c.Environment = "staging" }},
		"invalid address":     {change: func(c *Config) { c.HTTPAddress = "8080" }},
		"invalid database":    {change: func(c *Config) { c.DatabaseURL = "mysql://localhost/db" }},
		"unknown auth mode":   {change: func(c *Config) { c.AuthMode = "basic" }},
		"disabled production auth": {
			change: func(c *Config) { c.AuthMode = AuthModeDisabled },
		},
		"missing OIDC audience": {
			change: func(c *Config) { c.OIDCAudience = "" },
		},
		"invalid shutdown timeout": {
			change: func(c *Config) { c.ShutdownTimeout = 0 },
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			cfg := valid
			test.change(&cfg)
			assert.Error(t, cfg.Validate())
		})
	}

	require.NoError(t, valid.Validate())
}

func TestEnvironmentExampleMatchesConfig(t *testing.T) {
	t.Parallel()

	file, err := os.Open(filepath.Join("..", "..", "..", ".env.example"))
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, file.Close())
	})

	documented := make(map[string]struct{})
	scanner := bufio.NewScanner(file)
	for lineNumber := 1; scanner.Scan(); lineNumber++ {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		name, _, found := strings.Cut(line, "=")
		require.True(t, found, ".env.example:%d must use NAME=value syntax", lineNumber)
		name = strings.TrimSpace(name)
		require.NotEmpty(t, name, ".env.example:%d has an empty variable name", lineNumber)
		require.NotContains(t, documented, name, ".env.example:%d duplicates %s", lineNumber, name)
		documented[name] = struct{}{}
	}
	require.NoError(t, scanner.Err())

	assert.Equal(t, configEnvironmentVariables(t), documented)
}

func configEnvironmentVariables(t *testing.T) map[string]struct{} {
	t.Helper()

	variables := make(map[string]struct{})
	configType := reflect.TypeFor[Config]()
	for index := range configType.NumField() {
		field := configType.Field(index)
		tag := field.Tag.Get("env")
		require.NotEmpty(t, tag, "Config.%s must declare an env tag", field.Name)
		name, _, _ := strings.Cut(tag, ",")
		require.NotEmpty(t, name, "Config.%s has an empty env tag", field.Name)
		variables[name] = struct{}{}
	}

	return variables
}
