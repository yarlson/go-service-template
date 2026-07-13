package usersjobs

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
)

const QueueUsers = "users"

type ImportArgs struct {
	ImportID uuid.UUID `json:"importId"`
}

func (ImportArgs) Kind() string { return "users.import" }

type CleanupImportsArgs struct{}

func (CleanupImportsArgs) Kind() string { return "users.cleanup-imports" }

type ImportProcessor interface {
	Process(context.Context, uuid.UUID) error
}

type ImportCleaner interface {
	Cleanup(context.Context, time.Time) (int64, error)
}

type ImportWorker struct {
	river.WorkerDefaults[ImportArgs]
	imports ImportProcessor
}

func NewImportWorker(imports ImportProcessor) *ImportWorker {
	return &ImportWorker{imports: imports}
}

func (w *ImportWorker) Work(ctx context.Context, job *river.Job[ImportArgs]) error {
	if err := w.imports.Process(ctx, job.Args.ImportID); err != nil {
		return fmt.Errorf("process user import: %w", err)
	}
	return nil
}

type CleanupImportsWorker struct {
	river.WorkerDefaults[CleanupImportsArgs]
	imports ImportCleaner
	logger  *slog.Logger
}

func NewCleanupImportsWorker(logger *slog.Logger, imports ImportCleaner) *CleanupImportsWorker {
	return &CleanupImportsWorker{imports: imports, logger: logger}
}

func (w *CleanupImportsWorker) Work(ctx context.Context, _ *river.Job[CleanupImportsArgs]) error {
	deleted, err := w.imports.Cleanup(ctx, time.Now().UTC().Add(-7*24*time.Hour))
	if err != nil {
		return fmt.Errorf("cleanup completed user imports: %w", err)
	}
	w.logger.InfoContext(ctx, "cleaned up user imports", "deleted_count", deleted)
	return nil
}

type Enqueuer struct {
	client *river.Client[pgx.Tx]
}

func NewEnqueuer(client *river.Client[pgx.Tx]) *Enqueuer {
	return &Enqueuer{client: client}
}

func (e *Enqueuer) EnqueueImport(ctx context.Context, tx pgx.Tx, importID uuid.UUID) error {
	_, err := e.client.InsertTx(ctx, tx, ImportArgs{ImportID: importID}, &river.InsertOpts{
		Queue:       QueueUsers,
		MaxAttempts: 5,
	})
	if err != nil {
		return fmt.Errorf("insert users.import job: %w", err)
	}
	return nil
}

func PeriodicCleanup() *river.PeriodicJob {
	return river.NewPeriodicJob(dailyAtUTC{hour: 2}, func() (river.JobArgs, *river.InsertOpts) {
		return CleanupImportsArgs{}, &river.InsertOpts{Queue: QueueUsers, MaxAttempts: 5}
	}, &river.PeriodicJobOpts{ID: "users.cleanup-imports"})
}

type dailyAtUTC struct {
	hour int
}

func (s dailyAtUTC) Next(current time.Time) time.Time {
	current = current.UTC()
	next := time.Date(current.Year(), current.Month(), current.Day(), s.hour, 0, 0, 0, time.UTC)
	if !next.After(current) {
		next = next.AddDate(0, 0, 1)
	}
	return next
}
