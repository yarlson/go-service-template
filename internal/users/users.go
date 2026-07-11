package users

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"

	"github.com/google/uuid"
)

var (
	ErrConflict     = errors.New("user already exists")
	ErrInvalidEmail = errors.New("invalid email")
	ErrNotFound     = errors.New("user not found")
)

type User struct {
	ID        uuid.UUID
	Email     string
	CreatedAt time.Time
}

type Repository interface {
	Create(context.Context, User) (User, error)
	Get(context.Context, uuid.UUID) (User, error)
}

type Service struct {
	repository Repository
}

func NewService(repository Repository) *Service {
	return &Service{repository: repository}
}

func (s *Service) Create(ctx context.Context, email string) (User, error) {
	normalizedEmail := strings.ToLower(strings.TrimSpace(email))
	address, err := mail.ParseAddress(normalizedEmail)
	if err != nil || address.Address != normalizedEmail {
		return User{}, ErrInvalidEmail
	}

	user, err := s.repository.Create(ctx, User{
		ID:    uuid.New(),
		Email: normalizedEmail,
	})
	if err != nil {
		return User{}, fmt.Errorf("create user: %w", err)
	}

	return user, nil
}

func (s *Service) Get(ctx context.Context, id uuid.UUID) (User, error) {
	user, err := s.repository.Get(ctx, id)
	if err != nil {
		return User{}, fmt.Errorf("get user: %w", err)
	}

	return user, nil
}
