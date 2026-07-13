//go:build integration

package messaging

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/sns"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

const localStackImage = "localstack/localstack:4.14.0@sha256:3ebc37595918b8accb852f8048fef2aff047d465167edd655528065b07bc364a"

func TestSNSAndSQSTopologyIntegration(t *testing.T) {
	awsConfig := newLocalStackAWSConfig(t)
	snsClient := sns.NewFromConfig(awsConfig)
	sqsClient := sqs.NewFromConfig(awsConfig)
	topology := createTestTopology(t, snsClient, sqsClient, "redrive")
	publisher := NewSNSTopicPublisher(snsClient, topology.topicARN, nil)

	require.NoError(t, publisher.Publish(t.Context(), []byte(`{"type":"unrelated.event"}`)))
	want := Envelope[map[string]any]{
		ID:        "event-1",
		Timestamp: time.Date(2026, time.July, 13, 8, 0, 0, 0, time.UTC),
		Type:      "permissions.changed",
		Payload:   map[string]any{"userId": "0198a1f7-30b7-7df7-8491-c47f6033525b", "revision": float64(3), "permissions": []any{"read"}},
		Metadata: Metadata{
			SchemaVersion: "1.0.0", ProducedBy: "permissions", OriginatedFrom: "permissions", CorrelationID: "correlation-1",
		},
	}
	message, err := json.Marshal(want)
	require.NoError(t, err)
	require.NoError(t, publisher.Publish(t.Context(), message))

	firstDelivery := receiveOne(t, sqsClient, topology.queueURL, 0, "first event delivery")
	got, err := DecodeSNSNotification[map[string]any]([]byte(aws.ToString(firstDelivery.Body)), "permissions.changed")
	require.NoError(t, err)
	assert.Equal(t, want.ID, got.ID)
	assert.Equal(t, want.Metadata, got.Metadata)
	assert.Equal(t, want.Payload, got.Payload)
	for attempt := 1; attempt < 5; attempt++ {
		receiveOne(t, sqsClient, topology.queueURL, 0, fmt.Sprintf("unacknowledged message receive %d", attempt+1))
	}
	_, err = sqsClient.ReceiveMessage(t.Context(), &sqs.ReceiveMessageInput{
		QueueUrl: aws.String(topology.queueURL), MaxNumberOfMessages: 1, WaitTimeSeconds: 1,
	})
	require.NoError(t, err)
	receiveOne(t, sqsClient, topology.deadLetterQueueURL, 30, "redriven message")
	filtered, err := sqsClient.ReceiveMessage(t.Context(), &sqs.ReceiveMessageInput{
		QueueUrl: aws.String(topology.queueURL), MaxNumberOfMessages: 1, WaitTimeSeconds: 1,
	})
	require.NoError(t, err)
	assert.Empty(t, filtered.Messages, "the unrelated event must be filtered out")

	acknowledgementTopology := createTestTopology(t, snsClient, sqsClient, "ack")
	acknowledgementPublisher := NewSNSTopicPublisher(snsClient, acknowledgementTopology.topicARN, nil)
	require.NoError(t, acknowledgementPublisher.Publish(t.Context(), message))
	ctx, cancel := context.WithCancel(t.Context())
	client := &cancelAfterDeleteClient{Client: sqsClient, cancel: cancel}
	handler := &capturingHandler{body: make(chan []byte, 1)}
	require.NoError(t, NewSQSConsumer(client, acknowledgementTopology.queueURL, handler, discardLogger(), nil).Run(ctx))

	gotBody := <-handler.body
	got, err = DecodeSNSNotification[map[string]any](gotBody, "permissions.changed")
	require.NoError(t, err)
	assert.Equal(t, want.ID, got.ID)
	assert.Equal(t, want.Metadata, got.Metadata)
	assert.Equal(t, want.Payload, got.Payload)

	empty, err := sqsClient.ReceiveMessage(t.Context(), &sqs.ReceiveMessageInput{
		QueueUrl: aws.String(acknowledgementTopology.queueURL), MaxNumberOfMessages: 1, WaitTimeSeconds: 1,
	})
	require.NoError(t, err)
	assert.Empty(t, empty.Messages, "the acknowledged event must not remain")
}

type testTopology struct {
	topicARN           string
	queueURL           string
	deadLetterQueueURL string
}

func createTestTopology(t *testing.T, snsClient *sns.Client, sqsClient *sqs.Client, suffix string) testTopology {
	t.Helper()

	topic, err := snsClient.CreateTopic(t.Context(), &sns.CreateTopicInput{Name: aws.String("permissions-" + suffix)})
	require.NoError(t, err)
	deadLetterQueue, err := sqsClient.CreateQueue(t.Context(), &sqs.CreateQueueInput{
		QueueName: aws.String("permissions-" + suffix + "-dlq"),
		Attributes: map[string]string{
			string(types.QueueAttributeNameSqsManagedSseEnabled): "true",
		},
	})
	require.NoError(t, err)
	deadLetterQueueARN := queueARN(t, sqsClient, aws.ToString(deadLetterQueue.QueueUrl))
	redrivePolicy, err := json.Marshal(map[string]any{"deadLetterTargetArn": deadLetterQueueARN, "maxReceiveCount": 5})
	require.NoError(t, err)
	queue, err := sqsClient.CreateQueue(t.Context(), &sqs.CreateQueueInput{
		QueueName: aws.String("permissions-" + suffix + "-consumer"),
		Attributes: map[string]string{
			string(types.QueueAttributeNameRedrivePolicy):        string(redrivePolicy),
			string(types.QueueAttributeNameSqsManagedSseEnabled): "true",
		},
	})
	require.NoError(t, err)
	queueURL := aws.ToString(queue.QueueUrl)
	queueARN := queueARN(t, sqsClient, queueURL)
	policy := fmt.Sprintf(`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":{"Service":"sns.amazonaws.com"},"Action":"sqs:SendMessage","Resource":%q,"Condition":{"ArnEquals":{"aws:SourceArn":%q}}}]}`, queueARN, aws.ToString(topic.TopicArn))
	_, err = sqsClient.SetQueueAttributes(t.Context(), &sqs.SetQueueAttributesInput{
		QueueUrl: aws.String(queueURL), Attributes: map[string]string{string(types.QueueAttributeNamePolicy): policy},
	})
	require.NoError(t, err)
	_, err = snsClient.Subscribe(t.Context(), &sns.SubscribeInput{
		TopicArn: aws.String(aws.ToString(topic.TopicArn)),
		Protocol: aws.String("sqs"),
		Endpoint: aws.String(queueARN),
		Attributes: map[string]string{
			"RawMessageDelivery": "false",
			"FilterPolicyScope":  "MessageBody",
			"FilterPolicy":       `{"type":["permissions.changed"]}`,
		},
	})
	require.NoError(t, err)

	return testTopology{
		topicARN: aws.ToString(topic.TopicArn), queueURL: queueURL, deadLetterQueueURL: aws.ToString(deadLetterQueue.QueueUrl),
	}
}

func queueARN(t *testing.T, client *sqs.Client, queueURL string) string {
	t.Helper()

	attributes, err := client.GetQueueAttributes(t.Context(), &sqs.GetQueueAttributesInput{
		QueueUrl: aws.String(queueURL), AttributeNames: []types.QueueAttributeName{types.QueueAttributeNameQueueArn},
	})
	require.NoError(t, err)
	return attributes.Attributes[string(types.QueueAttributeNameQueueArn)]
}

func receiveOne(t *testing.T, client *sqs.Client, queueURL string, visibilityTimeout int32, description string) types.Message {
	t.Helper()

	for range 10 {
		output, err := client.ReceiveMessage(t.Context(), &sqs.ReceiveMessageInput{
			QueueUrl: aws.String(queueURL), MaxNumberOfMessages: 1, WaitTimeSeconds: 1, VisibilityTimeout: visibilityTimeout,
		})
		require.NoError(t, err)
		if len(output.Messages) == 1 {
			if visibilityTimeout == 0 {
				_, err = client.ChangeMessageVisibility(t.Context(), &sqs.ChangeMessageVisibilityInput{
					QueueUrl: aws.String(queueURL), ReceiptHandle: output.Messages[0].ReceiptHandle, VisibilityTimeout: 0,
				})
				require.NoError(t, err)
			}
			return output.Messages[0]
		}
	}
	require.FailNow(t, "message was not received", description)
	return types.Message{}
}

func newLocalStackAWSConfig(t *testing.T) aws.Config {
	t.Helper()

	container, err := testcontainers.Run(t.Context(), localStackImage,
		testcontainers.WithExposedPorts("4566/tcp"),
		testcontainers.WithEnv(map[string]string{"SERVICES": "sns,sqs"}),
		testcontainers.WithWaitStrategy(wait.ForHTTP("/_localstack/health").WithPort("4566/tcp").WithStartupTimeout(2*time.Minute)),
	)
	require.NoError(t, err)
	testcontainers.CleanupContainer(t, container)
	host, err := container.Host(t.Context())
	require.NoError(t, err)
	port, err := container.MappedPort(t.Context(), "4566/tcp")
	require.NoError(t, err)
	endpoint := "http://" + net.JoinHostPort(host, port.Port())
	config, err := awsconfig.LoadDefaultConfig(t.Context(),
		awsconfig.WithRegion("eu-west-1"),
		awsconfig.WithBaseEndpoint(endpoint),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider("test", "test", "")),
	)
	require.NoError(t, err)
	return config
}

type capturingHandler struct {
	body chan []byte
}

func (h *capturingHandler) Handle(_ context.Context, body []byte) error {
	h.body <- body
	return nil
}

type cancelAfterDeleteClient struct {
	*sqs.Client
	cancel context.CancelFunc
}

func (c *cancelAfterDeleteClient) DeleteMessage(ctx context.Context, input *sqs.DeleteMessageInput, options ...func(*sqs.Options)) (*sqs.DeleteMessageOutput, error) {
	output, err := c.Client.DeleteMessage(ctx, input, options...)
	if err == nil {
		c.cancel()
	}
	return output, err
}
