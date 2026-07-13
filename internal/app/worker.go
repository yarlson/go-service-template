package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sns"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"go.opentelemetry.io/contrib/instrumentation/github.com/aws/aws-sdk-go-v2/otelaws"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"github.com/your-org/go-service-template/internal/platform/config"
	"github.com/your-org/go-service-template/internal/platform/httpserver"
	"github.com/your-org/go-service-template/internal/platform/messaging"
	"github.com/your-org/go-service-template/internal/platform/telemetry"
	"github.com/your-org/go-service-template/internal/users"
	usersevents "github.com/your-org/go-service-template/internal/users/events"
	usersjobs "github.com/your-org/go-service-template/internal/users/jobs"
	userspostgres "github.com/your-org/go-service-template/internal/users/postgres"
)

func RunWorker(ctx context.Context, cfg config.WorkerConfig, logger *slog.Logger, build BuildInfo) (runErr error) {
	telemetryRuntime, err := telemetry.Setup(ctx, cfg.ServiceName, build.Version, cfg.OTLPHTTPEndpoint)
	if err != nil {
		return err
	}
	var shutdownDeadline time.Time
	defer func() {
		shutdownCtx, cancel := telemetryShutdownContext(shutdownDeadline)
		defer cancel()
		if err := telemetryRuntime.Shutdown(shutdownCtx); err != nil {
			runErr = errors.Join(runErr, fmt.Errorf("shutdown telemetry: %w", err))
		}
	}()

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("create PostgreSQL pool: %w", err)
	}
	defer pool.Close()

	startupCtx, startupCancel := context.WithTimeout(ctx, 10*time.Second)
	defer startupCancel()

	var snsClient *sns.Client
	var sqsClient *sqs.Client
	if cfg.UserEventsTopic != "" || cfg.PermissionsQueue != "" {
		loadOptions := []func(*awsconfig.LoadOptions) error{awsconfig.WithRegion(cfg.AWSRegion)}
		if cfg.AWSEndpointURL != "" {
			loadOptions = append(loadOptions, awsconfig.WithBaseEndpoint(cfg.AWSEndpointURL))
		}
		awsConfig, loadErr := awsconfig.LoadDefaultConfig(startupCtx, loadOptions...)
		if loadErr != nil {
			return fmt.Errorf("load AWS configuration: %w", loadErr)
		}
		otelaws.AppendMiddlewares(&awsConfig.APIOptions)
		snsClient = sns.NewFromConfig(awsConfig)
		sqsClient = sqs.NewFromConfig(awsConfig)
	}

	dependencies := workerDependencies{
		database: pool, sqs: sqsClient, sns: snsClient, observer: telemetryRuntime,
		queueURL: cfg.PermissionsQueue, topicARN: cfg.UserEventsTopic,
	}
	if err := dependencies.Ping(startupCtx); err != nil {
		return fmt.Errorf("validate worker dependencies: %w", err)
	}

	workers := river.NewWorkers()
	queues := map[string]river.QueueConfig{usersjobs.QueueUsers: {MaxWorkers: 5}}
	var eventPublisher *messaging.SNSTopicPublisher
	var permissionsConsumer *messaging.SQSConsumer
	if snsClient != nil && cfg.UserEventsTopic != "" {
		eventPublisher = messaging.NewSNSTopicPublisher(snsClient, cfg.UserEventsTopic, telemetryRuntime)
		queues[usersjobs.QueueEvents] = river.QueueConfig{MaxWorkers: 10}
	}
	if sqsClient != nil && cfg.PermissionsQueue != "" {
		permissionRepository := userspostgres.NewPermissionRepository(pool)
		permissionService := users.NewPermissionService(permissionRepository)
		permissionHandler := usersevents.NewPermissionHandler(permissionService, func(ctx context.Context, result users.PermissionChangeResult) {
			telemetryRuntime.RecordPermissionOutcome(ctx, string(result))
		})
		permissionsConsumer = messaging.NewSQSConsumer(sqsClient, cfg.PermissionsQueue, permissionHandler, logger, telemetryRuntime)
	}
	if eventPublisher == nil {
		logger.Warn("external event publication disabled", "reason", "USER_EVENTS_TOPIC_ARN is empty")
	}
	if permissionsConsumer == nil {
		logger.Warn("external event consumption disabled", "reason", "PERMISSIONS_QUEUE_URL is empty")
	}

	client, err := river.NewClient(riverpgxv5.New(pool), &river.Config{
		Logger: logger, Queues: queues, PeriodicJobs: []*river.PeriodicJob{usersjobs.PeriodicCleanup()},
		SkipUnknownJobCheck: eventPublisher == nil, Workers: workers,
	})
	if err != nil {
		return fmt.Errorf("create River worker client: %w", err)
	}
	importRepository := userspostgres.NewImportRepository(pool, usersjobs.NewEnqueuer(client))
	importService := users.NewImportService(importRepository)
	river.AddWorker(workers, usersjobs.NewImportWorker(importService))
	river.AddWorker(workers, usersjobs.NewCleanupImportsWorker(logger, importService))
	if eventPublisher != nil {
		river.AddWorker(workers, usersjobs.NewPublishCreatedWorker(eventPublisher, cfg.ServiceName))
	}

	readiness := httpserver.NewPausedReadiness(dependencies, nil)
	operationsHandler, err := httpserver.NewOperationsHandler(httpserver.OperationsHandlerOptions{
		Logger: logger, Readiness: readiness, Metrics: telemetryRuntime.MetricsHandler,
		Version: build.Version, Commit: build.Commit,
	})
	if err != nil {
		return err
	}
	operationsServer := &http.Server{
		Addr: cfg.HTTPAddress,
		Handler: instrumentHTTP(operationsHandler, otelhttp.WithFilter(func(request *http.Request) bool {
			return request.URL.Path != "/metrics"
		})),
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelError),
	}
	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", cfg.HTTPAddress)
	if err != nil {
		return fmt.Errorf("listen for worker operations HTTP: %w", err)
	}

	runtimeCtx := context.WithoutCancel(ctx)
	riverCtx, cancelRiver := context.WithCancel(runtimeCtx)
	defer cancelRiver()
	receiveCtx, stopReceiving := context.WithCancel(runtimeCtx)
	defer stopReceiving()
	handlerCtx, cancelHandlers := context.WithCancel(runtimeCtx)
	defer cancelHandlers()

	if err := client.Start(riverCtx); err != nil {
		_ = listener.Close()
		return fmt.Errorf("start River worker: %w", err)
	}
	serverErrors := make(chan error, 1)
	go func() {
		logger.Info("worker operations server listening", "address", listener.Addr().String())
		serverErrors <- operationsServer.Serve(listener)
	}()
	consumerErrors := make(chan error, 1)
	if permissionsConsumer != nil {
		go func() { consumerErrors <- permissionsConsumer.RunWithHandlerContext(receiveCtx, handlerCtx) }()
	}
	readiness.StartAccepting()
	workerFields := []any{"users_max_workers", 5}
	if eventPublisher != nil {
		workerFields = append(workerFields, "events_max_workers", queues[usersjobs.QueueEvents].MaxWorkers)
	}
	logger.Info("worker started", workerFields...)

	var triggerErr error
	consumerFinished := permissionsConsumer == nil
	select {
	case <-ctx.Done():
	case serverErr := <-serverErrors:
		if !errors.Is(serverErr, http.ErrServerClosed) {
			triggerErr = fmt.Errorf("serve worker operations HTTP: %w", serverErr)
		}
	case consumerErr := <-consumerErrors:
		consumerFinished = true
		if consumerErr != nil {
			triggerErr = fmt.Errorf("run permissions consumer: %w", consumerErr)
		} else {
			triggerErr = errors.New("permissions consumer stopped unexpectedly")
		}
	}

	readiness.StopAccepting()
	stopReceiving()
	shutdownDeadline = time.Now().Add(cfg.ShutdownTimeout)
	shutdownCtx, shutdownCancel := context.WithDeadline(context.Background(), shutdownDeadline)
	defer shutdownCancel()

	shutdownErrors := make(chan error, 2)
	go func() {
		if stopErr := client.Stop(shutdownCtx); stopErr != nil {
			shutdownErrors <- fmt.Errorf("stop River worker: %w", stopErr)
			return
		}
		shutdownErrors <- nil
	}()
	if permissionsConsumer == nil {
		shutdownErrors <- nil
	} else {
		go func() {
			if !consumerFinished {
				if consumerErr := <-consumerErrors; consumerErr != nil {
					shutdownErrors <- fmt.Errorf("stop permissions consumer: %w", consumerErr)
					return
				}
			}
			if waitErr := permissionsConsumer.Wait(shutdownCtx); waitErr != nil {
				shutdownErrors <- fmt.Errorf("drain permissions consumer: %w", waitErr)
				return
			}
			shutdownErrors <- nil
		}()
	}

	for remaining := 2; remaining > 0; remaining-- {
		select {
		case shutdownErr := <-shutdownErrors:
			if shutdownErr != nil {
				runErr = errors.Join(runErr, shutdownErr)
			}
		case <-shutdownCtx.Done():
			cancelHandlers()
			cancelRiver()
			runErr = errors.Join(runErr, fmt.Errorf("drain worker: %w", shutdownCtx.Err()))
			remaining = 0
		}
	}
	if err := operationsServer.Shutdown(shutdownCtx); err != nil {
		closeErr := operationsServer.Close()
		runErr = errors.Join(runErr, fmt.Errorf("shutdown worker operations HTTP: %w", err), closeErr)
	}
	cancelHandlers()
	cancelRiver()
	return errors.Join(triggerErr, runErr)
}

func telemetryShutdownContext(deadline time.Time) (context.Context, context.CancelFunc) {
	if deadline.IsZero() {
		return context.WithTimeout(context.Background(), 5*time.Second)
	}
	return context.WithDeadline(context.Background(), deadline)
}
