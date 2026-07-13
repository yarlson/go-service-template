package messaging

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/sns"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSNSTopicPublisher(t *testing.T) {
	t.Parallel()

	client := &snsPublishClientStub{}
	publisher := NewSNSTopicPublisher(client, "arn:aws:sns:eu-west-1:123456789012:user-events")
	require.NoError(t, publisher.Publish(t.Context(), []byte(`{"type":"user.created"}`)))
	require.NotNil(t, client.input)
	assert.Equal(t, "arn:aws:sns:eu-west-1:123456789012:user-events", *client.input.TopicArn)
	assert.JSONEq(t, `{"type":"user.created"}`, *client.input.Message)
}

func TestSNSTopicPublisherPreservesErrors(t *testing.T) {
	t.Parallel()

	want := errors.New("SNS unavailable")
	publisher := NewSNSTopicPublisher(&snsPublishClientStub{err: want}, "topic")
	err := publisher.Publish(t.Context(), []byte("message"))
	require.ErrorIs(t, err, want)
}

type snsPublishClientStub struct {
	input *sns.PublishInput
	err   error
}

func (c *snsPublishClientStub) Publish(_ context.Context, input *sns.PublishInput, _ ...func(*sns.Options)) (*sns.PublishOutput, error) {
	c.input = input
	return &sns.PublishOutput{}, c.err
}
