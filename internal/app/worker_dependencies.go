package app

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sns"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
)

type databasePinger interface {
	Ping(context.Context) error
}

type sqsReadinessClient interface {
	GetQueueAttributes(context.Context, *sqs.GetQueueAttributesInput, ...func(*sqs.Options)) (*sqs.GetQueueAttributesOutput, error)
}

type snsReadinessClient interface {
	GetTopicAttributes(context.Context, *sns.GetTopicAttributesInput, ...func(*sns.Options)) (*sns.GetTopicAttributesOutput, error)
}

type dependencyObserver interface {
	RecordDatabaseCheck(context.Context, time.Duration, error)
	RecordAWSCheck(context.Context, string, time.Duration, error)
	RecordSQSBacklog(context.Context, int64, int64)
}

type workerDependencies struct {
	database databasePinger
	sqs      sqsReadinessClient
	sns      snsReadinessClient
	observer dependencyObserver
	queueURL string
	topicARN string
}

func (d workerDependencies) Ping(ctx context.Context) error {
	started := time.Now()
	err := d.database.Ping(ctx)
	d.observer.RecordDatabaseCheck(ctx, time.Since(started), err)
	if err != nil {
		return fmt.Errorf("check PostgreSQL: %w", err)
	}

	if d.queueURL != "" {
		started = time.Now()
		attributes, checkErr := d.sqs.GetQueueAttributes(ctx, &sqs.GetQueueAttributesInput{
			QueueUrl: aws.String(d.queueURL),
			AttributeNames: []types.QueueAttributeName{
				types.QueueAttributeNameApproximateNumberOfMessages,
				types.QueueAttributeNameApproximateNumberOfMessagesNotVisible,
				types.QueueAttributeNameQueueArn,
			},
		})
		if checkErr != nil {
			d.observer.RecordAWSCheck(ctx, "sqs", time.Since(started), checkErr)
			return fmt.Errorf("check permissions queue: %w", checkErr)
		}
		visible, parseErr := parseQueueCount(attributes, types.QueueAttributeNameApproximateNumberOfMessages)
		if parseErr != nil {
			d.observer.RecordAWSCheck(ctx, "sqs", time.Since(started), parseErr)
			return parseErr
		}
		inFlight, parseErr := parseQueueCount(attributes, types.QueueAttributeNameApproximateNumberOfMessagesNotVisible)
		if parseErr != nil {
			d.observer.RecordAWSCheck(ctx, "sqs", time.Since(started), parseErr)
			return parseErr
		}
		d.observer.RecordAWSCheck(ctx, "sqs", time.Since(started), nil)
		d.observer.RecordSQSBacklog(ctx, visible, inFlight)
	}

	if d.topicARN != "" {
		started = time.Now()
		_, checkErr := d.sns.GetTopicAttributes(ctx, &sns.GetTopicAttributesInput{TopicArn: aws.String(d.topicARN)})
		d.observer.RecordAWSCheck(ctx, "sns", time.Since(started), checkErr)
		if checkErr != nil {
			return fmt.Errorf("check user events topic: %w", checkErr)
		}
	}
	return nil
}

func parseQueueCount(attributes *sqs.GetQueueAttributesOutput, name types.QueueAttributeName) (int64, error) {
	value := attributes.Attributes[string(name)]
	count, err := strconv.ParseInt(value, 10, 64)
	if err != nil || count < 0 {
		return 0, fmt.Errorf("invalid SQS attribute %s", name)
	}
	return count, nil
}
