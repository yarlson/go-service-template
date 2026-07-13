#!/bin/sh
set -eu

project=go-service-template-smoke
image=go-service-template:smoke
container=go-service-template-smoke-api
worker_container=go-service-template-smoke-worker
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
  fi
  docker rm -f "$container" >/dev/null 2>&1 || true
  docker rm -f "$worker_container" >/dev/null 2>&1 || true
  docker compose -p "$project" down -v >/dev/null 2>&1 || true
  exit "$status"
}
trap cleanup EXIT

docker compose -p "$project" up -d --wait postgres
postgres_container=$(docker compose -p "$project" ps -q postgres)
network=$(docker inspect -f '{{range $name, $_ := .NetworkSettings.Networks}}{{$name}}{{end}}' "$postgres_container")
database_url='postgres://serviceuser:pass@postgres:5432/service_db?sslmode=disable'

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
docker run -d --name "$worker_container" --network "$network" --no-healthcheck \
  -e APP_ENV=development \
  -e DATABASE_URL="$database_url" \
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

test "$(docker inspect -f '{{.Config.User}}' "$container")" = "nonroot:nonroot"
curl --fail --silent http://127.0.0.1:18080/readyz >/dev/null
curl --fail --silent http://127.0.0.1:18080/openapi.yaml | grep -q '^openapi: 3.0.3'
curl --fail --silent http://127.0.0.1:18080/asyncapi.yaml | grep -q '^asyncapi: 3.0.0'
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
docker run --rm --network "$network" -e DATABASE_URL="$database_url" "$image" \
  jobs list --queue users --state completed --limit 10 | grep -q '"kind":"users.import"'
metrics=$(curl --fail --silent http://127.0.0.1:18080/metrics)
printf '%s' "$metrics" | grep -q 'http_server_request_duration_seconds'
printf '%s' "$metrics" | grep -Eq '^service_database_available(\{[^}]*\})? 1$'

docker stop --time 5 "$container" >/dev/null
test "$(docker inspect -f '{{.State.ExitCode}}' "$container")" = 0
docker stop --time 5 "$worker_container" >/dev/null
test "$(docker inspect -f '{{.State.ExitCode}}' "$worker_container")" = 0
