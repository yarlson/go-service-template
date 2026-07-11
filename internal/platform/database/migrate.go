package database

import (
	"errors"
	"fmt"
	"net/url"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"

	"github.com/your-org/go-service-template/db/migrations"
)

func Migrate(databaseURL string) error {
	source, err := iofs.New(migrations.Files, ".")
	if err != nil {
		return fmt.Errorf("open embedded migrations: %w", err)
	}

	driverURL, err := migrationURL(databaseURL)
	if err != nil {
		return err
	}

	migrator, err := migrate.NewWithSourceInstance("iofs", source, driverURL)
	if err != nil {
		closeErr := source.Close()
		return errors.Join(fmt.Errorf("create migrator: %w", err), wrapError("close migration source", closeErr))
	}

	migrationErr := migrator.Up()
	if errors.Is(migrationErr, migrate.ErrNoChange) {
		migrationErr = nil
	}
	sourceErr, databaseErr := migrator.Close()

	return errors.Join(
		wrapError("apply migrations", migrationErr),
		wrapError("close migration source", sourceErr),
		wrapError("close migration database", databaseErr),
	)
}

func migrationURL(databaseURL string) (string, error) {
	parsed, err := url.Parse(databaseURL)
	if err != nil {
		return "", fmt.Errorf("parse database URL: %w", err)
	}
	parsed.Scheme = "pgx5"
	return parsed.String(), nil
}

func wrapError(operation string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", operation, err)
}
