//go:build integration

package postgres

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/your-org/go-service-template/internal/platform/database"
	"github.com/your-org/go-service-template/internal/users"
)

func TestUserRepositoryIntegration(t *testing.T) {
	pool := newTestPool(t)
	repository := NewUserRepository(New(pool))
	want := users.User{
		ID:    uuid.MustParse("8d37b313-f867-47bc-8e3d-0953db9c05c8"),
		Email: "person@example.com",
	}
	created, err := repository.Create(t.Context(), want)
	require.NoError(t, err)
	assert.Equal(t, want.ID, created.ID)
	assert.Equal(t, want.Email, created.Email)
	assert.False(t, created.CreatedAt.IsZero())

	got, err := repository.Get(t.Context(), want.ID)
	require.NoError(t, err)
	assert.Equal(t, created, got)

	_, err = repository.Create(t.Context(), users.User{ID: uuid.New(), Email: want.Email})
	require.ErrorIs(t, err, users.ErrConflict)

	_, err = repository.Get(t.Context(), uuid.New())
	require.ErrorIs(t, err, users.ErrNotFound)
}

func newTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	container, err := tcpostgres.Run(t.Context(),
		"postgres:18.4-alpine",
		tcpostgres.WithDatabase("service_test"),
		tcpostgres.WithUsername("serviceuser"),
		tcpostgres.WithPassword("pass"),
		tcpostgres.BasicWaitStrategies(),
	)
	require.NoError(t, err)
	testcontainers.CleanupContainer(t, container)

	databaseURL, err := container.ConnectionString(t.Context(), "sslmode=disable")
	require.NoError(t, err)
	require.NoError(t, database.Migrate(databaseURL))

	pool, err := pgxpool.New(t.Context(), databaseURL)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	return pool
}
