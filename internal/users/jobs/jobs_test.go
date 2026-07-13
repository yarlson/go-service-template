package usersjobs

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/riverqueue/river"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/your-org/go-service-template/internal/platform/messaging"
)

func TestDailyAtUTC(t *testing.T) {
	t.Parallel()

	schedule := dailyAtUTC{hour: 2}
	assert.Equal(t,
		time.Date(2026, time.July, 13, 2, 0, 0, 0, time.UTC),
		schedule.Next(time.Date(2026, time.July, 12, 23, 30, 0, 0, time.FixedZone("west", -2*60*60))),
	)
	assert.Equal(t,
		time.Date(2026, time.July, 14, 2, 0, 0, 0, time.UTC),
		schedule.Next(time.Date(2026, time.July, 13, 2, 0, 0, 0, time.UTC)),
	)
}

func TestPublishCreatedWorkerUsesCompatibleEnvelope(t *testing.T) {
	t.Parallel()

	publisher := &publisherStub{}
	worker := NewPublishCreatedWorker(publisher, "go-service-template")
	args := PublishCreatedArgs{
		EventID:       uuid.MustParse("0198a1f7-30b7-7df1-8491-c47f6033525b"),
		UserID:        uuid.MustParse("0198a1f7-30b7-7df2-8491-c47f6033525b"),
		Timestamp:     time.Date(2026, time.July, 13, 8, 0, 0, 0, time.UTC),
		CorrelationID: "request-123",
	}
	require.NoError(t, worker.Work(t.Context(), &river.Job[PublishCreatedArgs]{Args: args}))

	var envelope messaging.Envelope[CreatedPayload]
	require.NoError(t, json.Unmarshal(publisher.message, &envelope))
	assert.Equal(t, args.EventID.String(), envelope.ID)
	assert.Equal(t, args.Timestamp, envelope.Timestamp)
	assert.Equal(t, "user.created", envelope.Type)
	assert.Equal(t, args.UserID, envelope.Payload.UserID)
	assert.Equal(t, "1.0.0", envelope.Metadata.SchemaVersion)
	assert.Equal(t, "go-service-template", envelope.Metadata.ProducedBy)
	assert.Equal(t, "go-service-template", envelope.Metadata.OriginatedFrom)
	assert.Equal(t, args.CorrelationID, envelope.Metadata.CorrelationID)
}

type publisherStub struct {
	message []byte
}

func (p *publisherStub) Publish(_ context.Context, message []byte) error {
	p.message = message
	return nil
}
