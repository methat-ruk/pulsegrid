# MVP-013 — End-to-End Product Loop

Status: Ready for review (PR #24 open; not merged)

Branch: `test/mvp-013-end-to-end-product-loop`

Intended PR: One MVP-acceptance PR, primarily tests, harness safeguards, and docs

Milestone: M5 — MVP acceptance

Plan review date: 2026-10-03

Implementation authorization: User authorized execution of this revised scope
on 2026-10-06 and later authorized remediation of PR #24 review findings. The
implementation and author review are complete. The PR remains open and
unmerged; M5 is not complete until the candidate is accepted.

## Goal

From a clean local checkout, prove one device-to-operator-to-device journey:
provision in the console, publish MQTT telemetry, inspect persisted state and
a threshold alert, send PING in the console, and observe the device's stored
terminal result. Make the run reproducible, isolated, and diagnosable.

## Why

Separate registry, telemetry, alert, and command tests do not prove that an
operator can follow their connections for the same device. Acceptance needs
that joined journey and named failure/isolation evidence, without a new
platform layer or a production-readiness claim.

## Scope

- Add one unmocked browser product-loop scenario using existing PostgreSQL,
  Mosquitto, API, Nuxt, and the standalone simulator.
- Reuse existing real-boundary tests; extend only missing acceptance assertions
  for malformed telemetry, replay/conflict, command failure/timeout/recovery,
  and two registered tenant fixtures.
- Define and verify target preflight, fixture ownership, bounded child-process
  lifetime/diagnostics, interruption and teardown in existing harnesses.
- Verify setup without ignored environment files or prebuilt application
  output; document one joined developer/demo workflow and reconcile related docs.

## Out of Scope

- Production deployment/traffic, identity/RBAC/device credentials, broker
  ACL/TLS, full security readiness, load certification, durable broker replay,
  and multi-replica command execution.
- Tenant switching in console/simulator, rule-editor UI, effectful commands,
  alert acknowledgement/resolution, and other new product capability.
- New schema/migrations, public GraphQL/MQTT contracts, unrelated dependency
  changes, generic orchestration, service extraction, application images,
  Kafka or Kubernetes. F1 review remediation is limited to the compatible
  lockfile-only `source-map-js` resolution update from `1.2.1` to `1.2.2`; it
  adds no manifest entry or override and does not change the audit gate.
- General workspace dependency remediation, including `node-forge` and other
  findings from the broader workspace audit, or weakening the artifact gate.
  Keep workspace findings separately visible; new production-artifact exposure
  remains a blocker.

## Dependencies

- [MVP-009](../completed/MVP-009-alert-console.md) and
  [MVP-012](../completed/MVP-012-command-console.md), including accepted predecessors.
- Reviewed base: local HEAD, `origin/main` and remote `main` all resolve to
  `619e1c73fc850e510b6656b74e75c3d0fd719fe2`; working tree was clean on
  2026-10-03. The named branch already exists at that base; its name alone
  does not indicate implementation.
- MVP-012 merged as `1142eb7` in
  [PR #22](https://github.com/methat-ruk/pulsegrid/pull/22).
  [Main CI run 37002582118](https://github.com/methat-ruk/pulsegrid/actions/runs/37002582118)
  passed on `619e1c7`, including all four MQTT suites. This is baseline evidence,
  not MVP-013 acceptance evidence.
- Recheck base/dependencies and branch protection before execution/handoff.
  On 2026-10-06, 14 status contexts remained required and the approving-review
  count was zero. Author review and CI remain required. This snapshot neither
  creates nor satisfies an independent review requirement.

### Existing evidence and remaining gaps

| Surface | Source | Disposition |
| --- | --- | --- |
| UI registry/telemetry/alerts | `tests/browser/device-registry.spec.ts`, `device-telemetry.spec.ts`, `alerts.spec.ts` | Reuse real controls/publishers/navigation; join the separate journeys |
| Command success/failure/recovery/ACK-only expiry | `tests/browser/device-commands.spec.ts` | Required companion evidence; avoid another two-minute expiry wait |
| Real MQTT/DB/runtime failures | `scripts/mqtt-integration.mjs`: `core`, `deadlines`, `outage`, `shutdown` | Add missing no-write/replay/two-registered-tenant assertions only |
| Tenant-scoped contracts | `apps/api/graph/{telemetry,rules,commands}_graphql_integration_test.go`, `apps/api/internal/platform/httpserver/graphql_integration_test.go` | Inspect assertions and extend symmetric/scoped cases where missing |
| Setup/configuration/gates | Root scripts, configuration tests, `compose.yaml`, `.github/workflows/repository-quality.yml` | Preserve commands, target policy and required contexts |

No inspected source proves the complete UI-provision-to-alert-to-command loop
for one device. The MQTT wrong-tenant case currently uses unregistered
`other-tenant`; that alone does not prove two registered tenants are isolated.
Mocked UI cases are useful presentation evidence, not persistence proof.

## Architecture / Boundaries

### Locked decisions and alternatives

| Decision | Direction and reason | Revisit/stop trigger |
| --- | --- | --- |
| Runtime | Existing modular Go process, Nuxt adapter, PostgreSQL and local MQTT | Fix needs a new service/module owner or distributed layer |
| Evidence design | One joined unmocked browser journey plus focused integration companions | Test bypasses UI provisioning/command submission or cannot correlate IDs |
| Rule setup | One enabled `GT 25°C` rule through existing GraphQL; no rule-editor UI exists | Requirement changes to UI rule authoring |
| Tenant evidence | Paired trusted GraphQL handlers/real DB plus registered-tenant MQTT binding tests | New identity selector, auth policy or public tenant contract is needed |
| Expiry | Existing two-minute `commands.CommandLifetime` and long-test budgets | Proposal shortens domain expiry or writes a fake terminal state |
| Gate placement | Auto-discover the new spec through `test:browser`/`browser-smoke`; reuse DB/MQTT gates | Required scenario cannot fit a measured, justified runtime budget |

Alternatives challenged: documentation/manual demonstration alone lacks durable
regression proof; a new all-in-one runner duplicates working lifecycle/CI
machinery; repeating every failure in the main browser case adds slow coupling
without stronger evidence. Choose the joined happy path and the lowest faithful
existing companion suite. No new dependency or platform abstraction is needed.

### Preserved authority

- Registry owns organization/device association; GraphQL principals remain
  server-selected. Browser input, headers and storage cannot select tenants.
- Ingestion validates MQTT and resolves registry authority. Projection owns
  observation identity/history/current state. Rules evaluate in that transaction;
  alert snapshots correspond to committed input. Exact replay cannot advance
  state/last-seen or add alerts; conflict cannot partially commit.
- Commands remain the lifecycle writer. HTTP acceptance, MQTT PUBACK and ACK
  are distinct from device completion. Expiry and terminal states are server
  facts; browser clocks/tracking budgets cannot manufacture them.
- Console owns presentation/navigation, manual telemetry/alert refresh,
  command tracking and same-tab recovery. Preserve current UI design and
  section-local cleanup; no redesign or shared realtime transport is included.
- Simulator remains an external device-shaped process without API/DB imports.
  Harness SQL may arrange registry fixtures and inspect results, but must not
  write telemetry/projections/alerts/terminal commands to manufacture acceptance.

### Two-tenant boundary

The live development API and standalone simulator fix `pulsegrid-dev`. Do not
generalize their configuration for a test:

1. Tagged real-DB tests arrange A/B resources through existing repositories and
   construct existing GraphQL handlers with trusted server-selected principals
   for each. Exercise actual resolvers/repositories in both directions. Retain
   the Fiber context-composition coverage; extend only uncovered bridge cases.
2. MQTT `core` arranges a second organization/device in its owned disposable DB.
   Direct registry fixture insertion is allowed for setup because the public API
   has no organization-provisioning operation. Publish valid B telemetry with
   the existing raw Mosquitto test publisher and prove B persistence. P1 still
   uses the standalone simulator.
3. Publish A-slug/B-device and B-slug/A-device telemetry: neither may change
   protected state. Subsequent valid same-scope input must still work.
4. Live A GraphQL cannot read B device/current/history. Paired handler tests
   prove the other B rule/alert/command read/mutation denials, without claiming
   a second browser tenant session.
5. The command-response runtime subscribes only to the selected
   `pulsegrid-dev` tenant filter. Do not claim a second tenant's broker response
   reaches that runtime. Exercise the response service processor with a valid
   B-device envelope and persisted A command using the real registry resolver
   and PostgreSQL repository; assert it leaves A unchanged and accepts a valid
   B response. Keep the transport subscription filter covered by its existing
   test. This proves the downstream binding invariant, not multi-tenant broker
   subscription or production identity.

This is data-scoping/binding proof, not device authentication or production
authorization. Anonymous local MQTT peers remain outside that claim.

## Implementation Direction

### Sequence and affected files

1. Reconfirm the base; map actual existing assertions to P1–L1 below and identify
   reuse/gaps before adding tests.
2. Add `tests/browser/product-loop.spec.ts`. Extend `tests/browser/fixtures.ts`
   only for needed shared helpers, explicit simulator mode, bounded output/waits
   and failure-safe cleanup; keep scenario assertions in the spec.
3. Extend `scripts/mqtt-integration.mjs`, tagged GraphQL/Fiber tests, and the
   response-service integration test for D1/D2/T1/T2/T3 gaps. Reuse covered
   outage/shutdown/expiry/recovery.
4. Harden `scripts/test-browser.mjs` and cover target/cleanup behavior with
   `test:browser:runner`. Run safeguards in a service-free CI job parallel to
   browser E2E; the required `browser-smoke` aggregate passes only when both
   jobs succeed. The product loop continues to use `test:browser`.
5. Reconcile actual diff/assertions with the plan, author-review/fix, run final
   checks, record acceptance evidence here, then update canonical docs/status.

Routine fixture/test corrections stay in scope. Propose a discovered owning-module
fix with its regression; re-plan/approve before product behavior, security/tenant
controls, schema, public contracts, dependencies or significant architecture
changes. Do not turn acceptance into an unbounded repair/audit.

### Primary scenario P1

Use unique per-test/retry device keys and capture returned IDs:

1. Open `/devices/new`, create via visible controls and confirm `/devices/:id`,
   identity and empty telemetry/history.
2. Arrange one enabled `GT 25°C` rule through `/api/graphql`; verify rule/device
   association. This is explicit API fixture setup, not a claimed UI feature.
3. Publish `23.5°C` with the real one-shot simulator; bounded GraphQL polling
   confirms that exact message committed. Refresh telemetry in UI: current value,
   one history row, no matching alert for this observation.
4. Publish `31°C`; wait for that message and one matching alert, refresh telemetry,
   follow the device's alert link and open detail. Assert device/rule/message,
   `GT`, `25`, `31` and stored event times. Follow its device link back; confirm
   two history rows and the latest message, not an unrelated global row.
5. Start command simulator `success` mode; wait for subscription readiness before
   Send PING/confirmation. Match the selected command ID/device to GraphQL;
   observe `COMPLETED`, dispatch/ACK/terminal milestones and no failure code.
   Fast intermediate states need not render; D4 separately proves ACK semantics.
6. Reload device detail, refresh history and select that command: telemetry and
   terminal result remain server-backed. Assert no unexpected page/console
   errors and no mocked GraphQL/MQTT/DB responses.

Reuse 10-second telemetry/alert and 20-second command observation bounds; P1
may have a 60-second test budget. No arbitrary sleeps or fake backend clock.
UUIDs/timestamps vary; values, relationships, counts and outcomes are deterministic.

### Fixture/resource lifecycle

- Local browser/MQTT runs own unique Compose projects, disposable credentials,
  migrations through `008`, seed and child processes. Test ports: DB `15432`,
  MQTT `11883`, API `18080`, Nuxt `4173`. Refuse collisions; never reuse/kill an
  unrelated service. Local suites run sequentially; CI jobs use separate runners.
- Validate targets before migrations/dependency mutation. Local acceptance owns
  its DB and rejects an inherited DB override. The CI browser job opts into
  external ownership explicitly; the existing browser-service override is
  required in that mode (no generated-URL fallback) and must match the strict
  test DSN: `127.0.0.1`, `15432`, `pulsegrid_test`, user `pulsegrid`, explicit
  non-placeholder password, `sslmode=disable`, no fragment/extra URL parameters.
  Never print credentials/DSNs.
- Inject `PULSEGRID_ENV=test`, identity and both MQTT modes as `development`,
  broker `mqtt://127.0.0.1:11883`, `NUXT_APP_ENV=test` and backend origin
  `http://127.0.0.1:18080`; explicitly select telemetry/commands simulator mode
  and fixture values. Do not inherit development dotenv/production values. The
  [environment guide](../../../project-setup/environment-configuration.md)
  remains the configuration authority.
- Per-test namespaces prevent order/retry dependence. Reset through owned
  disposable infrastructure; never truncate shared data or delete development
  volumes. External CI DB lifetime belongs to CI, not harness teardown.
- On pass/failure/partial startup/SIGINT/SIGTERM, terminate and join owned children
  with bounded escalation, then tear down only owned Compose resources. Register
  ownership before startup can fail. Cleanup failure exits nonzero and retains
  the original failure. Preflight refusal grants no teardown rights.
- Bound output and publish/readiness waits; retain sanitized diagnostics before
  teardown. Verify a second run and controlled failure/interruption release
  owned ports/resources. Use test fixtures/process controls, not production
  fault-injection hooks.

Lifecycle evidence must include occupied-port refusal without touching the
occupant, startup failure after resource acquisition, and SIGINT/SIGTERM after
readiness. Use awaited child-process/signal handling as in the existing MQTT
runner rather than blocking synchronous calls that prevent timely cancellation.
Failing-path checks assert nonzero exit, bounded completion and absence of owned
children/resources; they must not delete or stop pre-existing resources.

## Validation

### Required acceptance matrix

Existing assertions can satisfy a row after inspection and execution on the
final candidate. Test names and baseline CI alone cannot close it.

| ID / guarantee | Required oracle | Evidence owner / gate |
| --- | --- | --- |
| P1 complete loop | UI-created device, two simulator messages, matching alert/navigation, UI-created PING/terminal record survive reload | New product-loop spec; `test:browser` / `browser-smoke` |
| D1 malformed telemetry | Publish malformed JSON to registered topic; observe new scoped rejection, compare identity/history/current/alerts before cleanup: no writes; valid follow-up works | MQTT `core`; `mqtt-integration` |
| D2 replay/conflict | Replay after acceptance/API restart: one identity/history row and matching alert, unchanged current/last-seen; conflicting same-ID payload changes none; retain pruned-history replay/out-of-order coverage | MQTT `core`, projection/rules DB tests; `mqtt-integration`, `api-db-integration` |
| D3 device failure | Real receiver `failure`: stored `FAILED`, `DEVICE_REPORTED_FAILURE`, terminal milestone, truthful UI failure and no completed label | Existing command browser case/MQTT `core`; `browser-smoke`, `mqtt-integration` |
| D4 timeout/late result | ACK remains intermediate; ACK-only/silent reach server `TIMED_OUT` at unchanged deadline across restart; late completion cannot change timeout; UI shows stored result | Existing browser expiry, MQTT `deadlines`, command DB tests; browser/MQTT/DB gates |
| D5 lost HTTP response | Commit through real backend, drop only response, reload and recover same device/type/key: one row and unchanged command ID | Existing browser recovery/GraphQL replay; browser/DB gates |
| T1 two-tenant API/data | Own reads succeed; foreign device/state/history/rule/alert/command reads null/empty or contract denial, scoped cursors rejected, foreign mutations write nothing; A→B and B→A | Real-DB GraphQL/Fiber tests; `api-db-integration` |
| T2 registered-tenant telemetry | Valid A/B telemetry persists separately; crossed tenant/device telemetry causes scoped rejection/no writes; valid follow-up works | Extend MQTT `core`; `mqtt-integration` |
| T3 command-response binding | Inject a valid B-device/A-command envelope at the response-service boundary with registered B and persisted A/B commands; A remains unchanged and matching B response completes B. Retain selected-tenant transport filter proof | New tagged PostgreSQL response-service integration case plus existing transport test; `api-db-integration` |
| E1 environment separation | External/development test targets refused before writes; test values explicit/private; existing production identity/MQTT rejection/disable and Nuxt adapter restrictions remain proven | Config/transport tests, harness refusal evidence, repository policy, artifact audit; applicable API/web gates |
| L1 clean setup/cleanup | Fresh tracked checkout without ignored dotenv/prebuilt output runs documented commands; cleanup passes on success/failure/interruption; rerun independent | Existing wrappers/focused lifecycle evidence; CI plus recorded clean-local run |

For async negatives, observe processing/rejection of the specific delivery after
the captured log offset, then inspect persisted state before cleanup. Immediate
absence after PUBACK is not a valid oracle. Match IDs/reason codes on the same
log line where available; unrelated lines cannot be combined into a false match.
SQL reads supplement, not replace, public-path product actions.

### Commands, gate authority and evidence record

After implementation, use the canonical toolchain pins and commands:

```sh
corepack pnpm run setup
corepack pnpm run setup:browser
corepack pnpm run repository-policy
corepack pnpm run check
```

`check` covers fast/static/generated-contract checks, unit/component/race tests,
builds, OpenAPI/AsyncAPI, audits, MQTT, DB integration, browser-runner lifecycle
tests and browser evidence. Use focused existing commands during iteration;
final checks apply to the reconciled candidate. Use a disposable verification
checkout for clean-local proof; never remove the contributor's `.env` or data
to simulate cleanliness.

Keep all 14 protected contexts: `repository-policy`, `api-static`, `api-test`,
`api-race`, `api-vulnerabilities`, `web-lint`, `web-typecheck`, `web-test`,
`web-build`, `node-dependency-audit`, `openapi-contract`, `browser-smoke`,
`api-db-integration`, `mqtt-integration`. Preserve the four-suite MQTT aggregate
and fail-on-flaky browser behavior. Measure before timeout changes; retries do
not establish stability.

Automated browser controls/navigation/state assertions are required. No visual
redesign is planned. Add interactive inspection only for a material UI gap
exposed during execution, using available Browser capabilities. If unavailable,
name the gap and obtain authorization for a concrete localhost browser-automation
or computer-use fallback before invocation. The implemented acceptance uses
automated Playwright journeys; no additional interactive inspection was needed.

Record final source SHA, commands/cases, actual boundaries, CI URLs, sanitized
artifacts, durations, passed/failed/skipped/unavailable/not-run results and risk
in this plan's completion section. Do not combine different candidates into a
final pass. Required missing evidence keeps acceptance incomplete unless its
risk is explicitly accepted; missing evidence never becomes a pass.

## Documentation Updates

| Owner | Bounded cleanup/change |
| --- | --- |
| This plan | Own scenarios/decisions/review/evidence; Ready for review while PR #24 remains open and unmerged |
| `docs/project-setup/local-development.md` | Describe the joined browser workflow, lifecycle commands, expected outcomes, cleanup behavior and limits |
| `docs/architecture/system-architecture.md` | Describe the validated product-loop candidate without changing module/data authority |
| `docs/architecture/technology-decisions.md` | Preserve existing conditional/deferred technology decisions; no change was needed |
| Product scope/API index/app READMEs/environment guide | Preserve canonical ownership; correct only affected contract/command/example drift; no new endpoint/schema/env key planned |
| Root README/roadmap/plan index | Link reviewed scope and mark Ready for review; keep M5 incomplete until accepted delivery |

Use lowercase `docs/` paths (the current filesystem also resolves `Docs/`).
Preserve useful historical completed-plan evidence; label superseded candidate
results and remove contradictory current claims instead of rewriting past
results or duplicating canonical guidance. Move this plan to `completed/` only
after the outcome is accepted, then update every inbound link in that same
change. Generated artifacts stay ignored; commit concise sanitized evidence
references only.

## Risks / Open Decisions

- Runtime cost/flakiness: reuse existing two-minute expiry evidence, bounded
  polling and isolated namespaces. Measure budget pressure; do not shorten the
  product deadline or silently skip a scenario.
- False isolation proof: T1/T2/T3 require two registered tenants and positive/negative
  oracles. They do not prove authenticated MQTT peers or production RBAC.
- Lost telemetry: PUBACK is not durable processing/replay. Retain transactional
  rollback and explicit republish recovery evidence; do not promise automatic replay.
- Data/resource leakage: inherited DSNs, fixed ports, hangs and partial startup
  are covered by E1/L1, not optional cleanup.
- The review-directed lockfile-only `source-map-js@1.2.2` resolution passed the
  rebuilt production artifact gate on local artifact SHA-256
  `17f2abf01a7e91b29f3e35b1a9b356c760c3bbe9eab0266665210546341dd9ca`. This
  closes the required artifact finding for that candidate. Exploit reachability
  was not established and is not needed to satisfy the fail-closed gate.
- The workspace audit still reports `node-forge`, `braces`, and `simple-git`;
  these remain visible and unresolved. The result does not claim their
  remediation or production readiness.
- Full local validation passed on 2026-10-06. Protected branch CI must still
  pass on the pushed candidate; M5 remains incomplete while PR #24 is unmerged.

Recovery: stop the failed owned run, retain minimal diagnostics, remove only its
disposable resources, correct within authorized scope and rerun affected evidence.
No production/schema rollback or development-volume reset is authorized. Re-plan
when a fix changes protected contracts, authority, ownership or risk.

Engineering improvement review: joined journey, registered-tenant proof,
no-write oracles and bounded fixture lifecycle are coupled to trustworthy
acceptance. No independent enhancement is admitted. Production identity,
broker durability, dependency remediation and fleet commands remain separate
work with their existing triggers.

## Plan Review / Challenge

This records author plan review, not independent implementation review. The
cross-boundary acceptance decision uses Material Change (Tier 2) reasoning.

| Original finding | Disposition |
| --- | --- |
| Major: no concrete joined journey/correlation oracle | P1 locks one UI-created device and matching rule/message/alert/command identities |
| Major: two tenants implied an absent live selector | T1/T2 use trusted handler scope and registered-tenant MQTT without new selectors |
| Major: failure paths lacked no-write/recovery assertions | D1–D5 lock persisted oracles, completion barriers and evidence owners |
| Major: setup/safety lacked target/teardown ownership | E1/L1 lock strict preflight, external CI DB ownership, interruption and rerun proof |
| Material boundary: command response subscription is scoped to `pulsegrid-dev` | Do not widen the broker subscription. T3 injects at the service boundary and records that no second-tenant transport claim is made |
| Major: evidence lacked candidate/gate linkage | Matrix maps to existing contexts and distinguishes baseline from final acceptance |
| Minor: current docs contradicted merged MVP-009/MVP-012 | Canonical prose corrected in this planning pass; completion updates remain conditional |
| Scope challenge: new monolithic runner/duplicate expiry tests | Rejected; reuse harnesses and add one joined browser spec |

Final plan challenge: the user's 2026-10-06 start request selected the revised
scope. A verified singleton response-subscription filter changed the test
method, not the product contract: T3 proves downstream tenant/device binding
with real PostgreSQL and explicitly disclaims cross-tenant broker delivery.
The PR #24 remediation is bounded to the findings and gaps below. The full local
check now passes, including the required runtime artifact gate; protected CI on
the pushed revision remains the final delivery gate. No production contract,
tenant selector, broker subscription, or gate policy was widened.

## Done Criteria

- Every P1–L1 guarantee has passing candidate-specific evidence or an explicit
  authorized risk disposition naming the missing guarantee.
- A contributor can run/repeat the joined loop and named failures from a clean
  tracked checkout without distributed infrastructure or production access.
- Actual diff/assertions/evidence reconcile with this plan; author review/fixes
  precede final validation and current protected CI contexts pass on delivery.
- Canonical docs/inbound links agree; only accepted verified outcomes become
  Complete. MVP proof, artifact audit, workspace advisories and production
  readiness remain separate claims.
- Satisfy independent review only if required by an applicable current authority;
  otherwise it is an optional recommendation, not an invented gate.

The following initial implementation record is retained as history and is
superseded by the PR #24 review-response evidence below.

### Implementation Evidence Disposition — 2026-10-06

The initial pushed candidate was `784601a` on
`test/mvp-013-end-to-end-product-loop`, based on merged `main` `619e1c7`.
Initial PR CI run [37405127371](https://github.com/methat-ruk/pulsegrid/actions/runs/37405127371)
passed every check except `browser-smoke` and `node-dependency-audit`. The
browser failure exposed that the simulator's nanosecond timestamp must be
compared at PostgreSQL `timestamptz` microsecond precision. Commit `d04018b`
normalizes the expected value at that storage boundary. Corrected-head CI run
[37406312056](https://github.com/methat-ruk/pulsegrid/actions/runs/37406312056)
passed every context except `node-dependency-audit`; browser-smoke passed all
36 cases. `check:fast` and the full browser suite also pass locally on the
corrected code. The artifact-gate failure remains unresolved by design.

| Plan items | Evidence observed | Current state |
| --- | --- | --- |
| P1 joined UI journey | CI run 37405127371 exposed an expected-timestamp precision mismatch; commit `d04018b` normalizes the simulator time to PostgreSQL storage precision. Corrected-head CI run 37406312056 and local `corepack pnpm run test:browser` both passed the joined journey | Passed; 36 browser cases passed on corrected head |
| D1/D2 and T2 telemetry | `corepack pnpm run mqtt:test:integration`: malformed no-write, replay after restart, conflicting ID no-partial-write and registered-tenant MQTT behavior; corrected-head CI run 37406312056 repeated all four suites | Passed; all 4 suites passed locally and in CI |
| T1 and T3 | `corepack pnpm run api:test:integration`: real-PostgreSQL symmetric GraphQL/Fiber tenant scope and response-service command/device binding; corrected-head CI run 37406312056 | Passed; all tagged API DB tests passed locally and in CI |
| D3/D4/D5 and browser regression | Corrected-head browser-smoke and local `corepack pnpm run test:browser` exercised device-reported failure, 2-minute expiry, lost-response recovery and the joined loop | Passed; 36 Chromium cases in CI and locally |
| E1 runner safeguards | `corepack pnpm run test:browser:runner`: unsafe DSN refusal, local rejection of CI-only mode, missing CI DSN refusal, occupied port, partial Compose startup cleanup and SIGTERM cleanup; repeated in browser-smoke on corrected head | Passed; all 6 node tests passed locally and in CI |
| L1 clean setup | Disposable clone started without ignored env files or build output; `corepack pnpm run setup`, repository policy, `check:fast`, runner safeguards and focused P1 all passed there | Passed for setup and targeted behavior; root full `check` remains blocked by the artifact advisory |
| Static, unit and contract checks | `corepack pnpm run check:fast`, `corepack pnpm run api:vuln`, and corrected-head CI run 37406312056 | Passed; Go vuln tool found 0 called vulnerabilities and 3 findings in required modules outside scanned code paths |
| Full local / branch gates | Local `corepack pnpm run check` passed through `check:fast`, race, build and OpenAPI, then failed at `node:audit`. Corrected-head CI run 37406312056 passed every context except `node-dependency-audit` | **Artifact audit remains blocking.** It found `source-map-js@1.2.1` in the exact production artifact; later local aggregate checks did not run on that attempt |

The prior artifact failure and CI result above are historical. The review
response below records the lockfile remediation, corrected test oracles, new
lifecycle proof, and full validation of the updated candidate.

### PR #24 Review Response — 2026-10-06

The published review comment reviewed head
`08ac70d505424f17c5c7af068b01e49db06997d4` against base
`619e1c73fc850e510b6656b74e75c3d0fd719fe2`. It requested changes for F1–F5 and
identified evidence gaps G1–G3. This section records the author's remediation;
it is not an independent review or merge approval. No schema, public API, MQTT
contract, or audit policy changed; the runtime dependency patch is limited to
the F1 lockfile resolution.

| Finding | Resolution and proof |
| --- | --- |
| F1 — production artifact audit | Updated only the frozen `source-map-js` resolution to `1.2.2`, which is compatible with its existing parent ranges. The rebuilt production artifact passed `corepack pnpm run node:audit`; artifact SHA-256 `17f2abf01a7e91b29f3e35b1a9b356c760c3bbe9eab0266665210546341dd9ca`, 21 physical packages and 54 bundled package contexts across 19 chunks. The separate workspace report still lists `node-forge`, `braces`, and `simple-git`; they are not claimed fixed. |
| F2 — browser descendants | The runner owns the Nuxt server and each command as a process group, starts bounded SIGTERM→SIGKILL escalation on the first interruption, and drains descendants even when a leader exits first. Regression coverage proves a stubborn descendant is removed. The full browser suite passed 36/36 and runner exit remained zero after process-group and Compose cleanup. |
| F3 — telemetry GraphQL isolation oracle | Both A→B and B→A responses are decoded and independently assert `deviceCurrentState == null` and empty telemetry history. `corepack pnpm run api:test:integration` passed the tagged real-PostgreSQL suite. |
| F4 — rule mutation no-write oracle | Snapshot protected rule identity, ownership, metric, comparator, threshold, enabled state, revision, and timestamps before and after each denied cross-tenant create/update. Assertions cover both tenant directions. The tagged PostgreSQL integration suite passed. |
| F5 — response fixture cleanup | Register pool close first so LIFO cleanup removes fixtures before closing the pool; register cleanup before inserts, track organizations as they are created, check DELETE/verification/commit errors, and support partial fixture creation. The tagged PostgreSQL suite passed with cleanup assertions active. |
| G1 — interruption lifecycle | `corepack pnpm run test:browser:lifecycle` ran real Compose scenarios for SIGINT and SIGTERM after API/web readiness. For each signal it verified no owned container, network, volume, or port remained, then reran and passed the focused product-loop journey. The full `check` repeated both cases. |
| G2 — registered-tenant telemetry | MQTT isolation snapshots cover observation keys, observations, current state, and alerts for protected A/B before and after both crossed deliveries; the crossed message ID is checked on the protected device. Live `pulsegrid-dev` GraphQL reads of B return null device/current state and empty history. Valid A and B follow-up messages persisted and A created its expected alert. All four MQTT suites passed. |
| G3 — clean-local proof | The initial clean checkout at `be3596ee3e7c565bc011fa6d10c963004cae9e83` exposed an additional setup gap: `check` failed in web lint because setup had not generated `.nuxt/eslint.config.mjs`. The root setup command now runs `nuxt prepare`; the post-fix clean checkout SHA and full command results are recorded in the final PR #24 response comment. |
| Additional — clean setup required validation | The clean-checkout failure was outside the published F1–F5/G1–G3 review scope but affected the required full check. Root setup now prepares the ignored Nuxt ESLint config, matching the existing CI setup step. No dependency or application contract changed. The post-fix exact-SHA clean run is reported in the final response comment. |
| Additional — CI lifecycle isolation | CI run [37414795819](https://github.com/methat-ruk/pulsegrid/actions/runs/37414795819) showed that the real lifecycle proof ran first against browser-smoke's shared external PostgreSQL database and left valid alert fixtures. The alert journey then failed a strict locator because three rows matched (35/36 browser cases passed). The workflow now runs lifecycle after browser-smoke, and lifecycle Playwright output uses `test-results/lifecycle/` so it preserves full-suite screenshots. Local `test:browser` passed 36/36; the subsequent real two-signal lifecycle passed, and all three command-console screenshot SHA-256 values were unchanged before/after. The next hosted run must verify DB ordering and upload. |

**Contributor-worktree validation:** `corepack pnpm run check` passed on
2026-10-06, including all suites below. That checkout already had generated
Nuxt files; the first clean-checkout replay exposed the separate setup gap
listed above. The exact post-fix clean-checkout result is recorded in the final
PR response comment.
This aggregate ran formatting/generated checks, static analysis, lint and
typechecks, unit tests (80 web tests), Go race tests, Go/web builds, OpenAPI,
the production and Go vulnerability audits, all four MQTT integration suites,
tagged PostgreSQL integration, all nine browser-runner tests, the real
two-signal lifecycle test, and all 36 Chromium cases. No required local check
was skipped or unavailable in that contributor-worktree run. The workspace
advisories above remain a known limitation; they are separate from the passed
runtime artifact gate. The lifecycle step is intentionally last in the shared
external-DB browser job; running the focused fixture first polluted the alert
journey in CI, so the workflow now isolates that evidence by ordering.

PR #24 remains open and unmerged. Implementation candidate
`55a5f8bd69baa7d8125143137163f7ef45b0a722` passed all 14 protected status
contexts. The plan stays in `planned/` with Ready for review status; move it to
`completed/` and mark M5 Complete only after the outcome is accepted.

### Latest PR #24 review follow-up — 2026-10-06

The latest published review comment, [#6009823637](https://github.com/methat-ruk/pulsegrid/pull/24#issuecomment-6009823637), reviewed candidate
`bec7149eab3c3428e8f0e2aa6b7956c60745b27b`, confirmed F6–F8, and left G1/G3
and the browser-smoke failure open. The fixes below were validated on
implementation candidate `55a5f8bd69baa7d8125143137163f7ef45b0a722`.

| Finding / gap | Resolution and evidence |
| --- | --- |
| F1 — production artifact advisory | Updated only the `source-map-js` lockfile resolution to compatible patched version `1.2.2`; the production runtime artifact gate passes without weakening its policy. The clean-check artifact SHA-256 is `fb63249fc6d03bce4ea69eaa5926ac06af9c1bedf26f0aadf14d8ed871adb1ab` (21 physical packages, 54 bundled package contexts across 126 modules, 23 physical package edges). The exact-head CI audit evidence is [artifact 11393267258](https://github.com/methat-ruk/pulsegrid/actions/runs/37421527287/artifacts/11393267258). Workspace-only advisories remain separately reported. |
| F2 — browser descendant cleanup | The runner owns detached process groups and performs bounded termination even when a leader exits before its descendants. The exact-head `test:browser:runner` run passed 14/14 with no skips, including descendant cleanup and interruption cases. |
| F3 — telemetry tenant oracle | Both A→B and B→A GraphQL responses are decoded and independently assert no current state and empty history. The tagged real-PostgreSQL integration suite passed in the clean full check and CI. |
| F4 — rule mutation no-write oracle | Protected rule identity, ownership, metric, comparator, threshold, enabled state, revision, and timestamps are compared before/after denied mutations in both tenant directions. The tagged real-PostgreSQL integration suite passed in the clean full check and CI. |
| F5 — response fixture cleanup | Fixture teardown runs before pool close, checks cleanup errors and verifies partial fixture cleanup. The tagged real-PostgreSQL integration suite passed in the clean full check and CI. |
| F6 — timestamp string oracle | Normalize UTC RFC3339 fractional seconds to PostgreSQL microsecond precision and remove insignificant trailing zeroes; reject malformed values. The exact-head Chromium suite passed 37/37, including `.921060789` → `.92106`, sub-microsecond normalization, and preservation of a distinct microsecond. |
| F7 — outer lifecycle cancellation | The wrapper handles SIGINT/SIGTERM, forwards cancellation to owned detached process groups, retains signals that arrive before child spawn, escalates repeated signals, tracks each identified Compose project, and performs bounded final cleanup. Controlled tests prove both signals remove simulated processes/resources and allow rerun; the real Compose lifecycle passed both signals in CI and the clean full check. |
| F8 — Docker timeout and cleanup result | Owned Docker commands have a 15-second timeout and SIGTERM/SIGKILL escalation; nonzero `compose down` fails the wrapper. Controlled tests inject a hanging inspection and final `compose down` exit 29; both fail closed. All 14 exact-head runner tests passed, including these cases. |
| G1 — real interruption lifecycle | Browser-smoke log for [run 37421527287](https://github.com/methat-ruk/pulsegrid/actions/runs/37421527287) records SIGINT and SIGTERM project removal followed by a passing focused rerun after each. The clean full check records the same two-signal result. |
| G2 — registered-tenant telemetry evidence | Protected-state snapshots cover observation keys, observations, current state, and alerts across crossed deliveries; follow-up valid A/B messages persist, A creates its expected alert, and live GraphQL reads of B remain empty for A. All four MQTT integration suites passed in CI and the clean full check. |
| G3 — clean local proof | A fresh clone at `/private/tmp/pulsegrid-pr24-55a5f8b` started without ignored env/build files. With pinned Node `24.20.0` and pnpm `12.3.4`, `corepack pnpm run setup` prepared Nuxt and completed; `corepack pnpm run check` passed formatting/generated checks, static analysis, lint/typechecks, 80 web unit tests, Go unit/race tests, builds, OpenAPI, runtime artifact and Go vulnerability audits, all four MQTT suites, tagged PostgreSQL integration, runner tests 14/14, browser tests 37/37, and the real two-signal lifecycle. |
| Additional — clean setup | The earlier clean-check failure exposed missing Nuxt-generated ESLint configuration. Root `setup` now runs `nuxt prepare`, matching CI. Fresh `setup` and the full clean-check passed on the exact implementation candidate. |
| Additional — CI lifecycle isolation | Browser-smoke now runs before lifecycle against the shared CI PostgreSQL service, and lifecycle output stays under `test-results/lifecycle/`. On exact-head run 37421527287, browser-smoke passed 37/37, lifecycle passed, and all three command-console images uploaded as [artifact 11393960277](https://github.com/methat-ruk/pulsegrid/actions/runs/37421527287/artifacts/11393960277). |
| Review performance note | A standalone lifecycle invocation builds API/Nuxt artifacts once and reuses them for its inner runs. Full `check` and CI run the browser suite first, then invoke `test:browser:lifecycle:reuse`; the exact-head CI log shows that command and a passing lifecycle without repeated API/Nuxt builds. Assertions, timeout, retries, and test scope are unchanged. |

All **14 branch-protected status contexts** passed on exact-head run 37421527287;
the complete run reports 18 successful check runs including matrix and
aggregate contexts. The PR remains open and unmerged. Keep MVP-013 in `planned/`
with Ready for review status until the outcome is accepted, then move it to
`completed/` and mark M5 Complete. The broader workspace advisories and three
Go module advisories outside reachable code remain known limitations; the
production artifact audit and Go called-vulnerability scan pass.

### Latest PR #24 review follow-up — CI safeguard skips

Review comment [#6010847819](https://github.com/methat-ruk/pulsegrid/pull/24#issuecomment-6010847819)
found that CI run [37423262221](https://github.com/methat-ruk/pulsegrid/actions/runs/37423262221)
reported **14 tests, 9 passed, 5 skipped** in the browser-runner test step.
Those five wrapper tests probed port `15432`, which the same job reserves for
its PostgreSQL service, even though the fake wrapper fixtures do not use that
database. The earlier `14/14` claim referred to local execution, not hosted CI;
this section corrects that evidence record.

The safeguards now run in a separate `browser-runner-safeguards` job without
services, in parallel with `browser-e2e`. The existing required `browser-smoke`
context remains an aggregate that fails unless both jobs succeed. This
preserves the runner's full owned-port collision checks and prevents a
PostgreSQL service from turning cases into skips. Local safeguards passed
**14/14**; repository policy, workflow YAML parsing, and `git diff --check`
passed.

Hosted validation on exact head `127334671c4a7bbe13139db9c9cc288de71ce1bb`
passed: run [37425948915](https://github.com/methat-ruk/pulsegrid/actions/runs/37425948915)
reports **14 tests, 14 passed, 0 failed, 0 skipped** in the service-free job.
The same run passed browser E2E **37/37**, real SIGINT/SIGTERM cleanup and
reruns, and the required `browser-smoke` aggregate. All 14 branch-protected
contexts passed; three command-console screenshots are in [artifact 11394923904](https://github.com/methat-ruk/pulsegrid/actions/runs/37425948915/artifacts/11394923904).

**Correction to the earlier author response:** its `14/14` hosted-safeguards
claim used the local result. Run 37423262221 actually had **9 passed and 5
skipped** in CI. The new hosted result above closes that regression-protection
gap; the separate final response records both runs and the correction.

The follow-up timing review [#6011307790](https://github.com/methat-ruk/pulsegrid/pull/24#issuecomment-6011307790)
measured **5:18** versus the previous **5:25**, a seven-second difference too
small to attribute. Its recommended expiry split is implemented in CI:

- `browser-e2e (fast)` runs the 36 non-expiry browser cases and then the real
  lifecycle proof.
- `browser-e2e (expiry)` runs only the two-minute ACK-expiry case with its own
  runner and PostgreSQL service.
- `browser-runner-safeguards` runs the 14 port/process tests without services;
  the required `browser-smoke` aggregate fails unless all three legs pass.

Separate Playwright `--list` checks confirm the selections partition the
current suite into **36 + 1 = 37** tests without overlap. Both E2E matrix legs
retain one worker, isolated ports and databases, the two-minute expiry, all
assertions, retries, and fail-on-flaky behavior. The estimated four-minute
target remains an estimate until the hosted run is measured. A cross-job
artifact producer remains deferred because transfer and consumer setup could
offset its savings; the expiry split is measured before reconsidering that
cost.
