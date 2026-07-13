package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"github.com/your-org/go-service-template/internal/platform/auth"
	"github.com/your-org/go-service-template/internal/platform/config"
	"github.com/your-org/go-service-template/internal/platform/database"
	"github.com/your-org/go-service-template/internal/platform/httpserver"
	"github.com/your-org/go-service-template/internal/platform/telemetry"
	"github.com/your-org/go-service-template/internal/users"
	usershttp "github.com/your-org/go-service-template/internal/users/http"
	usersjobs "github.com/your-org/go-service-template/internal/users/jobs"
	userspostgres "github.com/your-org/go-service-template/internal/users/postgres"
)

type BuildInfo struct {
	Version string
	Commit  string
}

func RunAPI(ctx context.Context, cfg config.Config, logger *slog.Logger, build BuildInfo) error {
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

	startupCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := pool.Ping(startupCtx); err != nil {
		return fmt.Errorf("connect to PostgreSQL: %w", err)
	}

	authentication, err := buildAuthentication(startupCtx, cfg)
	if err != nil {
		return err
	}

	queries := userspostgres.New(pool)
	repository := userspostgres.NewUserRepository(queries)
	userService := users.NewService(repository)
	jobClient, err := river.NewClient(riverpgxv5.New(pool), &river.Config{Logger: logger})
	if err != nil {
		return fmt.Errorf("create River enqueue client: %w", err)
	}
	importRepository := userspostgres.NewImportRepository(pool, usersjobs.NewEnqueuer(jobClient))
	importService := users.NewImportService(importRepository)
	usersHandler := usershttp.NewHandler(logger, userService, importService)
	readiness := httpserver.NewReadiness(pool, telemetryRuntime.RecordDatabaseCheck)
	handler, err := httpserver.NewHandler(httpserver.HandlerOptions{
		Logger:    logger,
		API:       usersHandler,
		Auth:      authentication,
		Readiness: readiness,
		Metrics:   telemetryRuntime.MetricsHandler,
		Version:   build.Version,
		Commit:    build.Commit,
	})
	if err != nil {
		return err
	}

	server := &http.Server{
		Addr: cfg.HTTPAddress,
		Handler: instrumentHTTP(handler, otelhttp.WithFilter(func(request *http.Request) bool {
			return request.URL.Path != "/metrics"
		})),
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelError),
	}

	serverErrors := make(chan error, 1)
	go func() {
		logger.Info("server listening", "address", cfg.HTTPAddress)
		serverErrors <- server.ListenAndServe()
	}()

	select {
	case err := <-serverErrors:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("serve HTTP: %w", err)
	case <-ctx.Done():
		readiness.StopAccepting()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer shutdownCancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			closeErr := server.Close()
			return errors.Join(fmt.Errorf("shutdown HTTP server: %w", err), closeErr)
		}
		return nil
	}
}

func instrumentHTTP(handler http.Handler, options ...otelhttp.Option) http.Handler {
	return otelhttp.NewHandler(handler, "http.server", options...)
}

func RunMigrations(databaseURL string) error {
	return database.Migrate(databaseURL)
}

func CheckHealth(address string) error {
	_, port, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("parse HTTP address: %w", err)
	}

	endpoint := url.URL{Scheme: "http", Host: net.JoinHostPort("127.0.0.1", port), Path: "/livez"}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	client := &http.Client{Transport: transport, Timeout: 3 * time.Second}
	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return fmt.Errorf("create liveness request: %w", err)
	}
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("request liveness endpoint: %w", err)
	}
	_, readErr := io.Copy(io.Discard, io.LimitReader(response.Body, 4<<10))
	closeErr := response.Body.Close()
	if err := errors.Join(readErr, closeErr); err != nil {
		return fmt.Errorf("close liveness response: %w", err)
	}
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("liveness endpoint returned %s", response.Status)
	}
	return nil
}

func buildAuthentication(ctx context.Context, cfg config.Config) (httpserver.Authentication, error) {
	if cfg.AuthMode == config.AuthModeDisabled {
		return httpserver.DisabledAuthentication(), nil
	}

	verifier, err := auth.NewVerifier(ctx, cfg.OIDCIssuerURL, cfg.OIDCAudience)
	if err != nil {
		return httpserver.Authentication{}, err
	}
	return httpserver.TokenAuthentication(verifier)
}
