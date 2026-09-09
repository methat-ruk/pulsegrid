# FND-001 — Go API Foundation

Status: Planned

Intended PR: One backend-foundation PR

Milestone: F0 — Executable repository foundation

## Goal

Create the first runnable Go/Fiber application with a narrow operational shell
and no product-domain behavior.

## Why

Later GraphQL, persistence, MQTT, and command work need a stable process,
configuration, shutdown, and test boundary.

## Scope

- Establish the initial Go application/module location.
- Add Fiber startup, health/readiness endpoints, graceful shutdown, and
  structured logging.
- Add typed development and test configuration for variables used by this PR.
- Add unit tests for configuration and lifecycle behavior.
- Document backend run and test commands.

## Out of Scope

- GraphQL, databases, MQTT, Kafka, domain modules, authentication, or
  production deployment.

## Dependencies

- DOC-001.

## Architecture / Boundaries

This is one modular-monolith process. Health transport, configuration, and
process lifecycle remain separate from future product modules.

## Implementation Direction

Use Go and Fiber. Prefer standard-library lifecycle and configuration behavior
unless a dependency proves necessary. Load local dotenv files only in explicit
development/test modes; production mode reads process environment only.

## Validation

- Unit tests for valid, missing, and invalid configuration.
- HTTP tests for health/readiness behavior.
- Graceful-shutdown test with bounded completion.
- `gofmt`, `go vet`, and `go test` pass.
- Production mode does not load development or test dotenv files.

## Documentation Updates

- Add backend commands to local-development documentation.
- Add only the environment keys consumed by this application to reviewed
  example files.

## Risks / Open Decisions

- Final Go module/workspace layout.
- Exact environment-selector key and logging library.
- Readiness semantics before external dependencies exist.

## Done Criteria

The Go process starts, reports accurate health, validates its configuration,
shuts down gracefully, and passes its documented local checks without claiming
domain capability.
