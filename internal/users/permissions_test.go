package users

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPermissionServiceNormalizesCompleteSet(t *testing.T) {
	t.Parallel()

	repository := &permissionRepositoryStub{result: PermissionChangeApplied}
	service := NewPermissionService(repository)
	change := PermissionChange{
		EventID: " event-1 ", UserID: uuid.New(), Revision: 2,
		Permissions: []string{"write", " read "},
	}
	result, err := service.Apply(t.Context(), change)
	require.NoError(t, err)
	assert.Equal(t, PermissionChangeApplied, result)
	assert.Equal(t, "event-1", repository.change.EventID)
	assert.Equal(t, []string{"read", "write"}, repository.change.Permissions)
}

func TestPermissionServiceRejectsInvalidEvents(t *testing.T) {
	t.Parallel()

	valid := PermissionChange{EventID: "event-1", UserID: uuid.New(), Revision: 1}
	for name, change := range map[string]PermissionChange{
		"missing event ID":     {UserID: valid.UserID, Revision: 1},
		"missing user ID":      {EventID: valid.EventID, Revision: 1},
		"invalid revision":     {EventID: valid.EventID, UserID: valid.UserID},
		"empty permission":     {EventID: valid.EventID, UserID: valid.UserID, Revision: 1, Permissions: []string{""}},
		"duplicate permission": {EventID: valid.EventID, UserID: valid.UserID, Revision: 1, Permissions: []string{"read", "read"}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := NewPermissionService(&permissionRepositoryStub{}).Apply(t.Context(), change)
			require.ErrorIs(t, err, ErrInvalidPermissionsEvent)
		})
	}
}

type permissionRepositoryStub struct {
	change PermissionChange
	result PermissionChangeResult
}

func (r *permissionRepositoryStub) ApplyPermissionChange(_ context.Context, change PermissionChange) (PermissionChangeResult, error) {
	r.change = change
	return r.result, nil
}
