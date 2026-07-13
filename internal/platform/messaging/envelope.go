package messaging

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

type Metadata struct {
	SchemaVersion  string `json:"schemaVersion"`
	ProducedBy     string `json:"producedBy"`
	OriginatedFrom string `json:"originatedFrom"`
	CorrelationID  string `json:"correlationId"`
}

type DeduplicationOptions struct {
	DeduplicationWindowSeconds int     `json:"deduplicationWindowSeconds,omitempty"`
	LockTimeoutSeconds         int     `json:"lockTimeoutSeconds,omitempty"`
	AcquireTimeoutSeconds      int     `json:"acquireTimeoutSeconds,omitempty"`
	RefreshIntervalSeconds     float64 `json:"refreshIntervalSeconds,omitempty"`
}

type Envelope[T any] struct {
	ID                   string                `json:"id"`
	Timestamp            time.Time             `json:"timestamp"`
	Type                 string                `json:"type"`
	Payload              T                     `json:"payload"`
	Metadata             Metadata              `json:"metadata"`
	DeduplicationID      *string               `json:"deduplicationId,omitempty"`
	DeduplicationOptions *DeduplicationOptions `json:"deduplicationOptions,omitempty"`
}

type snsNotification struct {
	Type    string `json:"Type"`
	Message string `json:"Message"`
}

type rawEnvelope struct {
	ID                   string                `json:"id"`
	Timestamp            time.Time             `json:"timestamp"`
	Type                 string                `json:"type"`
	Payload              json.RawMessage       `json:"payload"`
	Metadata             Metadata              `json:"metadata"`
	DeduplicationID      *string               `json:"deduplicationId"`
	DeduplicationOptions *DeduplicationOptions `json:"deduplicationOptions"`
}

func DecodeSNSNotification[T any](body []byte, expectedType string) (Envelope[T], error) {
	if expectedType == "" {
		return Envelope[T]{}, errors.New("expected message type is required")
	}

	var notification snsNotification
	if err := json.Unmarshal(body, &notification); err != nil {
		return Envelope[T]{}, fmt.Errorf("decode SNS notification: %w", err)
	}
	if notification.Type != "Notification" {
		return Envelope[T]{}, fmt.Errorf("unexpected SNS message type %q", notification.Type)
	}
	if notification.Message == "" {
		return Envelope[T]{}, errors.New("SNS notification message is required")
	}

	var raw rawEnvelope
	if err := json.Unmarshal([]byte(notification.Message), &raw); err != nil {
		return Envelope[T]{}, fmt.Errorf("decode message envelope: %w", err)
	}
	if err := raw.validate(expectedType); err != nil {
		return Envelope[T]{}, err
	}

	var payload T
	if err := json.Unmarshal(raw.Payload, &payload); err != nil {
		return Envelope[T]{}, fmt.Errorf("decode message payload: %w", err)
	}

	return Envelope[T]{
		ID:                   raw.ID,
		Timestamp:            raw.Timestamp,
		Type:                 raw.Type,
		Payload:              payload,
		Metadata:             raw.Metadata,
		DeduplicationID:      raw.DeduplicationID,
		DeduplicationOptions: raw.DeduplicationOptions,
	}, nil
}

func (e rawEnvelope) validate(expectedType string) error {
	if strings.TrimSpace(e.ID) == "" {
		return errors.New("message ID is required")
	}
	if e.Timestamp.IsZero() {
		return errors.New("message timestamp is required")
	}
	if e.Type != expectedType {
		return fmt.Errorf("unexpected message type %q", e.Type)
	}
	if len(e.Payload) == 0 || string(e.Payload) == "null" {
		return errors.New("message payload is required")
	}
	if strings.TrimSpace(e.Metadata.SchemaVersion) == "" {
		return errors.New("message schema version is required")
	}
	if strings.TrimSpace(e.Metadata.ProducedBy) == "" {
		return errors.New("message producer is required")
	}
	if strings.TrimSpace(e.Metadata.OriginatedFrom) == "" {
		return errors.New("message origin is required")
	}
	if strings.TrimSpace(e.Metadata.CorrelationID) == "" {
		return errors.New("message correlation ID is required")
	}
	return nil
}
