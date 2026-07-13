package config

import (
	"bufio"
	"log/slog"
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
		LogLevel:        LogLevelInfo,
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
		"invalid log level":   {change: func(c *Config) { c.LogLevel = "trace" }},
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

func TestLoadParsesLogLevel(t *testing.T) {
	t.Setenv("APP_ENV", EnvironmentTest)
	t.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5432/service?sslmode=disable")
	t.Setenv("AUTH_MODE", AuthModeDisabled)
	t.Setenv("LOG_LEVEL", "debug")

	cfg, err := Load()
	require.NoError(t, err)
	assert.Equal(t, LogLevelDebug, cfg.LogLevel)
	assert.Equal(t, slog.LevelDebug, cfg.LogLevel.SlogLevel())
}

func TestLoadWorkerDoesNotRequireAPIConfiguration(t *testing.T) {
	t.Setenv("APP_ENV", EnvironmentProduction)
	t.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5432/service?sslmode=disable")
	t.Setenv("AUTH_MODE", "")
	t.Setenv("OIDC_ISSUER_URL", "")
	t.Setenv("OIDC_AUDIENCE", "")
	t.Setenv("USER_EVENTS_TOPIC_ARN", "arn:aws:sns:eu-west-1:123456789012:user-events")
	t.Setenv("PERMISSIONS_QUEUE_URL", "https://sqs.eu-west-1.amazonaws.com/123456789012/permissions")

	cfg, err := LoadWorker()
	require.NoError(t, err)
	assert.Equal(t, EnvironmentProduction, cfg.Environment)
}

func TestWorkerProductionRequiresAWSConfiguration(t *testing.T) {
	t.Parallel()

	valid := WorkerConfig{
		Environment:      EnvironmentProduction,
		ServiceName:      "test-service",
		LogLevel:         LogLevelInfo,
		DatabaseURL:      "postgres://user:pass@localhost:5432/service?sslmode=disable",
		ShutdownTimeout:  10 * time.Second,
		AWSRegion:        "eu-west-1",
		UserEventsTopic:  "arn:aws:sns:eu-west-1:123456789012:user-events",
		PermissionsQueue: "https://sqs.eu-west-1.amazonaws.com/123456789012/permissions",
	}
	require.NoError(t, valid.Validate())

	missingTopic := valid
	missingTopic.UserEventsTopic = ""
	require.Error(t, missingTopic.Validate())

	customEndpoint := valid
	customEndpoint.AWSEndpointURL = "http://localstack:4566"
	require.Error(t, customEndpoint.Validate())

	missingQueue := valid
	missingQueue.PermissionsQueue = ""
	require.Error(t, missingQueue.Validate())
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
	for _, configType := range []reflect.Type{reflect.TypeFor[Config](), reflect.TypeFor[WorkerConfig]()} {
		for index := range configType.NumField() {
			field := configType.Field(index)
			tag := field.Tag.Get("env")
			require.NotEmpty(t, tag, "%s.%s must declare an env tag", configType.Name(), field.Name)
			name, _, _ := strings.Cut(tag, ",")
			require.NotEmpty(t, name, "%s.%s has an empty env tag", configType.Name(), field.Name)
			variables[name] = struct{}{}
		}
	}

	return variables
}
