package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/sns"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWorkerDependenciesCheckDatabaseAndMessaging(t *testing.T) {
	t.Parallel()

	observer := &dependencyObserverStub{}
	dependencies := workerDependencies{
		database: pingerStub{},
		sqs: &sqsReadinessStub{output: &sqs.GetQueueAttributesOutput{Attributes: map[string]string{
			string(types.QueueAttributeNameApproximateNumberOfMessages):           "4",
			string(types.QueueAttributeNameApproximateNumberOfMessagesNotVisible): "2",
		}}},
		sns: &snsReadinessStub{}, observer: observer, queueURL: "queue", topicARN: "topic",
	}

	require.NoError(t, dependencies.Ping(t.Context()))
	assert.Equal(t, int64(4), observer.visible)
	assert.Equal(t, int64(2), observer.inFlight)
	assert.Equal(t, []string{"sqs", "sns"}, observer.awsChecks)
}

func TestWorkerDependenciesReportIndividualFailures(t *testing.T) {
	t.Parallel()

	for name, dependencies := range map[string]workerDependencies{
		"database": {
			database: pingerStub{err: errors.New("database unavailable")}, observer: &dependencyObserverStub{},
		},
		"queue": {
			database: pingerStub{}, sqs: &sqsReadinessStub{err: errors.New("queue unavailable")},
			observer: &dependencyObserverStub{}, queueURL: "queue",
		},
		"topic": {
			database: pingerStub{}, sns: &snsReadinessStub{err: errors.New("topic unavailable")},
			observer: &dependencyObserverStub{}, topicARN: "topic",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			require.Error(t, dependencies.Ping(t.Context()))
		})
	}
}

type pingerStub struct {
	err error
}

func (s pingerStub) Ping(context.Context) error { return s.err }

type sqsReadinessStub struct {
	output *sqs.GetQueueAttributesOutput
	err    error
}

func (s *sqsReadinessStub) GetQueueAttributes(context.Context, *sqs.GetQueueAttributesInput, ...func(*sqs.Options)) (*sqs.GetQueueAttributesOutput, error) {
	return s.output, s.err
}

type snsReadinessStub struct {
	err error
}

func (s *snsReadinessStub) GetTopicAttributes(context.Context, *sns.GetTopicAttributesInput, ...func(*sns.Options)) (*sns.GetTopicAttributesOutput, error) {
	return &sns.GetTopicAttributesOutput{}, s.err
}

type dependencyObserverStub struct {
	awsChecks []string
	visible   int64
	inFlight  int64
}

func (*dependencyObserverStub) RecordDatabaseCheck(context.Context, time.Duration, error) {}

func (s *dependencyObserverStub) RecordAWSCheck(_ context.Context, dependency string, _ time.Duration, _ error) {
	s.awsChecks = append(s.awsChecks, dependency)
}

func (s *dependencyObserverStub) RecordSQSBacklog(_ context.Context, visible, inFlight int64) {
	s.visible = visible
	s.inFlight = inFlight
}
