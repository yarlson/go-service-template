//go:build integration

package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivertype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/your-org/go-service-template/internal/platform/database"
	"github.com/your-org/go-service-template/internal/platform/messaging"
	"github.com/your-org/go-service-template/internal/users"
	usersjobs "github.com/your-org/go-service-template/internal/users/jobs"
)

func TestUserRepositoryIntegration(t *testing.T) {
	pool := newTestPool(t)
	jobClient, err := river.NewClient(riverpgxv5.New(pool), &river.Config{})
	require.NoError(t, err)
	repository := NewUserRepository(pool, usersjobs.NewEnqueuer(jobClient))
	want := users.User{
		ID:    uuid.MustParse("8d37b313-f867-47bc-8e3d-0953db9c05c8"),
		Email: "person@example.com",
	}
	created, err := repository.Create(messaging.WithCorrelationID(t.Context(), "request-123"), want)
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

	jobs, err := jobClient.JobList(t.Context(), river.NewJobListParams().Kinds("users.publish-created"))
	require.NoError(t, err)
	require.Len(t, jobs.Jobs, 1)
	assert.NotContains(t, string(jobs.Jobs[0].EncodedArgs), want.Email)
	assert.Contains(t, string(jobs.Jobs[0].EncodedArgs), "request-123")
}

func TestImportRepositoryIntegration(t *testing.T) {
	pool := newTestPool(t)
	jobClient, err := river.NewClient(riverpgxv5.New(pool), &river.Config{})
	require.NoError(t, err)
	repository := NewImportRepository(pool, usersjobs.NewEnqueuer(jobClient))

	importID := uuid.MustParse("0198a1f7-30b7-7df1-8491-c47f6033525b")
	entries := []users.ImportEntry{
		{UserID: uuid.MustParse("0198a1f7-30b7-7df2-8491-c47f6033525b"), Email: "one@example.com"},
		{UserID: uuid.MustParse("0198a1f7-30b7-7df3-8491-c47f6033525b"), Email: "two@example.com"},
	}
	created, err := repository.CreateImport(t.Context(), users.Import{ID: importID, TotalCount: len(entries), Entries: entries})
	require.NoError(t, err)
	assert.Equal(t, users.ImportStatePending, created.State)

	jobs, err := jobClient.JobList(t.Context(), river.NewJobListParams().Kinds("users.import"))
	require.NoError(t, err)
	require.Len(t, jobs.Jobs, 1)
	assert.Equal(t, usersjobs.QueueUsers, jobs.Jobs[0].Queue)
	assert.NotContains(t, string(jobs.Jobs[0].EncodedArgs), entries[0].Email)

	require.NoError(t, repository.ProcessImport(t.Context(), importID))
	require.NoError(t, repository.ProcessImport(t.Context(), importID))
	completed, err := repository.GetImport(t.Context(), importID)
	require.NoError(t, err)
	assert.Equal(t, users.ImportStateCompleted, completed.State)
	assert.Equal(t, 2, completed.CompletedCount)
	assert.Zero(t, completed.FailedCount)
	assert.NotNil(t, completed.StartedAt)
	assert.NotNil(t, completed.FinishedAt)

	for _, entry := range entries {
		user, err := New(pool).GetUser(t.Context(), entry.UserID)
		require.NoError(t, err)
		assert.Equal(t, entry.Email, user.Email)
	}
	publicationJobs, err := jobClient.JobList(t.Context(), river.NewJobListParams().Kinds("users.publish-created"))
	require.NoError(t, err)
	assert.Len(t, publicationJobs.Jobs, 2)
}

func TestUserRepositoryRollsBackWhenPublicationEnqueueFails(t *testing.T) {
	pool := newTestPool(t)
	repository := NewUserRepository(pool, failingImportEnqueuer{})
	userID := uuid.MustParse("0198a1f7-30b7-7df6-8491-c47f6033525b")

	_, err := repository.Create(t.Context(), users.User{ID: userID, Email: "atomic@example.com"})
	require.Error(t, err)
	_, err = New(pool).GetUser(t.Context(), userID)
	require.ErrorIs(t, err, pgx.ErrNoRows)
}

func TestImportRepositoryRollsBackWhenEnqueueFails(t *testing.T) {
	pool := newTestPool(t)
	repository := NewImportRepository(pool, failingImportEnqueuer{})
	importID := uuid.MustParse("0198a1f7-30b7-7df4-8491-c47f6033525b")

	_, err := repository.CreateImport(t.Context(), users.Import{
		ID: importID, TotalCount: 1,
		Entries: []users.ImportEntry{{UserID: uuid.New(), Email: "rollback@example.com"}},
	})
	require.Error(t, err)
	_, err = repository.GetImport(t.Context(), importID)
	require.ErrorIs(t, err, users.ErrNotFound)
}

func TestImportRepositoryMarksConflictingEmailFailedAndCleansItUp(t *testing.T) {
	pool := newTestPool(t)
	jobClient, err := river.NewClient(riverpgxv5.New(pool), &river.Config{})
	require.NoError(t, err)
	repository := NewImportRepository(pool, usersjobs.NewEnqueuer(jobClient))
	_, err = NewUserRepository(pool, usersjobs.NewEnqueuer(jobClient)).Create(t.Context(), users.User{ID: uuid.New(), Email: "existing@example.com"})
	require.NoError(t, err)

	importID := uuid.MustParse("0198a1f7-30b7-7df5-8491-c47f6033525b")
	_, err = repository.CreateImport(t.Context(), users.Import{
		ID: importID, TotalCount: 1,
		Entries: []users.ImportEntry{{UserID: uuid.New(), Email: "existing@example.com"}},
	})
	require.NoError(t, err)
	require.NoError(t, repository.ProcessImport(t.Context(), importID))

	failed, err := repository.GetImport(t.Context(), importID)
	require.NoError(t, err)
	assert.Equal(t, users.ImportStateFailed, failed.State)
	assert.Zero(t, failed.CompletedCount)
	assert.Equal(t, 1, failed.FailedCount)

	_, err = pool.Exec(t.Context(), "UPDATE user_imports SET finished_at = $2 WHERE id = $1", importID, time.Now().Add(-8*24*time.Hour))
	require.NoError(t, err)
	deleted, err := repository.DeleteFinishedImportsBefore(t.Context(), time.Now().Add(-7*24*time.Hour))
	require.NoError(t, err)
	assert.Equal(t, int64(1), deleted)
}

func TestImportJobReachesFinalFailureState(t *testing.T) {
	pool := newTestPool(t)
	workers := river.NewWorkers()
	client, err := river.NewClient(riverpgxv5.New(pool), &river.Config{
		Queues:  map[string]river.QueueConfig{usersjobs.QueueUsers: {MaxWorkers: 1}},
		Workers: workers,
	})
	require.NoError(t, err)
	repository := NewImportRepository(pool, usersjobs.NewEnqueuer(client))
	service := users.NewImportService(repository)
	river.AddWorker(workers, usersjobs.NewImportWorker(service))

	events, unsubscribe := client.Subscribe(river.EventKindJobFailed)
	defer unsubscribe()
	require.NoError(t, client.Start(t.Context()))
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		require.NoError(t, client.Stop(ctx))
	})

	inserted, err := client.Insert(t.Context(), usersjobs.ImportArgs{ImportID: uuid.New()}, &river.InsertOpts{
		Queue:       usersjobs.QueueUsers,
		MaxAttempts: 1,
	})
	require.NoError(t, err)

	select {
	case event := <-events:
		assert.Equal(t, inserted.Job.ID, event.Job.ID)
		assert.Equal(t, rivertype.JobStateDiscarded, event.Job.State)
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for failed job")
	}
}

type failingImportEnqueuer struct{}

func (failingImportEnqueuer) EnqueueImport(context.Context, pgx.Tx, uuid.UUID) error {
	return errors.New("enqueue failed")
}

func (failingImportEnqueuer) EnqueueUserCreated(context.Context, pgx.Tx, uuid.UUID, string) error {
	return errors.New("enqueue failed")
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
