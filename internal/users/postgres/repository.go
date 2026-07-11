package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/your-org/go-service-template/internal/users"
)

type UserRepository struct {
	queries *Queries
}

func NewUserRepository(queries *Queries) *UserRepository {
	return &UserRepository{queries: queries}
}

func (r *UserRepository) Create(ctx context.Context, user users.User) (users.User, error) {
	created, err := r.queries.CreateUser(ctx, CreateUserParams{
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

	return toDomainUser(created), nil
}

func (r *UserRepository) Get(ctx context.Context, id uuid.UUID) (users.User, error) {
	user, err := r.queries.GetUser(ctx, id)
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
