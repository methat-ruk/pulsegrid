# MVP-012 — Command Console

Status: Planned — revised and author-reviewed; implementation has not started.
The 2026-10-02 request authorizes plan/document work only. Execution awaits a
separate implementation instruction.

Branch: `feat/mvp-012-command-console`

Intended PR: One frontend-command PR, including focused browser-fixture work,
regression evidence and documentation. No backend product-contract change.

Milestone: M4 — Remote command loop

Impact: Material Change (Tier 2) for the proposed tenant-scoped mutation and
asynchronous journey. This episode changes documentation only and uses full
reasoning to review that future implementation.

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

Owner: MVP-012 implementer; reviewed 2026-10-02. These decisions are planned,
not implemented behavior.

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

1. Obtain implementation authorization; recheck base/dependencies, schema,
   lockfile and test capabilities without dependency changes.
2. Add feature client/recovery helpers and input/contract/error tests.
3. Add device-keyed panel, confirmation, explicit states and bounded read
   ownership; prove uncertain/stale/hidden/unmount edges in components.
4. Extend owned receiver/config/budgets; prove real completion, failure,
   ACK-only expiry, lost-response recovery and adjacent regressions.
5. Update docs below; reconcile plan/actual/evidence, fix author-review findings,
   validate final candidate and hand off PR. Stop/re-plan on changed guarantees.
   This planning request does not authorize implementation, merge or deployment.

## Documentation Updates

Planning closeout in this episode:

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

No architecture, confirmation, retry or refresh decision remains open within
this local/test PING boundary. Authorization and implementation evidence remain.

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
interactive QA/advisory audits were not run for this documentation episode;
accepted main CI is baseline evidence. New behavior awaits implementation gates.

## Done Criteria

Planning closeout: decisions, ownership, API/data/security boundaries,
failure/recovery, validation, documentation and acceptance limits are explicit;
revised plan reviewed and document checks passed. Status remains Planned.

Feature closeout: an operator confirms PING, recovers unknown submission with
one logical identity, follows truthful server-owned progress and understands
COMPLETED, explicit FAILED or stored TIMED_OUT without logs. Required real-
boundary, negative, async/cleanup, accessibility and adjacent regression
evidence passes on the reviewed candidate; docs match actual behavior;
applicable CI/approvals/blockers are resolved; merge or owner acceptance is
recorded before completed placement. This does not certify MVP-013's combined
journey or production readiness.
