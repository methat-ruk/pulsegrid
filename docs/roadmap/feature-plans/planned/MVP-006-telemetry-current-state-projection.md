# MVP-006 — Telemetry and Current-State Projection

Status: In progress

Review state: Reviewed on 2026-09-22 against merged MVP-005, the current Go
composition and telemetry handoff, PostgreSQL migrations/repositories, GraphQL
contract and complexity controls, dependency pins, integration harnesses, CI
gates, and downstream MVP-007/MVP-008 needs. The reviewed implementation has
started within the boundary below. Local final validation has passed; plan-to-
actual reconciliation and independent review remain open.

Branch: `feat/mvp-006-telemetry-current-state-projection`

Intended PR: One backend-state PR

Milestone: M2 — Telemetry and current state

Impact: Material Change (Tier 2). This adds durable tenant-related data,
idempotent and concurrent write behavior, a derived current-state authority,
retention and ordering semantics, a migration, and additive GraphQL contracts.
It remains a local/test MVP slice inside the existing Go process and PostgreSQL
deployment. It does not add production identity, a new runtime, a new external
dependency, or a specialized telemetry store.

## Goal

Replace MVP-005's diagnostic telemetry sink with one transactional consumer
that stores an append-only logical-identity authority, a bounded set of unique
device observations, and a deterministic current-device-state projection.
Expose the current state and a bounded recent history through the existing
tenant-scoped development GraphQL API.

An MQTT delivery is application-accepted only after this consumer commits or
identifies an exact logical replay as an idempotent no-op. The slice must not
claim durable delivery for messages dropped or failed before that commit.

## Acceptance Boundary

This PR is complete when a contributor can:

1. start the existing isolated PostgreSQL and Mosquitto dependencies, migrate
   and seed the database, enable the existing development identity and MQTT
   ingestion modes, and start the existing API process;
2. register a device and publish the existing telemetry-v1 message;
3. query that device's current measurement, selected observation time,
   selected receive time, and logical last-seen time through GraphQL;
4. query a deterministic, cursor-paginated recent telemetry history with a
   default of 50 and a maximum page size of 100;
5. replay the same device/message pair without creating another history row,
   advancing last-seen, or changing current state;
6. submit late and equal-timestamp observations and observe the documented
   deterministic projection behavior across restart;
7. prove that another tenant cannot read or influence the device's history or
   current state; and
8. observe persistence failure as a processing failure rather than a false
   `telemetry_accepted` result.

The required runtime path is:

```text
MVP-005 AcceptedTelemetryConsumer port
-> telemetry persistence/projection application boundary
-> one PostgreSQL transaction
   -> insert or classify logical observation
   -> update last-seen for a new observation
   -> conditionally advance current measurement
   -> enforce the per-device history cap
-> MVP-005 emits application acceptance after consumer success

development GraphQL principal
-> tenant-scoped telemetry read interface
-> PostgreSQL device ownership join
-> current state or bounded history DTO
```

## Why

MVP-005 deliberately stops at a synchronous, non-durable consumer port. The
operator journey and later rule evaluation need restart-safe observation
history, deterministic latest state, and an API contract that does not confuse
MQTT delivery metadata with logical telemetry identity.

## Verified Repository Baseline (2026-09-22)

- The worktree was clean before this plan revision. The feature branch,
  `main`, and locally known `origin/main` all point to `8715e61`, the merged
  MVP-005 commit. MVP-001 through MVP-005 are recorded complete; MVP-006 is the
  next dependency in the roadmap.
- `cmd/api` already opens one PostgreSQL pool/repository whenever development
  GraphQL or MQTT ingestion is enabled. The enabled MQTT path supplies a
  validated, tenant-resolved `AcceptedTelemetry` value to a synchronous
  `AcceptedTelemetryConsumer`; the current consumer is a no-op diagnostic sink.
- `AcceptedTelemetry` already carries `IngestionID`, producer `MessageID`,
  registry-authoritative `OrganizationID` and `DeviceID`, `ObservedAt`, queue
  admission `ReceivedAt`, finite `TemperatureCelsius`, and transport-only
  `MQTTDuplicate`. The topic tenant is not authoritative.
- The current MQTT adapter has one worker and a 64-item queue. Broker
  acknowledgement, queue admission, and registry resolution remain
  non-durable; queue drops, consumer failures, and process interruption before
  commit are not replayed automatically.
- PostgreSQL 18.6, pgx v5.11.0, and Goose v3.28.0 are already selected and
  pinned. Migrations are embedded SQL, executed explicitly outside API startup,
  and the existing integration gate exercises up, idempotent up, down, and up.
- gqlgen v0.17.95 is schema-first with committed generated files, POST-only
  development exposure, fixed server-selected organization authority, opaque
  versioned device cursors, a 1–100 list bound, parser/body/response limits, and
  a fixed operation-complexity limit.
- The existing MQTT integration harness already starts real PostgreSQL,
  Mosquitto, the API, and simulator; enables both development identity and
  ingestion; creates a device over GraphQL; and exercises duplicate, failure,
  recovery, log-redaction, and shutdown paths. It is the correct end-to-end
  owner to extend rather than creating another runtime harness.
- No new third-party dependency is required. `go -C apps/api mod verify`, the
  repository-policy check, and `corepack pnpm run check:fast` pass on this
  baseline. Docker-backed, race, build, audit, and browser gates were not run as
  part of this documentation-only review.

## Scope

1. Add one forward/backward Goose migration for the append-only logical
   observation identity authority, bounded telemetry observations, and the
   per-device current-state projection, with the constraints and indexes needed
   for replay/conflict classification, projection ordering, tenant-scoped
   reads, and bounded history.
2. Add a telemetry persistence/projection package behind the existing
   `AcceptedTelemetryConsumer` port. Keep pgx and SQL inside this boundary; do
   not move persistence into MQTT transport, ingestion validation, registry,
   GraphQL resolvers, or `cmd/api`.
3. Classify each logical observation against an append-only canonical identity
   authority and update identity, bounded history, current-state projection,
   and per-device retention in one transaction. Exact replay is a successful
   no-op even after its history row is pruned; message-ID reuse with different
   logical content is a processing failure.
4. Retain at most 1,000 logical history observations per device: the selected
   current observation plus the 999 newest other rows by a database-generated
   internal storage sequence. The identity authority is not pruned in this MVP;
   its compact one-row-per-accepted-message growth is the explicit cost of the
   durable replay/conflict contract. The history bound is not a time-based
   retention promise.
5. Add narrow tenant-scoped repository reads for current state and recent
   history. Keep storage records internal and map them to explicit GraphQL
   models.
6. Add additive GraphQL query fields, telemetry/current-state output types, and
   a telemetry-specific opaque cursor. Bound `first` to 1–100 with a default of
   50 and update operation-complexity weights for the new fields.
7. Replace only the no-op consumer composition in `cmd/api`; preserve the
   existing process, one shared pool, ingestion queue, readiness/liveness,
   configuration, MQTT contract, and shutdown ownership.
8. Add unit, race, real-PostgreSQL, GraphQL contract, migration/restart, and
   real MQTT-to-GraphQL evidence for normal, duplicate, conflict, late,
   equal-time, concurrent, retention-bound, empty, invalid-cursor, and
   cross-tenant behavior.
9. Reconcile the implementation and reviewed plan, complete author
   self-review/fixes, run final validation, and close out the named project
   documentation only after actual behavior is proven.

## Out of Scope

- Production MQTT, public exposure, device credentials, production identity,
  RBAC, row-level security, audit, TLS, broker durability, replay, offline
  buffering, dead letters, or exactly-once delivery claims.
- Changing the telemetry-v1 topic/payload/AsyncAPI contract, five-minute future
  skew, input validation rules, Paho/Mosquitto configuration, ingress queue
  capacity, worker count, readiness, or shutdown policy.
- A time-based retention period, background retention job, aggregation,
  downsampling, historical export, arbitrary metric model, long-range
  analytics, performance/load claims, or permanent high-volume storage choice.
- Frontend code, charting, refresh/staleness UI policy, subscriptions,
  WebSockets, polling, cache infrastructure, rules, alerts, or commands.
- Kafka, Redis, MongoDB, a time-series database, ORM/query builder, generic
  repository framework, separate worker/service, or new deployment unit.
- Persisting every MQTT delivery attempt. History stores one logical
  observation; later delivery attempts retain diagnostic evidence only in the
  existing bounded logs.
- Backfill of pre-MVP-006 telemetry. MVP-005 persisted no telemetry, so the
  additive migration starts empty.

## Dependencies

- MVP-001 supplies the device/organization authority, PostgreSQL pool,
  migration runner, and real-store test pattern.
- MVP-002 supplies the development-only fixed-principal GraphQL boundary,
  schema generation, cursor/error conventions, complexity controls, and
  tenant-isolation tests.
- MVP-005 supplies the accepted application DTO, synchronous consumer port,
  one-worker MQTT runtime, real-broker harness, and the explicit non-durable
  pre-commit failure boundary.
- MVP-007 consumes the GraphQL state/history contract. MVP-008 consumes only a
  successfully stored logical observation and must not make bounded telemetry
  retention unbounded; alert history must preserve the triggering context it
  needs rather than depend on indefinite telemetry-row retention.

No dependency upgrade, new package, environment key, Compose service, or CI
context is expected. Discovery that correctness requires one is a re-plan
condition with dependency, rollback, and validation review.

## Architecture / Boundaries

### Ownership and dependency direction

```text
cmd/api composition root
├── device/registry                  organization/device authority
├── telemetry/ingestion              untrusted input validation and handoff
├── telemetry/projection             history + current-state write/read owner
│   └── PostgreSQL                   module-owned tables in the shared database
├── graph                            tenant-scoped public DTO/contract mapping
└── telemetry/mqtttransport          unchanged delivery lifecycle
```

- The registry remains authoritative for device ownership. Telemetry tables
  reference the globally unique device ID and do not duplicate an
  independently writable organization authority. Every write and read still
  receives the server-resolved organization ID and proves the device belongs
  to that organization at the database boundary.
- Telemetry history is the durable logical-observation record. Current state is
  a derived, single-writer read model owned by the telemetry projection
  boundary. GraphQL cannot mutate either table.
- `telemetry/projection` may depend on `AcceptedTelemetry` as its input contract
  and on pgx for storage. Ingestion and MQTT transport must not import the
  projection package or storage types; `cmd/api` supplies the consumer.
- GraphQL depends on narrow read interfaces and consumer-safe records, not SQL
  rows. The API does not expose `OrganizationID`, `IngestionID`,
  `MQTTDuplicate`, internal row identifiers, or storage errors.
- The runtime remains one Go process, one PostgreSQL pool, one MQTT client, and
  one ingestion worker. No service or database deployment boundary changes.

### Durable model and invariants

The migration adds three module-owned tables: an append-only identity authority
`telemetry_observation_keys`, bounded history `telemetry_observations`, and the
derived `device_current_state` projection:

- one identity row per `(device_id, message_id)`, retaining the first ingestion
  ID plus canonical observed time and temperature for replay/conflict
  classification;
- one bounded history row per newly accepted logical observation;
- a database-generated monotonic storage sequence used only for bounded
  retention and internal references, never as device/event time;
- the first accepted delivery's ingestion ID as unique correlation identity in
  the identity authority and retained history row;
- `observed_at`, first `received_at`, finite `temperature_celsius`, and first
  delivery's `mqtt_duplicate` diagnostic;
- one current-state row per device, including the selected observation identity
  and value, its observed/received times, and `last_seen_at`;
- foreign keys to the registry device, from history to the identity authority,
  and from current state to its selected observation, non-null checks, non-nil
  UUID checks, and finite-temperature protection; and
- indexes matching the exact idempotency lookup, projection/history order, and
  tenant-scoped device read paths. Indexes must be justified by these queries;
  no speculative time-series indexes are added.

The migration is additive and has no backfill. Its down migration drops current
state before history. Down is validated only against disposable local/test
data; it is not the default rollback after real telemetry exists.

### Logical identity and replay

- Producer `MessageID` is the logical observation identifier only within one
  registered device. The durable idempotency key is therefore
  `(DeviceID, MessageID)`, not a global message ID, MQTT packet ID,
  `IngestionID`, or DUP flag. The identity authority is the durable source of
  truth; bounded history cannot redefine identity when rows are pruned.
- An exact replay has the same device, message ID, observed time, and
  temperature. Differences in ingestion ID, receive time, or MQTT DUP are
  delivery diagnostics and do not make a new logical observation, regardless
  of whether its bounded history row still exists.
- Exact replay commits no new row and changes neither current state nor
  `last_seen_at`; it returns success so the ingestion boundary can truthfully
  report that the logical observation is accepted.
- Reuse of `(DeviceID, MessageID)` with a different observed time or
  temperature is a producer contract conflict. It fails safely with a stable
  diagnostic classification, logs no measurement/raw payload/database detail,
  and does not mutate history or state.

### Projection order and last-seen semantics

- Current measurement uses the total order `(ObservedAt, MessageID)`, compared
  ascending; the maximum tuple wins. PostgreSQL UUID order is the tie-breaker
  when distinct logical observations share the same observed time. This
  tie-break is deterministic, not a claim that one equal-time value is more
  physically recent.
- A strictly older tuple is stored in history but cannot replace the current
  measurement. A strictly newer tuple advances it. An equal tuple is possible
  only for the same logical key and follows replay/conflict behavior above.
- `last_seen_at` is the maximum `ReceivedAt` among newly stored logical
  observations. A new late observation can advance last-seen without replacing
  the selected current measurement. Exact replay cannot make a stale device
  appear newly seen.
- Current state exposes both the selected observation's `received_at` and the
  independent `last_seen_at`, so consumers do not infer one meaning from the
  other. Connectivity/staleness thresholds remain MVP-007 policy and are not
  stored here.

### Retention and query order

- After a new logical observation, the same transaction retains the selected
  current observation plus the 999 greatest internal storage sequences for
  that device excluding the selected row, and removes older history rows. This
  keeps the current-state source valid and makes newly received late
  observations inspectable without confusing receive order with device
  observation order. It never deletes the identity-authority row.
- The storage sequence is an internal retention mechanism, not a public
  timestamp, ordering promise, or cursor. Application acceptance means the
  bounded history policy and identity-authority write completed; it does not
  promise a time period for history retention. The identity authority has no
  MVP deletion policy and grows one compact row per accepted logical message.
- Recent-history GraphQL order is `ObservedAt DESC, MessageID DESC`. Its opaque
  telemetry cursor encodes exactly that continuation tuple, has a distinct
  type/version marker from the device cursor, and rejects empty, oversized,
  malformed, cross-type, or non-canonical values.
- The 1,000-row cap is an MVP product-learning bound. A time window,
  aggregation, specialized store, different cap, or retention of alert-linked
  context requires measured volume/query needs and a reviewed migration.

### Transaction and concurrency

- Identity classification/write, bounded-history insert, conditional projection
  update, last-seen update, and history pruning are one PostgreSQL transaction.
  Any failure rolls back the complete logical acceptance and cannot leave a
  replay authority without its corresponding history/state mutation.
- Database uniqueness is the final idempotency authority; application
  pre-checks cannot replace it. Conflicting concurrent inserts must converge on
  the same exact-replay or contract-conflict result.
- Projection update uses a conditional database compare on the total-order
  tuple. Per-device current-state row locking/upsert serializes state/pruning
  work narrowly; no process-global lock, registry-table write, or unbounded
  retry loop is introduced.
- Use the selected engine's default transaction mode only if real-store tests
  prove the invariant for concurrent order permutations. Otherwise the
  implementation must document the narrow stronger lock/isolation mechanism
  before proceeding; weakening the invariant is not an option.
- Context cancellation or database unavailability before commit is a consumer
  failure. Because MVP-005 may already have transport-acknowledged the message,
  recovery is explicit republish with the same logical message ID; automatic
  replay remains outside this slice.

### GraphQL contract

Add these additive query outcomes to the existing development-only schema:

- `deviceCurrentState(deviceId: ID!): DeviceCurrentState` returns `null` when
  the tenant-scoped device has no state or is not visible to the principal;
- `deviceTelemetry(deviceId: ID!, first: Int! = 50, after: String):
  TelemetryConnection!` returns a bounded ordered connection; missing,
  cross-tenant, and no-history devices expose no telemetry rows; and
- current-state and telemetry point types expose only `messageId`,
  `observedAt`, `receivedAt`, `temperatureCelsius`, plus `lastSeenAt` on current
  state. Connection edges use the telemetry-specific opaque cursor and existing
  `PageInfo` shape.

Canonical UUID validation, `first` bounds, cursor errors, safe public errors,
fixed principal authority, POST/media/body/parser limits, and generated-artifact
drift rules remain unchanged. Complexity weights must allow the canonical
single-device MVP-007 query at the maximum page size while rejecting repeated
aliases/fragments that multiply database or row work. No tenant argument,
filter/sort builder, total count, arbitrary time range, diagnostic field, or
mutation is added.

## Security and Failure Model

| Failure or abuse path | Required behavior and control | Evidence | Remaining risk |
| --- | --- | --- | --- |
| Cross-tenant read/write attempt | Server principal/registry IDs only; every SQL path proves organization/device association; absent and foreign devices disclose no telemetry | Two-tenant PostgreSQL and GraphQL negative tests | Production auth/RBAC/RLS remain deferred |
| QoS replay or duplicate publish | Append-only device-scoped identity authority; exact replay succeeds without state/last-seen change even after history pruning | Sequential, concurrent, prune-then-replay, restart, and real-MQTT replay tests | Transport attempts are not retained as an audit log; identity authority grows with accepted logical messages |
| Message ID reused with different content | Detect conflict, roll back, emit safe stable diagnostic, preserve first logical observation | Real-store conflict tests and log-redaction assertion | Publisher repair is manual in the local fixture |
| Late or equal-time input | Persist subject to cap; total-order conditional projection; independent last-seen update | Order-permutation and restart tests | Device clocks can still be inaccurate within MVP-005's accepted skew |
| Database unavailable or transaction canceled | No partial history/state/retention mutation and no false acceptance; readiness already reports dependency unavailable | Fault/cancellation and runtime recovery evidence | Transport-acknowledged message can be lost until manually republished |
| Concurrent writes race | Database uniqueness and narrow per-device serialization preserve one history row, total-order state, and cap | Race plus real-PostgreSQL concurrent tests | Current runtime has one worker; higher parallelism needs new measurements/review |
| Unbounded history or query amplification | Hard 1,000 history rows/device, append-only compact identity authority, page max 100, keyset cursor, selected fields, complexity limit | 1,001+ retention/prune-identity tests, page/cursor/complexity contract tests | Overall tenant device count and long-term identity-authority capacity are not scale-tested |
| Sensitive diagnostic exposure | GraphQL allowlist; errors/logs omit temperature, payload, SQL, URLs, and credentials | Response/log assertions | Local operators can inspect their own database by design |

## Decisions and Alternatives

### Selected

- Existing PostgreSQL/pgx/Goose/gqlgen stack and modular process; no new
  dependency or runtime.
- One transactional projection consumer behind the existing synchronous port.
- Device-scoped producer message ID for durable idempotency.
- An append-only compact identity authority separate from bounded observation
  history, so pruning cannot weaken replay/conflict semantics.
- `(ObservedAt, MessageID)` as the shared total order for projection, query
  pagination, and deterministic restart behavior; retention separately uses an
  internal storage sequence while always preserving the selected current row.
- Separate selected-observation receive time and logical `lastSeenAt`.
- A hard count cap of 1,000 observations/device, with GraphQL pages defaulting
  to 50 and capped at 100.
- Top-level single-device GraphQL read fields rather than nested list fan-out or
  a second REST API.

### Rejected for this slice

- **Do nothing / keep diagnostic logs:** cannot support restart-safe state,
  operator reads, or later rule evaluation.
- **Global `MessageID` uniqueness:** lets another device's UUID collision or
  misuse suppress a valid observation and makes producer identity broader than
  its authority.
- **`ReceivedAt` as current measurement order:** arrival order would let old
  device observations replace newer device state.
- **`ObservedAt` without a tie-breaker:** equal timestamps make the result
  arrival/concurrency dependent and non-deterministic after rebuild.
- **Update last-seen on replay:** broker redelivery could make a silent device
  appear active.
- **Unbounded history with query-only limits:** violates the MVP's bounded
  telemetry requirement and hides retention debt. The selected identity
  authority is a separate compact ledger with an explicit one-row-per-message
  capacity cost and no public history/query role.
- **Time-based retention or background cleanup:** no product period, scheduler,
  or operational owner exists yet; a transactional count cap is smaller and
  testable.
- **Redis idempotency/cache, Kafka, or time-series storage:** PostgreSQL already
  owns this bounded transactional MVP data; adoption triggers are unmet.
- **Nested telemetry on every `Device`:** creates an avoidable N+1/amplification
  path for the existing device list. Dedicated single-device fields match the
  MVP-007 consumer and are easier to bound.

### Re-plan triggers

- a new dependency, process, database, configuration key, or CI context;
- a need for production identity/exposure, broker durability, automatic
  replay, more than one writer/consumer, or more than one metric;
- inability to preserve transaction, concurrency, idempotency, tenant, or
  retention invariants with the selected PostgreSQL boundary;
- a GraphQL shape incompatible with the concrete MVP-007 device-detail journey;
- alert linkage that requires telemetry rows beyond the reviewed retention
  policy;
- measured volume/query latency that challenges the 1,000-row count, selected
  indexes, one worker, or PostgreSQL choice; or
- a migration/rollback requirement for non-disposable existing telemetry data.

## Implementation Direction

1. Change status to `In progress` when implementation starts (the first
   implementation change may be staged before its commit). Add migration `005`
   and integration tests first, including up/down/up on a disposable database,
   constraints, exact replay, conflicting reuse, and tenant/device ownership.
2. Add consumer-safe telemetry records, typed persistence errors, and the
   transactional repository/consumer. Prove identity authority, order,
   last-seen, rollback, history pruning, replay/conflict after pruning, and
   concurrent permutations at the real PostgreSQL boundary.
3. Replace the no-op `cmd/api` consumer with the persistence consumer while
   preserving one pool and existing lifecycle/readiness behavior. Add safe
   failure classification without logging raw values.
4. Add telemetry-specific cursor code and GraphQL read interfaces/types/
   resolvers. Regenerate committed gqlgen artifacts; calibrate complexity for
   canonical and amplified operations; keep storage types out of the schema.
5. Extend GraphQL unit/HTTP/PostgreSQL tests for null/empty, default/min/max
   page, page continuation, invalid/cross-type cursor, canonical IDs, safe
   errors, selected fields, tenant isolation, and complexity.
6. Extend the existing MQTT integration harness to publish normal, exact
   replay, late, and newer observations; query GraphQL; restart the API; and
   prove one logical history row, deterministic current state, last-seen,
   durability, and log redaction. Do not create a competing harness.
7. Validate the required telemetry schema before startup/listening. Update only
   the documentation listed below from the behavior and evidence
   that actually landed. Reconcile requirement/plan to the final diff, perform
   author self-review and fixes, and rerun final validation.
8. Open the PR only after local final validation. Inspect required checks on the
   exact head, complete independent review when required, and move this plan to
   `completed/` only after implementation, evidence, review, and acceptance
   support that status.

## Expected File Boundary

Expected new/changed surfaces include:

- `apps/api/internal/platform/migrations/005_*.sql`;
- a new `apps/api/internal/telemetry/projection/` package and tests;
- `apps/api/cmd/api/` composition and lifecycle tests;
- `apps/api/graph/schema/device.graphqls`, telemetry cursor/resolver/model
  mappings, generated gqlgen artifacts, server complexity, and contract tests;
- PostgreSQL/HTTP integration tests and `scripts/mqtt-integration.mjs`;
- API/local-development, architecture, technology, roadmap, and affected
  downstream feature-plan documentation named below.

No `go.mod`, `go.sum`, lockfile, environment example, Compose, Mosquitto,
AsyncAPI, OpenAPI, frontend, browser, registry schema ownership, or new
CI-context change is expected. The existing `package.json` modernization
command and repository-quality workflow may receive additional pinned analyzer
flags as a quality-gate correction, but no new job or runtime context is
introduced. A diff outside this boundary needs impact review and re-planning
when material.

## Validation Plan

### Static and contract evidence

- Migration review verifies table/constraint/index/down order and confirms no
  destructive backfill or registry ownership change.
- GraphQL SDL, generated Go, resolver DTOs, cursor representation, docs, and
  MVP-007 assumptions agree; generated drift remains zero.
- Existing format, modernization, Staticcheck, vet, lint, typecheck, build,
  module verification, dependency audit, OpenAPI, and AsyncAPI checks remain
  clean. No dependency diff is expected.

### Unit/component evidence

- Pure tests cover total-order comparison, exact replay versus content
  conflict, late/equal/new state decisions, last-seen semantics, finite values,
  safe error classification, and context cancellation.
- Cursor/GraphQL tests cover default 50, explicit 1 and 100, zero/over-limit,
  first/final page, empty history, malformed/oversized/wrong-type cursor,
  canonical IDs, output timestamps/numbers, null current state, safe errors,
  repeated aliases/fragments, and one canonical maximum-page operation.
- API composition tests prove GraphQL-only, ingestion-only, combined, and
  disabled modes reuse the existing pool correctly and preserve startup,
  readiness, drain, and close ordering. Run applicable packages under `-race`.

### Real-boundary integration evidence

- PostgreSQL tests prove constraints, first insert, exact sequential and
  concurrent replay, content conflict after history pruning, transaction
  cancellation rollback, new/late/equal order permutations, concurrent
  conflicting and distinct-message writes, last-seen monotonicity,
  current-source preservation when storage order is old, 1,000-row history
  retention after 1,001+ unique observations, pagination without gaps/
  duplicates, restart rebuild truth, and two-tenant isolation.
- Migration evidence runs up, second up, down, and up on disposable data. A
  populated-database down is destructive and is not presented as a safe
  operational rollback.
- Startup evidence runs dependency composition and the API process against a
  disposable pre-005 database, proves `database_schema_unavailable`, confirms
  no readiness/listener port opens, and only then migrates up. The normal
  PostgreSQL integration suite reruns the same packages with
  `-race -tags integration`.
- The existing real MQTT/PostgreSQL/API/simulator harness proves publish to
  committed query result, exact replay idempotency, late/new projection,
  process restart persistence, persistence-failure non-acceptance, and absence
  of raw telemetry/database details in logs.

### Regression and final gates

- Run targeted checks during implementation, then
  `corepack pnpm run check:fast` after code/doc integration.
- Run `corepack pnpm run check` as final local candidate validation. It composes
  race, build, audits, MQTT, database, API contracts, and browser regressions.
- Required GitHub checks on the exact PR head remain authoritative. Passing
  compile/startup, migration, or MQTT PUBACK alone does not prove idempotency,
  projection, tenant isolation, pagination, retention, or restart behavior.
- Required real-store, real-MQTT, race, and contract evidence is incomplete if
  skipped. Report unavailable, failed, skipped, and not-run gates separately.

## Documentation and Closeout

After implementation and evidence are reconciled, update:

- `docs/api/README.md` with GraphQL field/type/bound/cursor semantics,
  idempotent acceptance, current versus last-seen time, and local query
  examples;
- `docs/architecture/system-architecture.md` with implemented history/current
  ownership, transaction and ordering rules, retained non-durable pre-commit
  boundary, and data-flow status;
- `docs/architecture/technology-decisions.md` with PostgreSQL's bounded MVP
  telemetry use, the 1,000-row count policy, and the measured triggers for a
  different retention/store decision while leaving time-based retention open;
- `apps/api/README.md` and `docs/project-setup/local-development.md` with
  migrate/start/publish/query/restart/troubleshoot steps and rollback warning;
- `docs/roadmap/feature-plans/planned/MVP-007-telemetry-device-state-console.md`
  with the delivered GraphQL contract and the fact that UI staleness derives
  from `lastSeenAt`, not the current observation's time;
- `docs/roadmap/feature-plans/planned/MVP-008-threshold-rule-alert-backend.md`
  with the delivered stored-observation handoff and retention-safe triggering
  context requirement; and
- this plan, feature-plan index, and roadmap lifecycle/status only when the
  implementation and merge state justify those changes.

No environment key is expected, so environment strategy/examples change only
if implementation discovers a real configuration need and the plan is reviewed
again. Product scope, AsyncAPI, OpenAPI, UI design system, and frontend docs
change only if actual behavior crosses their ownership boundary.

## Rollback / Containment

- Immediate local containment: set
  `PULSEGRID_MQTT_INGESTION_MODE=disabled` and restart. This stops new telemetry
  writes without weakening database/tenant/API invariants; existing reads may
  remain available in development identity mode.
- Code rollback: revert the application/GraphQL changes while leaving the
  additive tables inert. Prefer a forward fix once telemetry exists.
- Schema down is safe only for explicitly disposable local/test data because it
  deletes telemetry history and current state. Do not run it against valued
  data without explicit destructive approval and recovery evidence.
- If the projection or retention invariant fails, disable ingestion and retain
  the tables for diagnosis; do not continue accepting writes, delete evidence,
  relax uniqueness/tenant constraints, or substitute arrival order.

## Engineering Improvement Review

- **Current scope:** device-scoped idempotency, conflicting-key detection,
  total ordering, independent last-seen semantics, atomic projection/retention,
  hard data/query bounds, type-specific cursor, complexity calibration,
  real-store concurrency/tenant evidence, and MQTT-to-GraphQL restart evidence.
  These are tightly coupled correctness, data-integrity, security,
  maintainability, and operability requirements for the first durable
  telemetry slice.
- **Future enhancements:** production auth/device identity and durable replay;
  time-based retention/aggregation/specialized storage from measured needs;
  metrics and parallel consumers from observed pressure; UI refresh/charting in
  MVP-007; rule/alert processing in MVP-008.
- **Scope effect:** the work remains one backend-state PR with one additive
  migration and no new dependency or runtime. Any future item above requires
  its owning plan and review.

## Final Plan Review (2026-09-22)

### Findings resolved by this revision

- **Blocker:** the original plan left retention, timestamp skew, and
  idempotency ownership open. MVP-005 already owns future skew and supplies the
  producer message ID; this plan now selects device-scoped idempotency and an
  exact 1,000-observation count bound.
- **Blocker:** current-state behavior for late or equal timestamps was
  ambiguous. One total order now governs projection, pagination, restart, and
  concurrent outcomes, while retention separately preserves the current source
  plus the newest stored inputs.
- **Blocker:** `last-seen` could have been incorrectly coupled to the selected
  measurement or advanced by a broker replay. It is now defined independently
  over new logical observations.
- **Blocker:** transaction, conflict, concurrency, and partial-failure behavior
  were unspecified. The plan now requires one atomic transaction, database
  uniqueness, narrow per-device serialization, rollback, and real-store proof.
- **Major:** “bounded GraphQL” did not define field shape, tenant behavior,
  cursor, page bounds, selected fields, or amplification controls. The additive
  single-device contract and evidence are now explicit.
- **Major:** the prior validation could pass without concurrency, retention,
  process restart, real MQTT-to-GraphQL, or migration recovery. The revised
  matrix assigns each to an existing real boundary and final gate.
- **Major:** rollback and documentation closeout did not distinguish reversible
  code containment from destructive schema down. That distinction and the
  affected downstream plans are now named.
- **Blocker:** bounded-history pruning could delete the only replay/conflict
  authority. The implementation now separates an append-only canonical
  identity authority from bounded history and proves prune-then-replay and
  prune-then-conflict behavior at PostgreSQL.
- **High:** API startup could report ready against a pre-005 schema. The
  composition now validates the required telemetry tables/columns before
  resolving the development organization or opening the listener/ingestion
  path, with a disposable pre-005 startup test.
- **Evidence:** real-store cancellation, old-current retention, concurrent
  conflict/distinct ordering, multi-page pagination, telemetry GraphQL default/
  boundary/complexity cases, and integration-tagged race execution are now
  covered by the validation gates.

### Remaining non-blocking limitations

- Transport-acknowledged messages can still be lost before database commit;
  local manual republish with the same logical ID is the recovery path.
- The append-only identity authority grows one compact row per accepted logical
  message and has no MVP deletion/compaction policy; changing that guarantee
  requires a new reviewed contract and capacity decision.
- The 1,000-row bound and one-worker throughput are unmeasured MVP choices with
  explicit revisit triggers, not capacity or production claims.
- Equal observed timestamps use an arbitrary but deterministic UUID tie-break;
  the model has no device sequence number.
- PostgreSQL remains the bounded MVP store; a retention period and permanent
  high-volume design intentionally remain open.
- Development GraphQL and MQTT remain loopback/local-test surfaces without
  production device or operator authentication.

### Verdict

Plan verdict: **Approved for implementation within the reviewed boundary.**

Reasoning budget: Full for a Material Change (Tier 2).

Confidence: High for current repository state, dependency reuse, ownership,
contract, and available evidence surfaces; medium for the initial 1,000-row
product-learning bound, which has no measured production workload and has a
named revisit trigger.

Implementation must stop for re-planning at any trigger above. This verdict
does not authorize production deployment, destructive rollback of valued data,
new infrastructure, or implementation beyond this plan.

## Implementation Checkpoint and Plan-to-Actual Reconciliation (2026-09-22)

The implementation is recorded in commit `42e114e` on the feature branch and
is pushed to the open Draft PR #15. The changed surfaces stay inside the
reviewed boundary: migration `005`, the telemetry projection package and
real-store tests, API composition, additive GraphQL schema/resolvers/cursors
and generated artifacts, GraphQL tests, the existing MQTT integration harness,
and the named API/architecture/local-development/downstream plan documents.
There is no dependency, environment key, Compose service, AsyncAPI/OpenAPI,
frontend, or registry-schema change. The existing modernization quality gate
was extended only to enable the `forvar` and `stringsseq` analyzers that caught
the two findings below; no new CI job or context was added.

The actual behavior matches the selected decisions: append-only device-scoped
logical identity authority, exact replay no-op after history pruning,
conflicting reuse rejection, deterministic `(observedAt,messageId)`
current-state ordering, independent last-seen updates, atomic identity/history/
current-state/retention work, a 1,000-row history bound, tenant-scoped
GraphQL reads, and a type-specific bounded cursor. Generated GraphQL artifacts
are synchronized and the staged diff has no whitespace errors.

Final local validation passed on 2026-09-22 with
`corepack pnpm run check`, including format/generated checks, modernize,
Staticcheck/vet, Go and web lint/typecheck/tests, race tests, builds, OpenAPI
and AsyncAPI checks, dependency audit, govulncheck, the disposable real MQTT
projection/recovery harness, the disposable PostgreSQL/API integration suite,
and all 17 browser tests. The integration suites exercised migration
up/down/up and cleanly removed their disposable resources.

This checkpoint is not a closeout claim. Exact-head CI result, independent
review, merge, production migration, and production identity/durability
evidence remain open. Startup validation now rejects a pre-005 schema before
the listener/ingestion path, and the live MQTT harness proves committed
normal/duplicate/late/restart projection behavior; persistence-failure
non-acceptance remains covered at the ingestion consumer-failure boundary and
is not presented as a production broker replay guarantee. Any remaining
evidence gap or material behavior change must be recorded before moving this
plan to `completed/`.

## Done Criteria

MVP-006 is complete only when the exact reviewed implementation stores each new
device-scoped logical observation in an append-only identity authority at most
once; rejects conflicting reuse after history pruning; atomically maintains
the deterministic current measurement, independent last-seen, and 1,000-row
history bound; validates the required schema before startup; exposes only
tenant-scoped bounded GraphQL
reads; preserves state across restart and late/concurrent input; passes the
required unit, race, real-PostgreSQL, real-MQTT, GraphQL, migration, regression,
and exact-head CI evidence; reconciles plan to actual; completes author and any
required independent review; updates the named documentation; and reports all
skipped/unavailable checks and remaining risk without overstating durability,
scale, security, or production readiness.
