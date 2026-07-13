package messaging

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sns"
)

type SNSPublishClient interface {
	Publish(context.Context, *sns.PublishInput, ...func(*sns.Options)) (*sns.PublishOutput, error)
}

type SNSTopicPublisher struct {
	client   SNSPublishClient
	topicARN string
}

func NewSNSTopicPublisher(client SNSPublishClient, topicARN string) *SNSTopicPublisher {
	return &SNSTopicPublisher{client: client, topicARN: topicARN}
}

func (p *SNSTopicPublisher) Publish(ctx context.Context, message []byte) error {
	if _, err := p.client.Publish(ctx, &sns.PublishInput{
		TopicArn: aws.String(p.topicARN),
		Message:  aws.String(string(message)),
	}); err != nil {
		return fmt.Errorf("publish SNS message: %w", err)
	}
	return nil
}
