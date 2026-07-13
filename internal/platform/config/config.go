package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"strconv"
	"time"

	"github.com/caarlos0/env/v11"
)

const (
	EnvironmentDevelopment = "development"
	EnvironmentTest        = "test"
	EnvironmentProduction  = "production"

	AuthModeDisabled = "disabled"
	AuthModeOIDC     = "oidc"

	LogLevelDebug = LogLevel("debug")
	LogLevelInfo  = LogLevel("info")
	LogLevelWarn  = LogLevel("warn")
	LogLevelError = LogLevel("error")
)

type LogLevel string

func (l LogLevel) SlogLevel() slog.Level {
	switch l {
	case LogLevelDebug:
		return slog.LevelDebug
	case LogLevelWarn:
		return slog.LevelWarn
	case LogLevelError:
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

type Config struct {
	Environment      string        `env:"APP_ENV" envDefault:"development"`
	ServiceName      string        `env:"SERVICE_NAME" envDefault:"go-service-template"`
	LogLevel         LogLevel      `env:"LOG_LEVEL" envDefault:"info"`
	HTTPAddress      string        `env:"HTTP_ADDRESS" envDefault:":8080"`
	DatabaseURL      string        `env:"DATABASE_URL,required"`
	AuthMode         string        `env:"AUTH_MODE" envDefault:"oidc"`
	OIDCIssuerURL    string        `env:"OIDC_ISSUER_URL"`
	OIDCAudience     string        `env:"OIDC_AUDIENCE"`
	ShutdownTimeout  time.Duration `env:"SHUTDOWN_TIMEOUT" envDefault:"10s"`
	OTLPHTTPEndpoint string        `env:"OTEL_EXPORTER_OTLP_ENDPOINT"`
}

type WorkerConfig struct {
	Environment      string        `env:"APP_ENV" envDefault:"development"`
	ServiceName      string        `env:"SERVICE_NAME" envDefault:"go-service-template"`
	LogLevel         LogLevel      `env:"LOG_LEVEL" envDefault:"info"`
	DatabaseURL      string        `env:"DATABASE_URL,required"`
	ShutdownTimeout  time.Duration `env:"SHUTDOWN_TIMEOUT" envDefault:"10s"`
	OTLPHTTPEndpoint string        `env:"OTEL_EXPORTER_OTLP_ENDPOINT"`
	AWSRegion        string        `env:"AWS_REGION" envDefault:"eu-west-1"`
	AWSEndpointURL   string        `env:"AWS_ENDPOINT_URL"`
	UserEventsTopic  string        `env:"USER_EVENTS_TOPIC_ARN"`
	PermissionsQueue string        `env:"PERMISSIONS_QUEUE_URL"`
}

func Load() (Config, error) {
	var cfg Config
	if err := env.Parse(&cfg); err != nil {
		return Config{}, fmt.Errorf("parse environment: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func LoadWorker() (WorkerConfig, error) {
	var cfg WorkerConfig
	if err := env.Parse(&cfg); err != nil {
		return WorkerConfig{}, fmt.Errorf("parse environment: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return WorkerConfig{}, err
	}
	return cfg, nil
}

func LoadDatabaseURL() (string, error) {
	var values struct {
		DatabaseURL string `env:"DATABASE_URL,required"`
	}
	if err := env.Parse(&values); err != nil {
		return "", fmt.Errorf("parse environment: %w", err)
	}
	if err := validateDatabaseURL(values.DatabaseURL); err != nil {
		return "", fmt.Errorf("DATABASE_URL: %w", err)
	}
	return values.DatabaseURL, nil
}

func LoadHTTPAddress() (string, error) {
	var values struct {
		HTTPAddress string `env:"HTTP_ADDRESS" envDefault:":8080"`
	}
	if err := env.Parse(&values); err != nil {
		return "", fmt.Errorf("parse environment: %w", err)
	}
	if err := validateAddress(values.HTTPAddress); err != nil {
		return "", fmt.Errorf("HTTP_ADDRESS: %w", err)
	}
	return values.HTTPAddress, nil
}

func (c Config) Validate() error {
	if !oneOf(c.Environment, EnvironmentDevelopment, EnvironmentTest, EnvironmentProduction) {
		return fmt.Errorf("APP_ENV must be development, test, or production")
	}
	if c.ServiceName == "" {
		return errors.New("SERVICE_NAME is required")
	}
	if !oneOf(c.LogLevel, LogLevelDebug, LogLevelInfo, LogLevelWarn, LogLevelError) {
		return errors.New("LOG_LEVEL must be debug, info, warn, or error")
	}

	if err := validateAddress(c.HTTPAddress); err != nil {
		return fmt.Errorf("HTTP_ADDRESS: %w", err)
	}

	if err := validateDatabaseURL(c.DatabaseURL); err != nil {
		return fmt.Errorf("DATABASE_URL: %w", err)
	}

	if !oneOf(c.AuthMode, AuthModeDisabled, AuthModeOIDC) {
		return fmt.Errorf("AUTH_MODE must be disabled or oidc")
	}
	if c.AuthMode == AuthModeDisabled && c.Environment == EnvironmentProduction {
		return errors.New("AUTH_MODE=disabled is not allowed in production")
	}
	if c.AuthMode == AuthModeOIDC && (c.OIDCIssuerURL == "" || c.OIDCAudience == "") {
		return errors.New("OIDC_ISSUER_URL and OIDC_AUDIENCE are required when AUTH_MODE=oidc")
	}

	if c.ShutdownTimeout <= 0 {
		return errors.New("SHUTDOWN_TIMEOUT must be greater than zero")
	}

	return nil
}

func (c WorkerConfig) Validate() error {
	if !oneOf(c.Environment, EnvironmentDevelopment, EnvironmentTest, EnvironmentProduction) {
		return fmt.Errorf("APP_ENV must be development, test, or production")
	}
	if c.ServiceName == "" {
		return errors.New("SERVICE_NAME is required")
	}
	if !oneOf(c.LogLevel, LogLevelDebug, LogLevelInfo, LogLevelWarn, LogLevelError) {
		return errors.New("LOG_LEVEL must be debug, info, warn, or error")
	}
	if err := validateDatabaseURL(c.DatabaseURL); err != nil {
		return fmt.Errorf("DATABASE_URL: %w", err)
	}
	if c.ShutdownTimeout <= 0 {
		return errors.New("SHUTDOWN_TIMEOUT must be greater than zero")
	}
	if c.AWSRegion == "" {
		return errors.New("AWS_REGION is required")
	}
	if c.Environment == EnvironmentProduction && c.AWSEndpointURL != "" {
		return errors.New("AWS_ENDPOINT_URL is not allowed in production")
	}
	if c.Environment == EnvironmentProduction && c.UserEventsTopic == "" {
		return errors.New("USER_EVENTS_TOPIC_ARN is required in production")
	}
	if c.Environment == EnvironmentProduction && c.PermissionsQueue == "" {
		return errors.New("PERMISSIONS_QUEUE_URL is required in production")
	}
	return nil
}

func oneOf[T comparable](value T, allowed ...T) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}

func validateAddress(address string) error {
	_, portValue, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("must be in host:port form: %w", err)
	}

	port, err := strconv.Atoi(portValue)
	if err != nil || port < 1 || port > 65535 {
		return errors.New("port must be between 1 and 65535")
	}

	return nil
}

func validateDatabaseURL(value string) error {
	parsed, err := url.Parse(value)
	if err != nil {
		return fmt.Errorf("parse URL: %w", err)
	}
	if parsed.Scheme != "postgres" && parsed.Scheme != "postgresql" {
		return errors.New("scheme must be postgres or postgresql")
	}
	if parsed.Host == "" || parsed.Path == "" {
		return errors.New("host and database name are required")
	}

	return nil
}
