package messaging

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSQSConsumerDeletesMessageAfterSuccessfulHandling(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	client := &sqsClientStub{message: types.Message{
		Body: aws.String(`{"message":"value"}`), MessageId: aws.String("message-1"), ReceiptHandle: aws.String("receipt-1"),
	}}
	handler := messageHandlerFunc(func(_ context.Context, body []byte) error {
		assert.JSONEq(t, `{"message":"value"}`, string(body))
		return nil
	})
	client.afterDelete = cancel

	err := NewSQSConsumer(client, "queue-url", handler, discardLogger()).Run(ctx)
	require.NoError(t, err)
	require.NotNil(t, client.receiveInput)
	assert.Equal(t, int32(10), client.receiveInput.MaxNumberOfMessages)
	assert.Equal(t, int32(20), client.receiveInput.WaitTimeSeconds)
	assert.Equal(t, int32(120), client.receiveInput.VisibilityTimeout)
	require.NotNil(t, client.deleteInput)
	assert.Equal(t, "queue-url", aws.ToString(client.deleteInput.QueueUrl))
	assert.Equal(t, "receipt-1", aws.ToString(client.deleteInput.ReceiptHandle))
}

func TestSQSConsumerLeavesFailedMessageForRedelivery(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	client := &sqsClientStub{message: types.Message{
		Body: aws.String(`{"message":"value"}`), MessageId: aws.String("message-1"), ReceiptHandle: aws.String("receipt-1"),
	}}
	handler := messageHandlerFunc(func(_ context.Context, _ []byte) error {
		cancel()
		return errors.New("processing failed")
	})

	err := NewSQSConsumer(client, "queue-url", handler, discardLogger()).Run(ctx)
	require.NoError(t, err)
	assert.Nil(t, client.deleteInput)
}

type sqsClientStub struct {
	message      types.Message
	receiveInput *sqs.ReceiveMessageInput
	deleteInput  *sqs.DeleteMessageInput
	afterDelete  func()
	delivered    bool
}

func (s *sqsClientStub) ReceiveMessage(ctx context.Context, input *sqs.ReceiveMessageInput, _ ...func(*sqs.Options)) (*sqs.ReceiveMessageOutput, error) {
	if !s.delivered {
		s.delivered = true
		s.receiveInput = input
		return &sqs.ReceiveMessageOutput{Messages: []types.Message{s.message}}, nil
	}
	<-ctx.Done()
	return nil, ctx.Err()
}

func (s *sqsClientStub) DeleteMessage(_ context.Context, input *sqs.DeleteMessageInput, _ ...func(*sqs.Options)) (*sqs.DeleteMessageOutput, error) {
	s.deleteInput = input
	if s.afterDelete != nil {
		s.afterDelete()
	}
	return &sqs.DeleteMessageOutput{}, nil
}

type messageHandlerFunc func(context.Context, []byte) error

func (f messageHandlerFunc) Handle(ctx context.Context, body []byte) error {
	return f(ctx, body)
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
