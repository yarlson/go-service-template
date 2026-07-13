package messaging

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/aws/smithy-go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

const (
	sqsConcurrency       = 10
	sqsVisibilitySeconds = 120
	sqsLeaseRefresh      = 30 * time.Second
)

var ErrVisibilityLeaseLost = errors.New("SQS visibility lease lost")

type SQSClient interface {
	ReceiveMessage(context.Context, *sqs.ReceiveMessageInput, ...func(*sqs.Options)) (*sqs.ReceiveMessageOutput, error)
	DeleteMessage(context.Context, *sqs.DeleteMessageInput, ...func(*sqs.Options)) (*sqs.DeleteMessageOutput, error)
	ChangeMessageVisibility(context.Context, *sqs.ChangeMessageVisibilityInput, ...func(*sqs.Options)) (*sqs.ChangeMessageVisibilityOutput, error)
}

type MessageHandler interface {
	Handle(context.Context, []byte) error
}

type MessageProcess struct {
	Duration time.Duration
	QueueAge time.Duration
	Attempt  int
	Outcome  string
}

type ConsumerObserver interface {
	RecordMessageReceiveFailure(context.Context, string)
	RecordMessageProcess(context.Context, MessageProcess)
	AddMessagesInFlight(context.Context, int64)
}

type SQSConsumer struct {
	client       SQSClient
	handler      MessageHandler
	logger       *slog.Logger
	observer     ConsumerObserver
	queueURL     string
	leaseRefresh time.Duration
	backoff      func(int) time.Duration
	inFlight     sync.WaitGroup
}

func NewSQSConsumer(client SQSClient, queueURL string, handler MessageHandler, logger *slog.Logger, observer ConsumerObserver) *SQSConsumer {
	return &SQSConsumer{
		client: client, queueURL: queueURL, handler: handler, logger: logger, observer: observer,
		leaseRefresh: sqsLeaseRefresh, backoff: receiveBackoff,
	}
}

func (c *SQSConsumer) Run(ctx context.Context) error {
	err := c.RunWithHandlerContext(ctx, ctx)
	c.inFlight.Wait()
	return err
}

func (c *SQSConsumer) RunWithHandlerContext(receiveCtx, handlerCtx context.Context) error {
	semaphore := make(chan struct{}, sqsConcurrency)
	slotAvailable := make(chan struct{}, 1)
	consecutiveFailures := 0
	fatalCode := ""
	fatalCount := 0
	for {
		available := cap(semaphore) - len(semaphore)
		if available == 0 {
			select {
			case <-receiveCtx.Done():
				return nil
			case <-slotAvailable:
				continue
			}
		}

		messages, err := c.client.ReceiveMessage(receiveCtx, &sqs.ReceiveMessageInput{
			QueueUrl:            aws.String(c.queueURL),
			MaxNumberOfMessages: int32(available), //nolint:gosec // available is capped at ten
			WaitTimeSeconds:     20,
			VisibilityTimeout:   sqsVisibilitySeconds,
			MessageSystemAttributeNames: []types.MessageSystemAttributeName{
				types.MessageSystemAttributeNameApproximateReceiveCount,
				types.MessageSystemAttributeNameSentTimestamp,
			},
		})
		if err != nil {
			if receiveCtx.Err() != nil {
				return nil
			}
			if candidate := fatalAWSErrorCode(err); candidate != "" {
				if c.observer != nil {
					c.observer.RecordMessageReceiveFailure(receiveCtx, "fatal_candidate")
				}
				if candidate == fatalCode {
					fatalCount++
				} else {
					fatalCode, fatalCount = candidate, 1
				}
				if fatalCount >= 3 {
					return &FatalReceiveError{Code: candidate, Err: err}
				}
			} else {
				if c.observer != nil {
					c.observer.RecordMessageReceiveFailure(receiveCtx, "transient")
				}
				fatalCode, fatalCount = "", 0
			}
			consecutiveFailures++
			c.logger.WarnContext(receiveCtx, "receive SQS messages failed", "error", err)
			timer := time.NewTimer(c.backoff(consecutiveFailures))
			select {
			case <-receiveCtx.Done():
				timer.Stop()
				return nil
			case <-timer.C:
				continue
			}
		}
		consecutiveFailures = 0
		fatalCode, fatalCount = "", 0

		for _, message := range messages.Messages {
			semaphore <- struct{}{}
			c.inFlight.Add(1)
			go func(message types.Message) {
				defer c.inFlight.Done()
				defer func() {
					<-semaphore
					select {
					case slotAvailable <- struct{}{}:
					default:
					}
				}()
				c.handle(handlerCtx, message)
			}(message)
		}
	}
}

// Wait blocks until all messages already accepted by the consumer finish.
// Call it only after RunWithHandlerContext has returned or its receive context
// has been canceled, so no further work can be added.
func (c *SQSConsumer) Wait(ctx context.Context) error {
	done := make(chan struct{})
	go func() {
		c.inFlight.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *SQSConsumer) handle(parent context.Context, message types.Message) {
	parent, span := otel.Tracer("github.com/your-org/go-service-template/internal/platform/messaging").Start(
		parent,
		"sqs.process",
		trace.WithSpanKind(trace.SpanKindConsumer),
		trace.WithAttributes(attribute.Int("messaging.message.receive_count", receiveCount(message))),
	)
	defer span.End()
	started := time.Now()
	process := MessageProcess{Attempt: receiveCount(message), QueueAge: queueAge(message), Outcome: "failed"}
	if c.observer != nil {
		c.observer.AddMessagesInFlight(parent, 1)
		defer c.observer.AddMessagesInFlight(parent, -1)
		defer func() {
			process.Duration = time.Since(started)
			c.observer.RecordMessageProcess(parent, process)
		}()
	}

	messageID := aws.ToString(message.MessageId)
	if message.Body == nil || message.ReceiptHandle == nil {
		span.SetStatus(codes.Error, "invalid message")
		c.logger.ErrorContext(parent, "invalid SQS message", "message_id", messageID)
		return
	}

	handlerCtx, cancelHandler := context.WithCancelCause(parent)
	defer cancelHandler(nil)
	leaseCtx, cancelLease := context.WithCancel(handlerCtx)
	leaseErrors := make(chan error, 1)
	go func() {
		leaseErrors <- c.refreshLease(leaseCtx, cancelHandler, message)
	}()

	handlerErr := c.handler.Handle(handlerCtx, []byte(*message.Body))
	cancelLease()
	leaseErr := <-leaseErrors
	if leaseErr != nil {
		span.RecordError(leaseErr)
		span.SetStatus(codes.Error, "visibility lease lost")
		process.Outcome = "lease_lost"
		c.logger.ErrorContext(parent, "SQS visibility lease lost", "error", leaseErr, "message_id", messageID)
		return
	}
	if handlerErr != nil {
		span.RecordError(handlerErr)
		span.SetStatus(codes.Error, "message processing failed")
		c.logger.WarnContext(parent, "SQS message processing failed",
			"error", handlerErr,
			"message_id", messageID,
			"receive_count", process.Attempt,
		)
		return
	}
	if parent.Err() != nil {
		return
	}
	if _, err := c.client.DeleteMessage(parent, &sqs.DeleteMessageInput{
		QueueUrl: aws.String(c.queueURL), ReceiptHandle: message.ReceiptHandle,
	}); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "message acknowledgement failed")
		process.Outcome = "ack_failed"
		c.logger.WarnContext(parent, "delete SQS message failed", "error", fmt.Errorf("delete message: %w", err), "message_id", messageID)
		return
	}
	process.Outcome = "success"
}

func (c *SQSConsumer) refreshLease(ctx context.Context, cancelHandler context.CancelCauseFunc, message types.Message) error {
	ticker := time.NewTicker(c.leaseRefresh)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			requestCtx, cancelRequest := context.WithTimeout(ctx, 10*time.Second)
			_, err := c.client.ChangeMessageVisibility(requestCtx, &sqs.ChangeMessageVisibilityInput{
				QueueUrl: aws.String(c.queueURL), ReceiptHandle: message.ReceiptHandle, VisibilityTimeout: sqsVisibilitySeconds,
			})
			cancelRequest()
			if err != nil {
				if ctx.Err() != nil {
					return nil
				}
				leaseErr := fmt.Errorf("refresh message visibility: %w", err)
				cancelHandler(errors.Join(ErrVisibilityLeaseLost, leaseErr))
				return leaseErr
			}
		}
	}
}

func receiveCount(message types.Message) int {
	value := message.Attributes[string(types.MessageSystemAttributeNameApproximateReceiveCount)]
	count, err := strconv.Atoi(value)
	if err != nil || count < 1 {
		return 1
	}
	return count
}

func queueAge(message types.Message) time.Duration {
	value := message.Attributes[string(types.MessageSystemAttributeNameSentTimestamp)]
	milliseconds, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0
	}
	age := time.Since(time.UnixMilli(milliseconds))
	if age < 0 {
		return 0
	}
	return age
}

func receiveBackoff(failures int) time.Duration {
	maximum := min(time.Second<<min(failures-1, 5), 30*time.Second)
	return time.Duration(rand.Int64N(int64(maximum) + 1)) //nolint:gosec // backoff jitter is not security-sensitive
}

type FatalReceiveError struct {
	Code string
	Err  error
}

func (e *FatalReceiveError) Error() string {
	return fmt.Sprintf("receive SQS messages failed repeatedly with %s: %v", e.Code, e.Err)
}

func (e *FatalReceiveError) Unwrap() error {
	return e.Err
}

func fatalAWSErrorCode(err error) string {
	var apiError smithy.APIError
	if !errors.As(err, &apiError) {
		return ""
	}
	code := strings.ToLower(apiError.ErrorCode())
	for _, candidate := range []string{
		"accessdenied",
		"invalidclienttoken",
		"invalidsecurity",
		"invalidsignature",
		"kmsaccessdenied",
		"kmsdisabled",
		"kmsinvalidkeyusage",
		"nonexistentqueue",
		"notfound",
		"queuedoesnotexist",
		"resourcenotfound",
		"signaturedoesnotmatch",
		"unrecognizedclient",
	} {
		if strings.Contains(code, candidate) {
			return apiError.ErrorCode()
		}
	}
	return ""
}
