package messaging_test

import (
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/your-org/go-service-template/internal/platform/messaging"
)

type userCreatedPayload struct {
	UserID string `json:"userId"`
	Name   string `json:"name"`
	Email  string `json:"email"`
}

func TestDecodeSNSNotification(t *testing.T) {
	t.Parallel()

	body, err := os.ReadFile("testdata/node-user-created-sns.json")
	require.NoError(t, err)

	event, err := messaging.DecodeSNSNotification[userCreatedPayload](body, "user.created")
	require.NoError(t, err)

	assert.Equal(t, "0197f7cc-f2aa-7a9e-95b1-44e127a2b42e", event.ID)
	assert.Equal(t, time.Date(2026, time.July, 13, 7, 0, 0, 0, time.UTC), event.Timestamp)
	assert.Equal(t, "user.created", event.Type)
	assert.Equal(t, "0197f7cc-e983-73a0-bb95-e0fbab2fc0be", event.Payload.UserID)
	assert.Equal(t, "Example User", event.Payload.Name)
	assert.Equal(t, "person@example.com", event.Payload.Email)
	assert.Equal(t, "1.0.0", event.Metadata.SchemaVersion)
	assert.Equal(t, "node-service-template", event.Metadata.ProducedBy)
	assert.Equal(t, "node-service-template", event.Metadata.OriginatedFrom)
	assert.Equal(t, "0197f7cc-dff1-7398-ab7e-8dc243c5fc82", event.Metadata.CorrelationID)
	assert.Nil(t, event.DeduplicationID)
	assert.Nil(t, event.DeduplicationOptions)
}

func TestDecodeSNSNotificationRejectsInvalidMessages(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		body         string
		expectedType string
		wantError    string
	}{
		"raw envelope": {
			body:         `{"id":"event-id","type":"user.created"}`,
			expectedType: "user.created",
			wantError:    `unexpected SNS message type "user.created"`,
		},
		"wrong event type": {
			body:         notification(`{"id":"event-id","timestamp":"2026-07-13T07:00:00Z","type":"user.deleted","payload":{},"metadata":{"schemaVersion":"1.0.0","producedBy":"users","originatedFrom":"users","correlationId":"request-id"}}`),
			expectedType: "user.created",
			wantError:    `unexpected message type "user.deleted"`,
		},
		"missing metadata": {
			body:         notification(`{"id":"event-id","timestamp":"2026-07-13T07:00:00Z","type":"user.created","payload":{}}`),
			expectedType: "user.created",
			wantError:    "message schema version is required",
		},
		"invalid timestamp": {
			body:         notification(`{"id":"event-id","timestamp":"yesterday","type":"user.created","payload":{},"metadata":{"schemaVersion":"1.0.0","producedBy":"users","originatedFrom":"users","correlationId":"request-id"}}`),
			expectedType: "user.created",
			wantError:    "decode message envelope",
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := messaging.DecodeSNSNotification[map[string]any]([]byte(test.body), test.expectedType)
			require.Error(t, err)
			assert.Contains(t, err.Error(), test.wantError)
		})
	}
}

func notification(message string) string {
	return `{"Type":"Notification","Message":` + strconv.Quote(message) + `}`
}
