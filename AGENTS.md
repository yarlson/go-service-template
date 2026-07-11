# Project instructions for AI agents

## Purpose

Treat this file as binding repository policy. Read it before changing code.

This repository is a small production baseline for Go HTTP services. Keep it
boring, explicit, and easy to remove or adapt. Complete the requested change
without adding speculative infrastructure, generic frameworks, or template
features that have no current use.

The README explains how to use the template. The Makefile is the authoritative
command interface. Local code, tests, and configuration are the source of truth
when documentation and implementation disagree.

## Architecture

This is one Go module and one deployable service, organized as a feature-first
modular monolith:

```text
cmd/service/             process entrypoint
internal/app/            composition, startup, and shutdown
internal/api/            generated OpenAPI types and server interface
internal/platform/       shared runtime and infrastructure adapters
internal/<feature>/      feature domain and use cases
internal/<feature>/http/ feature HTTP adapter
internal/<feature>/postgres/ feature PostgreSQL adapter and queries
api/                     canonical OpenAPI document
db/migrations/           application-wide ordered schema history
```

Preserve these dependency rules:

- Keep `cmd/service` thin. It selects a command, loads configuration, handles
  process signals, and delegates to `internal/app`.
- Keep `internal/app` as the only composition root. Construct concrete
  dependencies there; do not add a dependency-injection framework.
- A feature's root package owns its domain types, business rules, use cases,
  and consumer-owned ports. It must not import its HTTP or PostgreSQL adapter.
- Feature HTTP adapters may depend on their feature domain, generated API
  types, and narrowly required platform packages.
- Feature PostgreSQL adapters depend on their feature domain and local
  sqlc-generated code. They must not import HTTP or OpenAPI transport types.
- Platform packages must never import feature packages or construct feature
  handlers.
- If the generated API interface spans multiple features, compose feature
  handlers at the application boundary. Do not move feature logic into the
  shared HTTP server.
- Keep cross-feature calls explicit and wire them in `internal/app`. Do not
  create global service locators or hidden registries.
- Do not create generic `controllers`, `services`, `repositories`, `models`,
  `common`, `utils`, `helpers`, `lib`, or `pkg` trees. Add a package only when
  it represents a cohesive responsibility with a clear dependency direction.

Business logic belongs in feature packages, not in transport middleware,
generated code, SQL adapters, or `cmd/service`.

## Canonical and generated files

The canonical inputs are:

- `api/openapi.yaml` for the HTTP contract.
- `db/migrations/*.sql` for schema history.
- `internal/<feature>/postgres/queries.sql` for feature queries.
- `.env.example` for documented runtime configuration.

Generated Go files are committed but must not be edited manually:

- `internal/api/api.gen.go`
- `internal/<feature>/postgres/db.go`
- `internal/<feature>/postgres/models.go`
- `internal/<feature>/postgres/queries.sql.go`

After changing OpenAPI, migrations, SQL queries, or generation configuration,
run `make generate`. Generated output must be reproducible;
`make generate-check` must not report stale output.

Make API changes contract-first: update OpenAPI, regenerate, then implement the
generated interface. Keep `/openapi.yaml` serving the exact canonical document.

## Go practices

- Use the Go version declared in `go.mod`.
- Prefer the standard library and existing dependencies. Add a dependency only
  when it clearly improves correctness or materially reduces maintained code.
- Keep interfaces small and owned by the package that consumes them. Do not
  introduce an interface solely to wrap one concrete type without a real seam.
- Pass `context.Context` through request, database, and outbound I/O paths. Do
  not replace a live caller's context with `context.Background()`. After the
  process context is canceled, graceful shutdown may create a fresh, explicitly
  bounded background context so cleanup can complete.
- Wrap errors with operation context using `%w`; preserve `errors.Is` and
  `errors.As`. Translate implementation errors at adapter boundaries.
- Do not expose database errors or internal failure details in HTTP responses.
  Never write tokens, credentials, or other secrets to logs.
- Make goroutine ownership, cancellation, shutdown, and channel closure
  explicit. Do not start unbounded background work from request handlers.
- Use `log/slog` structured fields. Preserve request IDs across request logs and
  problem responses.
- Avoid mutable package globals and implicit initialization side effects.
- Comments describe current behavior, constraints, or non-obvious invariants.
  Do not narrate edit history or restate straightforward code.
- Run formatting through `make fmt`; repository formatting includes `gofumpt`
  and `goimports` via the versioned Go toolchain.

## HTTP and runtime behavior

- Continue using standard-library `net/http` with the generated strict OpenAPI
  server interface. Do not add a web framework without an explicit requirement.
- `internal/platform/httpserver` owns shared concerns: routing, validation,
  authentication context, request IDs, body limits, recovery, logging, probes,
  and serving the canonical contract.
- Feature HTTP handlers only translate transport data, call a use case, and map
  domain results or errors to generated responses.
- Return documented errors as `application/problem+json`. Do not invent
  undocumented response shapes.
- Preserve the distinction between `/livez` and `/readyz`: liveness checks the
  process; readiness checks whether it can serve traffic, including PostgreSQL.
- The API process must not apply schema migrations during startup. Migrations
  run through the explicit `service migrate` command before rollout.
- Authentication may be disabled only in development and tests. Production
  must use configured OIDC verification.
- Keep graceful shutdown bounded and stop reporting readiness before draining.

## PostgreSQL and migrations

- Use `pgx/v5` and `sqlc`; do not add an ORM alongside them.
- Keep feature queries and generated access code with the owning feature.
- Keep migrations application-wide in `db/migrations` so ordering is explicit.
- Add fixed-width matching `*.up.sql` and `*.down.sql` files. Never rewrite a
  migration that may have been applied; add a new migration instead.
- Keep migration execution explicit and safe to repeat. Treat
  `migrate.ErrNoChange` as success.
- Translate PostgreSQL-specific errors in the PostgreSQL adapter before they
  reach domain or HTTP layers.

## Testing

- Test behavior and boundaries, not private implementation details.
- Use Testify consistently: `require` for setup or preconditions that make the
  rest of a test invalid, and `assert` for independent result checks.
- Keep unit tests deterministic, isolated, and fast. Use `t.Parallel()` only
  when the test and its dependencies are safe to run concurrently.
- Keep HTTP infrastructure tests independent of feature implementations. Keep
  request/response mapping tests beside the owning feature HTTP adapter.
- Use Testcontainers only for integration tests where real PostgreSQL semantics
  matter. Do not require a developer-managed database, fixed port, or shared
  schema.
- Integration tests use the `integration` build tag and must register container
  cleanup immediately after successful startup.
- Do not replace meaningful PostgreSQL integration coverage with SQL mocks.
- For a bug fix, add or update a test that fails for the reported behavior when
  practical.
- Avoid sleeps, timing-sensitive assertions, network calls outside managed test
  containers, and tests that only assert a mock was called.

## Development workflow

Use repository commands rather than globally installed tools:

```sh
make generate          # regenerate OpenAPI and sqlc outputs
make fmt               # format Go files
make test              # unit tests
make test-integration  # isolated PostgreSQL integration tests; requires Docker
make check             # generation, format, lint, unit, race, integration,
                       # module, vulnerability, and build checks
make docker-test       # production-image migration and API smoke test
```

All Go developer tools are pinned in `go.mod` and invoked through `go tool`.
Do not add instructions requiring global tool installation.

When adding another feature with generated SQL or integration tests, update all
explicit coverage points: add its sqlc entry to `sqlc.yaml`, add its generated
files to `generate-check`, and add its integration package to
`test-integration`. `make check` must cover every committed generated file and
every integration-test package.

Before finishing:

- Run `make check` for Go, configuration, generation, or database changes.
- Run `make docker-test` when startup, migrations, container packaging,
  networking, health checks, or end-to-end HTTP behavior could be affected.
- For documentation-only changes, verify links and commands against the current
  repository and run `git diff --check` at minimum.
- If Docker or another required dependency is unavailable, run every unaffected
  check and report exactly what was not verified.
- Update README and `.env.example` when user-visible commands, configuration,
  contracts, or operational behavior change.

## Scope and template integrity

- Preserve existing APIs, environment variables, migration behavior, and
  operational semantics unless the task explicitly changes them.
- Do not add queues, caches, schedulers, background workers, service boundaries,
  or configurability for hypothetical future requirements.
- Keep secrets and local `.env` files out of the repository.
- Treat `.dockerignore` as the allowlist for production compilation inputs.
  Documentation, CI configuration, agent instructions, scripts, and local
  tooling must not invalidate the Docker build cache. When compilation starts
  depending on a new top-level path, add that path deliberately and verify it
  with `make docker-test`.
- Keep the placeholder module path until a template consumer intentionally runs
  `make rename MODULE=github.com/acme/service`.
- Make surgical changes and preserve unrelated user work in the worktree.
- Every changed line should map to the requested outcome or its concrete tests,
  generated artifacts, or documentation.
