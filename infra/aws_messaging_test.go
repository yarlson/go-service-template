package infra

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestAWSMessagingTemplateOwnsRequiredTopology(t *testing.T) {
	contents, err := os.ReadFile("aws-messaging.yaml")
	require.NoError(t, err)
	var template map[string]any
	require.NoError(t, yaml.Unmarshal(contents, &template))

	resources := requireMap(t, template, "Resources")
	for _, name := range []string{
		"UserEventsTopic",
		"PermissionsDeadLetterQueue",
		"PermissionsQueue",
		"PermissionsQueuePolicy",
		"PermissionsSubscription",
		"WorkerPolicy",
	} {
		assert.Contains(t, resources, name)
	}

	subscription := requireMap(t, requireMap(t, resources, "PermissionsSubscription"), "Properties")
	assert.Equal(t, false, subscription["RawMessageDelivery"])
	assert.Equal(t, "MessageBody", subscription["FilterPolicyScope"])
	filter := requireMap(t, subscription, "FilterPolicy")
	assert.Equal(t, []any{"permissions.changed"}, filter["type"])

	queue := requireMap(t, requireMap(t, resources, "PermissionsQueue"), "Properties")
	assert.Equal(t, 120, queue["VisibilityTimeout"])
	assert.Equal(t, true, queue["SqsManagedSseEnabled"])
	redrive := requireMap(t, queue, "RedrivePolicy")
	assert.Equal(t, 5, redrive["maxReceiveCount"])

	deadLetterQueue := requireMap(t, requireMap(t, resources, "PermissionsDeadLetterQueue"), "Properties")
	assert.Equal(t, true, deadLetterQueue["SqsManagedSseEnabled"])
	assert.Equal(t, 604800, deadLetterQueue["MessageRetentionPeriod"])

	outputs := requireMap(t, template, "Outputs")
	for _, name := range []string{
		"UserEventsTopicArn",
		"PermissionsQueueUrl",
		"PermissionsQueueArn",
		"PermissionsDeadLetterQueueArn",
		"WorkerPolicyArn",
	} {
		assert.Contains(t, outputs, name)
	}
}

func requireMap(t *testing.T, values map[string]any, key string) map[string]any {
	t.Helper()
	value, exists := values[key]
	require.True(t, exists, "%s must exist", key)
	result, ok := value.(map[string]any)
	require.True(t, ok, "%s must be a mapping", key)
	return result
}
