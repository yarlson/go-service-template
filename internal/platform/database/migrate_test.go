package database

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMigrationURL(t *testing.T) {
	t.Parallel()

	got, err := migrationURL("postgres://user:pass@localhost:5432/service?sslmode=disable")
	require.NoError(t, err)
	const want = "pgx5://user:pass@localhost:5432/service?sslmode=disable"
	assert.Equal(t, want, got)
}
