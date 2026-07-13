#!/bin/sh
set -eu

project=go-service-template-smoke
image=go-service-template:smoke
container=go-service-template-smoke-api
worker_container=go-service-template-smoke-worker
localstack_container=go-service-template-smoke-localstack
localstack_image='localstack/localstack:4.14.0@sha256:3ebc37595918b8accb852f8048fef2aff047d465167edd655528065b07bc364a'
version=smoke
commit=smoke-commit
source_url=https://github.com/example/service
export POSTGRES_PORT=0

image_label() {
  docker image inspect --format "{{ index .Config.Labels \"$1\" }}" "$image"
}

cleanup() {
  status=$?
  trap - EXIT
  if [ "$status" -ne 0 ]; then
    docker logs "$container" 2>/dev/null || true
    docker logs "$worker_container" 2>/dev/null || true
    docker logs "$localstack_container" 2>/dev/null || true
  fi
  docker rm -f "$container" >/dev/null 2>&1 || true
  docker rm -f "$worker_container" >/dev/null 2>&1 || true
  docker rm -f "$localstack_container" >/dev/null 2>&1 || true
  docker compose -p "$project" down -v >/dev/null 2>&1 || true
  exit "$status"
}
trap cleanup EXIT

docker compose -p "$project" up -d --wait postgres
postgres_container=$(docker compose -p "$project" ps -q postgres)
network=$(docker inspect -f '{{range $name, $_ := .NetworkSettings.Networks}}{{$name}}{{end}}' "$postgres_container")
database_url='postgres://serviceuser:pass@postgres:5432/service_db?sslmode=disable'

docker run -d --name "$localstack_container" --network "$network" --network-alias localstack \
  -v "$(pwd)/infra:/infra:ro" \
  -e SERVICES=sns,sqs,cloudformation,iam \
  -e AWS_DEFAULT_REGION=eu-west-1 \
  -e LOCALSTACK_HOST=localstack \
  -e SQS_ENDPOINT_STRATEGY=path \
  "$localstack_image" >/dev/null
attempt=0
until docker exec "$localstack_container" awslocal sns list-topics >/dev/null 2>&1; do
  attempt=$((attempt + 1))
  if [ "$attempt" -ge 60 ]; then
    echo "LocalStack did not become ready" >&2
    exit 1
  fi
  sleep 1
done

permissions_topic_arn=$(docker exec "$localstack_container" awslocal sns create-topic \
  --name smoke-permissions-upstream --query TopicArn --output text)
docker exec "$localstack_container" awslocal cloudformation deploy \
  --stack-name smoke-go-service-template \
  --template-file /infra/aws-messaging.yaml \
  --parameter-overrides \
    ResourcePrefix=smoke \
    ServiceName=go-service-template \
    PermissionsTopicArn="$permissions_topic_arn" \
  --capabilities CAPABILITY_NAMED_IAM >/dev/null
user_events_topic_arn=$(docker exec "$localstack_container" awslocal cloudformation describe-stacks \
  --stack-name smoke-go-service-template \
  --query "Stacks[0].Outputs[?OutputKey=='UserEventsTopicArn'].OutputValue | [0]" --output text)
permissions_queue_url=$(docker exec "$localstack_container" awslocal cloudformation describe-stacks \
  --stack-name smoke-go-service-template \
  --query "Stacks[0].Outputs[?OutputKey=='PermissionsQueueUrl'].OutputValue | [0]" --output text)

probe_queue_url=$(docker exec "$localstack_container" awslocal sqs create-queue \
  --queue-name smoke-user-events-probe --query QueueUrl --output text)
probe_queue_arn=$(docker exec "$localstack_container" awslocal sqs get-queue-attributes \
  --queue-url "$probe_queue_url" --attribute-names QueueArn --query 'Attributes.QueueArn' --output text)
probe_policy=$(printf '{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":{"Service":"sns.amazonaws.com"},"Action":"sqs:SendMessage","Resource":"%s","Condition":{"ArnEquals":{"aws:SourceArn":"%s"}}}]}' \
  "$probe_queue_arn" "$user_events_topic_arn")
probe_policy_escaped=$(printf '%s' "$probe_policy" | sed 's/"/\\"/g')
docker exec "$localstack_container" awslocal sqs set-queue-attributes \
  --queue-url "$probe_queue_url" --attributes "{\"Policy\":\"$probe_policy_escaped\"}" >/dev/null
docker exec "$localstack_container" awslocal sns subscribe \
  --topic-arn "$user_events_topic_arn" \
  --protocol sqs \
  --notification-endpoint "$probe_queue_arn" \
  --attributes RawMessageDelivery=false >/dev/null

docker build \
  --build-arg VERSION="$version" \
  --build-arg COMMIT="$commit" \
  --build-arg SOURCE_URL="$source_url" \
  -t "$image" .
test "$(image_label org.opencontainers.image.source)" = "$source_url"
test "$(image_label org.opencontainers.image.version)" = "$version"
test "$(image_label org.opencontainers.image.revision)" = "$commit"
test "$(image_label org.opencontainers.image.licenses)" = MIT
docker run --rm --network "$network" -e DATABASE_URL="$database_url" "$image" migrate
docker run -d --name "$container" --network "$network" -p 18080:8080 \
  -e APP_ENV=development \
  -e AUTH_MODE=disabled \
  -e DATABASE_URL="$database_url" \
  "$image" api >/dev/null
docker run -d --name "$worker_container" --network "$network" -p 18081:8080 \
  -e APP_ENV=development \
  -e DATABASE_URL="$database_url" \
  -e AWS_ACCESS_KEY_ID=test \
  -e AWS_SECRET_ACCESS_KEY=test \
  -e AWS_REGION=eu-west-1 \
  -e AWS_ENDPOINT_URL=http://localstack:4566 \
  -e USER_EVENTS_TOPIC_ARN="$user_events_topic_arn" \
  -e PERMISSIONS_QUEUE_URL="$permissions_queue_url" \
  "$image" worker >/dev/null

attempt=0
until [ "$(docker inspect -f '{{.State.Health.Status}}' "$container")" = healthy ]; do
  attempt=$((attempt + 1))
  if [ "$attempt" -ge 30 ]; then
    echo "container did not become healthy" >&2
    exit 1
  fi
  sleep 1
done
attempt=0
until [ "$(docker inspect -f '{{.State.Health.Status}}' "$worker_container")" = healthy ]; do
  attempt=$((attempt + 1))
  if [ "$attempt" -ge 30 ]; then
    echo "worker container did not become healthy" >&2
    exit 1
  fi
  sleep 1
done

test "$(docker inspect -f '{{.Config.User}}' "$container")" = "nonroot:nonroot"
curl --fail --silent http://127.0.0.1:18080/readyz >/dev/null
curl --fail --silent http://127.0.0.1:18080/openapi.yaml | grep -q '^openapi: 3.0.3'
curl --fail --silent http://127.0.0.1:18080/asyncapi.yaml | grep -q '^asyncapi: 3.0.0'
curl --fail --silent http://127.0.0.1:18081/readyz >/dev/null
worker_metrics=$(curl --fail --silent http://127.0.0.1:18081/metrics)
printf '%s' "$worker_metrics" | grep -q 'service_database_available'
created=$(curl --fail --silent -X POST http://127.0.0.1:18080/v1/users \
  -H 'Content-Type: application/json' \
  -d '{"email":"smoke@example.com"}')
user_id=$(printf '%s' "$created" | sed -n 's/.*"id":"\([^"]*\)".*/\1/p')
test -n "$user_id"
case "$user_id" in
  ????????-????-7???-????-????????????) ;;
  *) echo "created user ID is not UUIDv7: $user_id" >&2; exit 1 ;;
esac
curl --fail --silent "http://127.0.0.1:18080/v1/users/$user_id" >/dev/null
user_import=$(curl --fail --silent -X POST http://127.0.0.1:18080/v1/user-imports \
  -H 'Content-Type: application/json' \
  -d '{"emails":["import-one@example.com","import-two@example.com"]}')
import_id=$(printf '%s' "$user_import" | sed -n 's/.*"id":"\([^"]*\)".*/\1/p')
test -n "$import_id"
attempt=0
while :; do
  import_status=$(curl --fail --silent "http://127.0.0.1:18080/v1/user-imports/$import_id")
  printf '%s' "$import_status" | grep -q '"state":"completed"' && break
  attempt=$((attempt + 1))
  if [ "$attempt" -ge 30 ]; then
    echo "user import did not complete: $import_status" >&2
    exit 1
  fi
  sleep 1
done
attempt=0
while :; do
  completed_jobs=$(docker run --rm --network "$network" -e DATABASE_URL="$database_url" "$image" \
    jobs list --queue users --state completed --limit 10)
  printf '%s' "$completed_jobs" | grep -q '"kind":"users.import"' && break
  attempt=$((attempt + 1))
  if [ "$attempt" -ge 10 ]; then
    echo "user import job did not finalize: $completed_jobs" >&2
    exit 1
  fi
  sleep 1
done
attempt=0
while :; do
  completed_events=$(docker run --rm --network "$network" -e DATABASE_URL="$database_url" "$image" \
    jobs list --queue events --state completed --limit 10)
  printf '%s' "$completed_events" | grep -q '"kind":"users.publish-created"' && break
  attempt=$((attempt + 1))
  if [ "$attempt" -ge 30 ]; then
    echo "user.created publication job did not complete: $completed_events" >&2
    exit 1
  fi
  sleep 1
done
attempt=0
while :; do
  published_event=$(docker exec "$localstack_container" awslocal sqs receive-message \
    --queue-url "$probe_queue_url" --wait-time-seconds 1 --query 'Messages[0].Body' --output text)
  if [ "$published_event" != "None" ] && printf '%s' "$published_event" | grep -q 'user.created'; then
    printf '%s' "$published_event" | grep -q "$user_id"
    break
  fi
  attempt=$((attempt + 1))
  if [ "$attempt" -ge 20 ]; then
    echo "user.created was not delivered to the probe queue" >&2
    exit 1
  fi
done

permission_event_id=0198a2bb-1b8d-7a75-b26f-63be9db5df57
permission_event=$(printf '{"id":"%s","timestamp":"2026-07-13T10:00:00Z","type":"permissions.changed","payload":{"userId":"%s","revision":7,"permissions":["read","write"]},"metadata":{"schemaVersion":"1.0.0","producedBy":"permissions","originatedFrom":"permissions","correlationId":"smoke-correlation"}}' \
  "$permission_event_id" "$user_id")
docker exec "$localstack_container" awslocal sns publish \
  --topic-arn "$permissions_topic_arn" --message "$permission_event" >/dev/null
attempt=0
while :; do
  applied=$(docker exec "$postgres_container" psql -U serviceuser -d service_db -tAc \
    "SELECT count(*) FROM user_permissions WHERE user_id = '$user_id' AND revision = 7 AND permissions @> ARRAY['read','write']::text[]")
  processed=$(docker exec "$postgres_container" psql -U serviceuser -d service_db -tAc \
    "SELECT count(*) FROM processed_events WHERE event_id = '$permission_event_id'")
  [ "$applied" = 1 ] && [ "$processed" = 1 ] && break
  attempt=$((attempt + 1))
  if [ "$attempt" -ge 30 ]; then
    echo "permissions.changed was not committed" >&2
    exit 1
  fi
  sleep 1
done
attempt=0
while :; do
  queue_depth=$(docker exec "$localstack_container" awslocal sqs get-queue-attributes \
    --queue-url "$permissions_queue_url" \
    --attribute-names ApproximateNumberOfMessages ApproximateNumberOfMessagesNotVisible \
    --query '[Attributes.ApproximateNumberOfMessages,Attributes.ApproximateNumberOfMessagesNotVisible]' \
    --output text | tr '\t' ' ')
  [ "$queue_depth" = "0 0" ] && break
  attempt=$((attempt + 1))
  if [ "$attempt" -ge 30 ]; then
    echo "permissions.changed was not acknowledged: $queue_depth" >&2
    exit 1
  fi
  sleep 1
done
metrics=$(curl --fail --silent http://127.0.0.1:18080/metrics)
printf '%s' "$metrics" | grep -q 'http_server_request_duration_seconds'
printf '%s' "$metrics" | grep -Eq '^service_database_available(\{[^}]*\})? 1$'
worker_metrics=$(curl --fail --silent http://127.0.0.1:18081/metrics)
printf '%s' "$worker_metrics" | grep -q 'service_aws_available'
printf '%s' "$worker_metrics" | grep -q 'service_messaging_publish_duration_seconds'
printf '%s' "$worker_metrics" | grep -q 'service_messaging_processed_total'
printf '%s' "$worker_metrics" | grep -q 'service_permissions_changes_total'

docker stop --time 5 "$container" >/dev/null
test "$(docker inspect -f '{{.State.ExitCode}}' "$container")" = 0
docker stop --time 5 "$worker_container" >/dev/null
test "$(docker inspect -f '{{.State.ExitCode}}' "$worker_container")" = 0
