# MVP-013 — End-to-End Product Loop

Status: In progress

Branch: `test/mvp-013-end-to-end-product-loop`

Intended PR: One MVP-acceptance PR, primarily tests, harness safeguards, and docs

Milestone: M5 — MVP acceptance

Plan review date: 2026-10-03

Implementation authorization: User authorized execution of this revised scope
on 2026-10-06. Implementation and targeted evidence are underway; M5 acceptance
is not complete.

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
- New schema/migrations, public GraphQL/MQTT contracts, dependencies, generic
  orchestration, service extraction, application images, Kafka or Kubernetes.
- Workspace `node-forge` remediation or weakening the artifact gate. Keep the
  advisory separately visible; new production-artifact exposure is a blocker.

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
  At review, 14 status contexts are required; approving-review count is zero.
  Author review and CI remain required. This snapshot neither creates nor
  satisfies an independent review requirement.

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
   `test:browser:runner`. This adds a runner-test command only; the product loop
   uses the existing `test:browser` command. CI keeps the new test inside the
   existing `browser-smoke` status context.
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
or computer-use fallback before invocation. No browser session runs in this
planning pass.

Record final source SHA, commands/cases, actual boundaries, CI URLs, sanitized
artifacts, durations, passed/failed/skipped/unavailable/not-run results and risk
in this plan's completion section. Do not combine different candidates into a
final pass. Required missing evidence keeps acceptance incomplete unless its
risk is explicitly accepted; missing evidence never becomes a pass.

## Documentation Updates

| Owner | Bounded cleanup/change |
| --- | --- |
| This plan | Own scenarios/decisions/review/evidence; remain In progress until the acceptance and delivery gates pass |
| `docs/project-setup/local-development.md` | Fix stale MVP-009 candidate/open-PR prose now; after acceptance add one runnable joined workflow, expected outcomes, failure commands, cleanup and limits |
| `docs/architecture/system-architecture.md` | Fix stale MVP-012 in-progress summary now; after acceptance describe verified loop without changing module/data authority |
| `docs/architecture/technology-decisions.md` | Correct planned command-polling wording now; preserve conditional/deferred technologies |
| Product scope/API index/app READMEs/environment guide | Preserve canonical ownership; correct only affected contract/command/example drift; no new endpoint/schema/env key planned |
| Root README/roadmap/plan index | Link reviewed scope where useful; mark M5/capability complete only after required evidence and accepted delivery |

Use lowercase `docs/` paths (the current filesystem also resolves `Docs/`).
Preserve useful historical completed-plan evidence; remove contradictory current
claims instead of rewriting past results or duplicating canonical guidance.
At completion, move this plan to `completed/` and update all inbound links in
the same change. Generated artifacts stay ignored; commit concise sanitized
evidence references only.

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
- The workspace still reports `node-forge`; the latest full local check on
  2026-10-06 also failed the exact production-artifact gate on
  `source-map-js@1.2.1` (GHSA-68fv-2mgg-jv7q). That package was already in the
  base lockfile and this change edits no dependency manifest. Dependency
  remediation/gate policy is outside this plan, so this is a blocking delivery
  risk for an accepted artifact; do not claim artifact audit pass.
- Most targeted evidence now passes; the final full browser suite, clean
  tracked-checkout setup proof, and branch CI have not all passed on one
  immutable candidate. M5 is not complete.

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
Implementation is in progress; final delivery remains blocked by the production
artifact audit failure, clean-checkout proof and branch CI. Future evidence is
not counted as passed; dependency/gate changes require a separate decision and
re-plan.

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

Implementation progress on 2026-10-06: see the evidence disposition below.
This branch remains In progress and MVP-013/M5 is not accepted.

### Implementation Evidence Disposition — 2026-10-06

Candidate is the working tree on `test/mvp-013-end-to-end-product-loop`, based
on local HEAD `e076e47` / merged `main` `619e1c7`; implementation changes are
uncommitted and no current branch-protection run exists.

| Plan items | Evidence observed | Current state |
| --- | --- | --- |
| P1 joined UI journey | Final `corepack pnpm run test:browser` included the real Chromium scenario against runner-owned PostgreSQL/Mosquitto: UI-created device, correlated telemetry/alert/command, detail/history and reload. Focused P1 also passed from a disposable clean checkout | Passed; 36 browser cases passed with final P1 assertions |
| D1/D2 and T2 telemetry | `corepack pnpm run mqtt:test:integration`: malformed no-write, replay after restart, conflicting ID no-partial-write and registered-tenant MQTT behavior | Passed; all 4 suites passed |
| T1 and T3 | `corepack pnpm run api:test:integration`: real-PostgreSQL symmetric GraphQL/Fiber tenant scope and response-service command/device binding | Passed; all tagged API DB tests passed |
| D3/D4/D5 and browser regression | Full browser suite exercised device-reported failure, 2-minute expiry, lost-response recovery and the joined loop | Passed; 36 Chromium cases passed |
| E1 runner safeguards | `corepack pnpm run test:browser:runner`: unsafe DSN refusal, local rejection of CI-only mode, missing CI DSN refusal, occupied port, partial Compose startup cleanup and SIGTERM cleanup | Passed; 6 node tests passed |
| L1 clean setup | Disposable clone started without ignored env files or build output; `corepack pnpm run setup`, repository policy, `check:fast`, runner safeguards and focused P1 all passed there | Passed for setup and targeted behavior; root full `check` remains blocked by the artifact advisory |
| Static, unit and contract checks | `corepack pnpm run check:fast` and `corepack pnpm run api:vuln` | Passed; Go vuln tool found 0 called vulnerabilities and 3 findings in required modules outside scanned code paths |
| Full local gate | `corepack pnpm run check` passed through `check:fast`, race, build and OpenAPI; `node:audit` stopped on affected `source-map-js@1.2.1` in the exact production artifact | **Failed; blocking for artifact acceptance.** Later checks in the aggregate command did not run on that attempt |

The targeted browser and clean-checkout evidence now pass. Branch CI remains
outstanding, and the full local `check` remains blocked at the artifact advisory.
Independently run any useful checks after disposition; none can override that
gate. Do not resolve the advisory by editing dependencies, filtering reports,
or weakening controls inside MVP-013.
