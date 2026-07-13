package messaging

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sns"
)

type SNSPublishClient interface {
	Publish(context.Context, *sns.PublishInput, ...func(*sns.Options)) (*sns.PublishOutput, error)
}

type PublishObserver interface {
	RecordMessagePublish(context.Context, time.Duration, error)
}

type SNSTopicPublisher struct {
	client   SNSPublishClient
	observer PublishObserver
	topicARN string
}

func NewSNSTopicPublisher(client SNSPublishClient, topicARN string, observer PublishObserver) *SNSTopicPublisher {
	return &SNSTopicPublisher{client: client, topicARN: topicARN, observer: observer}
}

func (p *SNSTopicPublisher) Publish(ctx context.Context, message []byte) error {
	started := time.Now()
	if _, err := p.client.Publish(ctx, &sns.PublishInput{
		TopicArn: aws.String(p.topicARN),
		Message:  aws.String(string(message)),
	}); err != nil {
		if p.observer != nil {
			p.observer.RecordMessagePublish(ctx, time.Since(started), err)
		}
		return fmt.Errorf("publish SNS message: %w", err)
	}
	if p.observer != nil {
		p.observer.RecordMessagePublish(ctx, time.Since(started), nil)
	}
	return nil
}
