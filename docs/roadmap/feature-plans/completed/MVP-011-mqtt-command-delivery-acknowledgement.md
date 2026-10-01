# MVP-011 — MQTT Command Delivery and Acknowledgement

Status: Complete for the requested PR handoff — review findings F1/F2 and
evidence gap G1 are addressed and locally verified. PR #21 remains open and
unmerged; its required branch-protection checks must pass before merge.

Branch: `feat/mvp-011-mqtt-command-delivery-acknowledgement`

Intended PR: One command-transport PR, including additive retry metadata,
simulator receive mode, contracts, composition, tests, and documentation.

Milestone: M4 — Remote command loop

Impact: Material Change (Tier 2), full reasoning: durable retry, MQTT contracts,
untrusted responses, and background lifecycle behavior. This implementation
episode changes local Go runtime, additive schema, examples, contracts, and
integration evidence within the approved plan.

## Goal

Deliver committed `PING` intent to a registered local simulated device and
persist truthful dispatch, device acknowledgement, success, explicit failure,
or expiry. Prove recovery through broker/API restart and duplicate/out-of-order
responses. GraphQL creation remains persistence-only. MVP-012 owns command UI;
MVP-013 owns acceptance of the complete product loop.

## Why

MVP-010 supplies identity, guarded lifecycle, and an immutable two-minute
deadline, but no transport or expiry scheduler. This slice closes that backend
loop without treating broker acknowledgement as device completion.

## Verified baseline (2026-10-01)

| Surface | Verified evidence | Consequence |
| --- | --- | --- |
| Repository | Clean initial worktree. This branch, local `main`, cached `origin/main`, and live GitHub `main` identify `9d7830187b4f6dd6ce6300caeea2cd41fafd7964`. [PR #20](https://github.com/methat-ruk/pulsegrid/pull/20) merged 2026-09-30. | MVP-010 is accepted. Correct stale “open/unmerged” status; recheck base before implementation. |
| Command authority | [Model](../../../../apps/api/internal/commands/model.go), [repository](../../../../apps/api/internal/commands/repository.go), migration [007](../../../../apps/api/internal/platform/migrations/007_commands.sql), [GraphQL schema](../../../../apps/api/graph/schema/command.graphqls). | Reuse PING, six statuses, idempotency, row locks, clock injection, and `ExpireDue`. Dispatch selection, retry metadata, and workers do not exist. |
| API composition | [main.go](../../../../apps/api/cmd/api/main.go) opens one pgx pool, validates schemas, mounts development GraphQL and optional telemetry MQTT. | Compose commands here; no resolver publish or second pool. |
| Local transport | [Compose](../../../../compose.yaml), [Mosquitto config](../../../../docker/mqtt/mosquitto.conf), [telemetry transport](../../../../apps/api/internal/telemetry/mqtttransport/transport.go), [simulator](../../../../apps/api/cmd/device-simulator/main.go). | Anonymous loopback broker, no persistence, 16 KiB broker cap; simulator publishes once and exits. Add receiving runtime. |
| Dependencies | Go 1.27.1, Paho v1.5.1, pgx v5.11.0, Goose v3.28.0, gqlgen v0.17.95; pinned PostgreSQL 18.6/Mosquitto 2.1.2 images. Installed Node 24.20.0/pnpm 12.3.4 match workspace constraints. | Existing dependencies suffice; no upgrade, install, broker change, or lockfile edit selected. |
| Evidence | [Main CI](https://github.com/methat-ruk/pulsegrid/actions/runs/36660768645) passed all 14 jobs on `9d78301`, including DB/MQTT/browser regression, race, and dependency audits. Targeted local command/simulator/telemetry-transport/config unit tests and module verification passed. | Baseline evidence only; no proposed command-transport behavior is proven. |

Related sources: [MVP-010 handoff](../completed/MVP-010-command-model-graphql-api.md#lifecycle-decision-and-mvp-011-handoff),
[product scope](../../../product/product-scope.md), [architecture](../../../architecture/system-architecture.md),
[technology decisions](../../../architecture/technology-decisions.md), and
[environment policy](../../../project-setup/environment-configuration.md).
`Docs` and `docs` are the same directory on this checkout; tracked paths use
lowercase `docs`.

## Scope

- Versioned command/response contract and strict decoders.
- Durable bounded dispatch/retry from existing committed command rows.
- Authoritative tenant/device/command binding for device responses.
- Expiry independent of MQTT availability and dispatch throughput.
- Standalone simulator receive mode: success, failure, silent, ACK-only.
- Startup, readiness, reconnect, shutdown, crash-window and regression evidence.

## Out of Scope

Production identity/device credentials/ACLs/TLS, external brokers, actuators,
fleet/bulk commands, arbitrary payloads, scheduling/cancellation, new GraphQL
fields, UI, Kafka/Redis, generic buses, separate worker services, persistent
MQTT sessions, durable device response replay, exactly-once delivery/effects,
command retention, and multi-replica execution.

## Dependencies

- MVP-004: accepted broker/simulator fixture.
- MVP-010: merged command model, migration 007, GraphQL, lifecycle handoff.
- Existing MVP-005/006/008 runtime and telemetry/rule tests are coupled
  regression surfaces, not new product scope.
- No prerequisite gap remains on the verified base. Migration 008 and transport
  are work in this slice, not assumed existing capabilities.

## Architecture / Boundaries

```text
GraphQL -> commands.Repository.Create -> PostgreSQL committed intent
cmd/api composes one pool and two independently enabled MQTT runtimes:
  dispatch worker -> command reservation -> command MQTT -> broker -> simulator
  broker -> command MQTT -> 64-response queue -> validation + registry
      -> device-scoped command transition -> PostgreSQL
  independent expiry worker -> scoped commands expiry operation -> PostgreSQL
Existing telemetry runtime -> ingestion -> projection/rules stays separate.
GraphQL reads stored lifecycle; it never infers expiry/device success.
```

| Planned owner | Owns | Forbidden dependencies/responsibilities |
| --- | --- | --- |
| `internal/commands` | Lifecycle, deadline, retry reservation, device-scoped transition, schema/transactions | Paho, HTTP, topic parsing, simulator behavior |
| `internal/commands/delivery` | Dispatch/expiry orchestration through narrow repository/publisher ports | SQL, direct state writes, alternate state machine |
| `internal/commands/mqttcontract` | Wire types, strict codecs, topics, shared protocol limits | Registry, command parent package/repository, GraphQL, Paho |
| `internal/commands/mqtttransport` | One command client epoch, subscribe/publish evidence, queue admission, reconnect/stop | Tenant authority, business transitions, database |
| `internal/commands/responses` | Delivery validation, registry resolution, device-bound outcome handoff | Payload-derived trusted identity, arbitrary state mutation |
| `internal/mqttsimulator` | External-device-shaped local fixture, protocol/transport/config | API, GraphQL, domain repository, DB, migration |
| `cmd/api` | Composition, pool, config gates, readiness and start/stop ordering | Business transitions/retry rules |

Two Paho clients in the existing API process are intentional: keep telemetry's
client and add a dedicated command client. Resetting uncertain command publish
must not tear down working telemetry. Do not extract a generic shared transport
in this PR. The protocol-only subpackage must not import the command parent
package; simulator reuse introduces neither a storage dependency nor a cycle.

### Decision record

Date: 2026-10-01. Owner: MVP-011 implementer and PR reviewer.

| Option | Fit/trade-off | Decision |
| --- | --- | --- |
| Leave intent-only behavior | Smallest change, but no device delivery/expiry | Reject: cannot satisfy the goal. |
| Resolver publish or in-memory retry map | Less schema work; commit-before-publish and restart can lose work/reset budget | Reject: restart recovery is acceptance behavior. |
| Poll command authority with durable retry metadata and isolated adapter | Adds two columns, one index/client, bounded workers; no second durable queue | Select: keeps identity, state, and recovery together. |
| Extend/extract shared telemetry client | Saves a connection; uncertain-publish reset and shutdown couple working telemetry to commands | Defer until concrete connection/maintenance pressure justifies shared-runtime regression work. |
| Separate outbox/queue/service | Enables independent execution/replay; adds a publication owner and operating model | Defer until independent consumers or measured capacity require it. |

Commands remain the durable work authority. Reserve before publish and record
broker evidence afterward; DB and MQTT do not share an atomic commit. Duplicate
PING across that gap is safe because it has no actuator effect. A lost result
after ACK can still time out; no device exactly-once or durable response claim.
At 10x load serialized dispatch may miss deadlines: preserve truthful expiry,
not a fleet throughput promise. Revisit before effectful commands, production,
multiple dispatchers, longer deadlines, durable replay, or material backlog.

## Implementation Direction

### Wire contract — locked for this slice

- MQTT 3.1.1, QoS 1 publish/subscribe, `retain=false`, `CleanSession=true`,
  memory-only client state. Broker PUBACK is not application persistence or
  evidence that the device subscribed. DUP is diagnostic, not a dedup key.
- Command topic: `pulsegrid/v1/tenants/{tenantSlug}/devices/{deviceId}/commands`.
  Response topic: `pulsegrid/v1/tenants/{tenantSlug}/devices/{deviceId}/command-responses`.
  API filter: `pulsegrid/v1/tenants/pulsegrid-dev/devices/+/command-responses`.
  Simulator subscribes only to its configured exact device command topic.
- Command object has exactly `schemaVersion: 1`, canonical non-nil UUID
  `commandId`, `type: "PING"`, UTC RFC3339 `createdAt`/`expiresAt` from the
  stored row. Deadline is immutable (`createdAt + 2 minutes`); retries reuse
  identical content/identity. No payload or attempt ID.
- Response has exactly `schemaVersion: 1`, `commandId`, `outcome` (`ACK`,
  `COMPLETED`, `FAILED`). `failureCode: "DEVICE_REPORTED_FAILURE"` is required
  only for FAILED, forbidden otherwise. No free text/result/device timestamp:
  server commit-time clock owns state.
- Enforce 1 KiB before copy/admission, UTF-8, one JSON object, exact case-sensitive
  keys, no unknown/duplicate/missing/null fields or trailing JSON, exact version,
  enum, canonical IDs and topic. Seven levels, existing 1–63-character slug
  grammar, no identifier wildcards. Require QoS 1 and reject retained replay
  (`Retained=true`). MQTT clears RETAIN for live forwarding: this does not prove
  a publisher used retain=false.
- Simulator validates exact device/topic and rejects expired command before
  replying. Its times must parse as UTC, expiry must equal create + two minutes,
  require createdAt <= current fixture time, and execute before expiry.
  Same-host time is the fixture boundary.
- Resolve tenant/device via registry, then apply under the same row lock using
  `(organization_id, device_id, command_id)`. Existing transition methods filter
  only organization/command: add a narrow device-response entry point enforcing
  this triple and only device outcomes. Another device in the same tenant is
  also rejected. Payload never supplies organization authority.
- Anonymous loopback publishers can forge a matching response. Identity binding
  protects stored scope; it is not device authentication. Public/production
  exposure is prohibited and requires a separate security design.

### Durable dispatch and retry

Add `008_command_delivery.sql`; do not alter accepted migration 007. Add
internal `dispatch_attempts` (integer, default 0, constraint 0–4) and
`next_dispatch_at` (non-null timestamptz, database default CURRENT_TIMESTAMP
for compatibility with the previous API's inserts) to commands. Backfill 0/`created_at`;
new create initializes `next_dispatch_at = created_at`. Add partial index
`(next_dispatch_at, created_at, id)` for PENDING/DISPATCHED with attempts < 4.
Preserve status/timestamp/idempotency constraints and GraphQL shape. Validate
the new columns whenever the command repository is opened, even with command
transport disabled, because the updated Create path writes them. A health-only
process that opens no repository remains independent of this schema.

One dispatch worker polls each second, reserves at most one row per iteration,
and has at most one application publish in flight:

1. Short repository transaction selects oldest due PENDING/DISPATCHED row scoped
   to server-selected seeded organization, attempts < 4, not expired, with bounded
   row lock/SKIP LOCKED. Exclude ACK/terminal rows.
2. After locking, recheck current time/status/eligibility and more than five
   seconds remaining. Increment attempts and store next eligibility before return.
3. Commit before MQTT; never hold DB locks across network I/O. Unconfirmed commit
   causes no publish; later poll reconciles by reading the durable row.
4. Recheck readiness/cancellation/deadline immediately before Paho admission.
   Crash after reservation may consume a slot without sending: accepted trade-off
   for restart-safe budget. Readiness loss after reservation consumes that slot too.
5. Only successful QoS-1 token completion permits `MarkDispatched`. Response may
   win first; existing idempotent/no-regression rules remain authoritative.

At most four application Publish invocations per command across API restart.
After reservations 1/2/3, next eligibility is reservation time + 10/20/40 seconds;
after 4, await response/expiry. Fixed local defaults are injectable in tests;
no tuning env keys. Known offline adapter consumes no reservation. No missed-slot
catch-up burst or new command creation. Protocol retransmissions can add wire
duplicates; they are not new application invocations. Stop retries at ACK or
terminal state; response racing reservation may permit one duplicate PING.

Transient/unknown transport failures preserve PENDING/DISPATCHED. Exhaustion
never proves DELIVERY_FAILED. FAILED requires explicit device failure or proven
permanent local rejection before ACK (invalid internally built type/topic/
contract). Transport/DB errors and missing PUBACK are uncertain, not evidence
the device did nothing. Diagnose poison rows and persist failure via commands;
do not repeatedly publish invalid work.

### MQTT client epochs, budgets, and loss semantics

Command adapter disables Paho AutoReconnect and ConnectRetry. It supervises
fresh client epochs with connection/subscription retry delays 0.5/1/2/5 seconds,
capped at 5 seconds. Retire the previous client before starting another.
Generation-bound callbacks/readiness require an open connection and successful
current response SUBACK; inspect rejection/granted QoS, not just token error.
`IsConnected()` is not active-connection evidence. Initial connect/subscribe
failure returns classified startup failure and cleans started runtimes; later
failure clears readiness while supervisor reconnects. Never publish offline.

Connect/subscribe: five seconds each. Publish admission/write: two seconds,
then token wait: three seconds, clipped by command deadline/shutdown. DB calls
including pool/lock wait: two-second context; response processing total: two
seconds. Every SDK call/wait has bounded ownership/join/cleanup; an unbounded
goroutine around a blocking call is insufficient.

Timeout/disconnect makes publish outcome unknown. Clear readiness, stop outbound
admission, disconnect/retire epoch, then retry from durable row after backoff.
Late token completion in a retired epoch cannot mutate state. `WaitTimeout`
does not cancel Paho work. Packets already on the network cannot be recalled;
fresh epochs contain SDK pending work and simulator expiry contains obsolete
PING. Do not promise no late packet can arrive.

Callbacks enqueue copied bounded payload/metadata into a separate 64-item queue
without blocking. One response worker validates/commits. Safe reason codes cover
oversize, saturation, invalid/foreign/conflicting response, stale epoch and DB
failure. Transport PUBACK can precede commit; dropped/uncommitted responses are
not automatically replayed. Before stored ACK, command retry may elicit another
response; after ACK, a lost result or DB/queue failure can end in TIMED_OUT.

### Lifecycle, expiry, configuration, and simulator

- Reuse existing state machine: result in PENDING/DISPATCHED implies receipt/ACK;
  ACK after result and equal duplicates are no-ops. First committed terminal
  result wins; contradiction is rejected/diagnosed. At/after deadline active row
  becomes TIMED_OUT with post-lock clock check. Late result never reopens it.
  Judge response at processing time, not callback arrival/device timestamp.
- Add an organization-scoped bounded variant of existing `ExpireDue`, sharing
  its expiry SQL/state rules; this local runtime expires only the server-selected
  organization it dispatches. Keep existing repository expiry tests and assert
  foreign-organization rows are unaffected by the runtime scanner.
  Expiry starts immediately before dispatch when command mode is enabled; runs
  independently each second, one batch of at most 100 rows/tick, including
  during broker outage. DB failure retries next tick. Dispatch excludes expiry
  backlog; no unlimited drain loop. Visibility is eventual: a tick plus DB
  processing for healthy small backlog, longer during outage/backlog. No hard
  two-minute visibility SLA; disabled mode does not expire work.
- API key: `PULSEGRID_MQTT_COMMAND_MODE=disabled|development`, default disabled;
  enabled only in development/test with development identity. Telemetry/commands
  are independent. The shared broker URL is required and must match the exact
  environment loopback port when either API runtime is enabled. When both API
  runtimes are disabled, the API does not require the URL; the simulator still
  validates its own broker setting. API never reads simulator device/tenant
  config.
- Simulator keys: `PULSEGRID_SIMULATOR_MODE=telemetry|commands` default telemetry;
  `PULSEGRID_SIMULATOR_COMMAND_RESPONSE=success|failure|silent|ack-only` defaults
  success in command mode; reject explicitly supplied response setting in telemetry
  mode. Commands requires existing environment/broker/tenant/device keys, no
  temperature; telemetry keeps finite-temperature requirement. Reject unknown
  values/production/external targets; preserve process-over-dotenv precedence.
- Default one-shot telemetry stays compatible. Commands mode runs until SIGINT/
  SIGTERM, reports readiness after exact device SUBACK, and sends success ACK then
  COMPLETED, failure ACK then FAILED, silence neither, ACK-only no result.
  One bounded 64-item queue/worker; duplicate PING produces deterministic replies,
  no new operation or durable device-dedup claim. Restart can repeat harmless
  PING. Reconnect/resubscribe is supervised with fresh epochs; uncertain response
  publish retires its epoch, no unlimited response retry/cache. Overload is
  diagnosed/dropped and may result in command timeout.
- API readiness composes DB and each enabled runtime's connection/subscription/
  worker health; liveness remains process-only. Startup failure/HTTP exit stops
  all started workers/clients before pool close. Shutdown stops new dispatch,
  expiry and response admission, drains/cancels responses and disconnects both
  runtimes within the shared configured deadline, joins workers, then closes
  pool. Interrupted publish has no invented outcome; rows remain recoverable.
- Diagnostics correlate validated tenant/device/command, delivery ID, attempt,
  state and epoch; state acceptance only after commit. Invalid input logs safe
  bounded reason/size, no raw topic/payload/provider or DB error/credentials.

Protocol evidence: [OASIS MQTT 3.1.1](https://docs.oasis-open.org/mqtt/mqtt/v3.1.1/os/mqtt-v3.1.1-os.html)
and pinned Paho [client](https://github.com/eclipse-paho/paho.mqtt.golang/blob/v1.5.1/client.go),
[token](https://github.com/eclipse-paho/paho.mqtt.golang/blob/v1.5.1/token.go),
[options](https://github.com/eclipse-paho/paho.mqtt.golang/blob/v1.5.1/options.go).
Pinned local source was inspected for timeout, connection and publish buffering;
runtime tests still must prove adapter containment.

## Validation

Required implementation evidence, tied to the final submitted candidate:

| Guarantee/scenario | Boundary and assertion |
| --- | --- |
| Contract valid/boundary/invalid | Unit codecs + AsyncAPI parity: exact keys/enums/topic, 1 KiB edge, invalid UTF-8/JSON, duplicate/unknown/null/missing keys, trailing JSON, UUID/version/time, QoS, retained replay. Real broker carries the same contract. |
| Success/failure/silence | GraphQL -> PostgreSQL -> Mosquitto -> simulator -> response -> GraphQL. Deterministically observe ACK; persist success/failure; silent and ACK-only expire. One row per idempotent create/retry. |
| Tenant/device binding | Real DB + broker injection: unknown command/device, foreign tenant, another same-tenant device do not mutate/disclose. Device cannot select DELIVERY_FAILED. |
| Ordering/replay/conflict | Result before ACK/post-publish write, duplicate ACK/result, contradictory terminal, late timeout result, replay after API restart preserve authoritative state/timestamps. |
| Deadline and lock races | Fake clock + real row locks: before/at/after deadline, lock wait across expiry, reservation/result/expiry races, stale queued response. Assert both terminal winners, no expired dispatch admission. |
| Durable bound/migration | Real DB: old active/terminal backfill, previous API insert compatibility, new Create metadata, missing-schema startup with command mode disabled/enabled, concurrent reservation one slot, <=4 attempts through restart, no ACK/terminal eligibility, scoped expiry leaves foreign rows unchanged, >100 expiry batches/backlog excludes expired dispatch. |
| Crash/unknown windows | Hooks plus actual process restart after create commit, reservation before publish, broker accepted before dispatch write, response before DB commit. Verify counters, same ID, duplicate-safe PING and declared replay limits. |
| Broker/SDK degradation | Real broker restart + controllable SDK tokens: missing/late PUBACK, publish disconnect, retired callbacks/tokens, SUBACK rejection, readiness recovery, no offline publish, one active publish/epoch. Fakes alone do not prove network behavior. |
| DB/overload/stop | DB unavailable/lock timeout, full 64-response queue: no false completion, bounded waits, diagnosed loss/expiry recovery. Partial startup, HTTP failure, SIGTERM, forced drain cancel/join before pool; race evidence. |
| Coupled regression | Telemetry/projection/alert transaction, default simulator, command GraphQL scope/idempotency, readiness and health-only behavior with command disabled/alone/alongside telemetry; generated contract unchanged. |

Use fake clocks/ports for policy/token edges and real PostgreSQL/Mosquitto/
processes for persistence, network, migration, restart, readiness and cleanup.
A deterministic kill hook represents its named crash window; discarding a return
value is not proof of TCP loss.

- Existing API static/format/generated/modernize/staticcheck, `api:test`,
  `api:test:race`, `api:lint`, `api:build` protect changed Go surfaces.
- `api:test:integration`: migration 008 up/repeat/up-down-up in disposable data,
  old-row compatibility, binding, reservation and lock races.
- Extend `mqtt:test:integration` retaining telemetry cases. Its unique Compose
  project owns ports 11883/15432/18080, random test credentials, temporary
  binaries and cleanup. Refuse occupied ports; never reset dev resources.
  Keep real two-minute ACK-only and silent expiry proofs, running them
  concurrently on separate devices. CI runs `core`, `deadlines`, `outage`, and
  `shutdown` as a matrix; a fail-closed aggregate preserves the required
  `mqtt-integration` status context. Emit per-scenario timestamps/durations.
  Bound startup/assertions/teardown; verify owned processes/containers/ports
  are gone after success/failure/signal.
- Add `apps/api/api/asyncapi/commands.yaml`; extend existing `asyncapi:lint`
  command to both contracts, consumed by current CI. Lint is not decoder proof:
  contract examples must agree with strict parser, including FAILED-only code.
- Repository quality and dependency audit CI gates remain. Existing browser suite
  is shared-startup/readiness regression, not command UI evidence. No new
  interactive browser verification/frontend change belongs in this backend slice;
  MVP-012 owns that journey.

### Approved execution sequence

1. Recheck branch/base, accepted dependencies, and available migration number;
   confirm only the approved local/test PING boundary is being implemented.
2. Add wire contract and unit examples, then additive migration/repository
   reservation, device-bound response and scoped expiry operations with real-DB
   evidence. Verify old API insert compatibility before runtime wiring.
3. Add command adapter/client epochs and delivery/response workers with bounded
   fault-token/clock/queue tests; add simulator command mode behind explicit config.
4. Compose configuration, schema checks, readiness and start/stop in the API;
   extend the isolated real broker/process harness, preserving telemetry cases.
5. Update contract lint/docs/examples, reconcile actual diff against this plan,
   self-review/fix, run final candidate validation, and hand off the PR with
   review-response evidence. Do not merge without an explicit merge request.
   Stop/re-plan if the declared assumption or scope fails.

## Documentation Updates

The env examples and configuration guide match the implemented validators and
keep command delivery disabled by default. AsyncAPI, API/architecture guides,
local simulator walkthrough, integration commands, README, and roadmap record
the implementation and review disposition. F1 admission synchronization,
F2 failure-code property presence, and G1 outage-expiry/forced-shutdown
evidence are now part of this completed candidate.

## Risks / Open Decisions

No architecture/transport choice is open within the approved local/test slice.
The implementation is authorized; validation, self-review and review-comment
resolution are the completion gates for this handoff.

| Assumption / accepted limit | Falsifier, signal and action |
| --- | --- |
| Diagnostic PING, one local dispatcher, small backlog | Current type/product/topology evidence supports it. New actuator/dispatcher or observed starvation: stop and re-plan deduplication/coordination/capacity; no silent stronger guarantee. |
| Same-host clock suffices | Host-run simulator supports it. Test shows expired work accepted under clock divergence: resolve clock contract before broadening runtime. |
| Four slots/backoff suffice | Selected policy, not measured fleet guarantee. Recovery/backlog test failure: review changed policy/evidence; never reset attempts/extend deadline silently. |
| Fresh epochs contain SDK pending work | Pinned source shows waits do not cancel. Cleanup/timeout/leak tests falsify containment: stop rollout and revise adapter, not a cancellation claim. |
| ACK-only/lost result may time out | Existing overall deadline, ephemeral broker/device response supports it. Durable recovery requirement needs separately reviewed replay/dedup design. |

Owner of assumption verification/revisit: MVP-011 implementer/reviewer; revisit
on a falsifier above or before expanding the local/test boundary.

Containment: disable command mode, stop runtime before code revert, preserve
command rows/additive migration 008. Disabled mode stops automatic expiry.
Re-enable only after schema/candidate checks; exclude expired work on recovery.
Migration down removes retry history and is for disposable test data only,
never automatic rollback on valued data. No production migration/deployment/
external mutation is authorized. Check available migration number before
execution; collision never authorizes editing historical SQL.

## Engineering Improvement Review

- Current scope: device-bound transition, durable retry, strict wire validation,
  isolated uncertain-publish cleanup, independent expiry and crash evidence are
  necessary for truthful async behavior; owners/evidence are mapped above.
- Future enhancements: effectful-device dedup, credentials, replica ownership,
  replay, retention/admission and measured fleet throughput need their own trigger.
- Scope effect: the implementation adds additive retry metadata, an independent
  command client, strict response handling and local simulator receive mode. It
  adds no UI, production command path or service split. CI later exposed
  vulnerable transitive Node resolutions, so this branch also adds three
  same-major workspace overrides and lockfile updates; direct app dependency
  declarations and product behavior remain unchanged.

## Plan review and handoff (2026-10-01)

Initial findings: major — unspecified QoS/retry/restart ownership; major —
transition API did not bind response device; major — Paho timeout could be
mistaken for cancellation; major — response loss/expiry/partial shutdown had no
acceptance boundary; minor — stale dependency merge status. Revised plan resolves
these through explicit contract, transactions, epochs, loss/evidence and status.

Final author review traced create -> reserve -> publish -> device -> response ->
locked state -> read, including crash, deadline, authority, overload and stop.
Checked dependency direction, simpler alternatives, recovery and test mapping
against current code. The final pass also added previous-API insert compatibility,
schema checks with transport disabled, and organization-scoped expiry to avoid
rollback/startup gaps and unintended tenant mutation. Verdict: ready for
implementation approval, no unresolved
design blocker within local/test PING scope. This is an author plan review,
not an independent implementation/PR review or implemented behavior claim.

Planning evidence: targeted baseline unit suites, `go mod verify`, repository
policy, final diff/link/required-section checks passed. Default Go shim cache
was denied by sandbox; the pinned binary passed with a writable temporary build
cache and existing module cache. At that planning handoff, browser verification
and a fresh vulnerability scan were not run for the then dependency-neutral
backend slice; current implementation evidence below supersedes that limit.

## Implementation evidence (2026-10-01, current branch)

- `go test -race ./...`, Go format, `go vet ./...`, `api:generate:check`,
  `api:modernize`, `api:staticcheck`, repository `lint`, API build, repository
  policy, AsyncAPI lint for both contracts, and `git diff --check` passed.
- `api:test:integration` passed with real PostgreSQL. It exercised migration
  008 backfill for active and terminal pre-008 rows, old insert compatibility,
  missing-schema startup, concurrent durable reservations, device/org binding,
  scoped expiry, deadline/row-lock races, and all integration-tagged race tests.
- `mqtt:test:integration` passed with real PostgreSQL/Mosquitto and processes.
  It covered telemetry regression, command completion/failure, ACK-only/silent
  expiry, API restart, publish-before-state write failure/retry, broker
  recovery and shutdown.
- `test:browser` passed all 32 cases, including API readiness recovery and the
  existing device, telemetry and alert journeys. The sandboxed first attempt
  could not launch Chromium; rerunning the same isolated test runner with the
  required launch permission passed. No UI changed.
- Forced process death at every internal instruction boundary is not directly
  injected; the real restart and publish-before-write failure cases plus
  fake-token tests cover the selected recovery contract.
- Feature implementation commit `46b1ba0` also passed `check:fast`,
  `go test -race ./...`, and rerun real PostgreSQL and Mosquitto integration
  tests. Local and CI modernization checks include `-any` and
  `-testingcontext`. The current Node dependency remediation passed the
  `check:fast` suite again as recorded below.
- CI run `36820799246` exposed seven production-audit advisory records through
  transitive `brace-expansion` and `serialize-javascript` resolutions. The
  workspace now constrains only their affected major ranges to patched
  compatible releases (`2.1.7`, `5.0.12`, and `7.1.2`); direct dependency
  manifests are unchanged. `pnpm install --frozen-lockfile`,
  `pnpm run node:audit`, `pnpm audit --prod`, `pnpm run check:fast`,
  `pnpm run web:build`, and `pnpm run test:browser` passed on the updated
  lockfile. The resolved dependency tree contains only those patched versions.

### PR #21 review disposition and final candidate evidence (2026-10-01)

The review of head `4401170ee50b26729bb928c222763a9952d85bea` identified two
confirmed findings and one missing-evidence blocker. The current candidate
addresses them as follows:

- **F1 — callback admission race:** the transport holds a read lock from its
  callback admission guard through bounded queue insertion. `Stop` closes
  admission under the write lock and waits for already-admitted callbacks
  before the response worker drains. A deterministic blocked-callback test
  proves that a callback already inside admission is queued before `Stop`
  returns.
- **Shutdown cancellation follow-up:** required `api-db-integration` on
  `195dbce` exposed a second worker-selection race: a ready queue entry could
  win `select` alongside cancellation and start after the drain deadline. The
  worker now checks cancellation after receiving work before calling the
  repository. Its forced-deadline regression passed 25 race-enabled runs, the
  full real-PostgreSQL API integration suite, and the real-broker forced-
  shutdown scenario.
- **F2 — optional empty failure code:** the decoder now validates property
  presence as well as value, rejecting any `failureCode` on ACK/COMPLETED and
  requiring it on FAILED. Negative decoder tests cover empty ACK/COMPLETED and
  missing FAILED; the unchanged AsyncAPI outcome schemas remain the contract
  reference.
- **G1 — outage expiry:** the real broker/database test creates a command
  while Mosquitto is down and observes its stored `TIMED_OUT` before restarting
  the broker. It then proves simulator resubscription, no delivery of the
  expired command, and successful delivery/completion of a new command.
- **G1 — forced response shutdown:** a real PostgreSQL transaction holds the
  command row lock while an ACK is in-flight and a COMPLETED response is
  queued. With a one-second shutdown deadline, the API records one queued
  response, cancels the in-flight database operation, logs worker completion
  before exiting, and leaves the row `DISPATCHED` without ACK/terminal times.
- **CI duration recommendation:** ACK-only and silent expiry run concurrently
  on different devices (combined wait in the passing all-suites run: 118,956
  ms). Scenario timestamps and durations are emitted. Local runs measured
  command-outcomes 43,776 ms, broker recovery 7,147 ms, broker-outage expiry
  126,936 ms, and the final forced response shutdown 3,368 ms. Four independent
  suites run in CI, with the existing required `mqtt-integration` context
  retained by a fail-closed aggregate.

The first local outage-harness attempt observed expiry but exposed a test-only
reconnect-log offset race. The harness now records the offset before broker
restart; the repeated outage suite passed. This did not require a runtime
behavior change.

The first pushed CI candidate also exposed the cancellation race described
above in `api-db-integration`; the final local API integration and shutdown
suite passed after its fix. The replacement CI run on the pushed correction is
the final remote validation gate.

The full default `mqtt:test:integration` run passed on `195dbce` with all four
real PostgreSQL/Mosquitto suites (`core`, `deadlines`, `outage`, `shutdown`).
After the cancellation follow-up, the affected evidence passed again on the
current source: the response-worker test ran 25 times under the race detector,
`api:test:integration` passed across all API packages with PostgreSQL, and the
real-broker `shutdown` suite passed. Go format/vet/build, AsyncAPI lint,
repository policy, Node syntax, and `git diff --check` also passed. The PR
remains open and unmerged; the user requested that the plan move with the
implementation and review response, while branch protection remains the final
merge gate.

## Done Criteria

Every accepted PING keeps one logical identity, bounded durable retry and
truthful terminal COMPLETED, explicit FAILED or stored TIMED_OUT. ACK is
intermediate, not completion. Required real-store/transport/restart/negative-
input/identity/deadline/regression evidence and the requested review fixes pass
on this candidate; CI status remains enforced on PR #21. The author completed
self-review; no additional context-isolated review was requested. PR #21 is
open and unmerged. No production migration, deployment or external runtime
mutation was performed.
