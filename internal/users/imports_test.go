package users

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestImportServiceCreateNormalizesAndAssignsIDs(t *testing.T) {
	t.Parallel()

	repository := &importRepositoryStub{}
	service := NewImportService(repository)
	created, err := service.Create(t.Context(), []string{" One@Example.COM ", "two@example.com"})
	require.NoError(t, err)
	assert.Equal(t, uuid.Version(7), created.ID.Version())
	require.Len(t, created.Entries, 2)
	assert.Equal(t, "one@example.com", created.Entries[0].Email)
	assert.Equal(t, uuid.Version(7), created.Entries[0].UserID.Version())
	assert.Equal(t, 2, created.TotalCount)
}

func TestImportServiceCreateRejectsInvalidLists(t *testing.T) {
	t.Parallel()

	service := NewImportService(&importRepositoryStub{})
	for name, emails := range map[string][]string{
		"empty":     {},
		"invalid":   {"not-an-email"},
		"duplicate": {"Person@example.com", " person@EXAMPLE.com "},
		"too many":  make([]string, 101),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := service.Create(t.Context(), emails)
			require.ErrorIs(t, err, ErrInvalidImport)
		})
	}
}

type importRepositoryStub struct {
	created Import
}

func (r *importRepositoryStub) CreateImport(_ context.Context, userImport Import) (Import, error) {
	r.created = userImport
	return userImport, nil
}

func (r *importRepositoryStub) GetImport(context.Context, uuid.UUID) (Import, error) {
	return Import{}, nil
}

func (r *importRepositoryStub) ProcessImport(context.Context, uuid.UUID) error { return nil }

func (r *importRepositoryStub) DeleteFinishedImportsBefore(context.Context, time.Time) (int64, error) {
	return 0, nil
}
