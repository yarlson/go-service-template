package infrastructure

import (
	"github.com/stretchr/testify/assert"
	"os"
	"testing"
)

func TestIntRange_Decode(t *testing.T) {
	type fields struct {
		Value int
		Min   int
		Max   int
	}
	type args struct {
		value string
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		want    int
		wantErr bool
	}{
		{
			"Valid Value",
			fields{0, 0, 20},
			args{"10"},
			10,
			false,
		},
		{
			"Out of Range Value",
			fields{0, 0, 20},
			args{"25"},
			25,
			true,
		},
		{
			"Invalid Integer",
			fields{0, 0, 20},
			args{"abc"},
			0,
			true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ir := &IntRange{
				Value: tt.fields.Value,
				Min:   tt.fields.Min,
				Max:   tt.fields.Max,
			}

			err := ir.Decode(tt.args.value)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)
			assert.Equal(t, tt.want, ir.Value)
		})
	}
}

func TestStringEnum_Decode(t *testing.T) {
	type fields struct {
		Value string
		Enum  []string
	}
	type args struct {
		value string
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		wantErr bool
	}{
		{
			"Valid Value",
			fields{"", []string{"debug", "info", "warn", "error"}},
			args{"info"},
			false,
		},
		{
			"Invalid Value",
			fields{"", []string{"debug", "info", "warn", "error"}},
			args{"trace"},
			true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			se := &StringEnum{
				Value: tt.fields.Value,
				Enum:  tt.fields.Enum,
			}

			err := se.Decode(tt.args.value)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)
			assert.Equal(t, tt.args.value, se.Value)
		})
	}
}

func TestNewConfig(t *testing.T) {
	cfg := NewConfig()
	assert.NotNil(t, cfg)
	assert.Equal(t, 0, cfg.Redis.Db.Min)
	assert.Equal(t, 15, cfg.Redis.Db.Max)
	assert.Equal(t, []string{"debug", "info", "warn", "error"}, cfg.App.LogLevel.Enum)
}

func TestLoadDefaultConfig(t *testing.T) {
	// Set up environment variables as needed by the test
	_ = os.Setenv("DATABASE_URL", "test_db_url")
	_ = os.Setenv("REDIS_HOST", "localhost")
	_ = os.Setenv("REDIS_DB", "0")
	_ = os.Setenv("REDIS_PORT", "6379")
	_ = os.Setenv("JWT_PUBLIC_KEY", "test_key")
	_ = os.Setenv("LOG_LEVEL", "debug")

	// Load the default configuration
	cfg, err := LoadDefaultConfig()

	// Verify no error occurred
	assert.NoError(t, err)

	// Verify some of the expected values
	assert.Equal(t, "test_db_url", cfg.Db.DatabaseUrl)
	assert.Equal(t, "localhost", cfg.Redis.Host)
	assert.Equal(t, 0, cfg.Redis.Db.Value)
	assert.Equal(t, "test_key", cfg.App.JwtPublicKey)
	assert.Equal(t, "debug", cfg.App.LogLevel.Value)
}

func TestLoadDefaultConfigError(t *testing.T) {
	// Set up environment variables as needed by the test
	_ = os.Setenv("DATABASE_URL", "test_db_url")
	_ = os.Setenv("REDIS_HOST", "localhost")
	_ = os.Setenv("REDIS_DB", "0")
	_ = os.Unsetenv("REDIS_PORT")
	_ = os.Setenv("JWT_PUBLIC_KEY", "test_key")
	_ = os.Setenv("LOG_LEVEL", "debug")

	// Load the default configuration
	_, err := LoadDefaultConfig()

	// Verify if the error is as expected
	assert.Error(t, err)
}
