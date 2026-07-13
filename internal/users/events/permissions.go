package usersevents

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/your-org/go-service-template/internal/platform/messaging"
	"github.com/your-org/go-service-template/internal/users"
)

type PermissionApplier interface {
	Apply(context.Context, users.PermissionChange) (users.PermissionChangeResult, error)
}

type PermissionHandler struct {
	permissions PermissionApplier
	observe     func(context.Context, users.PermissionChangeResult)
}

func NewPermissionHandler(permissions PermissionApplier, observe func(context.Context, users.PermissionChangeResult)) *PermissionHandler {
	return &PermissionHandler{permissions: permissions, observe: observe}
}

type permissionsChangedPayload struct {
	UserID      uuid.UUID `json:"userId"`
	Revision    int64     `json:"revision"`
	Permissions []string  `json:"permissions"`
}

func (h *PermissionHandler) Handle(ctx context.Context, body []byte) error {
	event, err := messaging.DecodeSNSNotification[permissionsChangedPayload](body, "permissions.changed")
	if err != nil {
		return fmt.Errorf("decode permissions.changed: %w", err)
	}
	ctx = messaging.WithCorrelationID(ctx, event.Metadata.CorrelationID)
	result, err := h.permissions.Apply(ctx, users.PermissionChange{
		EventID: event.ID, UserID: event.Payload.UserID,
		Revision: event.Payload.Revision, Permissions: event.Payload.Permissions,
	})
	if err != nil {
		return fmt.Errorf("handle permissions.changed: %w", err)
	}
	if h.observe != nil {
		h.observe(ctx, result)
	}
	return nil
}
