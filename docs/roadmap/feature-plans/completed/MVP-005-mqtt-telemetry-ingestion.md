# MVP-005 — MQTT Telemetry Ingestion

Status: Complete

Review state: Implemented, reviewed, and reconciled on 2026-09-21 against the
merged MVP-004 runtime, current API composition, registry boundary, dependency
graph, CI gates, and planning sources. The architecture, contract, failure
behavior, security limits, validation, rollback, and documentation closeout are
explicit. PR #14 review findings were resolved in `7fe92c2`; the PR candidate
is ready for merge review within this boundary. It must stop for re-planning if
it needs production MQTT, credentials, durable delivery, persistence, schema
changes, a new runtime, or another dependency.

Branch: `feat/mvp-005-mqtt-telemetry-ingestion`

Intended PR: One ingestion-boundary PR

Milestone: M2 — Telemetry and current state

Impact: Material Change (Tier 2). This introduces the first application MQTT
consumer, a new untrusted realtime input boundary, registry reads from an
asynchronous path, broker-dependent readiness, reconnect and drain behavior,
and a machine-readable message contract. It remains a local/test-only module
inside the existing Go process and does not add persistence, migrations,
production identity, a new deployment unit, or a new runtime dependency.

## Goal

Subscribe the existing Go application to telemetry v1 on the reviewed local
broker, strictly validate every delivery, resolve its tenant/device pair
through the authoritative device registry, and emit a bounded, explicit
`AcceptedTelemetry` value for MVP-006.

The slice must distinguish MQTT transport acknowledgement from PulseGrid
application acceptance. It must not imply that telemetry is durable, deduplicated,
projected, queryable, authenticated, or production-ready.

## Acceptance Boundary

This PR is complete when a contributor can:

1. start the existing isolated PostgreSQL and Mosquitto dependencies, migrate
   and seed the database, and start the API with telemetry ingestion explicitly
   enabled;
2. register a device through the existing development GraphQL/console journey;
3. publish the documented telemetry-v1 message with the existing simulator;
4. observe one structured `telemetry_accepted` result containing safe
   ingestion, message, organization, and device identifiers but no raw payload;
5. observe deterministic rejection/failure reason codes for malformed,
   unsupported, retained, oversized, future-skewed, unknown-device,
   wrong-tenant, saturated, and dependency-failure cases;
6. observe readiness become unavailable while the enabled MQTT subscription is
   disconnected and recover after the broker returns; and
7. stop the process without accepting new work after drain begins or abandoning
   work already admitted to the bounded ingestion queue within the configured
   shutdown budget.

The required runtime path is:

```text
device-simulator or isolated test publisher
-> MQTT 3.1.1 QoS 1, retain=false
-> loopback Mosquitto
-> Paho subscription adapter in the existing API process
-> bounded in-memory ingress queue
-> strict topic and payload validation
-> tenant/device registry resolution in PostgreSQL
-> AcceptedTelemetryConsumer
-> diagnostic acceptance sink for MVP-005
```

The path ends before telemetry persistence and projection. A broker PUBACK, a
Paho callback, enqueue success, or successful registry lookup alone is not
application acceptance. For this slice, `telemetry_accepted` is emitted only
after the configured `AcceptedTelemetryConsumer` returns successfully.

## Why

MVP-004 proves the producer and transport fixture only. MVP-006 needs a trusted,
tenant-resolved application input with stable duplicate, clock, and correlation
semantics. Implementing that boundary now keeps persistence and state projection
out of the transport adapter while giving the next PR a narrow consumer
interface and real-broker evidence.

## Verified Repository Baseline (2026-09-21)

- Local `main`, `origin/main`, and the new local feature branch point to
  `6b2d128`, the merged MVP-004 commit. The remote has no MVP-005 feature branch,
  and the worktree was clean before this plan revision.
- MVP-001 through MVP-004 are merged. The feature-plan index and completed
  MVP-004 plan are current; the roadmap diagram/prose still omit some completed
  nodes and are corrected as planning-status drift with this review.
- The API is one Go/Fiber process. `cmd/api` currently opens PostgreSQL and the
  registry only when development GraphQL identity is enabled, composes one
  readiness callback, and owns signal-driven HTTP shutdown. MQTT ingestion does
  not exist yet.
- The registry already owns organization and device authority through
  `FindOrganizationBySlug` and tenant-scoped `GetDevice`. It lacks the
  single-purpose tenant-slug/device-ID resolution operation needed by the
  ingestion path; no schema or migration is required to add that read.
- MVP-004 supplies the exact topic, telemetry-v1 payload, loopback development
  and test brokers, one-shot simulator, and real-broker harness. Mosquitto is
  pinned at `2.1.2` by digest, keeps no persistence, binds host ports only to
  `127.0.0.1`, and rejects broker messages above 16 KiB.
- `github.com/eclipse/paho.mqtt.golang v1.5.1` is already a verified direct Go
  dependency. Reuse it for the consumer; this plan adds no Go runtime
  dependency and does not upgrade Paho.
- `@redocly/cli 2.51.2` is already pinned as a root development dependency.
  This pinned release supports AsyncAPI 3.1 validation, so the first concrete
  producer/consumer contract can reuse the existing API-contract toolchain
  without adding the substantially larger AsyncAPI CLI or parser dependency.
- The selected local toolchain is Node 24.20.0, pnpm 12.3.4, Go 1.27.1,
  Docker Engine 29.7.2, Docker Compose 5.4.0, and Redocly CLI 2.51.2.
  `go -C apps/api mod verify` passes.
- `main` protection is strict and enforced for administrators. Its 14 required
  contexts include `mqtt-integration`, `api-db-integration`, and
  `openapi-contract`; this plan expands those existing owners rather than
  introducing a new context that would require a separate protection mutation.

## Scope

1. Add a machine-readable AsyncAPI 3.1.0 receiver contract under
   `apps/api/api/asyncapi/` for the existing telemetry-v1 topic and payload.
   Pin MQTT binding `0.2.0`, describe only the implemented loopback
   development/test servers and receive operation, and keep dynamic runtime
   rules such as future-clock skew in human documentation and executable tests.
2. Register that contract in `redocly.yaml`, add an `asyncapi:lint` root
   command, compose it into local lint/check flows, and run it in the existing
   `openapi-contract` CI context. Do not add a new schema tool or CI context.
3. Add explicit API-process ingestion configuration. The safe default is
   disabled; `development` activation is allowed only in `development` or
   `test` and requires the exact environment-specific loopback broker URL.
   Production must reject enabled ingestion, non-loopback hosts, credentials,
   alternate schemes, paths, queries, fragments, and development/test port
   crossover.
4. Add a telemetry-ingestion application package that owns strict topic and
   payload validation, registry resolution, acceptance metadata, error
   classification, and the narrow synchronous `AcceptedTelemetryConsumer`
   port. Keep it independent of Paho, Fiber, GraphQL, pgx, and persistence.
5. Add one registry read operation that resolves a device by tenant slug and
   canonical device UUID in one parameterized query and returns the existing
   consumer-safe device record. Missing tenant, missing device, and a device
   belonging to another tenant must share the same not-found result.
6. Add an MQTT transport adapter using the already pinned Paho client. It owns
   connect, subscribe, reconnect, bounded queue admission, connection state,
   and drain/disconnect behavior, but no device authority, payload business
   rules, storage, retries of application work, or projection logic.
7. Compose the registry, ingestion boundary, diagnostic MVP-005 consumer, MQTT
   adapter, and HTTP server in `cmd/api`. Open one database pool/repository when
   development GraphQL or ingestion needs it; do not create a second pool or
   make the ingestion package depend on GraphQL identity.
8. Make readiness check PostgreSQL when the pool is active and MQTT
   connected-plus-subscribed when ingestion is enabled. Liveness remains
   process-only. Initial enabled MQTT connection/subscription failure blocks
   startup; a later outage makes readiness fail while bounded reconnect
   proceeds.
9. Add unit, race, PostgreSQL integration, real-Mosquitto/API integration,
   malformed-input, reconnect, saturation, and shutdown evidence. Extend the
   existing isolated MQTT harness and CI context instead of adding another
   broker harness.
10. Update environment examples, API and local-development documentation,
    architecture/technology status, the downstream MVP-006 boundary, and final
    plan/roadmap closeout after the implementation and evidence are reconciled.

## Out of Scope

- Telemetry tables, migrations, persistence, retention, current-state
  projection, GraphQL telemetry queries, UI behavior, rules, alerts, or command
  handling.
- Durable application delivery, offline buffering, a persistent MQTT session,
  dead letters, replay, application retries, exactly-once claims, or
  deduplication. MVP-006 owns durable idempotency by logical `messageId`.
- Production broker product/topology, public or non-loopback listeners, TLS,
  device credentials, tenant ACLs, secret management, credential rotation,
  broker HA, or production readiness.
- Kafka, schema registries, generic event/message-bus abstractions, a separate
  ingestion process, service extraction, gRPC, Redis, or a specialized
  telemetry store.
- A device-class temperature policy. MVP-005 validates representation and
  finiteness but does not invent sensor-specific minimum/maximum values without
  a product or device-capability contract.
- Rate-limit infrastructure, metrics backends, distributed tracing, load
  testing, or performance claims. The local fixture receives bounded in-process
  work and diagnostic logs only.
- Changes to the MQTT broker image, digest, listener exposure, payload cap,
  Compose lifecycle commands, GraphQL schema/generated files, frontend code,
  or browser-visible behavior.

## Dependencies

- MVP-001 supplies organization/device persistence and the authoritative
  tenant-device relationship.
- MVP-002 supplies the development registry composition and the operator path
  used to create a valid device.
- MVP-004 supplies the reviewed MQTT 3.1.1/QoS-1 contract, Mosquitto fixture,
  simulator, Paho dependency, integration harness, and required CI context.
- FND-001/FND-003 supply process lifecycle, operational readiness, root
  validation commands, dependency audit, and merge-gate ownership.
- PostgreSQL and Mosquitto are required dependencies only when ingestion is
  enabled. Disabled ingestion preserves the existing health-only/GraphQL modes.

No new third-party dependency is expected. Discovery of a required dependency,
broker change, or unsupported AsyncAPI 3.1 behavior in the pinned Redocly
version is a re-plan condition with dependency admission and rollback review.

## Architecture / Boundaries

### Logical ownership and dependency direction

```text
cmd/api composition root
├── device/registry                 PostgreSQL authority and tenant/device read
├── telemetry/ingestion             transport-independent validation and acceptance
│   └── AcceptedTelemetryConsumer   narrow port consumed by MVP-006 later
├── telemetry/mqtttransport         Paho lifecycle and bounded delivery adapter
└── platform/httpserver             HTTP lifecycle and generic readiness contract
```

- `telemetry/ingestion` owns the accepted application shape and rules. It may
  depend on narrow registry/consumer ports and standard value types, but not on
  Paho messages, MQTT tokens, pgx rows, GraphQL models, HTTP requests, or future
  telemetry tables.
- `telemetry/mqtttransport` converts a Paho delivery into an owned immutable
  topic/payload/metadata copy and submits it to ingestion. It does not parse a
  tenant as authority or expose Paho types across the adapter boundary.
- The registry remains the sole authority for tenant/device association. The
  ingestion boundary cannot create, mutate, or infer a device from the topic.
- The MVP-005 diagnostic consumer proves application acceptance observably but
  owns no state. MVP-006 will implement the same consumer port with bounded
  PostgreSQL persistence/projection rather than changing the transport contract.
- One API process, one database pool, and one Paho client are the runtime shape.
  A new process or generic broker abstraction has no current scaling,
  reliability, or ownership justification.

### Configuration and activation contract

| Key | Disabled behavior | Enabled development | Enabled test | Production |
| --- | --- | --- | --- | --- |
| `PULSEGRID_MQTT_INGESTION_MODE` | Missing or `disabled`; API does not connect to MQTT | Exact `development` | Exact `development` | `disabled` only; enabled mode is rejected |
| `PULSEGRID_MQTT_BROKER_URL` | Not required by the API; may still be used by the simulator | Exact `mqtt://127.0.0.1:1883` | Exact `mqtt://127.0.0.1:11883` | No ingestion broker is accepted |
| `PULSEGRID_DATABASE_URL` | Existing rules apply | Required when GraphQL or ingestion activates the registry | Required isolated test database when ingestion is enabled | Existing production database boundary remains deferred |

The simulator continues to consume its tenant, device, and temperature keys.
The API must not consume `PULSEGRID_MQTT_TENANT_SLUG` or a client-supplied tenant
authority; it subscribes to the versioned tenant wildcard and resolves every
pair through the registry.

### MQTT telemetry-v1 input contract

- Protocol: MQTT 3.1.1 over TCP.
- Subscription filter:
  `pulsegrid/v1/tenants/+/devices/+/telemetry` at QoS 1.
- Accepted topic has exactly seven non-empty levels. The fixed levels must match
  literally; `tenantSlug` must satisfy the registry slug grammar and length;
  `deviceId` must be a non-nil canonical lowercase UUID.
- Received QoS must be 1. A retained delivery is rejected. MQTT's DUP flag is
  recorded for diagnostics but is not a logical duplicate identifier.
- Payload must be valid UTF-8 JSON, strictly smaller than 1 KiB, one object with
  exactly the four fields below, no unknown or duplicate keys, and no trailing
  JSON value.

```json
{
  "schemaVersion": 1,
  "messageId": "5edacace-70a7-4a8f-846d-3f4d60c56f3a",
  "observedAt": "2026-09-18T04:00:00Z",
  "temperatureCelsius": 23.5
}
```

- `schemaVersion` is the JSON integer literal `1`; other versions are rejected.
- `messageId` is a non-nil canonical lowercase UUID and remains the downstream
  logical idempotency key. MQTT packet IDs and the DUP flag are not substitutes.
- `observedAt` is an RFC 3339/RFC 3339Nano string encoded in UTC with a
  literal `Z`. It must be non-zero and no later than five minutes after the
  injected server receive time. There is no lower-age rejection in MVP-005 so
  MVP-006 can define and test late/out-of-order behavior explicitly.
- `temperatureCelsius` is a finite JSON number representable as `float64`.
  MVP-005 adds no unsupported device-specific range.

JSON decoding must reject duplicate member names explicitly; Go's default
decoder accepting the last duplicate value is not strict enough for this trust
boundary. Clock and identifier generation must be injectable for deterministic
tests without making them public runtime configuration.

### Accepted application interface

Each accepted delivery carries only validated and resolved values:

```text
IngestionID          fresh UUID for this broker delivery attempt
MessageID            producer-supplied logical observation UUID
OrganizationID       registry-authoritative UUID
DeviceID             registry-authoritative UUID
ObservedAt           validated device time
ReceivedAt           injected server time captured at queue admission
TemperatureCelsius   validated measurement
MQTTDuplicate        transport diagnostic only
```

Topic tenant slug is used for lookup and safe diagnostic context but the
registry's organization UUID is the downstream authority. The interface is
synchronous and context-aware. A nil consumer, consumer error, or canceled
drain is a processing failure, never an accepted result.

### Duplicate and delivery semantics

- QoS 1 is at-least-once between publisher and broker/client transport; it does
  not make application handling durable.
- Every delivery receives a new `IngestionID`. Re-deliveries preserve the same
  producer `MessageID` and may reach the consumer more than once.
- MVP-005 does not deduplicate in memory because restart would make that claim
  false and would pre-empt MVP-006's durable idempotency boundary.
- The Paho callback performs only bounded validation needed for queue safety,
  copies owned message bytes, and attempts a non-blocking enqueue. It returns
  promptly so Paho may acknowledge the transport delivery. Queue-full,
  registry, or consumer failures are therefore not redelivered by MVP-005;
  they are explicit diagnostic failures. This local non-durable trade-off must
  remain visible in documentation and is not a production guarantee.

### Bounded work and lifecycle

- Use one worker and a fixed queue capacity of 64 deliveries. This preserves a
  simple deterministic MVP order, caps retained message memory and registry
  concurrency, and provides an observable saturation path. A measured workload
  or persistence latency is the trigger to revisit capacity/parallelism.
- Use Paho ordered callback dispatch because the callback is non-blocking; do
  not enable a mode that creates an unbounded goroutine per message.
- Use a process-unique client ID, MQTT protocol version 4 (3.1.1), clean
  session, no offline store, 10-second keepalive, five-second connect/write/
  subscribe bounds, automatic reconnect, and a maximum five-second reconnect
  interval. Initial connect retry remains disabled so startup fails fast.
- On each initial connection or reconnect, subscribe idempotently and expose
  ready state only after the subscribe token succeeds. Connection loss clears
  readiness immediately. Post-start reconnect may continue while the process is
  live, but each network attempt and backoff interval is bounded, no message
  backlog is retained, and the operator sees `503 dependency_unavailable`.
- Shutdown first prevents new admission and unsubscribes/disconnects MQTT, then
  closes and drains the bounded queue. HTTP drain and ingestion drain share one
  overall `PULSEGRID_SHUTDOWN_TIMEOUT` deadline and run concurrently. The
  database pool closes only after consumers stop. Timeout or cleanup failure
  exits non-zero and logs a stable reason code.

### Readiness

- Liveness remains process-only and does not call PostgreSQL or MQTT.
- Disabled ingestion does not affect readiness.
- Enabled ingestion requires both an active PostgreSQL probe and
  connected-plus-subscribed MQTT state. The public HTTP contract retains the
  existing generic `dependency_unavailable` reason and does not reveal which
  dependency, broker URL, tenant, or database failed.
- A broker outage after startup degrades readiness but leaves liveness and
  already configured HTTP/GraphQL handling available during recovery.

## Security / Threat Model

Protected assets are tenant/device association, local database availability,
process/broker availability, diagnostic output, and the boundary between local
fixture behavior and production claims. Credible actors are the local
contributor, isolated CI, and a malicious or compromised local process able to
reach loopback Mosquitto. Internet/production devices remain outside scope and
must not gain a supported path through this change.

| Abuse or failure path | Preventive/detective control | Required evidence | Remaining risk |
| --- | --- | --- | --- |
| Local client spoofs a tenant/device topic | Topic values grant no authority; registry resolves the exact tenant/device pair; wrong pair and unknown identity share one rejection | Real PostgreSQL cross-tenant tests and real-broker negative publish | Any local process can still impersonate an existing registered device because the fixture has no device credential |
| Malformed, ambiguous, or oversized payload consumes work or bypasses validation | Broker 16 KiB cap, application `<1 KiB` cap, UTF-8 check, exact topic levels, duplicate/unknown-field rejection, strict typed values | Table/fuzz seed corpus plus broker/API integration | Local clients can continuously send cheap invalid messages; no production rate-limit claim exists |
| Flood exhausts goroutines, memory, or database connections | Non-blocking callback, one worker, 64-item queue, bounded payload copy, observable saturation drop | Saturation/race test and log inspection | Messages are dropped when full; there is no durable backlog |
| Replay or QoS duplicate creates a second logical observation | Preserve `messageId`, generate delivery-specific `ingestionId`, expose DUP only as diagnostic, defer durable idempotency to MVP-006 | Duplicate integration/component evidence | Diagnostic MVP-005 consumer sees duplicates; no deduplication claim |
| Far-future device clock poisons later current-state ordering | Reject timestamps more than five minutes after receive time; accept old data for downstream late-event policy | Deterministic boundary tests | Production clock-sync and device-specific skew policy remain undecided |
| Broker or database outage is hidden | Enabled dependency participates in readiness; stable connection/processing reason codes; initial failure stops startup | Down/recovery integration and startup-negative tests | A delivery already transport-acknowledged can be lost during a later registry/consumer failure |
| Retained poison input reappears on subscribe | Reject deliveries marked retained; local simulator remains `retain=false` | Retained-message negative integration test | An active subscriber cannot infer the original publish retain flag in every MQTT delivery case; local broker access remains trusted only as a fixture |
| Input or configuration leaks through logs | Structured allowlisted fields and stable reason codes; never log payload, temperature, broker URL, environment, credentials, or raw database error | Log assertions on success/config/network/database failures | Identifiers remain visible in local diagnostic logs by design |
| Production or shared broker is enabled accidentally | Explicit mode, environment-specific exact loopback URL, production rejection, no credentials/TLS path | Configuration matrix and production-mode negative test | Production ingestion remains entirely unimplemented |

## Failure and Recovery Model

| Failure | System behavior | Recovery / next action |
| --- | --- | --- |
| Invalid configuration | Fail before opening database, MQTT, or HTTP listener; print safe actionable configuration error | Correct the local environment and restart |
| Database/schema unavailable at startup | Existing classified startup failure; close any opened resources | Start/migrate isolated database and restart |
| MQTT connect/subscribe unavailable at enabled startup | Fail startup within bounded token wait; close MQTT/database resources | Start broker or correct configuration and restart |
| Broker disconnect after startup | Clear MQTT-ready state, return readiness 503, reconnect with a bounded attempt timeout and backoff cadence, resubscribe before ready | Automatic when broker returns; operator inspects stable logs if it does not |
| Invalid/unsupported/unknown telemetry | Do not call consumer; emit one safe rejection classification; transport delivery is discarded | Correct publisher/topic/payload/registration and republish with a new logical message when appropriate |
| Registry temporarily fails for queued delivery | Emit processing failure; do not claim acceptance; no application retry in this slice | Restore database and republish; durable handling is a later boundary |
| Queue full | Non-blocking drop with `ingestion_overloaded`; no new goroutine or database query | Reduce local publish rate/restart; revisit capacity only with evidence |
| Consumer fails | Emit `consumer_failed`; no acceptance or retry claim | MVP-006 defines durable transaction/idempotency behavior before replacing the diagnostic consumer |
| Shutdown with queued/in-flight work | Stop admission, drain within shared deadline, report interruption/timeout, exit non-zero if incomplete | Restart and republish unpersisted local fixture data |

No recovery step mutates production or shared state. The containment switch is
`PULSEGRID_MQTT_INGESTION_MODE=disabled`; code rollback is a normal revert
because this PR has no schema, stored-data, broker, or public API migration.

## Decisions and Alternatives

### Selected

- Existing modular Go process, not a new service.
- Existing Paho v1.5.1 dependency, not another MQTT client or wrapper framework.
- Direct flow-specific adapter and consumer port, not a generic message bus.
- Existing local/test Mosquitto services, unchanged.
- AsyncAPI 3.1.0 receiver contract validated by existing Redocly 2.51.2.
- Bounded in-memory handoff with explicit non-durable loss behavior.
- Durable deduplication and projection deferred intact to MVP-006.

### Rejected for this slice

- **Do nothing / log raw broker messages:** cannot establish trusted tenant,
  schema, clock, or downstream interface semantics.
- **Separate ingestion worker/service:** adds deployment, health, database, and
  operational boundaries without a scaling or ownership requirement.
- **Kafka or generic event abstraction:** no replay, fan-out, or independent
  consumer requirement exists yet.
- **In-memory deduplication:** creates a misleading guarantee that disappears on
  restart and conflicts with the next plan's durable idempotency ownership.
- **Persistent MQTT session/offline queue:** local broker persistence is
  deliberately disabled and production session policy is undecided.
- **AsyncAPI CLI/parser dependency:** existing Redocly performs the required
  contract validation with lower dependency and maintenance cost.
- **Temperature min/max guess:** the current product documents no device class
  or capability authority from which to derive a safe business range.

### Revisit triggers

- non-loopback/shared/production broker, credentials, TLS, or device ACLs;
- a requirement not to lose accepted data across process/dependency failure;
- sustained queue saturation, measured throughput/latency pressure, or need for
  independent scaling;
- more than one independent telemetry consumer, durable replay, or fan-out;
- a second message version or producer requiring compatibility migration;
- Redocly failing to validate required AsyncAPI semantics; or
- MVP-006 discovering that the accepted interface lacks data required for
  deterministic persistence/projection.

Each trigger requires re-planning before broadening this PR.

## Implementation Direction

1. Change status to `In progress` only in the first implementation commit.
   Add the AsyncAPI receiver contract and Redocly/root validation wiring first,
   proving the existing simulator contract against the machine-readable shape.
2. Extend API configuration with disabled-by-default ingestion mode and strict
   environment-specific broker validation. Add example values and full
   development/test/production negative matrices before opening a connection.
3. Add the single-query registry resolver and real-PostgreSQL tests for valid,
   unknown tenant, unknown device, and cross-tenant pair behavior.
4. Implement pure topic/payload validation and `AcceptedTelemetry` construction
   with injected clock/ID sources. Prove duplicate JSON keys, trailing data,
   invalid UTF-8, exact size boundary, version, UUID, time, and numeric cases.
5. Implement the bounded MQTT adapter around a minimal local Paho-facing seam.
   Prove queue admission/saturation, state transitions, token timeout/error,
   reconnect/subscription readiness, and drain behavior without replacing the
   pure ingestion rules with mocks.
6. Refactor `cmd/api` composition only as far as needed to share one pool and
   repository, compose dependency readiness, run HTTP/MQTT lifecycles, preserve
   classified startup failures, and close resources in the reviewed order.
7. Extend the isolated MQTT integration harness to own unique PostgreSQL and
   Mosquitto test resources, apply migrations/seed, build/start the real API,
   register a device, publish with the real simulator, assert structured
   acceptance/rejection, stop/start the broker to prove readiness recovery,
   signal the API, and clean up success/failure/interrupt paths.
8. Update the documentation set below, reconcile requirement/plan to actual
   diff, perform author self-review and fixes, then run final validation on the
   reviewed candidate.
9. Open the PR only after local final validation. Keep the existing required CI
   context names, inspect all results on the exact head, complete independent
   review when required, then move this plan to `completed/` and update roadmap
   status only after the implementation evidence and PR state support it.

## Expected File Boundary

Expected new/changed surfaces include:

- `apps/api/api/asyncapi/telemetry.yaml`;
- `apps/api/internal/telemetry/ingestion/` and tests;
- `apps/api/internal/telemetry/mqtttransport/` and tests;
- `apps/api/internal/device/registry/repository.go` and integration tests;
- `apps/api/internal/platform/config/`, `apps/api/cmd/api/`, and lifecycle tests;
- API environment examples;
- `redocly.yaml`, root scripts, MQTT integration harness, and repository CI;
- API/local-development, architecture, technology, environment, roadmap, and
  MVP-006 planning documentation.

No migration, GraphQL SDL/generated artifact, frontend source, browser fixture,
Compose service, Mosquitto configuration, `go.mod`, or new package dependency is
expected. An implementation diff outside this boundary requires an explicit
impact check and re-plan when material.

## Validation Plan

### Contract and static evidence

- `corepack pnpm run asyncapi:lint` validates the pinned AsyncAPI document and
  MQTT binding using the existing Redocly dependency.
- Existing Go format, generated GraphQL drift, modernization, Staticcheck, vet,
  build, module verification, and vulnerability checks remain clean.
- Contract review compares topic levels, field names/types, QoS, version, and
  examples across AsyncAPI, simulator code/tests, ingestion code/tests, and
  human documentation.

### Unit/component evidence

- Configuration tests cover disabled defaults; development/test exact URLs;
  wrong port/environment; userinfo/path/query/fragment; production rejection;
  and process-over-dotenv precedence.
- Topic/payload tests and fuzz seed corpus cover valid, edge, malformed,
  duplicate-key, unknown-field, invalid UTF-8, nested/trailing, payload-size,
  schema-version, canonical UUID, UTC/future-skew, and number cases.
- Ingestion tests prove registry is never called for invalid input; consumer is
  never called for rejection/failure; accepted metadata comes from registry and
  injected sources; and duplicate deliveries retain `MessageID` but receive
  distinct `IngestionID` values.
- Adapter/lifecycle tests prove timeout/error classification, non-blocking full
  queue, one-worker bound, connection/subscription readiness, reconnect,
  subscribe-after-reconnect, no admission after drain, in-flight completion,
  and deadline failure. Run under the Go race detector.

### Real-boundary integration evidence

- PostgreSQL integration proves the new registry read against the selected
  engine, including cross-tenant non-disclosure.
- The expanded `mqtt:test:integration` owns unique Compose project/resources,
  real Mosquitto, real PostgreSQL/migrations, real API process, and real
  simulator. It proves valid acceptance, malformed/unknown/wrong-tenant/
  retained/application-oversized rejection, duplicate delivery metadata,
  readiness failure/recovery across broker stop/start, signal-safe drain, and
  cleanup.
- Inspect API and broker logs for unexpected errors and for absence of raw
  payloads, temperature values, URLs, credentials, and database details.

### Regression and final gates

- Run targeted tests during implementation, then `corepack pnpm run check:fast`
  after code/doc integration.
- Run `corepack pnpm run check` as the final local candidate validation. This
  includes race, build, dependency audits, MQTT, database, OpenAPI, AsyncAPI
  through composed lint, and browser regression evidence.
- GitHub's strict required checks remain authoritative on the exact PR head:
  `repository-policy`, five API checks, four web checks,
  `node-dependency-audit`, `mqtt-integration`, `openapi-contract`,
  `api-db-integration`, and `browser-smoke`.
- A passing compile/startup or simulator PUBACK is supporting evidence only;
  it does not replace strict rejection, tenant resolution, reconnect, drain,
  or real-boundary evidence.

Required integration/race/contract evidence is incomplete if skipped. If local
Docker is unavailable, report the real-boundary checks as unavailable and do
not claim the plan complete or PR ready solely from unit evidence.

## Documentation and Closeout

The implementation and closeout updated:

- `docs/api/README.md` with the AsyncAPI source of truth, accepted/rejected
  semantics, application-versus-transport acknowledgement, and operator test
  commands;
- `docs/architecture/system-architecture.md` with implemented ingestion
  ownership, runtime/readiness shape, duplicate/non-durable semantics, and the
  still-deferred persistence boundary;
- `docs/architecture/technology-decisions.md` to mark AsyncAPI 3.1 selected for
  this flow and Paho reused by the application consumer without changing the
  production broker decision;
- `docs/project-setup/environment-configuration.md`, API environment examples,
  `apps/api/README.md`, and `docs/project-setup/local-development.md` with the
  new mode, shared broker URL use, startup order, health, logs, and stop/down
  behavior;
- `docs/roadmap/feature-plans/planned/MVP-006-telemetry-current-state-projection.md`
  with the exact accepted-input fields, durable duplicate ownership, and the
  MVP-005 non-durable handoff limitation; and
- roadmap/feature-plan status and this plan's plan-to-actual evidence. The plan
  is now in `completed/` because the PR candidate is implemented, reviewed, and
  validated; merge approval remains an external repository action.

The completed MVP-004 plan remains historical evidence and is not rewritten.
Product scope, GraphQL/OpenAPI behavior, and frontend documentation change only
if implementation evidence reveals an actual contract impact; such impact is a
re-plan trigger, not silent documentation expansion.

## Rollback / Containment

- Immediate local containment: set `PULSEGRID_MQTT_INGESTION_MODE=disabled` and
  restart; the API must return to its existing non-MQTT behavior.
- Code rollback: revert the MVP-005 PR. There is no migration, persisted
  telemetry, broker data, generated client, public contract consumer, or
  branch-protection mutation to unwind.
- Test cleanup removes only the unique test Compose project and its disposable
  PostgreSQL volume; development data and containers are not targets.
- If enabled ingestion makes readiness unreliable, disable the mode or revert
  rather than weakening readiness, tenant resolution, strict validation, or
  production rejection.

## Engineering Improvement Review

- **Current scope:** strict duplicate-key/UTF-8/topic validation, five-minute
  future-skew protection, bounded queue/concurrency, explicit activation,
  registry pair resolution, dependency-aware readiness, real recovery/drain
  evidence, and a machine-readable AsyncAPI contract. These are tightly coupled
  correctness, security, operability, and testability requirements for the new
  untrusted asynchronous boundary.
- **Future enhancements:** durable persistence/idempotency (MVP-006);
  production authentication/TLS/ACLs and rate controls (post-MVP hardening);
  metrics/scale changes only after measured saturation; Kafka only after the
  existing durable fan-out/replay trigger.
- **Scope effect:** no expansion beyond one material ingestion-boundary PR.
  Any future item above requires its owning plan and approval.

## Final Plan Review (2026-09-21)

### Findings resolved by this revision

- **Blocker:** client, reconnect, payload-size, timestamp, duplicate, and
  acceptance semantics were open. The plan now selects exact behavior and
  records the non-durable trade-off.
- **Blocker:** application composition/readiness and shutdown ownership were
  unspecified. The plan now defines one pool, one MQTT client, dependency-aware
  readiness, bounded queue admission, shared drain budget, and close order.
- **Blocker:** strict JSON did not define unknown/duplicate fields, UTF-8,
  trailing data, clock, or canonical identifier handling. These rules and their
  evidence are now explicit.
- **Major:** the first concrete MQTT consumer triggered the architecture's
  AsyncAPI adoption rule but the original plan omitted it. The plan now reuses
  the existing Redocly toolchain with no new dependency or CI context.
- **Major:** tenant resolution, duplicate delivery, and downstream MVP-006
  ownership were vague. The accepted DTO and authority/dedup boundaries are now
  explicit.
- **Major:** the prior validation list could pass without the real API,
  PostgreSQL, reconnect-readiness, saturation, or drain path. The revised
  harness and evidence matrix cover those boundaries.

### Remaining non-blocking limitations

- Anonymous loopback publishers can impersonate any existing local device.
- A transport-acknowledged delivery can be lost after enqueue, on saturation,
  or on registry/consumer failure because MVP-005 is intentionally non-durable.
- The five-minute future-skew bound is an MVP safety guard, not a production
  clock-synchronization policy; old telemetry is deliberately passed onward.
- Queue capacity and single-worker throughput are unmeasured MVP choices with
  explicit saturation and revisit signals, not scale claims.

### Prior approval verdict

Plan verdict: **Approved for implementation within the reviewed boundary.**

Reasoning budget: Full for a Material Change (Tier 2).

Confidence: High for repository state, selected dependencies, architecture,
contract, and test surfaces; medium for future production delivery semantics,
which remain explicitly outside this slice.

The initial planning/env commit was `fc27f61`; the delivered candidate adds the
AsyncAPI, ingestion boundary, Paho runtime, composition, integration harness,
and documentation updates. This prior approval authorized only the scope and
decisions in the reviewed version.
Production MQTT, durable delivery/persistence, migrations, new dependencies,
broker changes, service extraction, or a materially different acceptance
contract remains a re-planning condition rather than an implicit extension of
this completed plan.

## Implementation checkpoint (2026-09-21)

The reviewed candidate now implements the approved local/test boundary: shared
telemetry contract and AsyncAPI receiver document, disabled-by-default strict
MQTT configuration, tenant-slug/device registry resolution, transport-free
validation and `AcceptedTelemetry` handoff, Paho reconnect/readiness/drain
runtime, API composition, diagnostic allowlisted logs, and the real
PostgreSQL/Mosquitto/API integration harness. No migration, new dependency,
Compose service, production broker path, or persistence was added.

Evidence on the delivered candidate:

- `corepack pnpm run check` passed, including format/generated checks,
  modernization, Staticcheck, vet, lint, typecheck, unit/Vitest tests, race,
  build, OpenAPI/AsyncAPI validation, Node/Go audits, MQTT integration,
  PostgreSQL integration, and 17 browser smoke tests;
- the MQTT integration passed valid acceptance, malformed/oversized/
  wrong-tenant/unknown-device/retained rejection, duplicate logical delivery,
  broker outage/readiness recovery, log redaction, signal-driven drain, and
  disposable cleanup; and
- author self-review added explicit initial connect/subscribe timeout tests,
  nil-callback protection, fuzz seeds for strict payload decoding, and timeout
  failure handling in the integration runner; PR review follow-up now also
  synchronizes readiness generation commits, bounds Paho write/unsubscribe
  shutdown, removes MQTT 5-only AsyncAPI fields, scopes integration assertions
  to each delivery, restores broker-cap coverage, and adds deterministic drain,
  saturation, deadline, and missing-device evidence; and
- PR #14's required GitHub checks passed on the implementation/review head,
  covering repository policy, API static/test/race/database/security checks,
  web lint/typecheck/test/build, dependency audit, MQTT integration, OpenAPI
  contract validation, and browser smoke. The documentation-only closeout is
  limited to plan/index/link updates and is revalidated by the same PR gates
  before merge.

Plan-to-actual result: the approved boundary, ownership, exclusions, failure
semantics, validation matrix, rollback path, and named documentation all match
the delivered implementation. The plan is complete and remains unmerged only
because merge is a separate repository action.

## Done Criteria

The ingestion boundary is complete only when the exact approved implementation
emits a validated, tenant-resolved `AcceptedTelemetry` value for valid local/test
MQTT deliveries; rejects or fails every named invalid/degraded case observably;
recovers readiness after broker interruption; drains within the shared shutdown
budget; keeps production, persistence, and device authentication out of scope;
passes the required local and exact-head CI evidence; reconciles plan to actual;
completes author self-review and any required independent review; updates the
named documentation; and records all skipped/unavailable checks and remaining
risk without overstating durability or production readiness.
