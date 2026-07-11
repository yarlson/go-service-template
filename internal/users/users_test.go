package users

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServiceCreateNormalizesEmail(t *testing.T) {
	t.Parallel()

	repository := &repositoryStub{}
	service := NewService(repository)

	created, err := service.Create(context.Background(), " Person@Example.COM ")
	require.NoError(t, err)
	assert.Equal(t, "person@example.com", created.Email)
	assert.NotEqual(t, uuid.Nil, created.ID)
	assert.Equal(t, uuid.Version(7), created.ID.Version())
}

func TestServiceCreateRejectsInvalidEmail(t *testing.T) {
	t.Parallel()

	service := NewService(&repositoryStub{})
	_, err := service.Create(context.Background(), "not-an-email")
	require.ErrorIs(t, err, ErrInvalidEmail)
}

type repositoryStub struct {
	createErr error
	getErr    error
}

func (r *repositoryStub) Create(_ context.Context, user User) (User, error) {
	return user, r.createErr
}

func (r *repositoryStub) Get(_ context.Context, id uuid.UUID) (User, error) {
	return User{ID: id}, r.getErr
}
