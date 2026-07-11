package config

import (
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
