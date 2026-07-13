package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

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
	if len(arguments) == 0 {
		return usageError()
	}

	switch arguments[0] {
	case "api":
		if len(arguments) != 1 {
			return usageError()
		}
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		logger := newLogger(cfg.Environment, cfg.LogLevel.SlogLevel())
		ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		defer stop()
		return app.RunAPI(ctx, cfg, logger, app.BuildInfo{Version: version, Commit: commit})
	case "worker":
		if len(arguments) != 1 {
			return usageError()
		}
		cfg, err := config.LoadWorker()
		if err != nil {
			return err
		}
		logger := newLogger(cfg.Environment, cfg.LogLevel.SlogLevel())
		ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		defer stop()
		return app.RunWorker(ctx, cfg, logger, app.BuildInfo{Version: version, Commit: commit})
	case "migrate":
		if len(arguments) != 1 {
			return usageError()
		}
		databaseURL, err := config.LoadDatabaseURL()
		if err != nil {
			return err
		}
		return app.RunMigrations(databaseURL)
	case "healthcheck":
		if len(arguments) != 1 {
			return usageError()
		}
		address, err := config.LoadHTTPAddress()
		if err != nil {
			return err
		}
		return app.CheckHealth(address)
	case "jobs":
		return runJobs(arguments[1:], os.Stdout)
	default:
		return usageError()
	}
}

func runJobs(arguments []string, output io.Writer) error {
	if len(arguments) == 0 || arguments[0] != "list" {
		return usageError()
	}
	flags := flag.NewFlagSet("jobs list", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	queue := flags.String("queue", "", "filter by queue")
	state := flags.String("state", "", "filter by state")
	limit := flags.Int("limit", 100, "maximum jobs to return")
	if err := flags.Parse(arguments[1:]); err != nil {
		return fmt.Errorf("parse jobs list flags: %w", err)
	}
	if flags.NArg() != 0 {
		return usageError()
	}

	databaseURL, err := config.LoadDatabaseURL()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return app.ListJobs(ctx, databaseURL, output, app.JobListOptions{Queue: *queue, State: *state, Limit: *limit})
}

func newLogger(environment string, level slog.Level) *slog.Logger {
	options := &slog.HandlerOptions{Level: level}
	if environment == config.EnvironmentDevelopment {
		return slog.New(slog.NewTextHandler(os.Stdout, options))
	}
	return slog.New(slog.NewJSONHandler(os.Stdout, options))
}

func usageError() error {
	return errors.New("usage: service <api|worker|migrate|healthcheck|jobs list [--queue name] [--state state] [--limit count]>")
}
