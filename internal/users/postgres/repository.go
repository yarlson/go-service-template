package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/your-org/go-service-template/internal/users"
)

type UserRepository struct {
	pool     *pgxpool.Pool
	enqueuer UserCreatedEnqueuer
}

type UserCreatedEnqueuer interface {
	EnqueueUserCreated(context.Context, pgx.Tx, uuid.UUID, string) error
}

func NewUserRepository(pool *pgxpool.Pool, enqueuer UserCreatedEnqueuer) *UserRepository {
	return &UserRepository{pool: pool, enqueuer: enqueuer}
}

func (r *UserRepository) Create(ctx context.Context, user users.User) (users.User, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return users.User{}, fmt.Errorf("begin user transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	created, err := New(tx).CreateUser(ctx, CreateUserParams{
		ID:    user.ID,
		Email: user.Email,
	})
	if err != nil {
		var postgresError *pgconn.PgError
		if errors.As(err, &postgresError) && postgresError.Code == "23505" {
			return users.User{}, users.ErrConflict
		}
		return users.User{}, fmt.Errorf("insert user: %w", err)
	}
	if err := r.enqueuer.EnqueueUserCreated(ctx, tx, created.ID, ""); err != nil {
		return users.User{}, fmt.Errorf("enqueue user.created: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return users.User{}, fmt.Errorf("commit user: %w", err)
	}

	return toDomainUser(created), nil
}

func (r *UserRepository) Get(ctx context.Context, id uuid.UUID) (users.User, error) {
	user, err := New(r.pool).GetUser(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return users.User{}, users.ErrNotFound
		}
		return users.User{}, fmt.Errorf("select user: %w", err)
	}

	return toDomainUser(user), nil
}

func toDomainUser(user User) users.User {
	return users.User{
		ID:        user.ID,
		Email:     user.Email,
		CreatedAt: user.CreatedAt,
	}
}
