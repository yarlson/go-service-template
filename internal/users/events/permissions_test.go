package usersevents

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/your-org/go-service-template/internal/platform/messaging"
	"github.com/your-org/go-service-template/internal/users"
)

func TestPermissionHandlerDecodesStandardSNSMessage(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	envelope, err := json.Marshal(messaging.Envelope[permissionsChangedPayload]{
		ID: "event-1", Timestamp: time.Now().UTC(), Type: "permissions.changed",
		Payload:  permissionsChangedPayload{UserID: userID, Revision: 3, Permissions: []string{"read"}},
		Metadata: messaging.Metadata{SchemaVersion: "1.0.0", ProducedBy: "permissions", OriginatedFrom: "permissions", CorrelationID: "correlation-1"},
	})
	require.NoError(t, err)
	body := []byte(fmt.Sprintf(`{"Type":"Notification","Message":%q}`, envelope))
	applier := &permissionApplierStub{}
	var observed users.PermissionChangeResult
	require.NoError(t, NewPermissionHandler(applier, func(_ context.Context, result users.PermissionChangeResult) {
		observed = result
	}).Handle(t.Context(), body))
	assert.Equal(t, "event-1", applier.change.EventID)
	assert.Equal(t, userID, applier.change.UserID)
	assert.Equal(t, int64(3), applier.change.Revision)
	assert.Equal(t, []string{"read"}, applier.change.Permissions)
	assert.Equal(t, "correlation-1", applier.correlationID)
	assert.Equal(t, users.PermissionChangeApplied, observed)
}

type permissionApplierStub struct {
	change        users.PermissionChange
	correlationID string
}

func (s *permissionApplierStub) Apply(ctx context.Context, change users.PermissionChange) (users.PermissionChangeResult, error) {
	s.change = change
	s.correlationID = messaging.CorrelationID(ctx)
	return users.PermissionChangeApplied, nil
}
