# PulseGrid API documentation

Status: MVP-002 development GraphQL device contract, MVP-003 console
integration, MVP-004 local MQTT producer fixture, MVP-005 local/test telemetry
ingestion, and MVP-006 local/test telemetry persistence/current-state projection
are implemented on the feature branch and locally validated. The MVP-008
rules/alerts backend and review fixes are validated; PR #18 is ready to merge
but remains unmerged. Production identity, production MQTT, and permanent
high-volume storage remain deferred.

## Purpose and ownership

This page is the human entry point for PulseGrid API contracts. It explains
which protocol owns each boundary and how to test the contracts locally. It
does not duplicate the full request and response schema.

The machine-readable operational HTTP contract is
[apps/api/api/openapi/operational.yaml](../../apps/api/api/openapi/operational.yaml).
The OpenAPI document is the source of truth for the wire shape of the health
endpoints. Go handler tests remain the runtime evidence that the implementation
matches that contract.

The machine-readable telemetry contract is
[apps/api/api/asyncapi/telemetry.yaml](../../apps/api/api/asyncapi/telemetry.yaml).
It is the source of truth for the local/test MQTT topic, payload, and QoS shape;
the ingestion package and integration harness own executable validation for
strict fields, tenant/device resolution, clock skew, and failure behavior.

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
| Device telemetry | MQTT + PostgreSQL | MVP-004 producer fixture, MVP-005 local/test consumer, and MVP-006 bounded persistence/current state implemented; production delivery deferred | [AsyncAPI telemetry contract](../../apps/api/api/asyncapi/telemetry.yaml), [MVP-005 plan](../roadmap/feature-plans/completed/MVP-005-mqtt-telemetry-ingestion.md), and [MVP-006 plan](../roadmap/feature-plans/completed/MVP-006-telemetry-current-state-projection.md) |
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
the payload is untrusted at the API ingestion boundary. The simulator does not
verify registry membership or call the API; the MVP-005 consumer performs strict
validation and registry resolution before emitting diagnostic acceptance.

## MVP-005 telemetry ingestion

When `PULSEGRID_MQTT_INGESTION_MODE=development` is explicitly enabled, the
API subscribes to the loopback broker at QoS 1 with a bounded 64-item in-memory
queue. It accepts only the exact telemetry-v1 topic and four-field payload,
resolves the tenant/device pair through PostgreSQL, and passes the accepted DTO
to the synchronous consumer. MVP-006 replaced the original diagnostic sink
with a PostgreSQL transaction before writing `telemetry_accepted`; the MVP-008
candidate additionally evaluates rules and writes alerts in that transaction.
The accepted log contains safe IDs and timestamps, never the raw payload or
temperature. Unknown devices, wrong-tenant topics,
malformed/oversized/future/retained messages, queue drops, and dependency
failures use stable `reason_code` values.

MQTT PUBACK and queue admission are transport evidence only. The original
MVP-005 handoff was non-durable; MVP-006 now provides durable logical
idempotency, and the MVP-008 candidate makes telemetry and alert writes atomic.
The in-memory queue still does not promise automatic replay after a failure or
crash. Enabled ingestion participates in readiness, so a broker disconnect
returns `503 dependency_unavailable` while bounded reconnect and resubscription
proceed. Liveness remains process-only.

Validate the real API, PostgreSQL, Mosquitto, simulator, rejection, duplicate,
readiness-recovery, and shutdown paths with:

```sh
corepack pnpm run mqtt:test:integration
```

## MVP-006 telemetry persistence and current state

The configured `AcceptedTelemetryConsumer` now commits accepted logical
observations to PostgreSQL before the API emits `telemetry_accepted`. The
durable idempotency authority is an append-only canonical identity table keyed
by `(device_id, message_id)`; it is deliberately separate from bounded history
so pruning cannot make an old replay look new. An exact replay is a successful
no-op: it does not create another history row, advance current state, or advance
`lastSeenAt`. Reusing the same device/message ID with a different observed time
or temperature is a safe processing failure with no partial mutation.

Current measurement uses the greatest `(observedAt, messageId)` tuple. A late
observation remains in bounded history and may advance `lastSeenAt`, but cannot
replace a newer current measurement. History retains at most 1,000 logical
observations per device; the identity authority retains one compact canonical
row per accepted logical message and has no MVP retention deletion policy. The
history bound is an MVP count bound, not a time-retention or permanent
telemetry-store decision.

The additive development GraphQL reads are:

- `deviceCurrentState(deviceId: ID!): DeviceCurrentState` — nullable when the
  fixed tenant cannot see a state;
- `deviceTelemetry(deviceId: ID!, first: Int! = 50, after: String):
  TelemetryConnection!` — forward keyset pagination, `first` 1–100, ordered by
  `observedAt DESC, messageId DESC`.

Only message ID, observed/received times, temperature, and `lastSeenAt` on
current state are exposed. Ingestion IDs, MQTT duplicate flags, organization
IDs, storage sequence values, and database errors remain internal. Cross-tenant
and unknown devices return no telemetry rows. The GraphQL contract remains
development/test-only and uses the existing server-selected principal.

The local API-to-database-to-GraphQL evidence is included in:

```sh
corepack pnpm run api:test:integration
corepack pnpm run mqtt:test:integration
```

## MVP-008 threshold rules and alerts

The development GraphQL API configures up to 20 temperature rules per device
and exposes immutable matching alert occurrences. The only metric is
`TEMPERATURE_CELSIUS`; comparators are `GT`, `GTE`, `LT`, and `LTE`, applied
directly to finite Celsius values. Rule updates require the expected revision.
Rule creation/update does not backfill prior telemetry; every newly stored
observation, including late observations, uses the enabled rule configuration
visible to its evaluation query. Exact replay skips evaluation.

Rule evaluation and alert insertion share the MVP-006 PostgreSQL transaction.
A rule/alert persistence failure rolls back the new observation and prevents
`telemetry_accepted`; earlier telemetry remains readable. The current MQTT
queue does not replay failed work, so recovery may require explicit republish
with the same `messageId`. Identity and alert uniqueness make that retry
idempotent. No notification, suppression, severity, acknowledgement, or
active/resolved lifecycle is exposed.

The additive operations are `createThresholdRule`, `updateThresholdRule`,
`thresholdRules`, `alert`, and paginated `alerts`. Rule and alert reads/writes
use the server-selected organization and tenant-scoped SQL; alert detail
includes its measurement/rule snapshot and remains readable after source
history pruning. The API contract and local PostgreSQL/MQTT integration
evidence are verified by the MVP-008 plan and its implementation tests.

## Current operational contract

### `GET /health/live`

Returns `200` with `{"status":"ok"}` when the process is serving HTTP. It does
not check PostgreSQL, MQTT, Kafka, or any other external dependency.

### `GET /health/ready`

Returns `200` with `{"status":"ready"}` while the process accepts normal
traffic. It returns `503` with one of these stable states while it cannot accept
normal traffic:

- `{"status":"not_ready","reason":"starting"}` during startup;
- `{"status":"not_ready","reason":"draining"}` during graceful shutdown;
- `{"status":"not_ready","reason":"dependency_unavailable"}` when an
  enabled PostgreSQL or MQTT dependency is unavailable.

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

The MVP-008 candidate validates both the MVP-006 telemetry schema from
migration `005` and the rules/alerts schema from migration `006` before the
development API starts listening. A missing required schema fails startup with
the safe `database_schema_unavailable` action rather than reporting ready and
exposing a partially usable API. The documented migration command applies the
latest migration before startup.

The contract exposes `device`, bounded forward `devices` pagination
(`first` 1–100 with opaque versioned cursors), `createDevice`, and the additive
MVP-006 `deviceCurrentState`/`deviceTelemetry` reads described above. Device
IDs are canonical UUID strings and timestamps are UTC RFC3339Nano. Expected
resolver codes are `BAD_USER_INPUT`, `CONFLICT`, and `INTERNAL_SERVER_ERROR`;
parse and validation failures use gqlgen's `GRAPHQL_PARSE_FAILED` and
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
corepack pnpm run asyncapi:lint
corepack pnpm run mqtt:test:integration
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
- Keep the concrete local/test MQTT receiver contract in AsyncAPI and validate
  it with the existing Redocly toolchain; do not imply production broker
  identity or durable delivery from this document.
- Treat local `curl` commands as onboarding and smoke checks; automated Go and
  CI contract tests remain the authority.

## Related sources

- [System architecture](../architecture/system-architecture.md)
- [Technology decisions](../architecture/technology-decisions.md)
- [FND-001 Go API Foundation](../roadmap/feature-plans/completed/FND-001-go-api-foundation.md)
- [MVP-005 MQTT Telemetry Ingestion](../roadmap/feature-plans/completed/MVP-005-mqtt-telemetry-ingestion.md)
- [Roadmap](../roadmap/roadmap.md)
