SHELL := /bin/sh

BINARY := bin/service
MODULE :=
VERSION ?= dev
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
SOURCE_URL ?= https://github.com/yarlson/go-service-template
LDFLAGS := -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT)
ASYNCAPI_CLI_IMAGE := asyncapi/cli:5.0.7@sha256:b861d57b05dc1afeb8ddd52efe0acd8313367938c2ca86f55a9a26428af4f1d2

.DEFAULT_GOAL := help

.PHONY: help bootstrap dev migrate generate generate-check asyncapi-check fmt fmt-check lint test test-race test-integration check build docker-build docker-test compose-up compose-down clean rename

help:
	@awk 'BEGIN {FS = ":.*## "} /^[a-zA-Z_-]+:.*## / {printf "%-20s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

bootstrap: .env compose-up migrate ## Create local config, start PostgreSQL, and apply migrations

.env:
	cp .env.example .env

dev: .env ## Run the API with local configuration
	@set -a; . ./.env; set +a; go run ./cmd/service api

migrate: .env ## Apply all pending database migrations
	@set -a; . ./.env; set +a; go run ./cmd/service migrate

generate: ## Regenerate OpenAPI and database code
	go generate ./internal/api
	go tool sqlc generate

generate-check: ## Fail when generated code is stale
	@set -e; \
	before=$$(cksum internal/api/api.gen.go internal/users/postgres/db.go internal/users/postgres/models.go internal/users/postgres/queries.sql.go); \
	$(MAKE) --no-print-directory generate; \
	after=$$(cksum internal/api/api.gen.go internal/users/postgres/db.go internal/users/postgres/models.go internal/users/postgres/queries.sql.go); \
	test "$$before" = "$$after" || (echo 'generated code is stale; run make generate' >&2; exit 1)

asyncapi-check: ## Validate the asynchronous message contract
	docker run --rm --network none --entrypoint /bin/sh \
		-v '$(CURDIR):/app:ro' -w /app '$(ASYNCAPI_CLI_IMAGE)' -c \
		'printf '\''%s\n'\'' '\''{"analyticsEnabled":"false","infoMessageShown":"true","userID":"local"}'\'' > /tmp/asyncapi-analytics && ASYNCAPI_METRICS_CONFIG_PATH=/tmp/asyncapi-analytics SUPPRESS_NO_CONFIG_WARNING=true /usr/local/bin/asyncapi validate api/asyncapi.yaml'

fmt: ## Format Go source
	go tool golangci-lint fmt

fmt-check: ## Check Go formatting
	go tool golangci-lint fmt --diff

lint: ## Run static analysis
	go tool golangci-lint run ./...

test: ## Run unit tests
	go test ./...

test-race: ## Run unit tests with the race detector
	go test -race ./...

test-integration: ## Run tests against isolated PostgreSQL containers
	go test -count=1 -tags=integration ./internal/users/postgres

check: asyncapi-check generate-check fmt-check lint test test-race test-integration ## Run the complete local verification suite
	go mod tidy -diff
	go mod verify
	go tool govulncheck ./...
	$(MAKE) build

build: ## Build the production binary
	mkdir -p bin
	CGO_ENABLED=0 go build -trimpath -ldflags '$(LDFLAGS)' -o $(BINARY) ./cmd/service

docker-build: ## Build the production container image
	docker build --build-arg VERSION='$(VERSION)' --build-arg COMMIT='$(COMMIT)' --build-arg SOURCE_URL='$(SOURCE_URL)' -t go-service-template:local .

docker-test: ## Smoke-test the production container
	./scripts/docker-smoke-test.sh

compose-up: ## Start local PostgreSQL
	docker compose up -d --wait

compose-down: ## Stop local PostgreSQL
	docker compose down

clean: ## Remove build artifacts
	rm -rf bin coverage.out

rename: ## Replace the placeholder module path: make rename MODULE=github.com/acme/service
	@test -n '$(MODULE)' || (echo 'MODULE is required' >&2; exit 1)
	./scripts/rename-module.sh '$(MODULE)'
