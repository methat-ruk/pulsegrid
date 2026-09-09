# FND-001 — Go API Foundation

Status: Planned

Branch: `feat/fnd-001-go-api-foundation`

Intended PR: One backend-foundation PR

Milestone: F0 — Executable repository foundation

## Goal

Create the first runnable Go/Fiber application with a narrow operational shell
and no product-domain behavior.

## Why

Later GraphQL, persistence, MQTT, and command work need a stable process,
configuration, shutdown, and test boundary.

## Scope

- Create `apps/api` as module
  `github.com/methat-ruk/pulsegrid/apps/api`, with `cmd/api` as its executable
  and `internal/platform/{config,httpserver,logging}` as its initial internal
  boundaries.
- Pin Go 1.27 and Fiber v3 through `go.mod`; do not add `go.work` while only one
  Go module exists.
- Add Fiber startup, `GET /health/live`, `GET /health/ready`, graceful
  shutdown, and structured logging with the standard-library `log/slog`.
- Add typed configuration for `PULSEGRID_ENV`, `PULSEGRID_HTTP_HOST`,
  `PULSEGRID_HTTP_PORT`, `PULSEGRID_LOG_LEVEL`, and
  `PULSEGRID_SHUTDOWN_TIMEOUT`.
- Add application-owned reviewed examples for development, test, and
  production-mode configuration; add no credential placeholders not consumed
  by this PR.
- Add unit tests for configuration and lifecycle behavior.
- Document backend run and test commands.

## Out of Scope

- GraphQL, databases, MQTT, Kafka, domain modules, authentication, or
  production deployment.

## Dependencies

- DOC-001.

## Architecture / Boundaries

This is one modular-monolith process. Health transport, configuration, and
process lifecycle remain separate from future product modules. The operational
endpoints return only status and stable reason codes; they do not expose
configuration, dependency versions, stack traces, or internal errors.

Liveness means that the process is serving HTTP. Readiness becomes `ready` only
after application initialization completes; with no external dependency in
this PR, it does not simulate database or broker checks. CORS remains disabled
until FND-004 selects and tests the browser-to-backend development boundary.

## Implementation Direction

Use Go 1.27, Fiber v3, and `log/slog`. Keep `main` as composition only; config,
HTTP construction, and lifecycle code must be testable without starting a
production listener. Bind development to `127.0.0.1` by default. Configure
bounded request/server timeouts and a small body limit appropriate for
bodyless operational endpoints.

Process environment has highest precedence. Load `.env.development` or
`.env.test` only when `PULSEGRID_ENV` explicitly selects that mode. Production
reads process environment only and fails closed on missing or invalid required
values. Public error responses use stable codes; logs include correlation data
where available and never record request bodies, credentials, cookies, or
authorization headers.

## Validation

- Unit tests for valid, missing, and invalid configuration.
- Tests proving environment precedence, test isolation, production fail-closed
  behavior, and that production never loads development/test dotenv files.
- HTTP contract tests for method, status code, content type, minimal response
  body, readiness transition, and safe 404/error behavior.
- Tests proving CORS is not enabled by this PR and public responses do not
  expose framework versions or configuration.
- Graceful-shutdown test with bounded completion.
- `gofmt`, `go vet`, and `go test` pass.

## Documentation Updates

- Add backend commands to local-development documentation.
- Add only the environment keys consumed by this application to reviewed
  example files.

## Risks / Open Decisions

- Exact patch releases are resolved and pinned during implementation; changing
  a major version requires plan review.
- Fiber uses pooled request context data. Any value retained beyond a handler
  must be copied; FND-001 should avoid retaining request-derived values.
- Production deployment probes and exposure policy remain out of scope; later
  packaging/deployment work must decide whether these endpoints are internal.

## Done Criteria

The Go process starts, reports accurate health, validates its configuration,
shuts down gracefully, and passes its documented local checks without claiming
domain capability.
