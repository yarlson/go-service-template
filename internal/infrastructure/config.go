package infrastructure

import (
	"fmt"
	"github.com/joho/godotenv"
	"github.com/kelseyhightower/envconfig"
	"strconv"
)

// IntRange represents a constrained integer value within a specific range.
type IntRange struct {
	Value int
	Min   int
	Max   int
}

// Decode validates and sets the integer value ensuring it's within the defined range.
func (ir *IntRange) Decode(value string) error {
	i, err := strconv.Atoi(value)
	if err != nil {
		return err
	}

	if i < ir.Min || i > ir.Max {
		return fmt.Errorf("value %d is not in range %d-%d", i, ir.Min, ir.Max)
	}

	ir.Value = i
	return nil
}

// StringEnum represents a string value constrained to a predefined set of values
type StringEnum struct {
	Value string
	Enum  []string
}

// Decode validates and sets the string value ensuring it's one of the predefined enum values.
func (se *StringEnum) Decode(value string) error {
	for _, v := range se.Enum {
		if v == value {
			se.Value = value
			return nil
		}
	}

	return fmt.Errorf("value %s not in enum %v", value, se.Enum)
}

// DbConfig defines the parameters required for database connectivity.
type DbConfig struct {
	DatabaseUrl string `envconfig:"DATABASE_URL" required:"true"`
}

// MetricsConfig captures whether application metrics are enabled or not.
type MetricsConfig struct {
	Port        int    `envconfig:"METRICS_PORT" default:"2112"`
	BindAddress string `envconfig:"METRICS_BIND_ADDRESS" default:"0.0.0.0"`
	IsEnabled   bool   `envconfig:"METRICS_ENABLED" default:"true"`
}

// AppConfig groups together configuration for application-level parameters.
type AppConfig struct {
	Port         int        `envconfig:"APP_PORT" default:"3000"`
	BindAddress  string     `envconfig:"APP_BIND_ADDRESS" default:"0.0.0.0"`
	JwtPublicKey string     `envconfig:"JWT_PUBLIC_KEY" required:"true"`
	LogLevel     StringEnum `envconfig:"LOG_LEVEL" required:"true"`
	AppVersion   string     `envconfig:"APP_VERSION" default:"VERSION_NOT_SET"`
	GitCommitSha string     `envconfig:"GIT_COMMIT_SHA" default:"COMMIT_SHA_NOT_SET"`
}

// RedisConfig contains parameters for establishing a connection to a Redis instance.
type RedisConfig struct {
	Host           string   `envconfig:"REDIS_HOST" required:"true"`
	Db             IntRange `envconfig:"REDIS_DB" required:"true"`
	Port           int      `envconfig:"REDIS_PORT" required:"true"`
	Username       string   `envconfig:"REDIS_USERNAME" default:""`
	Password       string   `envconfig:"REDIS_PASSWORD" default:""`
	IsTlsEnabled   bool     `envconfig:"REDIS_TLS_ENABLED" default:"true"`
	CommandTimeout int      `envconfig:"REDIS_COMMAND_TIMEOUT" default:"0"`
	ConnectTimeout int      `envconfig:"REDIS_CONNECT_TIMEOUT" default:"0"`
}

// Config aggregates configurations from different components of the application.
type Config struct {
	App     AppConfig
	Db      DbConfig
	Metrics MetricsConfig
	Redis   RedisConfig
}

// LoadDefaultConfig loads configurations from environment variables, including defaults for specific fields.
func LoadDefaultConfig() (*Config, error) {
	err := godotenv.Load()
	if err != nil {
		log.Warn("No .env file found")
	} else {
		log.Info("Loaded .env file")
	}

	config := NewConfig()

	if err := envconfig.Process("", config); err != nil {
		return nil, fmt.Errorf("Failed to load configuration: %s", err)
	}

	return config, nil
}

// NewConfig initializes a configuration object with default settings.
func NewConfig() *Config {
	var cfg Config

	cfg.Redis.Db.Min = 0
	cfg.Redis.Db.Max = 15
	cfg.App = AppConfig{
		LogLevel: StringEnum{
			Enum: []string{"debug", "info", "warn", "error"},
		},
	}

	return &cfg
}
