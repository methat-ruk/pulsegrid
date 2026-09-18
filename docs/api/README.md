# PulseGrid API documentation

Status: MVP-002 development GraphQL device contract, MVP-003 console
integration, and the MVP-004 local MQTT producer fixture are implemented and
validated. Platform ingestion, production identity, and product expansion
remain deferred.

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

Repository-level contract validation is provided by FND-003:

```sh
corepack pnpm run api:generate:check
corepack pnpm run openapi:lint
corepack pnpm run openapi:bundle
corepack pnpm run openapi:html
```

The generated bundle and static HTML live in the ignored `.openapi/` directory
and are review artifacts only; they are not published or served at runtime.

## API surfaces

| Surface | Protocol | Status | Source of truth |
| --- | --- | --- | --- |
| Process health | REST/HTTP | Implemented in FND-001 | [`operational.yaml`](../../apps/api/api/openapi/operational.yaml) |
| Console readiness adapter | Same-origin Nuxt server route | Implemented in FND-004; local process-readiness adapter only | [`ready.get.ts`](../../apps/web-console/server/api/operational/ready.get.ts) and the FND-001 operational contract |
| Console GraphQL adapter | Same-origin Nuxt server route | Implemented in MVP-003; development/test transport adapter only | [`graphql.post.ts`](../../apps/web-console/server/api/graphql.post.ts) and [`graphql-proxy.ts`](../../apps/web-console/server/utils/graphql-proxy.ts) |
| Operator product API | GraphQL/gqlgen | Implemented for development-only MVP-002 scope | [`device.graphqls`](../../apps/api/graph/schema/device.graphqls) and committed generated artifacts |
| Device telemetry | MQTT | MVP-004 producer fixture implemented; platform consumer planned for MVP-005 onward | [MVP-004 telemetry v1 contract](../roadmap/feature-plans/completed/MVP-004-mqtt-local-runtime-and-simulator.md) |
| Device commands | MQTT | Planned for MVP-011 onward | AsyncAPI/message schema when a concrete flow exists |
| Durable event distribution | Kafka | Post-MVP conditional | A flow-specific AsyncAPI/message contract |
| Internal synchronous service calls | gRPC/Protobuf | Post-MVP conditional | A flow-specific protobuf contract |

REST is intentionally limited to operational HTTP in the current foundation.
The Nuxt routes are fixed same-origin adapters for readiness and the console's
development GraphQL journey; they are not a second product REST API or a
generic proxy. The console product API uses the existing GraphQL contract; no
parallel REST CRUD API is created without a separate consumer and an approved
boundary.

## MVP-004 telemetry producer fixture

The local/test `device-simulator` publishes one UTF-8 JSON telemetry observation
to a loopback Mosquitto broker. The producer contract is:

- topic: `pulsegrid/v1/tenants/{tenantSlug}/devices/{deviceId}/telemetry`;
- MVP-004 tenant slug: `pulsegrid-dev`;
- device ID: canonical lowercase UUID copied from the registry journey;
- MQTT 3.1.1 over TCP, QoS 1, `retain=false`, clean session;
- payload fields: `schemaVersion` (integer `1`), `messageId` (new UUID),
  `observedAt` (UTC RFC 3339), and `temperatureCelsius` (finite JSON number).

The broker is local-only and ephemeral. The simulator's PUBACK proves only that
the broker acknowledged the publish; MQTT QoS 1 can duplicate delivery, and
the payload is untrusted for the future MVP-005 ingestion boundary. No API
subscriber, registry lookup, persistence, current-state projection, or console
telemetry behavior is implemented by this producer fixture.

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

## MVP-002 development GraphQL contract

`POST /graphql` is mounted only when `PULSEGRID_IDENTITY_MODE=development` and
`PULSEGRID_HTTP_HOST` is a literal IPv4 loopback address (`127.0.0.0/8`).
Wildcard, hostname, non-loopback, and IPv6 binds are rejected in this mode.
The process resolves the seeded `pulsegrid-dev` organization before listening;
missing migrations, seed data, or PostgreSQL fail startup. The request cannot
select a tenant through an argument, header, cookie, or client state.

The endpoint accepts `Content-Type: application/json` and returns
`application/graphql-response+json`. GET, batching, subscriptions, multipart
uploads, persisted queries, and playground routes are not enabled. Introspection
is available only in this explicit development mode. The SDL is the source of
truth; generated gqlgen files are committed and checked for drift.

The first contract exposes `device`, bounded forward `devices` pagination
(`first` 1–100 with opaque versioned cursors), and `createDevice`. Device IDs
are canonical UUID strings and timestamps are UTC RFC3339Nano. Expected resolver
codes are `BAD_USER_INPUT`, `CONFLICT`, and `INTERNAL_SERVER_ERROR`; parse and
validation failures use gqlgen's `GRAPHQL_PARSE_FAILED` and
`GRAPHQL_VALIDATION_FAILED` codes. Errors include a safe request correlation ID
when one is available and never reveal SQL, credentials, tenant existence, or
request bodies.

### Console GraphQL adapter

The browser calls only same-origin `POST /api/graphql`. The Nuxt server adapter
appends the fixed upstream `/graphql` path to the private
`NUXT_BACKEND_ORIGIN`; it does not forward browser cookies, authorization,
origin, arbitrary headers, or a client-selected path. Browser requests and
valid upstream responses use `application/graphql-response+json` and
`cache-control: no-store`. The adapter bounds request bodies at 64 KiB,
responses at 256 KiB, and the upstream call at five seconds. It maps missing,
unreachable, timed-out, oversized, or invalid-media-type upstream responses to
a safe HTTP 503 GraphQL envelope with code `SERVICE_UNAVAILABLE`; unsupported
browser media types use 415 and oversized requests use 413. Malformed JSON is
forwarded so the Go GraphQL parser remains the parsing authority. This route
does not add authentication, tenant selection, caching, retries, or production
exposure.

## Local testing

From `apps/api`:

```sh
goenv exec go test ./...
goenv exec go test -race ./...
goenv exec go vet ./...
PULSEGRID_ENV=development corepack pnpm run dev:api
```

From another terminal:

```sh
curl -i http://127.0.0.1:8080/health/live
curl -i http://127.0.0.1:8080/health/ready
curl -i http://127.0.0.1:8080/not-found
```

Press `Ctrl-C` while the process is running to exercise the draining and
stopped lifecycle. The repository-wide Redocly lint, bundle, and static-docs
commands run from the repository root and are not a runtime dependency of the
API process.

## Contract rules

- Pin the OpenAPI specification version in the contract; do not float to a
  moving `latest` version.
- Treat status codes, response fields, error codes, and field meaning as
  compatibility-sensitive.
- Prefer additive changes and deprecation before removal.
- Keep REST JSON naming consistent with the existing operational contract.
- Keep GraphQL SDL as the source of truth for the MVP-002 product API; generated
  gqlgen output is not hand-edited.
- Introduce AsyncAPI only with a concrete MQTT or Kafka producer and consumer.
- Treat local `curl` commands as onboarding and smoke checks; automated Go and
  CI contract tests remain the authority.

## Related sources

- [System architecture](../architecture/system-architecture.md)
- [Technology decisions](../architecture/technology-decisions.md)
- [FND-001 Go API Foundation](../roadmap/feature-plans/completed/FND-001-go-api-foundation.md)
- [Roadmap](../roadmap/roadmap.md)
