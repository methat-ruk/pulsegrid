# MVP-010 — Command Model and GraphQL API

Status: Complete — merged as `9d78301` (PR #20) on 2026-09-30;
main CI passed on the merge commit.

Branch: `feat/mvp-010-command-model-graphql-api`

Intended PR: One command-domain and GraphQL PR

Milestone: M4 — Remote command loop

Impact: Material Change (Tier 2). This adds durable command data and a
development GraphQL mutation/read contract. It does not change production
identity, permissions, broker transport, or deployment topology.

## Goal

Persist one tenant-scoped command intent and expose its truthful asynchronous
state through GraphQL. A successful create returns `PENDING`, not evidence of
MQTT publish, device acknowledgement, or completion. The command module owns
all state changes. MVP-011 adds transport and automatic timeout processing;
MVP-012 adds the operator UI.

## Why

Remote commands need stable identity and recoverable state through duplicate
requests, delayed delivery, acknowledgement, failure, and timeout. A
synchronous GraphQL response cannot stand for a device result.

## Verified planning baseline before implementation (2026-09-29)

- The feature branch was based on `main` at `7e3c336` (MVP-009 merged as
  PR #19). MVP-001/002 device registry and GraphQL, MVP-004 simulator,
  MVP-006 projection, and MVP-008 rules/alerts were present.
- No command table, package, GraphQL field, or MQTT command consumer exists.
  Migration `006` is the latest. The simulator publishes telemetry once and
  exits. MVP-011 must add a receiving runtime before device effects are claimed.
- The API is a Go modular process using one PostgreSQL pool, pgx, Goose, and
  gqlgen. Its local/test GraphQL endpoint uses a server-selected fixed
  organization, POST JSON, bounded requests/complexity, and tenant-scoped
  repositories. Production product API/identity is disabled. Existing API/DB
  harnesses can prove real-store behavior.
- Product scope requires a persisted asynchronous command loop, but specifies
  no actuator or mutable device setting. The first command is diagnostic
  `PING`: a real simulator round trip without a claim of physical control.

## Scope

1. Add command domain, migration `007`, and tenant/device-scoped repository
   operations for create, get, bounded list, and lifecycle transitions.
2. Add additive GraphQL create, detail, and per-device history operations.
3. Support only `PING` with no arguments. ACK and terminal success/failure
   arrive through internal transition methods for MVP-011 to wire; this plan
   adds no response-ingestion runtime. No arbitrary JSON payload or catalog is
   accepted.
4. Make duplicate create and transition behavior durable and deterministic
   with an injected clock.
5. Add state-machine, real-PostgreSQL, migration, GraphQL contract, tenant
   isolation, failure, and concurrency evidence; document the handoff.

## Out of Scope

- MQTT topics/payloads, broker publish/subscribe, simulator receiver, ACK
  ingestion, retry worker, timeout scanner, and automatic timeout execution
  (MVP-011).
- Console controls, polling, or command presentation (MVP-012).
- Arbitrary command types/payloads, scheduling, bulk commands, cancellation,
  production identity/RBAC, audit policy, production migration, Kafka, Redis,
  a new service, or an outbox.

## Dependencies

MVP-001/002 provide registry authority and GraphQL. MVP-004 is a downstream
fixture dependency for MVP-011, not a runtime dependency of this PR. MVP-010
can merge independently of alert UI and MQTT ingestion.

## Architecture / Boundaries

```text
GraphQL (trusted organization principal, input/error mapping)
  -> command application boundary (validation, clock, lifecycle)
  -> command repository (authoritative PostgreSQL rows)
  -> registry device authority (scoped existence; no registry write)

MVP-011 later: MQTT adapter -> narrow command transition methods
```

The command module alone writes command rows. Resolvers neither mutate tables
nor compute status from wall time. Registry remains device/tenant authority;
every command write/read checks the server-resolved organization and device
relationship. GraphQL supplies no organization ID. No transport call occurs
in the create transaction or request path. The command UUID is the stable
correlation identity for future MQTT messages; the HTTP request ID is
diagnostic metadata, never device or tenant authority.

### Command and payload decision

- `CommandType` has exactly `PING`. `CreateCommandInput` accepts `deviceId`,
  `type: PING`, and `idempotencyKey` only. `PING` has no parameter or actuator
  effect; it is an application command, not MQTT's protocol PINGREQ.
  GraphQL rejects unsupported enums/extra fields. Do not add a nullable
  generic `payload` escape hatch.
- A future simulator success result proves it processed `PING`; it does not
  imply telemetry publication or general device health. A different command
  type/effect needs reviewed domain, GraphQL, MQTT, simulator, and UI contracts.

### Durable identity, tenant scope, and time

- One `commands` row has server-generated `id`, authoritative
  `organization_id`, `device_id`, `type`, `status`, canonical UUID
  `idempotency_key`, `created_at`, `updated_at`, and immutable `expires_at`.
  Nullable `dispatched_at`, `acknowledged_at`, `terminal_at`, and a bounded
  `failure_code` record proven milestones only. `failure_code` is either
  `DEVICE_REPORTED_FAILURE` or `DELIVERY_FAILED`, and is non-null only for
  `FAILED`. Retain the creating request ID (already bounded by the HTTP
  middleware to 64 safe characters) for internal correlation; expose neither
  it nor tenant ID in GraphQL.
- Unique `(organization_id, idempotency_key)` lasts for the row lifetime. The
  same key and `(device_id, type)` return the stored command and its current
  status even after a terminal outcome. Reuse with different intent returns
  `CONFLICT`. After an unknown commit outcome, retry the **same** key; a new
  key creates a different logical intent.
- Create checks device ownership before idempotency replay and atomically
  with insertion, and the database enforces the command/device relationship.
  Thus a foreign/absent device always gets the same non-disclosing result,
  even when its caller reuses an existing key. Migration `007` adds a unique
  `(organization_id, id)` device key and a command composite FK to that key
  with restricted deletion. Registry ownership and existing IDs stay intact.
- No command deletion, key reuse, or idempotency TTL in this MVP. Restrict
  device deletion while referenced. Row growth and retention need a later
  reviewed decision.
- Normalize injected application-clock times to UTC/PostgreSQL microseconds.
  Set one fixed end-to-end deadline of two minutes at creation; ACK and retry
  do not extend it. At `now >= expires_at`, timeout wins. This is a local
  MVP policy independent of device time.

### Lifecycle decision and MVP-011 handoff

```text
PENDING -- broker accepted --> DISPATCHED -- device ACK --> ACKNOWLEDGED
   |                              |                         |       |
   +-- device response proves ---+-------------------------+       +--> COMPLETED / FAILED
   |   receipt and implies ACK
   +-- non-retryable delivery rejection --> FAILED
   +-- deadline --> TIMED_OUT
                                  +-- deadline --> TIMED_OUT
                                                            +-- deadline --> TIMED_OUT
```

- `PENDING` is stored intent. `DISPATCHED` requires a broker-accepted publish,
  not an attempted publish. `ACKNOWLEDGED` requires device ACK or a terminal
  response that itself proves receipt. `COMPLETED` requires device success.
  `FAILED` requires explicit device failure or a classified non-retryable
  delivery failure before device acknowledgement. A transient broker error
  cannot falsely mark `DISPATCHED` or `FAILED`; MVP-011 owns bounded retry
  policy.
  `TIMED_OUT` requires a stored deadline transition, not GraphQL inference.
- The module exposes narrow transition methods for MVP-011. A terminal result
  received in `PENDING` or `DISPATCHED` proves receipt and can atomically
  record any missing dispatch/ACK milestones followed by the result. This
  handles a response racing the post-publish state write and a delayed/lost
  separate ACK; implied timestamps may coincide. ACK after terminal result
  is a no-op. Equal duplicates are no-ops; contradictory terminal results
  are rejected and diagnosed without changing state.
- Terminal states never regress. Late result after timeout stays `TIMED_OUT`.
  Unknown/foreign responses cannot change another tenant's row. Use a
  conditional update or row lock for one winner in concurrent timeout/result
  and duplicate races. Normalize `now` before comparing and writing.
- MVP-010 implements and tests transition and due-expiration operations but
  does not run a scheduler. An intent may remain stored as `PENDING` past
  `expiresAt` until MVP-011's scanner starts. Document this temporary limit;
  MVP-011 must expire old work before dispatching it.

## GraphQL contract

- `CommandStatus`: `PENDING`, `DISPATCHED`, `ACKNOWLEDGED`, `COMPLETED`,
  `FAILED`, `TIMED_OUT`. `CommandType`: `PING`. `CommandFailureCode`:
  `DEVICE_REPORTED_FAILURE`, `DELIVERY_FAILED`.
- `Command`: `id`, `deviceId`, `type`, `status`, `createdAt`, `updatedAt`,
  `expiresAt`, nullable `dispatchedAt`, `acknowledgedAt`, `terminalAt`, and
  nullable `failureCode`. Null milestone means unproven. Do not expose the
  idempotency key, organization ID, request ID, storage version, MQTT metadata,
  or raw errors.
- `createCommand(input: CreateCommandInput!): Command!` persists intent and
  returns the current row. `command(id: ID!): Command` and
  `deviceCommands(deviceId: ID!, first: Int! = 20, after: String):
  CommandConnection!` support recovery after lost response/browser refresh.
  `CommandConnection` has bounded `CommandEdge` rows and the existing
  `PageInfo` shape. The list orders by `(createdAt DESC, id DESC)`, bounds
  `first` to 1–100, and uses a versioned, opaque, size-limited cursor bound
  to tenant and device.
- Canonical UUIDs are required for IDs and idempotency key. Invalid ID, key,
  page size, or cursor returns `BAD_USER_INPUT`; conflicting key reuse returns
  `CONFLICT`. Missing/foreign command detail is `null`; missing/foreign device
  list is empty. Missing/foreign device create shares one safe
  `BAD_USER_INPUT` error. Store failures use a safe internal error.
- Preserve POST-only development exposure, request limits, fixed principal,
  complexity accounting, and committed gqlgen output. No REST CRUD route or
  GraphQL mutation for lifecycle transitions.

## Failure and recovery model

| Failure | Observable result and recovery |
| --- | --- |
| Database unavailable before/at create | Do not claim accepted intent; return safe internal error. Retry with the same key and inspect the stored result after recovery. |
| Concurrent duplicate create | Database uniqueness yields one row; equivalent requests return it, conflicting intent gets `CONFLICT`. |
| API stops after commit before response | Outcome is unknown to caller; retry the same key or inspect device history, never invent a key for a network retry. |
| Missing/foreign device or command | Safe create error or null/empty read; no cross-tenant disclosure or mutation. |
| Delayed/duplicate/conflicting transition | Equal event is idempotent, one race winner is authoritative, and impossible/contradictory transition is diagnosed. |
| Deadline reached before transport exists | Stored state remains `PENDING` in MVP-010; MVP-011 must expire before dispatch. |

Migration `007` is additive and starts empty. Goose `down` may drop command
data only in disposable local/test databases; real-environment recovery needs
separate review and approval. Validate the schema before opening development
GraphQL, as existing startup does for telemetry/rules. Disabled product API
remains health-only. A failed local rollout can revert code while preserving
the additive table; do not run destructive `down` on valued data.

## Implementation Direction

1. Add migration `007` with status/type checks, tenant/device relationship,
   idempotency uniqueness, and indexes for scoped detail/list/due scans.
2. Add command model/application service and repository with injected clock,
   exact replay/conflict, guarded transitions, and due expiration. Keep SQL
   behind the module and external calls outside transactions.
3. Wire repository/schema preflight into development API startup. Add narrow
   GraphQL interface, SDL/resolvers, safe errors, complexity weights,
   command cursor, and generated artifacts.
4. Update API and architecture/technology status only for proven behavior.
   Hand transition/deadline contract to MVP-011 and key-reuse rule to MVP-012.
5. Reconcile implementation with this plan, self-review changed/impacted
   paths, run final validation, and get independent PR review when required.

## Validation

- Pure state table: every allowed/rejected transition, terminal immutability,
  duplicate ACK/result, response before dispatch is persisted, result before
  ACK, contradictory result, and before/at/after deadline behavior with
  injected clock.
- Real PostgreSQL: migration up/idempotent up/down/up on disposable data;
  constraints and tenant/device FK; concurrent same-key create and conflicting
  reuse; uncertain-commit retry; concurrent result vs timeout; restart
  persistence; due scan ordering/bounds; schema preflight failure.
- GraphQL pipeline: create returns only `PENDING`; same-key replay returns
  current row; safe errors; fixed-principal isolation for create/detail/list;
  malformed/foreign IDs; default/max page and cursor scope; complexity/body
  limits; generated drift. Prove no GraphQL path can transition lifecycle or
  claim device delivery.
- Run applicable Go format, generation check, unit/race/static/build,
  API/DB integration, repository policy, and unchanged browser regression
  gates. MQTT/browser command journey belongs to MVP-011/012. Report passed,
  failed, unavailable, skipped, and not-run evidence separately.

Stop and re-plan before adding another command type/effect, MQTT worker,
dependency/service, production identity or permissions, breaking GraphQL
contract, retention/deletion policy, production migration, or another timeout
guarantee. No production/external-system mutation is authorized here.

## Documentation Updates

Document GraphQL command semantics, idempotency, timestamp/deadline behavior,
schema startup and migration recovery, and the temporary pre-MVP-011 pending
limit. Update architecture and technology status only after implementation.
Keep later MQTT wire and console behavior in their owning plans.

## Risks / Open Decisions

Selected decisions: `PING` with no payload; caller-supplied canonical UUID
idempotency key scoped to tenant; command UUID as transport correlation;
injected application clock; fixed two-minute overall deadline; MVP-011 owns
automatic timeout scanning and transport retries. There is no remaining
design choice required to start MVP-010 under these assumptions.

Residual risks: expired `PENDING` rows before MVP-011 exists, unbounded MVP
command/key rows, and no physical control demonstrated by `PING`. A physical
actuation requirement would change scope and need renewed design review.

## Engineering improvement review and alternatives

- Current scope: bounded history enables refresh recovery; durable
  idempotency prevents duplicate intent after ambiguous create; implicit ACK
  on early result avoids losing valid completion. These are coupled to the
  command/API guarantee and need real-store and contract evidence.
- Future: device-setting command, retention, automatic retries, and outbox
  each need a concrete consumer or operational requirement. Scope remains one
  command-domain PR.
- Generic JSON command catalogs add versioning/validation obligations without
  an MVP consumer. In-memory or Redis dedupe is insufficient for restart-safe
  intent. Publishing from GraphQL conflates accepted intent with uncertain
  delivery. PostgreSQL and one explicit type are the smallest sufficient fit.

## Done Criteria

A registered tenant-visible device can receive one persisted `PING` intent
through GraphQL, retrieve it after restart, and see only valid stored states.
Concurrent duplicate requests create one logical command; cross-tenant
operations disclose or mutate nothing. The API never claims delivery or
completion before MVP-011 supplies device evidence.

## Plan review verdict (2026-09-29)

The original outline left command semantics, idempotency, tenant/device
integrity, timeout ownership, status recovery, and early/duplicate responses
open. This revision locks those boundaries using the selected stack and hands
transport execution to MVP-011. Assumption: diagnostic `PING` suffices for the
first simulator command loop. Remaining risks are the temporary expired-
pending window and row growth. Confidence comes from repository/document
inspection only; no implementation or runtime validation occurred in this
review. Implementation remains a separate step.

## Implementation review and handoff (2026-09-29)

The branch implements the reviewed scope: migration `007`, the command model
and PostgreSQL repository, development GraphQL create/detail/history, tenant
and cursor scoping, command schema startup validation, and the documented
MVP-011 transition boundary. No MQTT delivery, response-ingestion runtime,
timeout scheduler, UI, new Go module dependency, environment key, or production
exposure was added. The API has no GraphQL lifecycle mutation.

Author review confirmed that tenant/device ownership is checked before
idempotency replay and enforced by a composite foreign key; conflicting key
reuse is rejected; guarded row-locked transitions preserve terminal states
and arbitrate response/timeout races; due expiration is bounded and uses
`SKIP LOCKED`; and GraphQL reads/mutations do not accept tenant authority from
the caller. These guarantees have unit, real-PostgreSQL, and GraphQL contract
coverage. Migration rollback was exercised only against disposable test data.
Follow-up review evidence now includes an ACK/Complete before/at/after deadline
table, a PostgreSQL waiter observed through `pg_blocking_pids` while the
injected clock crosses the deadline, and a dropped GraphQL response retried
through a newly created pool, repository, and handler with identity,
timestamps, state, and single-row assertions. Local and CI modernization
commands enable the same `mapsloop` and `minmax` analyzers.

Validation completed:

- `corepack pnpm run check:fast` passed, including Go checks and 56 web unit
  tests.
- `node scripts/test-api-integration.mjs` passed, including migration
  up/down/up, missing-schema startup checks, and the Go race-enabled
  PostgreSQL integration suite.
- `corepack pnpm run build` passed. Nuxt emitted a non-blocking Rollup
  annotation warning.
- `node scripts/test-browser.mjs` passed all 32 Chromium tests; its isolated
  database, broker, volume, and network were cleaned up.
- `node scripts/check-repository-policy.mjs`,
  `node scripts/check-graphql-generated.mjs`, and `git diff --check` passed.
- `go test ./...`, `go test -race ./graph`, and the integration-tag compile
  check passed.

Remaining product behavior is deliberately deferred: until MVP-011 starts the
expiry scanner, an expired command may remain `PENDING`; no device has received
or acknowledged a command in this plan. The browser suite is the existing
regression suite and contains no command-console journey; MVP-012 owns that
coverage. See the dated review follow-up below for current PR and review status.

## Review follow-up and completion record (2026-09-30)

The follow-up review identified two evidence blockers and one local/CI
modernization drift. The implementation and latest evidence close all three:

- Deadline/race coverage now checks ACK and Complete before, at, and after the
  deadline, asserts both race outcomes, and proves the transition waits on a
  PostgreSQL row lock while the injected clock reaches `expiresAt`.
- The GraphQL replay integration case discards the create result, verifies the
  commit, rebuilds the pool/repository/handler, retries the same key, and checks
  the same identity, state, timestamps, and one stored row.
- CI `api-static` now enables the same `mapsloop` and `minmax` analyzers as the
  local modernization command.

The [review response](https://github.com/methat-ruk/pulsegrid/pull/20#issuecomment-5902756922)
records the evidence and its limits. Response loss is simulated by discarding
the handler result in the test harness; the test does not simulate a TCP
disconnect. The review found evidence gaps and CI drift, with no confirmed
runtime defect in the inspected scope.

The [GitHub Actions run](https://github.com/methat-ruk/pulsegrid/actions/runs/36658828368)
passed all 14 checks on commit `83f648e495d85d81f1b8b428f294765444e8567f`.
At that review checkpoint, the plan was recorded Complete for implementation
and evidence while PR #20 remained open; the plan status itself was not merge
approval.

## Verified merge follow-up (2026-10-01)

[PR #20](https://github.com/methat-ruk/pulsegrid/pull/20) merged on 2026-09-30
as `9d7830187b4f6dd6ce6300caeea2cd41fafd7964`, now the verified local and
GitHub `main` baseline for MVP-011. The [main CI run](https://github.com/methat-ruk/pulsegrid/actions/runs/36660768645)
passed all 14 jobs on this commit. Earlier review records above describe their
dated candidate checkpoints; current merge state supersedes their open-PR
status. MQTT delivery and automatic expiry remain unimplemented.
