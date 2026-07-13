package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"

	"github.com/your-org/go-service-template/internal/platform/config"
	"github.com/your-org/go-service-template/internal/platform/telemetry"
	"github.com/your-org/go-service-template/internal/users"
	usersjobs "github.com/your-org/go-service-template/internal/users/jobs"
	userspostgres "github.com/your-org/go-service-template/internal/users/postgres"
)

func RunWorker(ctx context.Context, cfg config.WorkerConfig, logger *slog.Logger, build BuildInfo) error {
	telemetryRuntime, err := telemetry.Setup(ctx, cfg.ServiceName, build.Version, cfg.OTLPHTTPEndpoint)
	if err != nil {
		return err
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := telemetryRuntime.Shutdown(shutdownCtx); err != nil {
			logger.Error("shutdown telemetry", "error", err)
		}
	}()

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("create PostgreSQL pool: %w", err)
	}
	defer pool.Close()

	startupCtx, startupCancel := context.WithTimeout(ctx, 10*time.Second)
	defer startupCancel()
	if err := pool.Ping(startupCtx); err != nil {
		return fmt.Errorf("connect to PostgreSQL: %w", err)
	}

	workers := river.NewWorkers()
	client, err := river.NewClient(riverpgxv5.New(pool), &river.Config{
		Logger: logger,
		Queues: map[string]river.QueueConfig{
			usersjobs.QueueUsers: {MaxWorkers: 5},
		},
		PeriodicJobs: []*river.PeriodicJob{usersjobs.PeriodicCleanup()},
		Workers:      workers,
	})
	if err != nil {
		return fmt.Errorf("create River worker client: %w", err)
	}

	importRepository := userspostgres.NewImportRepository(pool, usersjobs.NewEnqueuer(client))
	importService := users.NewImportService(importRepository)
	river.AddWorker(workers, usersjobs.NewImportWorker(importService))
	river.AddWorker(workers, usersjobs.NewCleanupImportsWorker(logger, importService))

	workerCtx, cancelWorker := context.WithCancel(context.WithoutCancel(ctx))
	defer cancelWorker()
	if err := client.Start(workerCtx); err != nil {
		return fmt.Errorf("start River worker: %w", err)
	}
	logger.Info("worker started", "queue", usersjobs.QueueUsers, "max_workers", 5)

	<-ctx.Done()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer shutdownCancel()
	if err := client.Stop(shutdownCtx); err != nil {
		cancelWorker()
		return errors.Join(fmt.Errorf("stop River worker: %w", err), context.Cause(ctx))
	}
	return nil
}
