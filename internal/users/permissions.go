package users

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"
)

var ErrInvalidPermissionsEvent = errors.New("invalid permissions event")

type PermissionChange struct {
	EventID     string
	UserID      uuid.UUID
	Revision    int64
	Permissions []string
}

type PermissionChangeResult string

const (
	PermissionChangeApplied   PermissionChangeResult = "applied"
	PermissionChangeDuplicate PermissionChangeResult = "duplicate"
	PermissionChangeStale     PermissionChangeResult = "stale"
)

type PermissionRepository interface {
	ApplyPermissionChange(context.Context, PermissionChange) (PermissionChangeResult, error)
}

type PermissionService struct {
	repository PermissionRepository
}

func NewPermissionService(repository PermissionRepository) *PermissionService {
	return &PermissionService{repository: repository}
}

func (s *PermissionService) Apply(ctx context.Context, change PermissionChange) (PermissionChangeResult, error) {
	change.EventID = strings.TrimSpace(change.EventID)
	if change.EventID == "" || change.UserID == uuid.Nil || change.Revision < 1 {
		return "", ErrInvalidPermissionsEvent
	}

	normalized := make([]string, 0, len(change.Permissions))
	seen := make(map[string]struct{}, len(change.Permissions))
	for _, permission := range change.Permissions {
		permission = strings.TrimSpace(permission)
		if permission == "" {
			return "", ErrInvalidPermissionsEvent
		}
		if _, exists := seen[permission]; exists {
			return "", ErrInvalidPermissionsEvent
		}
		seen[permission] = struct{}{}
		normalized = append(normalized, permission)
	}
	slices.Sort(normalized)
	change.Permissions = normalized

	result, err := s.repository.ApplyPermissionChange(ctx, change)
	if err != nil {
		return "", fmt.Errorf("apply permission change: %w", err)
	}
	return result, nil
}
