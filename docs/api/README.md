# PulseGrid API documentation

Status: Foundation operational contract documented; product APIs are not
implemented.

## Purpose and ownership

This page is the human entry point for PulseGrid API contracts. It explains
which protocol owns each boundary and how to test the contracts locally. It
does not duplicate the full request and response schema.

The machine-readable operational HTTP contract is
[apps/api/api/openapi/operational.yaml](../../apps/api/api/openapi/operational.yaml).
The OpenAPI document is the source of truth for the wire shape of the health
endpoints. Go handler tests remain the runtime evidence that the implementation
matches that contract.

Product semantics remain owned by the [product scope](../product/product-scope.md)
and the relevant feature plan. Logical API boundaries remain owned by the
[system architecture](../architecture/system-architecture.md).

## API surfaces

| Surface | Protocol | Status | Source of truth |
| --- | --- | --- | --- |
| Process health | REST/HTTP | Implemented in FND-001 | [`operational.yaml`](../../apps/api/api/openapi/operational.yaml) |
| Operator product API | GraphQL/gqlgen | Planned for MVP-002 | `apps/api/graph/schema/*.graphqls` when introduced |
| Device telemetry | MQTT | Planned for MVP-004 onward | AsyncAPI/message schema when a concrete flow exists |
| Device commands | MQTT | Planned for MVP-011 onward | AsyncAPI/message schema when a concrete flow exists |
| Durable event distribution | Kafka | Post-MVP conditional | A flow-specific AsyncAPI/message contract |
| Internal synchronous service calls | gRPC/Protobuf | Post-MVP conditional | A flow-specific protobuf contract |

REST is intentionally limited to operational HTTP in the current foundation.
The console product API will use GraphQL; no parallel REST CRUD API is created
without a separate consumer and an approved boundary.

## Current operational contract

### `GET /health/live`

Returns `200` with `{"status":"ok"}` when the process is serving HTTP. It does
not check PostgreSQL, MQTT, Kafka, or any other external dependency.

### `GET /health/ready`

Returns `200` with `{"status":"ready"}` while the process accepts normal
traffic. It returns `503` with one of these stable states while it cannot accept
normal traffic:

- `{"status":"not_ready","reason":"starting"}` during startup;
- `{"status":"not_ready","reason":"draining"}` during graceful shutdown.

The `503` response is an expected readiness state. A probe or local test must
not treat it as proof that the process has crashed.

Both endpoints accept an optional bounded `X-Request-ID` and return a safe
correlation value when available. Public errors use the FND-001 envelope:

```json
{
  "error": {
    "code": "not_found",
    "message": "route not found"
  }
}
```

The body never contains stack traces, configuration, credentials, or framework
details. Authentication and deployment exposure are intentionally not claimed
by FND-001; deployment work must keep probe access internal and define any
proxy/cache policy.

## Local testing

From `apps/api`:

```sh
goenv exec go test ./...
goenv exec go test -race ./...
goenv exec go vet ./...
PULSEGRID_ENV=development go run ./cmd/api
```

From another terminal:

```sh
curl -i http://127.0.0.1:8080/health/live
curl -i http://127.0.0.1:8080/health/ready
curl -i http://127.0.0.1:8080/not-found
```

Press `Ctrl-C` while the process is running to exercise the draining and
stopped lifecycle. The repository-wide Redocly lint, bundle, and static-docs
commands will be added with the pnpm/CI workflow in FND-003; they are not a
runtime dependency of the API process.

## Contract rules

- Pin the OpenAPI specification version in the contract; do not float to a
  moving `latest` version.
- Treat status codes, response fields, error codes, and field meaning as
  compatibility-sensitive.
- Prefer additive changes and deprecation before removal.
- Keep REST JSON naming consistent with the existing operational contract.
- Keep GraphQL SDL as the source of truth when MVP-002 introduces the product
  API; generated gqlgen output is not hand-edited.
- Introduce AsyncAPI only with a concrete MQTT or Kafka producer and consumer.
- Treat local `curl` commands as onboarding and smoke checks; automated Go and
  CI contract tests remain the authority.

## Related sources

- [System architecture](../architecture/system-architecture.md)
- [Technology decisions](../architecture/technology-decisions.md)
- [FND-001 Go API Foundation](../roadmap/feature-plans/completed/FND-001-go-api-foundation.md)
- [Roadmap](../roadmap/roadmap.md)
