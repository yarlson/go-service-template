package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/your-org/go-service-template/internal/app"
	"github.com/your-org/go-service-template/internal/platform/config"
)

var (
	version = "dev"
	commit  = "unknown"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		slog.Error("service failed", "error", err)
		os.Exit(1)
	}
}

func run(arguments []string) error {
	if len(arguments) != 1 {
		return usageError()
	}

	switch arguments[0] {
	case "api":
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		logger := newLogger(cfg.Environment, cfg.LogLevel.SlogLevel())
		ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		defer stop()
		return app.RunAPI(ctx, cfg, logger, app.BuildInfo{Version: version, Commit: commit})
	case "migrate":
		databaseURL, err := config.LoadDatabaseURL()
		if err != nil {
			return err
		}
		return app.RunMigrations(databaseURL)
	case "healthcheck":
		address, err := config.LoadHTTPAddress()
		if err != nil {
			return err
		}
		return app.CheckHealth(address)
	default:
		return usageError()
	}
}

func newLogger(environment string, level slog.Level) *slog.Logger {
	options := &slog.HandlerOptions{Level: level}
	if environment == config.EnvironmentDevelopment {
		return slog.New(slog.NewTextHandler(os.Stdout, options))
	}
	return slog.New(slog.NewJSONHandler(os.Stdout, options))
}

func usageError() error {
	return errors.New("usage: service <api|migrate|healthcheck>")
}
