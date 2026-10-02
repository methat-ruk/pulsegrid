# MVP-012 — Command Console

Status: PR #22 review fixes are implemented and locally validated. The active
delivery candidate and hosted check results are tracked on PR #22; this plan
remains under `planned/` until the outcome is accepted. The runtime gate audits
the exact production artifact, including bundled package provenance. The
workspace audit still reports unresolved high `node-forge@1.4.0`; no dependency
remediation is claimed. See [PR #22 review response](#pr-22-review-response)
for finding-by-finding evidence.

Branch: `feat/mvp-012-command-console`

Intended PR: One frontend-command PR, including focused browser-fixture work,
regression evidence and documentation. No backend product-contract change.

Milestone: M4 — Remote command loop

Impact: Material Change (Tier 2) for the tenant-scoped mutation and asynchronous
journey. Preserve full reasoning through implementation and validation.

## Goal

Let a controlled development operator confirm and issue one parameterless
`PING` from registered device detail, recover an uncertain submission without
creating a second logical command, and inspect server-owned progress and
terminal state without consulting logs.

## Why

The backend command loop is merged, but device detail has no command consumer.
The operator must distinguish accepted intent, broker dispatch, device receipt,
completion, explicit failure and absence of a confirmed result. An HTTP success
or elapsed browser timer cannot prove completion.

## Verified baseline (2026-10-02)

| Surface | Evidence | Consequence |
| --- | --- | --- |
| Repository | Initial worktree clean. This branch, local `main`, cached `origin/main`, and live GitHub `main` identify `b209861d7ce204e0e94ab02510ea66c1502d51d5`. [PR #21](https://github.com/methat-ruk/pulsegrid/pull/21) merged 2026-10-01. | Dependencies are accepted; earlier open-PR wording is stale. Recheck base before execution. |
| Command authority | [SDL](../../../../apps/api/graph/schema/command.graphqls), [resolvers](../../../../apps/api/graph/command.resolvers.go), [model](../../../../apps/api/internal/commands/model.go), [repository](../../../../apps/api/internal/commands/repository.go), migrations `007`/`008`. | Reuse PING, six statuses, two failure codes, durable idempotency and immutable two-minute deadline. No API/schema/lifecycle change. |
| Console seam | [Device detail](../../../../apps/web-console/app/pages/devices/[id].vue), [telemetry panel](../../../../apps/web-console/app/components/DeviceTelemetryPanel.vue), [shared client](../../../../apps/web-console/app/features/graphql/client.ts), [proxy](../../../../apps/web-console/server/utils/graphql-proxy.ts). | Add an independent section after successful device load; reuse same-origin fetch, safe errors, abort/sequence guards, SSR shell and design tokens. Client/proxy timeouts are 6 s/5 s. |
| Dependencies | Installed Node `24.20.0`, pnpm `12.3.4`, Go `1.27.1`; resolved Nuxt `4.5.2`, Nuxt UI `4.11.1`, TypeScript `5.9.3`, Vitest `5.0.0`, Playwright `1.63.0`; existing gqlgen/Paho/pgx/Goose and pinned PostgreSQL/Mosquitto. | Existing dependencies suffice; no install, upgrade, library, lockfile, broker image or runtime-topology change selected. Preserve workspace overrides. |
| Test capability | [Browser fixture](../../../../tests/browser/fixtures.ts) enables telemetry but does not explicitly enable command delivery/manage a command receiver. [Runner](../../../../scripts/test-browser.mjs) bounds Playwright to 180 s; [config](../../../../playwright.config.ts) defaults tests to 20 s. | Required fixture work must prove real command outcomes. Two-minute expiry needs a targeted test/runner budget, retaining ordinary test defaults. |
| Evidence | [Main CI](https://github.com/methat-ruk/pulsegrid/actions/runs/36838619395) succeeded on `b209861`, including DB/MQTT/browser/audits. Local existing frontend suite passed 56 tests; targeted command/GraphQL unit suites and `go mod verify` passed. | Baseline confidence only; no new command UI is implemented or proven. |

Related sources: [product scope](../../../product/product-scope.md),
[architecture](../../../architecture/system-architecture.md),
[technology decisions](../../../architecture/technology-decisions.md),
[API guide](../../../api/README.md), [UI reference](../../../design/ui-design/ui-design-system.md),
[local workflow](../../../project-setup/local-development.md), and
[environment policy](../../../project-setup/environment-configuration.md).
`Docs` and `docs` resolve to the same directory here; tracked paths use `docs`.

## Scope

- Device-detail panel: PING confirmation, submission/recovery, one selected
  command's status/timestamps, latest 20 commands with manual refresh/selection.
- Strict validation of the loaded device UUID, fixed type and generated
  idempotency UUID; no editable payload, tenant or deadline.
- Bounded automatic reads for the selected nonterminal command, separate from
  submission/load state and manual recent-history reads.
- Same-tab reload/navigation recovery metadata, duplicate-submit and stale-
  response guards, accessible independent section and responsive presentation.
- Unit/component/browser tests, real command simulator fixtures, targeted
  expiry budgets and operator/developer documentation.

## Out of Scope

Global `/commands` routes/sidebar activation, bulk commands, scheduling,
cancellation, arbitrary payloads, effectful commands, history pagination,
fleet monitoring, realtime transports, global stores/GraphQL caches, production
identity/permissions, backend/SDL/schema changes, new configuration keys,
dependencies, services, deployment, cross-tab coordination, browser-close
recovery and exactly-once MQTT/device effects.

MVP-013 owns clean-checkout acceptance of the combined telemetry -> alert ->
command loop; it does not replace this slice's command browser proof.

## Dependencies

- MVP-003: accepted device list/detail and same-origin GraphQL boundary.
- MVP-010: merged command intent/detail/history and durable key reuse.
- MVP-011: merged dispatch, responses, expiry and command simulator modes.
- MVP-007/009 are adjacent device-detail regressions, not additional features.
- Existing schema `008`, seeded development organization and opt-in command
  mode are local/test prerequisites, not new work in this slice.

## Architecture / Boundaries

```text
loaded device detail -> DeviceCommandPanel -> feature-scoped command client
  -> existing executeGraphQL -> same-origin /api/graphql -> fixed /graphql
  -> server-selected tenant -> commands repository -> committed PostgreSQL intent
existing MVP-011 runtime -> MQTT -> simulator -> validated stored lifecycle
command(id) / deviceCommands -> validated snapshots -> panel presentation
sessionStorage -> recovery metadata only; never lifecycle or tenant authority
```

| Planned owner | Owns | Boundary |
| --- | --- | --- |
| `app/pages/devices/[id].vue` | Mount panel only for the loaded visible device; key it by UUID | No command mutation/timer in page; loading/error/not-found cannot submit |
| `app/components/DeviceCommandPanel.vue` | Local confirmation, submission/recovery, selected command, history, polling/focus/teardown | No MQTT, tenant selector, lifecycle writes or telemetry/alerts refresh coupling |
| `app/features/commands/command-graphql.ts` | Typed operations, strict input/snapshot validation, public error mapping/status helpers | Consume existing SDL; no new transport/cache or business state machine |
| Colocated recovery helper under `features/commands/` | Validate/persist versioned device/key/type/optional command-ID metadata | Browser hints only; no payload, credentials, organization authority or trusted status |
| Existing shared client / Nuxt proxy | Same-origin HTTP, fixed upstream, request bounds/no-store/error translation | Preserve cookie/header/origin behavior; no automatic mutation retry or server-cancellation claim |
| Existing Go commands/runtime | Tenant/device binding, persistence, idempotency, deadline, dispatch, responses and terminal authority | Unchanged; UI cannot manufacture dispatch, ACK, completion, failure or expiry |
| Browser fixtures/runner | Disposable DB/broker/API and per-device receiver lifecycle | Test-only enablement/budgets; no development/production reset or new runtime service |

Direct impact is the panel/client/recovery metadata. Coupled impact is device-
detail mounting and browser startup/cleanup; adjacent impact is telemetry,
alerts, device navigation and readiness. Keep shared transport unchanged;
re-plan before changing its guarantees for a reproduced blocking defect.
Existing Go/API/MQTT regression and generated checks protect those reused
boundaries; this is not a whole-codebase audit.

## Implementation Direction

### Locked decisions and alternatives

Owner: MVP-012 implementer; reviewed 2026-10-02. These decisions govern the
implementation; document verified behavior at feature closeout.

| Decision | Selected approach / accepted trade-off | Alternative / revisit |
| --- | --- | --- |
| Updates | Native section-local bounded polling plus manual refresh; automatic progress for this two-minute journey without a new library/channel. | Manual-only is simpler but leaves unattended progress unclear. Subscriptions/SSE/WebSocket add runtime/contract work without current need. Revisit for measured fleet load or explicit realtime requirement. |
| Confirmation | Inline confirmation names device/display key, harmless parameterless diagnostic PING and asynchronous result; `Send PING` confirms, cancel sends nothing. | Destructive modal/typed phrase overstates the effect. An actuator command needs a separate effect/permission/confirmation decision. |
| Recovery | Before mutation, persist one latest unresolved intent per device in `sessionStorage`; explicit recovery reuses exact key/type/device. | Memory-only loses the key on reload; `localStorage` adds cross-tab/lifetime policy. No retry queue/backend lookup endpoint. Same-tab storage is a recovery aid, not delivery authority. |
| History | Latest 20 newest-first, manual refresh/selection; `hasNextPage` means “older commands are not shown.” Selected IDs read directly even outside this page. | Pagination/global navigation is independently deliverable and outside this slice. |

### Submission and recovery

1. Keep the console's existing English language. Show device identity, PING's
   diagnostic effect and acceptance-versus-completion copy before confirmation.
   Generate one canonical nonzero lowercase UUID per confirmed new intent using
   the browser's UUID capability; fail visibly before sending if unavailable.
   Validate device/type/key without silent coercion.
2. Write/read back a versioned record of at most 1 KiB scoped to current origin,
   device and fixed development context before mutation. Store only device UUID,
   PING, idempotency UUID and optional returned command UUID; reject malformed/
   mismatched records. Unavailable/corrupt storage disables new submission with
   guidance while history stays readable; do not replace an uncertain key.
3. Guard duplicate click/Enter synchronously while submitting. Send exactly
   `{ deviceId, type: PING, idempotencyKey }`; no tenant, payload, deadline,
   status or retry metadata. Backend validation/organization selection remains
   authoritative.
4. Validate a returned snapshot/device binding, persist its ID, show its actual
   status and track if nonterminal. Create may already return a terminal result;
   never force PENDING. A stored terminal result resolves that intent and allows
   a separately confirmed new PING/new key. Metadata cleanup failure is visible
   and preserves safe same-key recovery instead of hiding a stale record.
   Only a snapshot matching this intent's returned/recovered command ID can
   resolve it; selecting another completed history row cannot unlock new intent.
5. Network/proxy timeout, abort, internal error or malformed response means
   unknown submission outcome, not rejection. Preserve metadata, prohibit a
   fresh key and offer `Recover this submission`: repeat the exact create once
   per deliberate recovery. It returns the existing row or creates the original
   intent if never committed; it cannot extend an existing deadline or restart
   a terminal command. No automatic mutation retry exists.
6. `BAD_USER_INPUT` is definite rejection: show actionable validation feedback
   and require correction/reconfirmation. `CONFLICT` preserves the key and stops
   submission for diagnosis; never mint a replacement key to hide it. Unknown
   error codes use safe generic copy.
7. Reload/return restores metadata and performs reads only. Refresh a known ID
   with `command(id)`; an unknown ID requires explicit same-key recovery. Never
   guess identity from the newest history row or mutate on mount/focus/visibility
   change. A known ID returning `null` stays unavailable/unknown and cannot
   automatically unlock a replacement command.
8. A new PING is a new logical command, not “retry the failed command.” Other
   tabs may create independent intents; no global single-command guarantee is
   claimed. An unresolved same-tab intent must be reconciled before replacement.

### State language and safe presentation

Keep UI submitting/loading/refreshing/recovery separate from stored lifecycle:

| Server status | Operator meaning | Terminal? |
| --- | --- | --- |
| `PENDING` | Intent stored; delivery not yet confirmed | No |
| `DISPATCHED` | Broker accepted publish; device receipt not yet confirmed | No |
| `ACKNOWLEDGED` | Device reported receipt; final result pending | No |
| `COMPLETED` | Device reported successful PING completion | Yes |
| `FAILED` | Explicit stored failure; explain the failure code | Yes |
| `TIMED_OUT` | Stored expiry without confirmed terminal device result | Yes |

Map `DEVICE_REPORTED_FAILURE` to device failure and `DELIVERY_FAILED` to delivery
failure. Timeout does not prove absence of a device effect. Display creation,
expiry, dispatch, ACK and terminal times where present, otherwise an explicit
unavailable marker. Use labels plus restrained styling; success styling belongs
only to COMPLETED. Snapshots can skip intermediate states.

Unknown enums, invalid UUID/timestamp fields, inconsistent terminal/failure
fields or wrong-device responses render unavailable/unconfirmed, never success.
Read failure preserves the last valid snapshot with a stale label and last
successful-check time. Escape device names/errors through ordinary text rendering;
do not expose raw errors or render them as HTML.

### Polling, races and teardown

- Track at most one selected command. Read `command(id)` immediately, then 2 s
  after the previous read settles; one selected-command read in flight, no
  interval overlap. History loads initially/manually and refreshes after a valid
  create/terminal result; no history polling. History failure cannot erase a
  valid creation or selected snapshot.
  On entry select a restored known ID, otherwise the newest valid history row
  if present. Row selection starts its own bounded read session. Manual history
  refresh preserves selection; an unresolved submission still blocks replacement
  regardless of the selected row's status.
- Each automatic tracking session permits at most 75 reads or 150 s elapsed
  wall time, whichever comes first. Use monotonic elapsed time. Existing 6 s
  HTTP timeout is separate from the 120 s server lifetime. Budget exhaustion
  clears scheduled work, aborts an in-flight read and invalidates its late result,
  then shows `Tracking paused — result not yet confirmed` plus
  manual refresh/resume. Never set TIMED_OUT from a browser deadline/countdown.
- Stop automatic reads at terminal, missing/invalid snapshot or read error.
  Manual refresh retries a read; explicit resume starts a new bounded session
  for a validated nonterminal command. Guard manual/poll reads against overlap.
  Request generation and device/selection identity ignore stale results;
  `updatedAt` prevents an older snapshot replacing a newer one. Equal-time
  contradictory snapshots require a fresh read, not invented lifecycle order.
- Hidden pages pause timers/abort reads. Visibility return resumes only within
  the remaining budget; exhausted budgets need explicit resume. Navigation/
  unmount clears timers/listeners, aborts reads and invalidates results. Aborting
  create only stops UI waiting; keep metadata because server commit may continue.
- Confirmation/cancel/selection support keyboard and visible focus. Announce
  state changes politely without moving focus each poll. Loading, empty,
  unavailable, stale, unknown, disabled and recovery states have exits. Long
  IDs/names and narrow layout remain readable with existing tokens/reduced motion.
  Command failure never blanks telemetry or alert access.

## Validation

### Required implementation evidence

| Guarantee | Evidence boundary |
| --- | --- |
| Fixed input, typed SDL agreement, strict snapshots/safe code mapping | Command-client unit tests: bad UUID/type/status/time/device/failure combinations and valid intermediate/terminal snapshots. API tests retain contract authority. |
| Confirmation/cancel, duplicate guard, same-key recovery, terminal versus new intent | Nuxt components with controlled promises and keyboard feedback; exact mutation count/variables/key assertions. Selecting an unrelated terminal row cannot resolve uncertain intent. |
| Reload/navigation, corrupt/unavailable storage, known-ID null, no implicit create | Recovery-helper/components plus browser reload after a lost response from a real committed create; same identity on recovery. |
| Bounded/nonoverlapping reads, stale results, hidden/unmounted cleanup and manual restart | Fake-time component tests at polling/budget boundaries, abort/late results across devices/selections. Clock crossing alone cannot change server status. |
| Completion and explicit device failure | Browser -> Nuxt -> Go GraphQL -> real PostgreSQL/Mosquitto -> real simulator `success`/`failure` -> browser terminal display correlated by command ID. No intercepted terminal result. |
| ACK is intermediate; expiry is stored | Real browser ACK-only case waits through the unchanged two-minute server deadline, asserts ACK is not success, then reads stored TIMED_OUT. Existing MQTT silent-expiry proof remains regression; deterministic UI cases cover PENDING/DISPATCHED and both failure codes. |
| Lost HTTP response does not duplicate intent | Forward a real create, deliberately withhold its browser response, recover with the same key and assert one durable row/ID and unchanged expiry. Interception models that HTTP gap only; DB/MQTT/device result remain real. |
| Tenant/device isolation | Existing real-DB GraphQL tests with two organizations: foreign create denied, foreign detail null/history empty and scoped cursors. UI tests reject wrong-device/stale data; disabled controls/storage are not server isolation evidence. |
| Usability and adjacent regressions | Real browser device/telemetry/alerts/readiness suites; keyboard/cancel/recovery, desktop/narrow layout, long IDs and no blocking console errors. Targeted interactive inspection closes unasserted visual/focus gaps. |

### Fixtures and gates

- Explicitly enable existing `PULSEGRID_MQTT_COMMAND_MODE=development` in browser
  runner and API process, retaining telemetry. Start a receiver per uniquely
  registered test device using existing command mode/response selections. Wait
  for `simulator_command_ready` subscription before submission. Stop receivers
  on success/failure/interruption with bounded graceful/forced cleanup.
- Retain isolated Compose project, test credentials and occupied-port refusal.
  Never reset development resources or borrow an unknown DB. Verify owned
  processes/resources stopped after teardown; preserve primary and cleanup
  failures separately.
- Set only the real-expiry case's test/terminal-assertion budget to 160 s; raise
  the runner's entire Playwright bound from 180 s to 360 s. Preserve ordinary
  20 s tests and other startup/teardown bounds. No shortened backend deadline,
  manufactured SQL status, silent skip or global timeout relaxation. Review
  measured runtime if approaching the existing 12-minute CI job budget.
- Iterate with `web:test`, `web:lint`, `web:typecheck`, `browser:typecheck` and
  affected browser evidence. Before PR handoff, reconcile actual diff against
  requirements/plan, self-review/fix, then run repository `pnpm run check` on
  the reviewed candidate: build, generated, race, DB/MQTT/browser and audits.
- Existing CI gates stay authoritative/fail closed. Live protection currently
  requires 14 status contexts, strict base freshness and zero approving PR
  reviews. Do not invent an independent-review blocker; author review is
  required and is not independent review. Recheck protection at handoff and
  preserve any subsequently binding review requirement.
- Prefer the available Browser plugin for required interactive inspection.
  If absent/insufficient, record the gap and obtain authorization for the
  specific localhost automation/computer-use fallback before invocation,
  unless already authorized in that implementation session. Missing required
  evidence leaves validation incomplete.

### Reviewed implementation sequence

1. Recheck base/dependencies, schema, lockfile and test capabilities without
   dependency changes.
2. Add feature client/recovery helpers and input/contract/error tests.
3. Add device-keyed panel, confirmation, explicit states and bounded read
   ownership; prove uncertain/stale/hidden/unmount edges in components.
4. Extend owned receiver/config/budgets; prove real completion, failure,
   ACK-only expiry, lost-response recovery and adjacent regressions.
5. Update docs below; reconcile plan/actual/evidence, fix author-review findings,
   validate final candidate and hand off PR. Stop/re-plan on changed guarantees.
   This request authorizes implementation within this plan; merge/deployment
   remain separate actions.

## Documentation Updates

Planning decisions (completed before this implementation episode):

- Keep this plan in `planned/`; it owns detailed decisions, assumptions,
  evidence selection and implementation/closeout boundaries.
- Correct stale MVP-011/MVP-009 merge wording in README, roadmap/index, API,
  architecture and technology records. Add a superseding merged closeout to
  MVP-011 while preserving its dated candidate/review history.
- Record MVP-012 polling/module boundaries as planned, without claiming UI exists.

Required feature closeout in the implementation PR:

- [Local development](../../../project-setup/local-development.md): console
  PING walkthrough, opt-in prerequisite, simulator modes, same-key recovery and
  browser budget explanation.
- [API guide](../../../api/README.md): link the implemented consumer and explain
  acceptance versus terminal state; retain SDL wire authority.
- [Architecture](../../../architecture/system-architecture.md) and
  [technology decisions](../../../architecture/technology-decisions.md): replace
  planned text with verified behavior/evidence and revisit triggers.
- [UI reference](../../../design/ui-design/ui-design-system.md): only reusable
  async/unknown/stale/confirmation patterns; command semantics stay here/API.
- README/roadmap/index: claim supported capability only. M4 becomes Complete
  after MVP-012 acceptance with required evidence; M5/production remain separate.
- Record candidate SHA, command outcomes, real/substituted boundaries, finding
  dispositions, failed/unavailable/skipped/not-run checks, residual risk and PR.
  Move to `completed/` and fix inbound links only after merge or explicit owner
  acceptance with required evidence; branch/PR existence alone is not completion.

No environment key, SDL/generated code or migration change is planned. Re-plan
and update its canonical docs/examples if one becomes necessary.

## Risks / Open Decisions

No command-console architecture, confirmation, retry or refresh decision
remains open within this local/test PING boundary. The earlier workspace-audit
blocker was replaced by the approved production-artifact gate. PR #22 review
identified four implementation gaps and missing evidence; the fixes and local
evidence are now complete without changing the product contract or claiming
that the workspace `node-forge` finding is fixed. Hosted checks and review
outcome remain tracked on PR #22.

| Assumption / accepted limit | Evidence, falsifier and response |
| --- | --- |
| Diagnostic duplicate-safe PING | Current SDL/model/MVP-011 prove the selected type. Actuator/payload/production-permission demand invalidates this confirmation/retry decision: review a new contract. |
| Stable server-selected development organization during a tab session | Current API/proxy have no browser tenant selector. Do not replay metadata across environment/organization reconfiguration; explicitly reset the local session. Tenant switching needs a separate scoped-session design. |
| Same-tab recovery suffices | Storage survives reload but is not trusted/durable server identity. Closing tab, clearing/corrupting storage or another tab is outside continuity; communicate limits and inspect history before a separately intended PING. Revisit for durable/cross-tab recovery demand. |
| One tracked command / small local history | Existing bounded API/operator scope supports it. Multi-command monitoring or measured poll load requires capacity/transport review, not unbounded polling. |
| Expiry fits fixture budgets | Backend lifetime is 120 s; 160 s case / 360 s runner are proposed budgets, not measured MVP-012 runtime. Measure on candidate; exceeding them requires fixture/budget review without weakened assertions or shorter server deadline. |
| Delivery can be disabled/unavailable | GraphQL has no runtime capability flag. Do not infer mode/online state from readiness or telemetry. Render truthful stored pending/unknown and document opt-in; no synthesized expiry when scanner is disabled. |

Owner: MVP-012 implementer; product owner decides changed acceptance scope.
Revisit on a named falsifier or before leaving loopback development/test.

Containment: revert/remove panel/client and test-only fixture changes, preserving
accepted command rows/migrations. Stopping UI tracking cannot cancel committed
commands. Inspect unresolved submissions before clearing metadata. No schema
rollback, production operation or identity/permission change is in scope.

## Engineering Improvement Review

Current scope: same-key unknown-outcome recovery, bounded polling, stale-response
protection, readable state/timestamps and receiver cleanup make the existing
contract usable and testable. Latest-20 history adds reload context without
new navigation. Owners/evidence are mapped above.

Future enhancements: paginated/global history, production permissions,
effectful-command approval, cross-tab recovery and realtime fleet monitoring
need separate acceptance boundaries. No global store, generalized retry layer
or new infrastructure is justified here.

## Plan review and planning closeout (2026-10-02)

Initial major findings: retry/new-intent identity unspecified; refresh had no
bounds/teardown or distinction from server expiry; confirmation/input did not
name PING's effect; browser outcomes lacked command-enabled receiver/expiry
budget. Minor drift: merged PR #21 was labelled open and a roadmap paragraph
still called merged PR #19 pending.

Revised author review traced confirm -> persist key -> create -> commit ->
existing MQTT/device -> stored read -> visible result, then challenged lost
response/reload, skipped milestones, invalid/null snapshots, clock crossing,
selection/route races, hidden cleanup, duplicate interaction, disabled command
mode, tenant/device scope, fixtures and closeout. The second pass clarified
that create can already return terminal state, history failure cannot undo a
valid create, and recovery cannot guess identity or mutate on mount. Final
review additionally bound intent resolution to its own command ID and made
budget exhaustion abort/invalidate an outstanding poll rather than merely
prevent scheduling another read.
Manual-only refresh and memory-only recovery were considered; the selected
bounded alternatives satisfy async progress and logical-command continuity.

Verdict: ready for implementation authorization; no unresolved design blocker
within the stated scope. This is author plan review, not independent code/PR
review, implementation permission or feature-completion evidence. Contract/
boundary confidence is high from source/accepted dependencies; new usability
and fixture timing remain unverified until implementation.

Planning evidence: live PR/main/CI/protection, installed dependency inventory,
selected code/test traces, 56 frontend tests, targeted command/GraphQL units,
module verification, repository policy, final diff/required-section/local-link
checks. The Go shim initially hit sandbox cache permissions; the pinned binary
succeeded with temporary writable build cache and existing module cache. No
dependencies were installed/changed. Fresh local build/race/DB/MQTT/browser/
interactive QA/advisory audits were not run for that documentation episode;
accepted main CI was baseline evidence. The implementation evidence below
supersedes that planning-only limit where applicable.

## Implementation evidence and handoff state (2026-10-02)

The user authorized implementation of this reviewed plan on 2026-10-02. The
command-console candidate is committed on `feat/mvp-012-command-console` as
`27b18ca` (`feat(web): add device command console`), after the earlier planning
commit `216af3e`. The dependency-audit remediation is a separate, user-authorized
scope extension dated 2026-10-02. No API schema or Go behavior changed.

The device-detail panel and command feature client now validate canonical
identifiers/snapshots, confirm a diagnostic PING, persist same-tab idempotency
metadata before mutation, recover uncertain submission with the same key,
render stored lifecycle/timestamps, refresh bounded history and track one
selected nonterminal command with bounded polling. Recovery data never owns
tenant/lifecycle authority. Browser fixtures enable the existing command
runtime only in the isolated test process and stop real per-device receivers.
Documentation now describes the local operator path and reusable async UI
states. No production runtime was mutated.

| Check | Result and evidence boundary |
| --- | --- |
| Frontend and component behavior | Passed: `web:test`, 73 tests across 12 files. Covers request/snapshot validation, recovery metadata, explicit confirmation, exact-key retry, intent scoping, intermediate states, history-vs-detail freshness, sequential polling, budget exhaustion and unmount cleanup. |
| Static and source checks | Passed on the reviewed working tree: frontend lint, Nuxt/TypeScript/test typechecks, browser fixture typecheck, repository policy and `git diff --check`. |
| Browser and user journey | Passed after the dependency update: `test:browser`, all 35 Chromium cases. Command cases exercise actual browser → Nuxt proxy → GraphQL → PostgreSQL/Mosquitto → simulator success/failure, lost-response recovery after reload with one stored command ID, ACK-only → server-recorded timeout (~2.0 min), keyboard confirm/cancel, a long device key at 320 px, no horizontal overflow and no page/console errors. The prior implementation screenshot was visually inspected. The runner removed its isolated Compose resources. |
| API/database integration | Passed for command-console commit `27b18ca`: `api:test:integration`, including migrations/schema behavior and integration-tagged race tests against disposable PostgreSQL; owned containers/volume were removed. Not rerun after the dependency-only update; API/Go sources and contracts did not change. |
| MQTT integration | Passed for command-console commit `27b18ca`: `mqtt:test:integration`, all core, deadlines, broker-outage expiry and forced-shutdown suites against disposable PostgreSQL/Mosquitto. The deadline cases ran concurrently (~119 s); outage expiry passed (~127 s); runner removed owned resources. Not rerun after the dependency-only update; MQTT sources and runtime did not change. |
| Nuxt/Nitro production runtime | Passed after the dependency update: `web:build` generated Nuxt 4.5.2/Nitro 2.13.4 with the existing `node-server` preset. `.output/server/package.json` lists `devalue@5.9.4` and has no `listhen`/`node-forge`; generated server files contain no references to either package. The known generated Rollup annotation warning remains non-fatal. |
| Dependency security regression | Passed focused Node reproduction using a pooled 512-byte backing buffer with a secret canary outside a 2-byte view. `stringify`, `stringifyAsync`, and `uneval` emitted only the visible bytes; `stringify`/`stringifyAsync` parse round-tripped `[65, 65]`, `uneval` evaluated to the same values, and malformed `__proto__` parsing was rejected. |
| Frozen dependency install | Passed: `pnpm install --frozen-lockfile` selected `devalue@5.9.4`; only the devalue lock entry/snapshots changed. The install's Husky prepare hook could not write `.git/config` under the workspace's Git metadata permissions, but the install exited successfully and subsequent checks ran. |
| Patch candidate review | A read-only review found no concrete compatibility regression or audit bypass. The review confirmed both Nuxt parent ranges accept `5.9.4`, the production audit remains unchanged/fail-closed, and the generated Nitro artifact omits `listhen`/`node-forge`. This was a focused candidate review, not an independent PR review. |
| Go vulnerability check | Passed separately: govulncheck reports no vulnerable imported/called code paths; three required modules were reported as not apparently called by code. |
| Production dependency audit | **Failed required gate after remediation.** `pnpm run node:audit` and `pnpm audit --prod --json` report one high advisory: `node-forge@1.4.0` via `listhen`; all six `devalue` advisories are gone. npm currently publishes no `node-forge` version after `1.4.0`; the verified GitHub advisory lists no patched release and upstream PR [#1152](https://github.com/digitalbazaar/forge/pull/1152) remains open. The Nuxt CLI/Nitro dependency graph remains subject to the project gate even though the inspected production artifact excludes the affected verification path. No finding was suppressed or waived. |

### Authorized dependency-audit scope extension (2026-10-02)

The user authorized resolving the production dependency audit blocker before
PR handoff. The extension is limited to published, same-major compatible
security patches and the smallest necessary lockfile changes. Do not change
Nuxt/Nitro architecture or majors, reclassify dependencies, alter audit policy,
or consume an unmerged dependency patch without a new review and re-plan.

| Advisory group | Root cause and path | Exposure assessment | Decision |
| --- | --- | --- | --- |
| Six `devalue@5.9.2` advisories: GHSA-j22f-vq7h-c4qm, GHSA-hx4r-w6wj-j8fg, GHSA-mcm9-63f2-9j32, GHSA-wf3x-273g-mvxv, GHSA-x5rw-q4pp-hg5g, GHSA-4q55-j62x-fr9h | Nuxt and `@nuxt/nitro-server` resolve the same `devalue` package. Nitro serializes SSR payload/config with `stringify` and `uneval` in `@nuxt/nitro-server/dist/runtime/utils/renderer/payload.mjs`; Nuxt reads serialized payloads with `parse` in `nuxt/dist/app/composables/payload.js`. The advisories cover Buffer backing-memory disclosure, `uneval` sparse/repeated-value amplification, client allocation from generated sparse arrays, an unhandled rejection in `stringifyAsync`, and malformed `__proto__` keys in `parse`. `stringifyAsync` is not called by the inspected Nuxt/Nitro paths. | SSR serialization and client parsing are part of the framework runtime. The current app does not establish every advisory-specific hostile input, but the framework exposes the affected serialization/parser operations and the registry provides a compatible patch. | Resolve the existing `^5.9.0` range to published `devalue@5.9.4` in the lockfile. Advisory fixes begin at `5.9.3`; `5.9.4` is the latest compatible patch selected by pnpm and adds no manifest override or framework upgrade. |
| `node-forge@1.4.0`, [GHSA-86w9-cpqp-85rv](https://github.com/advisories/GHSA-86w9-cpqp-85rv) / CVE-2026-85393 | `nuxt -> @nuxt/cli -> listhen -> node-forge` and `nuxt -> @nuxt/nitro-server -> nitropack -> listhen -> node-forge`. The vulnerable sink is RSA PKCS#1 v1.5 signature verification. The installed `listhen` callsites use Forge to parse configured certificates/keystores, generate keys/certificates, and sign generated certificates; the inspected paths do not call signature verification. PulseGrid's generated production output uses Nitro's native Node HTTP(S) server and excludes `listhen` and `node-forge`. | The advisory is real and the vulnerable API is present in the dependency, but no call to that API was found in the app, `listhen`, or generated production artifact. The production-only audit still reports it because Nuxt is in the app dependency graph. npm lists `1.4.0` as latest and GitHub lists no patched release; upstream PR [#1152](https://github.com/digitalbazaar/forge/pull/1152) is open. | Do not suppress the finding, reclassify Nuxt to change audit scope, or resolve to an unmerged contributor patch. Under the former workspace-wide required gate this finding blocked handoff; that disposition was superseded by the approved production-artifact gate. Keep the workspace finding visible and unresolved. The current gate blocks if an affected `node-forge` version appears in the verified artifact; this boundary change does not claim a vulnerability fix. `listhen`'s existing `^1.4.0` range can accept a future compatible patch, so re-resolve the lockfile then. |

The `devalue` lockfile update is reversible by restoring the lockfile to
checkpoint `27b18ca`; rollback would restore the six published advisories, so
keep the patch unless validation finds a compatibility regression. After the
update, web tests (73), lint, Nuxt/test/browser typechecks, repository policy,
the production build, the security regression, and all 35 Chromium browser
tests passed. The required audit remains failed on the single `node-forge`
advisory. The inspected package callsites do not reach RSA signature
verification, and the generated production artifact omits Forge, but this
exposure assessment does not clear the audit gate. Do not represent it as a
pass by changing scanner scope, dependency classification, or policy. Revisit
after upstream publishes a patched `node-forge` and the existing transitive
ranges resolve it.

At this historical checkpoint, the workspace audit finding still blocked
handoff. The later approved artifact gate changed the required evidence
boundary while preserving the unresolved workspace finding; see Re-plan 3 and
the current review response below. No production deployment or readiness is
implied by that checkpoint.

### Re-plan 1: audit the supported Nitro runtime artifact (approved 2026-10-02; implementation paused)

The previous workspace audit blocks on `node-forge@1.4.0` because the root app
manifest lists `nuxt` as a production dependency. The production build is the
Nuxt/Nitro `node-server` preset. Its generated `.output/server/package.json`
lists the external runtime dependencies, including patched `devalue@5.9.4`,
and does not list Nuxt, `listhen`, or `node-forge`. The official Nuxt v4 Node
deployment contract runs `node .output/server/index.mjs`; the official minimal
Nuxt v4 package example still lists `nuxt` under `dependencies`. Keep that
supported workspace convention and audit the generated runtime graph as a
separate, isolated artifact boundary. See [Nuxt Node deployment](https://nuxt.com/docs/4.x/getting-started/deployment),
[Nuxt package guidance](https://nuxt.com/docs/4.x/directory-structure/package),
and the [node-forge advisory](https://github.com/advisories/GHSA-86w9-cpqp-85rv).

Current registry checks on 2026-10-02 find no published patched dependency
route: `node-forge` latest is `1.4.0`; Nuxt `4.5.2`, `@nuxt/cli` `3.37.0`,
Nitro `2.13.4`, and Listhen `1.10.1` are also their current latest versions.
Listhen `1.10.1` still declares `node-forge: ^1.4.0`. The advisory lists no
patched release, and upstream [PR #1152](https://github.com/digitalbazaar/forge/pull/1152)
remains open. A same-major update or override cannot select a published fixed
version today.

| Option | Impact / disposition |
| --- | --- |
| Wait for an official patched `node-forge`, Listhen, Nuxt, or Nitro release | Smallest future dependency fix: resolve the existing compatible range and lock it, then rerun the current gate and runtime regression checks. It does not unblock MVP-012 now. |
| Consume upstream PR #1152, a fork, or a locally patched package | Rejected: the fix is unmerged and unpublished, package provenance/review is unresolved, and the user prohibits using it to pass validation. |
| Upgrade Nuxt/Nitro to a major or replace their listener architecture | Rejected for this plan: no newer published parent version fixes the path; changing the framework/runtime adds unnecessary compatibility and maintenance risk. Re-plan separately if a supported parent release requires it. |
| Audit the exact generated Nitro production runtime package | **Selected and approved.** Keep the current Nuxt/Nitro versions and workspace dependency classifications. Build `.output` with the `node-server` preset and stage the complete output outside the workspace. Use its generated `server/package.json` to create a lock with the pinned pnpm version, install that locked runtime closure, run a fail-closed `pnpm audit --prod`, and smoke the same staged artifact with those exact installed dependencies. The workspace advisory remains visible as a separate report and is not described as fixed or as a clean workspace audit. Only the artifact-scoped production audit can satisfy the production runtime gate. |

An isolated feasibility check already copied the generated Nitro runtime
manifest to a temporary directory, generated a temporary pnpm lock, and ran
`pnpm audit --prod`: it returned zero advisories. The temporary directory was
removed. This proves the audit target is technically usable; it does not prove
the staged runtime can start, that CI passes the same artifact, or that a
production installation uses this package boundary.

This is a **Material Change (Tier 2)** to the required audit graph and CI
ordering, rather than a package-version remediation. The user approved the
artifact-scoped boundary on 2026-10-02. To preserve fail-closed behavior, the
implementation must:

- build and pass the exact complete `.output` artifact from the audited
  revision into the audit job; missing or stale output is a failure
- reject an unexpected Nitro preset or malformed/missing runtime manifest
- create and retain the isolated runtime lock with the audited artifact; run
  the audit and production smoke using that same locked dependency tree
- fail if the artifact runtime audit reports any moderate-or-higher advisory,
  package installation fails, or the production smoke fails
- emit the workspace `pnpm audit --prod` result separately, retain the
  `node-forge` advisory and its dependency path, and state that the workspace
  advisory remains unresolved; do not label that workspace scan clean or
  suppress its output
- fail if the artifact manifest, lock/install, runtime audit, dependency-tree
  inspection, or production smoke is incomplete; the artifact audit must not
  pass by falling back to the root workspace graph
- revisit the disposition if `listhen`/`node-forge` enters the generated
  artifact, a vulnerable verification call becomes reachable, or the actual
  production start/install contract changes

The approved change touches the Node audit runner, `node-dependency-audit` CI
job, and local audit instructions. It does not require changing
`package.json`, `pnpm-lock.yaml`, Nuxt/Nitro versions, production app code, or
the supported Node server entry point. Rollback is to restore the
workspace-based audit job/script; the production gate will then fail on the
visible `node-forge` workspace finding. The workspace scan remains independent
evidence after this change and continues to report `node-forge` while that
published advisory applies. At the time of Re-plan 1, MVP-012 remained Blocked
pending proof of the artifact-scoped gate, install contract and runtime smoke;
that historical disposition was superseded by Re-plan 3. MVP-013/M5 remains
planned behind M4.

**Approval record:** the user approved implementation of this exact
artifact-scoped audit boundary on 2026-10-02, with no suppression, no
unmerged patch, and no dependency-classification changes. This approval does
not authorize changing the production deployment artifact or its install/start
contract; if evidence requires either change, stop and re-plan.

### Re-plan 2: preserve the frozen build dependency resolutions (approved 2026-10-02)

Implementation of re-plan 1 is paused. An end-to-end probe that generated a
new lock from `.output/server/package.json` passed its own audit and HTTP smoke,
but its installed graph did not match the graph used by the frozen workspace
build: it selected `@antfu/install-pkg@2.1.0` instead of `2.0.1`,
`nanoid@3.3.19` instead of `3.3.18`, and `unplugin@3.4.0` instead of the
workspace's `3.3.0`/`2.3.11` resolutions. That zero-advisory result is not
valid evidence for this gate and must not be used to pass it.

A temporary feasibility probe appeared to seed an isolated runtime-package
workspace from the repository's `pnpm-lock.yaml`, audit it, and return HTTP
200. Later direct-resolution and filesystem checks invalidated that result.
The 30 package name/version entries appeared in the workspace production
graph, but that did not prove their installed peer-context snapshots or package
contents existed.

That probe is not yet proof of an identical full graph. A follow-up comparison
found different peer-context snapshot keys for `unhead@3.4.0` and
`vue@3.5.42` between the runtime-only importer and the full workspace importer.
Those may reflect build-only peers absent from the Nitro runtime manifest, but
they must be reconciled against the intended production install contract before
this becomes a required gate. The repository has no production deploy/container
workflow today (`OPS-001` remains deferred), so the verified target is the
supported Nitro Node-server output, not an unconfigured deployment platform.

| Option | Impact / disposition |
| --- | --- |
| Keep resolving the generated runtime manifest against current registry ranges | Rejected: the installed tree drifted from the build lock for three packages, so its audit/smoke result did not prove the dependency set under review. |
| Seed an isolated runtime-package lock from the audited revision's frozen workspace lock | **Selected and approved, but implementation stopped at the proof gate.** Transfer the exact complete `.output` tar from `web-build`; construct a temporary `runtime` package around the unchanged output; copy its Nitro server manifest to that staging package root; seed lock generation from the same revision's `pnpm-lock.yaml` and existing workspace overrides/build-script policy; prove a frozen install, artifact-package-scoped audit, and smoke all use that staged tree. Preserve a separate raw workspace audit report showing `node-forge`. |
| Put a new lockfile or a different package manifest into the shipped `.output`, change dependency classification, or alter the deployment start/install contract | Rejected for this re-plan: each changes the deployment artifact or a key assumption and requires a separate plan/approval. |

Before this revised gate can pass, implementation must validate the exact
runtime importer and every installed package resolution against the source
lock, including integrity and peer-context identity. It must explicitly
resolve the observed `unhead` and `vue` peer-context differences; name/version
membership alone is insufficient. Any runtime resolution not derivable from
the frozen source lock, or any required install/build-script behavior that
could not be reproduced with the repository's policy, kept MVP-012 Blocked at
the Re-plan 2 checkpoint and required another re-plan. That status was
superseded by Re-plan 3. CI must continue to pass the exact same-run build
artifact, fail on missing/stale output, preserve the original `.output`, and
use the same installed tree for audit and smoke. The workspace report must
remain separate and must state that the `node-forge` advisory remains in the
workspace dependency graph.

The user approved this revised lock provenance and staging layout on
2026-10-02. The earlier fresh-lock implementation attempt was reverted after
it selected transitive versions outside the build graph; no result from that
attempt is counted as gate evidence. Re-plan 2 was then implemented far enough
to test frozen staging, but the peer-context proof below failed. No result from
the earlier audit-zero/HTTP-200 probe is counted as gate evidence.

### Re-plan 3: standalone Nitro contract and artifact-gate implementation (approved 2026-10-02)

The failed Re-plan 2 staging experiments were not the production install
contract. The isolated package importer tried to install a new peer-resolved
tree, although Nitro already places the required package files inside the
production output. The default frozen install left dangling `unhead` and `vue`
links. Setting `autoInstallPeers: false` instead retained build peer contexts
and added `vite`/`typescript` to the 145-package staged graph. Neither staging
tree is used as evidence for the current contract.

#### Verified production contract

| Contract item | Evidence |
| --- | --- |
| Runtime target and command | Repository build and fresh artifact metadata select Nuxt 4.5.2 with Nitro 2.13.4 `node-server`; the supported command is `NODE_ENV=production node .output/server/index.mjs`. Nuxt and Nitro document copying the standalone `.output` directory and running Node; they prescribe no runtime package manager, install command, or runtime lock for this preset ([Nuxt deployment](https://nuxt.com/docs/4.x/getting-started/deployment), [Nitro Node runtime](https://nitro.build/deploy/runtimes/node)). |
| Runtime package tree | The generated `.output/server/package.json` has 21 exact-version dependencies and no `packageManager` or lockfile. `.output/server/node_modules` contains exactly those 21 package directories as pruned files inside the artifact; all 145 non-`package.json` files match the frozen-install package contents byte-for-byte. Running a byte-identical `.output` copy from a temporary directory with no workspace or parent `node_modules` returned HTTP 200 for `/`; the artifact hash was unchanged by the smoke. Nitro prunes package exports, so verification must resolve emitted import specifiers rather than assume every manifest package root is independently importable. |
| Build package manager and lock provenance | The repository pins Node `24.20.0` and pnpm `12.3.4`. `web-build` runs `corepack pnpm install --frozen-lockfile`, then `corepack pnpm run web:build`; that script runs `nuxt build` in production mode. The project lock document in `pnpm-lock.yaml` is the second YAML document and contains the `apps/web-console` importer; the first document records pnpm's environment packages. This two-document distinction is part of pnpm's documented lock format ([pnpm lockfile](https://pnpm.io/lockfile)). |
| Package resolution and integrity | All 21 vendored name/version pairs have resolution-integrity entries in the frozen project lock and appear in the workspace production graph. Nitro's 145 non-`package.json` package files match the corresponding frozen-install package files byte-for-byte. The generated manifest and physical package inventory match 21/21. |
| Runtime edges | A static traversal from `.output/server/index.mjs`, including literal dynamic imports, reached 154 JavaScript module files and found 23 package-to-package edges. All 23 edges and all 21 runtime nodes are present in the frozen workspace production graph; no emitted import was unresolved and no non-literal dynamic import or `require` was found. |
| Optional peers | The frozen workspace contexts bind `unhead@3.4.0` to optional peer `vite` and `vue@3.5.42` to optional peer `typescript`. Their package metadata marks both peers optional. Nitro's vendored output contains neither provider; the emitted runtime import graph has no edge to them and the isolated server smoke passes. The runtime graph is therefore the exact packaged graph, not a peerless pnpm reconstruction or the larger workspace graph. |
| Production advisory report | The current raw workspace `pnpm audit --prod` remains failed with one high `node-forge@1.4.0` advisory through `nuxt > @nuxt/cli > listhen` and `nuxt > @nuxt/nitro-server > nitropack > listhen`. Neither `node-forge` nor `listhen` is in the 21-package Nitro artifact. Intersecting the workspace audit findings with the verified artifact package/version inventory currently yields no matching advisory; this is a scope-projection result, not a required-gate pass. |

The repo has no provider-specific deployment workflow or container image
(`OPS-001` remains deferred). That does not leave the Node-server runtime
install contract open: the selected production output is the complete Nitro
`.output` tree and the deployed process starts it directly with Node. No
package-manager install runs at runtime; pnpm and the frozen root lock govern
build-time dependency resolution only. There is no runtime lockfile or install
option to reconstruct.

#### Required-gate implementation

The approved change keeps Nuxt's standard standalone Node-server contract:
`apps/web-console/.output` is built once by `web-build`, and its complete
contents plus a lock-backed evidence sidecar are uploaded as one same-run
workflow artifact. `node-dependency-audit` depends on `web-build`, downloads
that artifact, and does not install packages, rebuild `.output`, or construct a
second dependency tree. It verifies the source commit/run, frozen-lock hash,
artifact hash, Nitro manifest and physical package inventory, package
resolution integrity and source file content, emitted runtime imports, and
peer dispositions. It then runs the production smoke directly from the
downloaded `.output` and checks that the artifact hash is unchanged.

The build evidence is generated from the frozen production importer and
contains the complete static module traversal. It matched all 21 artifact
packages to the frozen production graph and their lock integrity entries. The
emitted traversal reached 154 JavaScript modules and verified all 23
package-to-package edges against that graph. `unhead@3.4.0`'s optional `vite`
peer and `vue@3.5.42`'s optional `typescript` peer retain their frozen workspace
contexts; both are absent from the artifact and have no emitted runtime edge.
All 145 non-`package.json` files across the vendored packages match the frozen
source package contents. Package identity and dependency/peer metadata match
the selected frozen package instances separately.

The required job runs `pnpm audit --prod` without installing or reconstructing
dependencies. It keeps the full workspace JSON report as a separate uploaded
artifact and projects each finding onto the verified runtime package/version
inventory. Any affected artifact version blocks, regardless of severity. The
gate also fails when artifact metadata or provenance is unavailable or
mismatched, a package or lock integrity cannot be proven, an import is
unresolved or absent from the frozen graph, peer disposition changes,
`node-forge`/`listhen` enters the artifact, or the production smoke fails. The
required GitHub status context remains `node-dependency-audit`.

The workspace audit continues to report high `node-forge@1.4.0` through both
`nuxt > @nuxt/cli > listhen > node-forge` and
`nuxt > @nuxt/nitro-server > nitropack > listhen > node-forge`. The exact
artifact inventory contains neither `node-forge` nor `listhen`, so the current
workspace finding does not match the production artifact. The finding remains
visible and unresolved; this gate change does not suppress it, alter dependency
classification, or claim that the vulnerability was fixed.

#### Re-plan 3 implementation checkpoint (historical)

The 2026-10-02 Re-plan 3 checkpoint passed its local build, artifact smoke,
workspace audit, and then-eight gate tests. Its artifact hash and 73-test count
refer to that earlier candidate and are superseded by the post-review evidence
below. At that checkpoint, MVP-012 was unblocked for PR handoff with hosted
checks pending; the later PR #22 review requested changes. MVP-013 remains
planned behind M4.

### PR #22 review response (2026-10-02)

The published review comment on PR #22 ([findings and requested evidence](https://github.com/methat-ruk/pulsegrid/pull/22#issuecomment-5948444989))
reviewed original head `e87d731be0bf0ca7feb3f47db398b146ba5dc53e` against
base `b209861d7ce204e0e94ab02510ea66c1502d51d5` and requested changes for
F1–F4. The changes below address that comment; this is author remediation, not
a claim of a new independent review.

| Finding / gap | Fix and evidence |
| --- | --- |
| F1 — bundled runtime packages were absent from the physical `server/node_modules` inventory | Nitro's `rollup:before` hook writes `.runtime-bundle-provenance.json` beside `.output`, outside the deployable tree. It records normalized module identities for every emitted chunk. Evidence preparation reconciles those IDs and every `.output` source map with exact package/store-key contexts from the frozen `pnpm list --prod` graph, then verifies lock integrity/snapshot and package source-file hashes. The gate consumes the same-run `.output` plus this sidecar, rechecks the 19 chunk/map pairs and inventories, and matches advisories against physical and bundled package/version sets. The production build proved 21 physical packages, 54 bundled package contexts across 126 Rollup modules, and 19 source maps. It detected `h3@1.15.11`, `destr@2.0.5`, `defu@6.1.7`, `ofetch@1.5.1`, `hookable@5.5.3` and `@iconify-json/lucide@1.2.130`. Linux CI exposed that Nuxt can name its generated `.nuxt/nuxt-icon-server-bundle.mjs` source directly in a map; the provenance collector now accepts it only when the resolved path exactly matches an application module identity recorded for that same Rollup chunk. A focused regression proves the matching path is accepted and a different generated path fails closed. A synthetic advisory for bundled-only `h3@1.15.11` blocks the gate; missing sidecar/map and mismatched provenance tests also fail closed. The actual artifact audit passed with no matching advisory. |
| F2 — failed direct lookup of a saved command ID exposed no retry when it was outside the latest 20 rows | Selected-command loading/error/retry presentation is independent of history. A regression seeds a known ID absent from latest history, fails the first direct read, then retries and verifies the saved command becomes visible without a new submission. |
| F3 — initial history failure hid a successful selected-command read | The selected-command card now renders independently of `historyState`. A regression makes initial history fail while a recovered direct read returns a terminal snapshot, then verifies the command ID and status remain visible alongside the separate history error. |
| F4 — storage failure while recording a pending command ID showed terminal-cleanup copy | The write/update failure remains in the submission-recovery alert. Separate regressions prove that a pending result never shows terminal cleanup copy and that cleanup retry appears only after a known terminal result and failed storage removal. |
| Failure combinations in F1–F4 | The runtime gate suite passes 13 tests, including a bundled-only advisory, missing/inconsistent evidence, mismatched generated-source attribution, smoke failure and artifact mutation. The panel suite passes 80 tests across 12 files. |
| Visibility and stale-result evidence | Component regressions prove hidden-tab polling pauses and resumes, an older read cannot replace a newly selected command, and a read for the previous device cannot replace the new device selection. |
| Visual/focus evidence | All 35 Chromium browser cases pass. The command journey verified page title/route, no Vite error overlay, keyboard focus and Enter-driven confirmation/cancel, server-recorded completion/failure, and no page/console errors. Desktop and 320px mobile views have no horizontal overflow. Confirmation-focus, desktop and mobile screenshots are captured; the workflow retains them as a successful-run artifact. |
| Status/documentation conflict | The active roadmap, architecture, local audit instructions, and plan describe the artifact-scoped gate and keep the workspace `node-forge` finding separate. Earlier Blocked/unblocked dispositions are labeled as historical. This plan remains under `planned/` until the PR outcome is accepted. |

The final local production build produced artifact SHA-256
`3a5281df3217d06fee32a2f4693e8e614059c5167e70564a328539a96b4205be`. Local
`node:audit` passed, smoked that artifact with HTTP 200, and reported no
runtime-artifact advisory. Its workspace report still reports high
`node-forge@1.4.0` through both Nuxt/Listhen paths. This gate change proves the
current finding does not match this production artifact; it does not fix or
suppress the workspace vulnerability.

The first hosted check run, [36994171786](https://github.com/methat-ruk/pulsegrid/actions/runs/36994171786),
failed `web-build` because the Linux-generated source-map path did not match
the macOS cache-prefixed path. The dependent artifact audit then failed closed
because the build had not uploaded an artifact. The exact-source identity check
and regression above address that build blocker; hosted checks must be rerun on
the new head before calling the candidate ready for merge review.

Other local checks passed: `web:test` (80 tests), `web:lint`, `web:typecheck`
(including test types), `browser:typecheck`, repository policy, 13 runtime-gate
tests, and `git diff --check`. Do not merge or move this plan to `completed/`
in this task.

## Done Criteria

Planning closeout: decisions, ownership, API/data/security boundaries,
failure/recovery, validation, documentation and acceptance limits are explicit;
revised plan was reviewed and document checks passed.

Feature closeout: an operator confirms PING, recovers unknown submission with
one logical identity, follows truthful server-owned progress and understands
COMPLETED, explicit FAILED or stored TIMED_OUT without logs. Required real-
boundary, negative, async/cleanup, accessibility and adjacent regression
evidence passes on the reviewed candidate; docs match actual behavior. The
required dependency-audit gate and any later applicable approvals/blockers must
be resolved before PR handoff/merge and completed placement. This does not
certify MVP-013's combined journey or production readiness.
