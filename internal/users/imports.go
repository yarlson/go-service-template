package users

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

var ErrInvalidImport = errors.New("invalid user import")

type ImportState string

const (
	ImportStatePending   ImportState = "pending"
	ImportStateRunning   ImportState = "running"
	ImportStateCompleted ImportState = "completed"
	ImportStateFailed    ImportState = "failed"
)

type Import struct {
	ID             uuid.UUID
	State          ImportState
	TotalCount     int
	CompletedCount int
	FailedCount    int
	CreatedAt      time.Time
	StartedAt      *time.Time
	FinishedAt     *time.Time
	Entries        []ImportEntry
}

type ImportEntry struct {
	UserID uuid.UUID
	Email  string
}

type ImportRepository interface {
	CreateImport(context.Context, Import) (Import, error)
	GetImport(context.Context, uuid.UUID) (Import, error)
	ProcessImport(context.Context, uuid.UUID) error
	DeleteFinishedImportsBefore(context.Context, time.Time) (int64, error)
}

type ImportService struct {
	repository ImportRepository
}

func NewImportService(repository ImportRepository) *ImportService {
	return &ImportService{repository: repository}
}

func (s *ImportService) Create(ctx context.Context, emails []string) (Import, error) {
	if len(emails) < 1 || len(emails) > 100 {
		return Import{}, ErrInvalidImport
	}

	entries := make([]ImportEntry, 0, len(emails))
	seen := make(map[string]struct{}, len(emails))
	for _, email := range emails {
		normalized, err := normalizeEmail(email)
		if err != nil {
			return Import{}, ErrInvalidImport
		}
		if _, exists := seen[normalized]; exists {
			return Import{}, ErrInvalidImport
		}
		seen[normalized] = struct{}{}

		userID, err := uuid.NewV7()
		if err != nil {
			return Import{}, fmt.Errorf("generate imported user ID: %w", err)
		}
		entries = append(entries, ImportEntry{UserID: userID, Email: normalized})
	}

	importID, err := uuid.NewV7()
	if err != nil {
		return Import{}, fmt.Errorf("generate user import ID: %w", err)
	}

	created, err := s.repository.CreateImport(ctx, Import{
		ID:         importID,
		State:      ImportStatePending,
		TotalCount: len(entries),
		Entries:    entries,
	})
	if err != nil {
		return Import{}, fmt.Errorf("create user import: %w", err)
	}
	return created, nil
}

func (s *ImportService) Get(ctx context.Context, id uuid.UUID) (Import, error) {
	userImport, err := s.repository.GetImport(ctx, id)
	if err != nil {
		return Import{}, fmt.Errorf("get user import: %w", err)
	}
	return userImport, nil
}

func (s *ImportService) Process(ctx context.Context, id uuid.UUID) error {
	if err := s.repository.ProcessImport(ctx, id); err != nil {
		return fmt.Errorf("process user import: %w", err)
	}
	return nil
}

func (s *ImportService) Cleanup(ctx context.Context, cutoff time.Time) (int64, error) {
	deleted, err := s.repository.DeleteFinishedImportsBefore(ctx, cutoff)
	if err != nil {
		return 0, fmt.Errorf("cleanup user imports: %w", err)
	}
	return deleted, nil
}
