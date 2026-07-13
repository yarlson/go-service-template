package usersjobs

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/your-org/go-service-template/internal/platform/messaging"
)

const QueueEvents = "events"

type PublishCreatedArgs struct {
	EventID       uuid.UUID `json:"eventId"`
	UserID        uuid.UUID `json:"userId"`
	Timestamp     time.Time `json:"timestamp"`
	CorrelationID string    `json:"correlationId"`
}

func (PublishCreatedArgs) Kind() string { return "users.publish-created" }

type CreatedPayload struct {
	UserID uuid.UUID `json:"userId"`
}

type EventPublisher interface {
	Publish(context.Context, []byte) error
}

type PublishCreatedWorker struct {
	river.WorkerDefaults[PublishCreatedArgs]
	publisher   EventPublisher
	serviceName string
}

func NewPublishCreatedWorker(publisher EventPublisher, serviceName string) *PublishCreatedWorker {
	return &PublishCreatedWorker{publisher: publisher, serviceName: serviceName}
}

func (w *PublishCreatedWorker) Work(ctx context.Context, job *river.Job[PublishCreatedArgs]) error {
	envelope := messaging.Envelope[CreatedPayload]{
		ID:        job.Args.EventID.String(),
		Timestamp: job.Args.Timestamp,
		Type:      "user.created",
		Payload:   CreatedPayload{UserID: job.Args.UserID},
		Metadata: messaging.Metadata{
			SchemaVersion:  "1.0.0",
			ProducedBy:     w.serviceName,
			OriginatedFrom: w.serviceName,
			CorrelationID:  job.Args.CorrelationID,
		},
	}
	message, err := json.Marshal(envelope)
	if err != nil {
		return fmt.Errorf("encode user.created event: %w", err)
	}
	if err := w.publisher.Publish(ctx, message); err != nil {
		return fmt.Errorf("publish user.created event: %w", err)
	}
	return nil
}

func (e *Enqueuer) EnqueueUserCreated(ctx context.Context, tx pgx.Tx, userID uuid.UUID, correlationID string) error {
	eventID, err := uuid.NewV7()
	if err != nil {
		return fmt.Errorf("generate user.created event ID: %w", err)
	}
	if correlationID == "" {
		correlationID = messaging.CorrelationID(ctx)
		if correlationID == "" {
			correlationID = eventID.String()
		}
	}
	_, err = e.client.InsertTx(ctx, tx, PublishCreatedArgs{
		EventID: eventID, UserID: userID, Timestamp: time.Now().UTC(), CorrelationID: correlationID,
	}, &river.InsertOpts{Queue: QueueEvents, MaxAttempts: 10})
	if err != nil {
		return fmt.Errorf("insert users.publish-created job: %w", err)
	}
	return nil
}
