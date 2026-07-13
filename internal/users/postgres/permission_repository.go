package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/your-org/go-service-template/internal/users"
)

type PermissionRepository struct {
	pool *pgxpool.Pool
}

func NewPermissionRepository(pool *pgxpool.Pool) *PermissionRepository {
	return &PermissionRepository{pool: pool}
}

func (r *PermissionRepository) ApplyPermissionChange(ctx context.Context, change users.PermissionChange) (users.PermissionChangeResult, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return "", fmt.Errorf("begin permission change transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	queries := New(tx)
	if _, err := queries.RecordProcessedEvent(ctx, RecordProcessedEventParams{
		EventID: change.EventID, EventType: "permissions.changed",
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return users.PermissionChangeDuplicate, nil
		}
		return "", fmt.Errorf("record processed event: %w", err)
	}

	result := users.PermissionChangeApplied
	if _, err := queries.ApplyUserPermissions(ctx, ApplyUserPermissionsParams{
		UserID: change.UserID, Revision: change.Revision, Permissions: change.Permissions,
	}); err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			return "", fmt.Errorf("upsert user permissions: %w", err)
		}
		result = users.PermissionChangeStale
	}
	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("commit permission change: %w", err)
	}
	return result, nil
}
