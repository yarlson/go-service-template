package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/your-org/go-service-template/internal/platform/messaging"
	"github.com/your-org/go-service-template/internal/users"
)

type ImportJobEnqueuer interface {
	EnqueueImport(context.Context, pgx.Tx, uuid.UUID) error
	EnqueueUserCreated(context.Context, pgx.Tx, uuid.UUID, string) error
}

type ImportRepository struct {
	pool     *pgxpool.Pool
	enqueuer ImportJobEnqueuer
}

func NewImportRepository(pool *pgxpool.Pool, enqueuer ImportJobEnqueuer) *ImportRepository {
	return &ImportRepository{pool: pool, enqueuer: enqueuer}
}

func (r *ImportRepository) CreateImport(ctx context.Context, userImport users.Import) (users.Import, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return users.Import{}, fmt.Errorf("begin user import transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	queries := New(tx)
	created, err := queries.CreateUserImport(ctx, CreateUserImportParams{
		ID:            userImport.ID,
		TotalCount:    int32(userImport.TotalCount),
		CorrelationID: correlationID(ctx, userImport.ID.String()),
	})
	if err != nil {
		return users.Import{}, fmt.Errorf("insert user import: %w", err)
	}
	for _, entry := range userImport.Entries {
		if err := queries.CreateUserImportEntry(ctx, CreateUserImportEntryParams{
			ImportID: userImport.ID,
			UserID:   entry.UserID,
			Email:    entry.Email,
		}); err != nil {
			return users.Import{}, fmt.Errorf("insert user import entry: %w", err)
		}
	}
	if err := r.enqueuer.EnqueueImport(ctx, tx, userImport.ID); err != nil {
		return users.Import{}, fmt.Errorf("enqueue user import: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return users.Import{}, fmt.Errorf("commit user import: %w", err)
	}

	return toDomainImport(created), nil
}

func (r *ImportRepository) GetImport(ctx context.Context, id uuid.UUID) (users.Import, error) {
	userImport, err := New(r.pool).GetUserImport(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return users.Import{}, users.ErrNotFound
		}
		return users.Import{}, fmt.Errorf("select user import: %w", err)
	}
	return toDomainImport(userImport), nil
}

func (r *ImportRepository) ProcessImport(ctx context.Context, id uuid.UUID) error {
	queries := New(r.pool)
	if _, err := queries.StartUserImport(ctx, id); err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("mark user import running: %w", err)
		}
		userImport, getErr := queries.GetUserImport(ctx, id)
		if errors.Is(getErr, pgx.ErrNoRows) {
			return users.ErrNotFound
		}
		if getErr != nil {
			return fmt.Errorf("select user import state: %w", getErr)
		}
		if userImport.State == UserImportStateCompleted || userImport.State == UserImportStateFailed {
			return nil
		}
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin import processing transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	transactionQueries := New(tx)
	entries, err := transactionQueries.ListPendingUserImportEntries(ctx, id)
	if err != nil {
		return fmt.Errorf("list user import entries: %w", err)
	}
	for _, entry := range entries {
		createdID, createErr := transactionQueries.CreateImportedUser(ctx, CreateImportedUserParams{
			ID:    entry.UserID,
			Email: entry.Email,
		})
		if createErr == nil {
			if createdID != entry.UserID {
				return errors.New("insert imported user returned unexpected ID")
			}
			if err := transactionQueries.CompleteUserImportEntry(ctx, CompleteUserImportEntryParams{ImportID: id, UserID: entry.UserID}); err != nil {
				return fmt.Errorf("complete user import entry: %w", err)
			}
			if err := r.enqueuer.EnqueueUserCreated(ctx, tx, entry.UserID, entry.CorrelationID); err != nil {
				return fmt.Errorf("enqueue imported user.created: %w", err)
			}
			continue
		}
		if !errors.Is(createErr, pgx.ErrNoRows) {
			return fmt.Errorf("insert imported user: %w", createErr)
		}

		existing, lookupErr := transactionQueries.GetUserByEmail(ctx, entry.Email)
		if lookupErr == nil && existing.ID == entry.UserID {
			if err := transactionQueries.CompleteUserImportEntry(ctx, CompleteUserImportEntryParams{ImportID: id, UserID: entry.UserID}); err != nil {
				return fmt.Errorf("complete retried user import entry: %w", err)
			}
			continue
		}
		if lookupErr != nil && !errors.Is(lookupErr, pgx.ErrNoRows) {
			return fmt.Errorf("select imported user conflict: %w", lookupErr)
		}
		if err := transactionQueries.FailUserImportEntry(ctx, FailUserImportEntryParams{ImportID: id, UserID: entry.UserID}); err != nil {
			return fmt.Errorf("fail user import entry: %w", err)
		}
	}

	if _, err := transactionQueries.FinishUserImport(ctx, id); err != nil {
		return fmt.Errorf("finish user import: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit import processing: %w", err)
	}
	return nil
}

func correlationID(ctx context.Context, fallback string) string {
	if correlationID := messaging.CorrelationID(ctx); correlationID != "" {
		return correlationID
	}
	return fallback
}

func (r *ImportRepository) DeleteFinishedImportsBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	deleted, err := New(r.pool).DeleteFinishedUserImportsBefore(ctx, pgtype.Timestamptz{Time: cutoff, Valid: true})
	if err != nil {
		return 0, fmt.Errorf("delete finished user imports: %w", err)
	}
	return deleted, nil
}

func toDomainImport(userImport UserImport) users.Import {
	return users.Import{
		ID:             userImport.ID,
		State:          users.ImportState(userImport.State),
		TotalCount:     int(userImport.TotalCount),
		CompletedCount: int(userImport.CompletedCount),
		FailedCount:    int(userImport.FailedCount),
		CreatedAt:      userImport.CreatedAt,
		StartedAt:      optionalTime(userImport.StartedAt),
		FinishedAt:     optionalTime(userImport.FinishedAt),
	}
}

func optionalTime(value pgtype.Timestamptz) *time.Time {
	if !value.Valid {
		return nil
	}
	return &value.Time
}
