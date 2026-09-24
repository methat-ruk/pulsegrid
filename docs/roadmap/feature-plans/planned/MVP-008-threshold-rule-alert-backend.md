# MVP-008 — Threshold Rule and Alert Backend

Status: Ready for review — implementation and author review complete on
2026-09-24; candidate is locally validated on its feature branch

Branch: `feat/mvp-008-threshold-rule-alert-backend`

Intended PR: One backend-condition PR

Milestone: M3 — Rules and alerts

Impact: Material Change (Tier 2). The eventual PR adds durable data, an
additive GraphQL contract, and rule evaluation inside the accepted-telemetry
transaction. It does not change production identity or deployment topology.

## Goal and acceptance boundary

For a registered device with an enabled temperature rule, each newly stored
logical telemetry observation is evaluated against the rule configuration
visible to the evaluation transaction. A match creates one immutable alert
occurrence carrying the rule and measurement context. The observation, current
state, and all matching alerts commit together before `telemetry_accepted` is
logged. A non-match commits telemetry without an alert. Exact replay is a no-op
for both telemetry and alerts; a conflicting reuse of a message ID still fails.

The development GraphQL API lets the fixed tenant configure a rule and read
tenant-scoped alert history after simulator publication. Another tenant's
device, rules, and alerts remain undiscoverable. A failed evaluation causes a
processing failure and no partial commit for that input; earlier committed
telemetry remains inspectable.

## Verified repository baseline at implementation start (2026-09-24)

- The clean feature branch and `main` both point to `5772b99`; MVP-006 and
  MVP-007 are merged (`8c7a0f5` and `9ce9e7c`). No rules package, alert table,
  GraphQL rule/alert field, or migration after `005` exists yet.
- MQTT ingestion validates the v1 payload, resolves device/tenant authority,
  and calls a synchronous `AcceptedTelemetryConsumer`. `projection.Consume`
  owns the PostgreSQL transaction for identity classification, observation,
  current state, and pruning. Exact replay returns before inserting a new
  observation. The success log follows the consumer return.
- Telemetry exposes one finite `temperatureCelsius` (`float64` / PostgreSQL
  `DOUBLE PRECISION`). Its durable `(device_id, message_id)` identity key
  survives pruning; recent history is capped at 1,000 observations per device.
- The development GraphQL endpoint has a server-selected tenant, bounded
  requests and operation complexity, gqlgen generated artifacts, and
  tenant-scoped repository reads. Existing API/DB and MQTT harnesses exercise
  real PostgreSQL and Mosquitto. Go, pgx, Goose, gqlgen, PostgreSQL, and
  Mosquitto are already selected; no new dependency, service, environment key,
  or worker is justified. This inspection is not runtime validation.

## Selected rule and alert semantics

1. A rule belongs to exactly one registered device. The device registry stays
   the tenant authority. Every rule/alert repository operation requires the
   server-resolved organization ID and checks device ownership in SQL; clients
   cannot supply or override a tenant ID.
2. The only metric is `temperatureCelsius` in Celsius. Compare finite
   `float64` values directly with `GT`, `GTE`, `LT`, or `LTE`. There is no
   rounding, tolerance, conversion, equality operator, window, or aggregate.
   Reject non-finite values at API and persistence boundaries.
3. Permit at most 20 stored rules per device, including disabled rules.
   Creation enforces the cap under a device-scoped database lock so concurrent
   creates cannot exceed it. Rules can be created and edited, including
   `enabled`; they cannot be deleted in this slice. Full-field updates require
   an integer revision precondition to reject stale edits. The cap bounds work
   in the single ingestion worker; reaching it requires editing an existing
   rule or a later reviewed capacity change.
4. Evaluate every new accepted observation, including late observations,
   against enabled rules visible when the evaluation query runs. Creation or
   editing never backfills prior telemetry. Exact replay never reevaluates,
   even if a rule changed since first acceptance.
5. Each match is an immutable occurrence. Consecutive matching observations
   create separate alerts; no active/resolved status, severity,
   acknowledgement, cooldown, notification, or suppression policy is implied.
   Disabling a rule leaves earlier alerts readable.
6. Snapshot rule ID, device ID, message ID, observed/received times,
   triggering temperature, metric, comparator, threshold, and alert creation
   time. Alert detail survives a rule edit and pruning of the telemetry history
   row. `messageId` remains a trace reference and can link to retained
   telemetry, but alert detail does not require the history row. Normalize
   observed/received times to the same PostgreSQL microsecond precision used
   by the telemetry projection.
7. Enforce one alert per `(rule_id, device_id, message_id)` in the database.
   Reference the durable telemetry identity key, not the prunable observation
   row. Constraints/FKs prevent cross-device links, non-finite snapshots, and
   unintended cascade deletion. Alert occurrences have no MVP retention
   policy; growth is an explicit capacity trade-off.

## Architecture and failure decision

The selected path extends the existing synchronous transaction:

```text
MQTT -> ingestion validation and registry resolution
     -> projection.Consume transaction
        -> classify new vs exact replay/conflict
        -> write observation/current state and enforce history bound
        -> rules evaluator reads this device's enabled rules
        -> alerts boundary inserts matching immutable snapshots
        -> commit all effects
     -> telemetry_accepted only after success (or exact replay)
```

`projection` keeps transaction ownership and calls a narrow injected evaluator
only for a new logical observation, before commit. The evaluator/alerts module
owns rule queries and alert writes; `cmd/api` wires the boundary. An internal
pgx transaction handle may cross these two persistence modules, but transport,
registry, and GraphQL must not write their tables or depend on storage rows.
GraphQL uses narrow tenant-scoped interfaces and explicit DTOs. Validate the
new migration schema before the development listener opens or MQTT subscribes,
following MVP-006 startup behavior. A disabled-ingestion, non-development API
remains unaffected.

Rule evaluation, alert insertion, telemetry mutation, and pruning succeed or
roll back as one unit. A rule read/insert error returns
`telemetry_consumer_failed` with a safe diagnostic reason, never
`telemetry_accepted`; logs exclude raw payload, threshold, and temperature.
Previously committed telemetry remains readable. The failed input is not
accepted and may need explicit republish after repair. MQTT QoS acknowledgement
and the in-memory queue do not guarantee replay after failure or crash. If
commit outcome is uncertain, republish the same message ID; durable identity
and alert uniqueness make the retry safe. Readiness keeps its existing
database/MQTT meaning and does not claim future rule evaluation will succeed.

This atomic choice rejects a post-commit direct call, which could leave
accepted telemetry permanently without an alert. A durable evaluation
queue/outbox would preserve accepted telemetry through a later rule failure,
but adds a worker, retry/backlog policy, and status authority beyond this MVP.
Revisit it when automatic recovery or independent consumers become required.
Revisit the 20-rule cap and unbounded identity/alert growth when measured rule
count, write latency, or retention needs justify a new contract.

## GraphQL contract to implement

- `ThresholdComparator`: `GT`, `GTE`, `LT`, `LTE`. `ThresholdRule`: `id`,
  `deviceId`, fixed `metric` enum `TEMPERATURE_CELSIUS`, `comparator`,
  `thresholdCelsius`, `enabled`, `revision`, `createdAt`, `updatedAt`.
- `createThresholdRule(input: CreateThresholdRuleInput!): ThresholdRule!`
  accepts `deviceId`, `comparator`, `thresholdCelsius`, and `enabled` (default
  true). `updateThresholdRule(input: UpdateThresholdRuleInput!):
  ThresholdRule!` accepts rule ID, expected revision, and full replacement of
  comparator, threshold, and enabled state; metric/device never change.
  `thresholdRules(deviceId: ID!): [ThresholdRule!]!` is capped by the stored
  rule limit and ordered deterministically.
- `AlertOccurrence`: `id`, `deviceId`, `ruleId`, `messageId`, `observedAt`,
  `receivedAt`, `temperatureCelsius`, `metric`, `comparator`,
  `thresholdCelsius`, `createdAt`. `alert(id: ID!): AlertOccurrence` and
  `alerts(first: Int! = 50, after: String, deviceId: ID): AlertConnection!`
  expose tenant-scoped history. Order by `(createdAt DESC, id DESC)` with an
  opaque alert-specific keyset cursor; bound `first` to 1–100. Optional
  `deviceId` filters within the fixed tenant. `AlertConnection` uses bounded
  edges and the existing `PageInfo` shape; its cursor is versioned, size
  limited, and bound to the chosen device filter so a cursor from another
  list scope cannot silently continue a different list.
- Invalid IDs/non-finite values return `BAD_USER_INPUT`; stale revisions and
  the rule cap return `CONFLICT`. Unknown and foreign IDs share
  non-disclosing null/empty reads and the same safe mutation failure. SQL
  errors and internal organization IDs remain private. Update GraphQL operation
  complexity for new lists/mutations; preserve POST-only development exposure
  and existing parser/body/response limits. Commit generated gqlgen files.

## Implementation sequence and affected surfaces

1. Add forward/backward Goose migration `006` for rule/alert tables, uniqueness,
   finite checks, same-device FKs, ownership and pagination indexes. Up starts
   empty; no backfill. Down drops rule/alert data and is safe only for
   disposable local/test databases. A real environment needs a separately
   reviewed data recovery plan.
2. Implement pure comparator evaluation and the rules/alerts repository.
   Serialize concurrent per-device creates for the cap; enforce revision
   checks in SQL. Keep persistence private to this module.
3. Extend the projection consumer with the narrow in-transaction evaluator
   call. Preserve replay/conflict, current-state, last-seen, 1,000-row
   retention, and logging behavior. Wire evaluator and schema preflight in
   `cmd/api` without another pool, queue, broker consumer, or config key.
4. Add additive gqlgen SDL/resolvers/complexity weights, safe error mapping,
   and alert cursor. Update API contract documentation. Do not add REST CRUD or
   change telemetry-v1 MQTT/AsyncAPI.
5. Reconcile implementation against this plan, self-review the actual diff
   and impacted paths, run final validation, and close out architecture, API,
   technology-decision, local-development, roadmap, and plan text only for
   proven behavior. Independent PR review remains separate when required.

## Validation and stop conditions

- Unit table: all four comparators at below/equal/above boundaries, disabled
  rule, finite validation, and snapshot construction.
- Real PostgreSQL: migration up/idempotent up/down/up; concurrent create cap;
  stale revision; tenant-scoped create/update/read; multiple matches,
  non-match, disabled and late input; rule edit then new input; exact replay
  after edit; conflicting ID; injected rule read/alert insert failure with no
  new telemetry or alert commit; restart/history pruning with alert context
  intact; concurrent duplicate delivery with one observation/alert pair.
- GraphQL contract: fixed-principal isolation on every field, invalid/foreign
  IDs, safe errors, finite input, pagination, malformed/wrong-type cursor,
  default/max page, complexity, and generated drift. Prove the MVP-009
  consumer can read the snapshot without a retained telemetry row.
- Real MQTT: configure rule over GraphQL, publish via simulator, observe one
  alert and `telemetry_accepted`; republish same ID with no duplicate; inject
  rule failure with no acceptance log or partial write, then repair and
  explicitly republish successfully.
- Run applicable format, generated, Go unit/race/static/build,
  repository-policy, API/DB, MQTT, and unchanged web regression gates. A new
  browser journey belongs to MVP-009 unless an MVP-008 regression needs it.
  Compile/startup alone does not prove alert correctness or tenant isolation.

Stop and re-plan before adding a second process/queue, dependency, public
identity/permission model, breaking API contract, production migration, or a
changed requirement that telemetry must commit despite rule failure.
Production migration, credentials, permissions, and deployment require
separate explicit approval. No production action is in this plan.

## Downstream handoff and done criteria

MVP-009 consumes immutable alert occurrences and stored context. It may link
to device detail and a retained telemetry row, but must show the snapshot when
that row is gone. Active/resolved, severity, and acknowledgement presentation
are unsupported. The operator can see recent alert history and what caused
each item.

MVP-008 is complete only when the simulator-to-GraphQL journey, atomic failure
semantics, idempotency, tenant isolation, retained-context read, migration and
rollback behavior, and applicable gates pass on the final candidate. Record
passed, failed, unavailable, skipped, and not-run checks separately, plus
remaining risk and independent-review status.

## Plan review verdict (2026-09-24)

The original short plan was not implementation-ready: it required telemetry
to remain inspectable after rule failure without a recovery owner, left
numeric/context/repeat semantics open, omitted bounded rule work and a usable
alert-list contract, and conflicted with MVP-009's active/status copy. The
revised decisions use existing transaction, dependency, and test boundaries.
The second pass found no remaining plan blocker after aligning MVP-009's
history navigation with the available GraphQL contract and clarifying cursor
scope. Assumptions: local/test MVP only, one synchronous ingestion worker,
and explicit republish for a failed pre-commit input. Residual risks are
manual recovery, growing identity/alert tables, and possible rule edits racing
with observation acceptance; the evaluation query's visible rule snapshot is
the stated tie-break. Confidence is based on repository/code inspection, not
runtime proof. No implementation or runtime validation occurred in this review.

## Implementation and author review closeout (2026-09-24)

The working-tree candidate implements the reviewed scope: additive migration
`006`, the rules/alerts module, in-transaction evaluation from the telemetry
projection, tenant-scoped GraphQL operations and cursor, startup schema
preflight, and real-store/API/MQTT evidence. The candidate adds no external
dependency, service, environment key, broker consumer, or frontend behavior.

Validation passed on this candidate:

- `corepack pnpm run check:fast` — Go format/generation/modernize/staticcheck,
  Go vet, web lint/typecheck, OpenAPI/AsyncAPI lint, and unit tests (53 web
  tests included);
- `corepack pnpm run build` — Go API and Nuxt production builds; Nuxt emitted
  a non-blocking Rollup warning for a `@__NO_SIDE_EFFECTS__` annotation in its
  generated server bundle and completed successfully;
- `corepack pnpm run api:test:integration` — Goose up/idempotent up/down,
  pre-005 and pre-006 startup schema rejection, then all Go integration tests
  with `-race`, including concurrent rule-cap creation, tenant isolation,
  revision conflict, replay, history pruning, alert snapshots, and atomic
  rollback;
- `corepack pnpm run mqtt:test:integration` — real simulator/Mosquitto/API/
  PostgreSQL path, rule/alert GraphQL reads, duplicate message, injected alert
  insert failure with no partial rows or acceptance log, explicit same-ID
  republish after repair, and existing broker recovery/shutdown checks;
- `corepack pnpm run audit` — Node production dependency audit found no
  advisories. `govulncheck` found no vulnerable source call paths, with three
  module-level advisories in existing `golang.org/x/crypto@v0.55.0`
  (`GO-2026-6355`, `GO-2026-6354`, `GO-2026-5932`); this change adds no
  dependency;
- `corepack pnpm run check` — the full repository pre-CI handoff passed,
  including all 21 existing browser smoke tests. Chromium required running the
  local check outside the managed sandbox;
- repository-policy check, GraphQL regeneration idempotence, JavaScript syntax
  checks, and `git diff --check`.

The existing browser suite passed; no alert-console browser case was added
because no frontend code changed and MVP-009 owns visible alert behavior. No
production migration, production identity, deployment, or live external state
was exercised. The known runtime risk remains pre-commit MQTT loss requiring
explicit republish;
durable telemetry identity and alert uniqueness make a same-message retry
safe. Durable identity and alert tables remain unbounded by policy in this MVP.
The module-level `x/crypto` advisories have no reachable vulnerable call path
in this scan; revisit them when dependency maintenance or crypto/SSH usage
changes.

Plan-to-actual reconciliation and author self-review found no scope or
architecture deviation. The implementation candidate is based on the
documentation commit `8b94019` on branch
`feat/mvp-008-threshold-rule-alert-backend` and is ready for PR review; an
independent review remains pending. This plan remains under `planned/` with
status `Ready for review`. M3 remains in progress until the backend candidate
is accepted and MVP-009 is complete.
