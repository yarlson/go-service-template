#!/bin/sh
set -eu

project=go-service-template-smoke
image=go-service-template:smoke
container=go-service-template-smoke-api
export POSTGRES_PORT=0

cleanup() {
  status=$?
  trap - EXIT
  if [ "$status" -ne 0 ]; then
    docker logs "$container" 2>/dev/null || true
  fi
  docker rm -f "$container" >/dev/null 2>&1 || true
  docker compose -p "$project" down -v >/dev/null 2>&1 || true
  exit "$status"
}
trap cleanup EXIT

docker compose -p "$project" up -d --wait postgres
postgres_container=$(docker compose -p "$project" ps -q postgres)
network=$(docker inspect -f '{{range $name, $_ := .NetworkSettings.Networks}}{{$name}}{{end}}' "$postgres_container")
database_url='postgres://serviceuser:pass@postgres:5432/service_db?sslmode=disable'

docker build -t "$image" .
docker run --rm --network "$network" -e DATABASE_URL="$database_url" "$image" migrate
docker run -d --name "$container" --network "$network" -p 18080:8080 \
  -e APP_ENV=development \
  -e AUTH_MODE=disabled \
  -e DATABASE_URL="$database_url" \
  "$image" api >/dev/null

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
created=$(curl --fail --silent -X POST http://127.0.0.1:18080/v1/users \
  -H 'Content-Type: application/json' \
  -d '{"email":"smoke@example.com"}')
user_id=$(printf '%s' "$created" | sed -n 's/.*"id":"\([^"]*\)".*/\1/p')
test -n "$user_id"
curl --fail --silent "http://127.0.0.1:18080/v1/users/$user_id" >/dev/null

docker stop --time 5 "$container" >/dev/null
test "$(docker inspect -f '{{.State.ExitCode}}' "$container")" = 0
