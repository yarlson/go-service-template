package messaging

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/aws/smithy-go"
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

	err := NewSQSConsumer(client, "queue-url", handler, discardLogger(), nil).Run(ctx)
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

	err := NewSQSConsumer(client, "queue-url", handler, discardLogger(), nil).Run(ctx)
	require.NoError(t, err)
	assert.Nil(t, client.deleteInput)
}

func TestSQSConsumerCancelsHandlerWhenVisibilityLeaseIsLost(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	client := &sqsClientStub{
		message:   types.Message{Body: aws.String(`{}`), MessageId: aws.String("message-1"), ReceiptHandle: aws.String("receipt-1")},
		changeErr: errors.New("lease unavailable"),
	}
	causes := make(chan error, 1)
	handler := messageHandlerFunc(func(handlerCtx context.Context, _ []byte) error {
		<-handlerCtx.Done()
		causes <- context.Cause(handlerCtx)
		cancel()
		return handlerCtx.Err()
	})
	observer := &messagingObserverStub{}
	consumer := NewSQSConsumer(client, "queue-url", handler, discardLogger(), observer)
	consumer.leaseRefresh = time.Millisecond

	require.NoError(t, consumer.Run(ctx))
	require.ErrorIs(t, <-causes, ErrVisibilityLeaseLost)
	assert.Nil(t, client.deleteInput)
	require.NotNil(t, client.changeInput)
	assert.Equal(t, int32(120), client.changeInput.VisibilityTimeout)
	require.Len(t, observer.processes, 1)
	assert.Equal(t, "lease_lost", observer.processes[0].Outcome)
}

func TestSQSConsumerRecoversAfterTransientReceiveError(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	client := &sqsClientStub{
		message:       types.Message{Body: aws.String(`{}`), MessageId: aws.String("message-1"), ReceiptHandle: aws.String("receipt-1")},
		receiveErrors: []error{errors.New("temporary network failure")},
		afterDelete:   cancel,
	}
	consumer := NewSQSConsumer(client, "queue-url", messageHandlerFunc(func(context.Context, []byte) error { return nil }), discardLogger(), nil)
	consumer.backoff = func(int) time.Duration { return 0 }

	require.NoError(t, consumer.Run(ctx))
	assert.GreaterOrEqual(t, client.receiveCalls, 2)
	require.NotNil(t, client.deleteInput)
}

func TestSQSConsumerStopsAfterRepeatedFatalReceiveError(t *testing.T) {
	t.Parallel()

	fatal := &smithy.GenericAPIError{Code: "AccessDeniedException", Message: "denied", Fault: smithy.FaultClient}
	client := &sqsClientStub{receiveErrors: []error{fatal, fatal, fatal}}
	consumer := NewSQSConsumer(client, "queue-url", messageHandlerFunc(func(context.Context, []byte) error { return nil }), discardLogger(), nil)
	consumer.backoff = func(int) time.Duration { return 0 }

	err := consumer.Run(t.Context())
	var fatalError *FatalReceiveError
	require.ErrorAs(t, err, &fatalError)
	assert.Equal(t, "AccessDeniedException", fatalError.Code)
	assert.Equal(t, 3, client.receiveCalls)
}

func TestSQSConsumerReportsFatalReceiveErrorWithoutWaitingForHandler(t *testing.T) {
	t.Parallel()

	fatal := &smithy.GenericAPIError{Code: "AccessDeniedException", Message: "denied", Fault: smithy.FaultClient}
	client := &fatalAfterMessageSQSClient{fatal: fatal}
	started := make(chan struct{})
	release := make(chan struct{})
	handler := messageHandlerFunc(func(context.Context, []byte) error {
		close(started)
		<-release
		return nil
	})
	consumer := NewSQSConsumer(client, "queue-url", handler, discardLogger(), nil)
	consumer.backoff = func(int) time.Duration { return 0 }
	errors := make(chan error, 1)
	go func() { errors <- consumer.RunWithHandlerContext(t.Context(), t.Context()) }()

	<-started
	err := <-errors
	var fatalError *FatalReceiveError
	require.ErrorAs(t, err, &fatalError)
	assert.Equal(t, "AccessDeniedException", fatalError.Code)
	close(release)
	require.NoError(t, consumer.Wait(t.Context()))
}

func TestSQSConsumerDoesNotReceiveBeyondAvailableCapacity(t *testing.T) {
	t.Parallel()

	client := &batchSQSClient{}
	started := make(chan struct{}, sqsConcurrency)
	release := make(chan struct{})
	handler := messageHandlerFunc(func(context.Context, []byte) error {
		started <- struct{}{}
		<-release
		return errors.New("stop")
	})
	receiveCtx, stopReceiving := context.WithCancel(t.Context())
	handlerCtx, cancelHandlers := context.WithCancel(t.Context())
	defer cancelHandlers()
	consumer := NewSQSConsumer(client, "queue-url", handler, discardLogger(), nil)
	done := make(chan error, 1)
	go func() {
		done <- consumer.RunWithHandlerContext(receiveCtx, handlerCtx)
	}()
	for range sqsConcurrency {
		<-started
	}
	stopReceiving()
	close(release)
	require.NoError(t, <-done)
	require.NoError(t, consumer.Wait(t.Context()))
	assert.Equal(t, 1, client.receiveCalls)
}

func TestSQSConsumerKeepsLeaseWhileDraining(t *testing.T) {
	t.Parallel()

	client := &sqsClientStub{
		message:      types.Message{Body: aws.String(`{}`), MessageId: aws.String("message-1"), ReceiptHandle: aws.String("receipt-1")},
		changeCalled: make(chan struct{}, 1),
	}
	started := make(chan struct{})
	handler := messageHandlerFunc(func(ctx context.Context, _ []byte) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	})
	receiveCtx, stopReceiving := context.WithCancel(t.Context())
	handlerCtx, cancelHandlers := context.WithCancel(t.Context())
	consumer := NewSQSConsumer(client, "queue-url", handler, discardLogger(), nil)
	consumer.leaseRefresh = time.Millisecond
	done := make(chan error, 1)
	go func() { done <- consumer.RunWithHandlerContext(receiveCtx, handlerCtx) }()

	<-started
	stopReceiving()
	<-client.changeCalled
	cancelHandlers()
	require.NoError(t, <-done)
	require.NoError(t, consumer.Wait(t.Context()))
	assert.Nil(t, client.deleteInput)
}

func TestSQSConsumerAcknowledgesAfterCompletedHandlerCancelsRenewal(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	client := &sqsClientStub{
		message:       types.Message{Body: aws.String(`{}`), MessageId: aws.String("message-1"), ReceiptHandle: aws.String("receipt-1")},
		changeStarted: make(chan struct{}),
		blockChange:   true,
		afterDelete:   cancel,
	}
	handler := messageHandlerFunc(func(context.Context, []byte) error {
		<-client.changeStarted
		return nil
	})
	consumer := NewSQSConsumer(client, "queue-url", handler, discardLogger(), nil)
	consumer.leaseRefresh = time.Millisecond

	require.NoError(t, consumer.Run(ctx))
	require.NotNil(t, client.deleteInput)
}

type sqsClientStub struct {
	mu            sync.Mutex
	message       types.Message
	receiveInput  *sqs.ReceiveMessageInput
	deleteInput   *sqs.DeleteMessageInput
	afterDelete   func()
	delivered     bool
	receiveCalls  int
	receiveErrors []error
	changeInput   *sqs.ChangeMessageVisibilityInput
	changeErr     error
	changeCalled  chan struct{}
	changeStarted chan struct{}
	blockChange   bool
}

func (s *sqsClientStub) ReceiveMessage(ctx context.Context, input *sqs.ReceiveMessageInput, _ ...func(*sqs.Options)) (*sqs.ReceiveMessageOutput, error) {
	s.mu.Lock()
	s.receiveCalls++
	if len(s.receiveErrors) > 0 {
		err := s.receiveErrors[0]
		s.receiveErrors = s.receiveErrors[1:]
		s.mu.Unlock()
		return nil, err
	}
	if !s.delivered {
		s.delivered = true
		s.receiveInput = input
		s.mu.Unlock()
		return &sqs.ReceiveMessageOutput{Messages: []types.Message{s.message}}, nil
	}
	s.mu.Unlock()
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

func (s *sqsClientStub) ChangeMessageVisibility(ctx context.Context, input *sqs.ChangeMessageVisibilityInput, _ ...func(*sqs.Options)) (*sqs.ChangeMessageVisibilityOutput, error) {
	s.changeInput = input
	if s.changeCalled != nil {
		s.changeCalled <- struct{}{}
	}
	if s.changeStarted != nil {
		close(s.changeStarted)
	}
	if s.blockChange {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return &sqs.ChangeMessageVisibilityOutput{}, s.changeErr
}

type messageHandlerFunc func(context.Context, []byte) error

func (f messageHandlerFunc) Handle(ctx context.Context, body []byte) error {
	return f(ctx, body)
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

type batchSQSClient struct {
	receiveCalls int
}

func (c *batchSQSClient) ReceiveMessage(ctx context.Context, _ *sqs.ReceiveMessageInput, _ ...func(*sqs.Options)) (*sqs.ReceiveMessageOutput, error) {
	c.receiveCalls++
	if c.receiveCalls == 1 {
		messages := make([]types.Message, 0, sqsConcurrency)
		for index := range sqsConcurrency {
			messages = append(messages, types.Message{
				Body: aws.String(`{}`), MessageId: aws.String("message"), ReceiptHandle: aws.String(string(rune('a' + index))),
			})
		}
		return &sqs.ReceiveMessageOutput{Messages: messages}, nil
	}
	<-ctx.Done()
	return nil, ctx.Err()
}

func (*batchSQSClient) DeleteMessage(context.Context, *sqs.DeleteMessageInput, ...func(*sqs.Options)) (*sqs.DeleteMessageOutput, error) {
	return &sqs.DeleteMessageOutput{}, nil
}

func (*batchSQSClient) ChangeMessageVisibility(context.Context, *sqs.ChangeMessageVisibilityInput, ...func(*sqs.Options)) (*sqs.ChangeMessageVisibilityOutput, error) {
	return &sqs.ChangeMessageVisibilityOutput{}, nil
}

type fatalAfterMessageSQSClient struct {
	mu    sync.Mutex
	calls int
	fatal error
}

func (c *fatalAfterMessageSQSClient) ReceiveMessage(context.Context, *sqs.ReceiveMessageInput, ...func(*sqs.Options)) (*sqs.ReceiveMessageOutput, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	if c.calls == 1 {
		return &sqs.ReceiveMessageOutput{Messages: []types.Message{{
			Body: aws.String(`{}`), MessageId: aws.String("message-1"), ReceiptHandle: aws.String("receipt-1"),
		}}}, nil
	}
	return nil, c.fatal
}

func (*fatalAfterMessageSQSClient) DeleteMessage(context.Context, *sqs.DeleteMessageInput, ...func(*sqs.Options)) (*sqs.DeleteMessageOutput, error) {
	return &sqs.DeleteMessageOutput{}, nil
}

func (*fatalAfterMessageSQSClient) ChangeMessageVisibility(context.Context, *sqs.ChangeMessageVisibilityInput, ...func(*sqs.Options)) (*sqs.ChangeMessageVisibilityOutput, error) {
	return &sqs.ChangeMessageVisibilityOutput{}, nil
}
