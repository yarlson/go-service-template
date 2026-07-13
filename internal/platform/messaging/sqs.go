package messaging

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
)

type SQSClient interface {
	ReceiveMessage(context.Context, *sqs.ReceiveMessageInput, ...func(*sqs.Options)) (*sqs.ReceiveMessageOutput, error)
	DeleteMessage(context.Context, *sqs.DeleteMessageInput, ...func(*sqs.Options)) (*sqs.DeleteMessageOutput, error)
}

type MessageHandler interface {
	Handle(context.Context, []byte) error
}

type SQSConsumer struct {
	client   SQSClient
	handler  MessageHandler
	logger   *slog.Logger
	queueURL string
}

func NewSQSConsumer(client SQSClient, queueURL string, handler MessageHandler, logger *slog.Logger) *SQSConsumer {
	return &SQSConsumer{client: client, queueURL: queueURL, handler: handler, logger: logger}
}

func (c *SQSConsumer) Run(ctx context.Context) error {
	semaphore := make(chan struct{}, 10)
	var inFlight sync.WaitGroup
	defer inFlight.Wait()

	for {
		messages, err := c.client.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
			QueueUrl:            aws.String(c.queueURL),
			MaxNumberOfMessages: 10,
			WaitTimeSeconds:     20,
			VisibilityTimeout:   120,
			MessageSystemAttributeNames: []types.MessageSystemAttributeName{
				types.MessageSystemAttributeNameApproximateReceiveCount,
			},
		})
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			c.logger.WarnContext(ctx, "receive SQS messages failed", "error", err)
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(time.Second):
				continue
			}
		}

		for _, message := range messages.Messages {
			select {
			case semaphore <- struct{}{}:
			case <-ctx.Done():
				return nil
			}
			inFlight.Add(1)
			go func(message types.Message) {
				defer inFlight.Done()
				defer func() { <-semaphore }()
				c.handle(ctx, message)
			}(message)
		}
	}
}

func (c *SQSConsumer) handle(ctx context.Context, message types.Message) {
	messageID := aws.ToString(message.MessageId)
	if message.Body == nil || message.ReceiptHandle == nil {
		c.logger.ErrorContext(ctx, "invalid SQS message", "message_id", messageID)
		return
	}
	if err := c.handler.Handle(ctx, []byte(*message.Body)); err != nil {
		c.logger.WarnContext(ctx, "SQS message processing failed",
			"error", err,
			"message_id", messageID,
			"receive_count", message.Attributes[string(types.MessageSystemAttributeNameApproximateReceiveCount)],
		)
		return
	}
	if _, err := c.client.DeleteMessage(ctx, &sqs.DeleteMessageInput{
		QueueUrl: aws.String(c.queueURL), ReceiptHandle: message.ReceiptHandle,
	}); err != nil {
		c.logger.WarnContext(ctx, "delete SQS message failed", "error", fmt.Errorf("delete message: %w", err), "message_id", messageID)
	}
}
