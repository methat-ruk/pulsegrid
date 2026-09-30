# MVP-009 — Alert Console

Status: Complete — merged as `7e3c336` on 2026-09-29 (PR #19).

Branch: `feat/mvp-009-alert-console`

Intended PR: One frontend alert-console PR

Milestone: M3 — Rules and alerts

Impact: Material Change (Tier 2). This adds a tenant-scoped browser journey
and protected navigation. Extracting the existing GraphQL transport affects
the device console too; the server contract and tenant authority stay intact.

## Goal

Let a development operator discover recent alert occurrences, open one, and
understand its affected device and stored trigger after source history pruning.

## Why

MVP-008 stores immutable alert snapshots, but operators currently need to
query GraphQL directly to discover and investigate them.

## Verified baseline (2026-09-24)

- At review start, `main` and the clean plan branch pointed to `fd0a383`;
  PR #18 merged MVP-008 on
  2026-09-24. MVP-007 was merged as `9ce9e7c`. No alert UI or client exists.
- GraphQL exposes tenant-scoped `alerts(first, after, deviceId)` and
  `alert(id)`. Lists are ordered by `(createdAt DESC, id DESC)`, allow 1–100
  records, and use an opaque cursor bound to the device filter. Unknown and
  foreign alert IDs both return `null`; foreign device filters return empty.
- `AlertOccurrence` snapshots `deviceId`, `ruleId`, `messageId`, `observedAt`,
  `receivedAt`, `temperatureCelsius`, `metric`, `comparator`,
  `thresholdCelsius`, and `createdAt`. It has no device or rule name, severity,
  lifecycle state, or direct telemetry-row link. `device(id)` can enrich one
  detail view with a tenant-visible display name.
- The Nuxt console already has `/devices/:id`, a fixed same-origin GraphQL
  adapter, a client `fetch` helper with a six-second timeout, no-store caching,
  omitted credentials, manual-refresh patterns, and a real browser
  API/PostgreSQL/Mosquitto/simulator harness. Generic transport currently
  lives in the device feature client.
- Existing real PostgreSQL tests prove `GetAlert` survives history-row
  removal; a GraphQL integration test proves `alerts` still returns the
  snapshot after removal. A direct `alert(id)` post-pruning assertion is
  still needed for this route. Pinned Nuxt/Vue/TypeScript,
  Vitest, Playwright, and UI dependencies suffice; no new package, service,
  schema, or environment key is required.

## Scope

1. Add `Alerts` to primary navigation and an `/alerts` route. Show newest
   recorded occurrences with triggering measurement/comparison, recorded
   time, affected device ID linked to `/devices/:id`, and a detail link. Use
   the full device ID because the alert list has no device display name; avoid
   one `device(id)` query per row.
2. Add `/alerts/:id` with the immutable snapshot: device ID/link, rule ID,
   message ID, measurement, metric, comparator, threshold, observed time,
   received time, and recorded time. Make one tenant-scoped `device(id)`
   lookup for the display name; its failure must not hide the snapshot.
   Never read the current rule as the triggering rule state.
3. Add a link on `/devices/:id` to `/alerts?deviceId=<id>`. Resolve the device
   through `device(id)` before showing its name or loading the filtered list.
   Unknown, foreign, and invalid IDs get one non-disclosing unavailable-device
   state. This simple device scope is the only filter in this PR.
4. Use `first: 50`, opaque cursor continuation, and explicit manual Refresh.
   Cover initial loading, empty, failure/retry, refreshing, next-page pending,
   and next-page failure/retry without losing previously loaded rows.
5. Add focused frontend tests and a browser journey from rule creation and
   simulator publication through GraphQL to the alert list and detail.

## Out of Scope

- Rule creation/edit UI, rule detail, notifications, acknowledgement,
  severity, active/resolved lifecycle, escalation, suppression, bulk action,
  arbitrary search, time-range filters, charts, and fleet dashboard counts.
- Telemetry-row navigation, client-side alert evaluation, persistent cache,
  automatic polling/realtime, and optimistic alert state.
- Backend schema/resolver/SQL changes, tenant selector, new permissions or
  identity, production deployment, and production migration.

## Dependencies

- MVP-007 supplies device navigation, UI patterns, and the browser harness.
- Merged MVP-008 supplies the alert snapshot/list/detail contract. Development
  API startup requires migrations `005` and `006` before browser evidence.
- Reuse the current same-origin adapter, fixed development tenant, pinned
  packages, and repository commands.

## Architecture / Boundaries

```text
Alerts nav -> /alerts [optional deviceId URL scope] -> /alerts/:id
Device detail -> /alerts?deviceId=<tenant-visible device ID>
Alert routes -> alert feature client -> feature-neutral GraphQL transport
             -> existing Nuxt same-origin adapter -> Go GraphQL API
             -> tenant-scoped PostgreSQL alert/device reads
```

- PostgreSQL/MVP-008 remains alert authority. The browser owns only route,
  request, pagination, and display state. It does not infer matches from
  telemetry or treat the URL as tenant authority.
- Extract the generic GraphQL request/error/timeout behavior into a small
  feature-neutral frontend module consumed by device and alert clients.
  Keep alert documents/types in an alert feature module and preserve existing
  device-client behavior and proxy safeguards. Add no GraphQL framework or
  global server-state store.
- Reuse client-side route loading. The list fetches only displayed fields;
  detail fetches the full snapshot. One best-effort tenant-scoped device-name
  read follows the alert response. Neither route fetches telemetry or current
  rule state.
- `createdAt` controls list order and is labeled **Recorded**. Label
  `observedAt` **Observed** and `receivedAt` **Received**; late telemetry can
  make Observed earlier than Recorded. Format dates with the existing locale
  pattern and an unavailable fallback. Render the returned finite numbers
  without rounding that could reverse the apparent comparison. Show the
  stored metric/comparator/threshold and measurement in text, without a
  severity color or status badge.
- Treat route IDs, filters, and cursors as untrusted. Pass opaque cursors
  unchanged; encode path/query IDs; render API values as escaped text. The
  server-selected organization and tenant-scoped SQL remain authorization.

## Request and failure behavior

- Initial load or a global/device scope change clears old rows and cursors,
  cancels or ignores older requests, then reaches success, empty, or error.
  A filtered route resolves its device first and never confirms a foreign ID.
- Refresh cancels a pending page, starts at `after: null`, keeps previous
  successful rows visible with **Refreshing alerts**, and replaces rows/cursor
  only on success. Failure preserves the old rows, labels them previously
  loaded, and offers Retry. No automatic retry or polling is implied.
- `Load more` requires `hasNextPage` and a non-null end cursor. Disable and
  guard it during Refresh or another page request. Failure preserves rows and
  cursor for Retry. New head occurrences appear after Refresh; pagination
  does not promise a frozen historical snapshot during concurrent inserts.
- `alert(id) == null` shows one not-found state for unknown and foreign IDs.
  Invalid IDs and service errors receive safe feedback and a list link. A
  failed device-name lookup does not replace a loaded alert with an error.
  Navigation/unmount prevents late responses from overwriting new route state.

## Implementation Direction

1. Extract/regression-test the GraphQL transport without changing same-origin
   POST, timeout/cancellation, error mapping, no-store, or credential policy.
2. Add alert client, list/detail routes, navigation, device entry link, and
   accessible responsive presentation using existing design tokens.
3. Reconcile the actual diff with this plan, self-review changed/impacted
   paths, run final validation, update docs for proven behavior, and hand off
   the PR for independent review when required.

## Validation

- Frontend tests cover query/response behavior, empty/error/not-found,
  global/device scope, refresh replacement and failure preservation, page
  continuation/failure/retry, Refresh versus Load-more races, navigation
  cancellation, device-name failure, exact comparison text, and time labels.
  Existing device-client tests protect the transport extraction.
- Add a focused real PostgreSQL/GraphQL assertion that `alert(id)` returns
  the same snapshot after its history row is removed. This closes the exact
  detail-route contract gap without changing backend behavior.
- Real browser journey creates a visible device and rule, publishes matching
  telemetry through the simulator, Refreshes `/alerts`, follows detail and
  device links, and verifies stored context. Cover direct detail navigation,
  filtered entry, keyboard focus, and 390 px/320 px reflow. A browser fixture
  can exercise missing source history; the existing real PostgreSQL/GraphQL
  pruning test is the authority for actual persistence after pruning. The UI
  must not request the source history row.
- Negative evidence checks unknown/foreign alert and device behavior,
  malformed IDs/cursors, safe errors, and no stale protected content. Reuse
  existing API integration tests for server isolation/cursor rejection and
  add browser checks for the new presentation.
- On the implementation candidate run applicable `web:lint`,
  `web:typecheck`, `web:test`, `browser:typecheck`, `web:build`,
  `test:browser`, `repository-policy`, and final repository `check`/CI gates.
  Inspect list/detail, focus, and narrow layout in a visible browser. Report
  passed, failed, skipped, unavailable, and not-run evidence separately.

## Documentation Updates

- At implementation closeout, document the actual operator alert journey in
  local development, and update README, roadmap, index, and system
  architecture for proven behavior. M3 remains in progress until MVP-009 is
  accepted along with MVP-008.
- Update the UI reference only for a reusable occurrence pattern; its target
  dashboard sketch does not authorize MVP active-alert lifecycle or severity.
  Do not change API/technology decisions unless implementation changes them.
- Move this plan to `completed/` and update inbound links only after outcome
  and evidence are accepted. Keep dated review evidence as history.

## Risks / Open Decisions

- Full device IDs on a global list are less friendly than names. Revisit a
  server projection only if operator evidence shows that ID/link and one
  detail enrichment are insufficient. Avoid client N+1 reads.
- Manual Refresh can leave the list behind new occurrences. Revisit realtime
  only with a measured unattended-update requirement and reviewed lifecycle.
- Stop and re-plan if backend contract, identity/tenant model, dependency,
  persistent cache, automatic delivery, or alert lifecycle must change.
  Production deployment/migration needs separate explicit approval.
- Recovery is a revert of this frontend PR; no alert data or schema changes.
  Revisit if implementation changes that reversibility assumption.

## Done Criteria

An operator can discover a recent occurrence globally or from device
context, inspect its stored trigger and times, identify and open the affected
device, and do so without logs, the current rule, or retained telemetry row.
Loading, empty, error, refresh, pagination, responsive, accessible, and
tenant-safe navigation have proportionate evidence. Plan-to-actual review,
documentation closeout, final gates, and required independent review precede
marking this plan Complete.

## Plan review verdict (2026-09-24)

The original plan left routes, request lifecycle, event-time labels, device
naming, and filtered navigation open. It also assigned actual pruning proof to
a browser case; the existing real PostgreSQL/GraphQL test owns that guarantee.
This revision selects a bounded frontend consumer of merged MVP-008. An alert
panel only on device detail was considered but would not provide discovery
across devices. A server join or realtime transport has no demonstrated MVP
need. No implementation, runtime validation, or PR action occurred in this
review. The remaining assumptions fit the development-only slice; approval
of this reviewed version was the gate before implementation.

The second pass found no remaining plan blocker. The main assumptions are
manual freshness, full device IDs in global rows, and reuse of the existing
development-only tenant/API boundary. Confidence is high in the available
contract and repository fit, with rendered UI and browser behavior still
unproven until implementation. The direct `alert(id)` post-pruning assertion
is a required evidence gap assigned to the implementation PR.

Engineering Improvement Review: current scope includes bounded pagination,
recoverable refresh, stale-response cancellation, non-disclosing filtered
navigation, and the exact detail contract test because omitting them would
leave the new journey ambiguous or fragile. Frontend patterns, UX states, web
security baseline, and test strategy own their design and evidence. Device
name projection and realtime updates remain future enhancements with the
operator-evidence and unattended-update triggers above. This review changes
only the planned frontend slice and its tests.

## Plan-to-actual reconciliation and author review (2026-09-29)

The approved scope was implemented on `feat/mvp-009-alert-console`:

- The shared same-origin GraphQL transport now owns POST, abort/timeout,
  error mapping, no-store, and omitted-credential behavior for both device and
  alert clients. Existing device API operations keep the same request/error
  behavior; no package or server contract changed.
- The alert feature client and `/alerts` / `/alerts/:id` routes consume the
  existing tenant-scoped operations. The list uses 50-row pages, opaque cursor
  continuation, manual refresh, preserved rows on refresh/page failure, and
  route cancellation. It displays device IDs without an N+1 list lookup.
- Detail presents the stored measurement/comparison, rule and message IDs, and
  Observed, Received, and Recorded times. A best-effort device lookup adds its
  display name without making it necessary to render the snapshot. No
  telemetry or current-rule query was added.
- Primary navigation and the device-detail link provide global and
  device-scoped discovery. Invalid, unknown, and foreign device scopes do not
  disclose a device; unknown/foreign alert IDs share the API's null result.
- The API integration test now calls `alert(id)` after deleting the source
  history row and verifies the saved snapshot. The real browser journey uses
  the simulator and database-backed API; focused browser tests cover refresh
  failure, pagination cancellation, unknown IDs, filtered navigation, and
  narrow viewport layout.

Validation passed on this candidate:

- `corepack pnpm run check` — formatting, gqlgen drift, Go modernize,
  staticcheck/vet, frontend lint/typecheck, OpenAPI/AsyncAPI lint, Go and web
  tests, Go race tests, builds, audit, MQTT/API/PostgreSQL integration, and
  all 28 browser tests.
- Visible Playwright QA on the built routes at 1440×900 and 320×844 confirmed
  page identity, alert list/detail rendering, heading focus, the list-to-detail
  interaction, no application console errors/warnings, and no horizontal
  overflow at 320 px. Screenshots are saved under `/private/tmp` for this
  review and are not repository artifacts.
- `govulncheck` found no vulnerable source call paths; it reported three
  module-level advisories in the existing `golang.org/x/crypto` dependency.
  This PR adds no dependency. Nuxt build retains the existing non-blocking
  Rollup annotation warning.

Author self-review found no scope or boundary deviation. The implementation
uses only the merged MVP-008 contract and preserves server tenant authority;
MVP-009 changes no persistence, API, environment key, or service. On
2026-09-29 the project owner accepted the exact published candidate for merge
and will perform the merge. The prior review requested an exact-head re-review;
no separate re-review was performed in this task, and this owner acceptance is
recorded as the disposition. PR #19 remains open and unmerged. M3 remains in
progress until the PR is merged into `main`.

## PR review evidence follow-up (2026-09-29)

The PR #19 review comment identified evidence gaps in visible pagination
recovery, device-scope changes while a request is in flight, failed device-name
enrichment, and exact-candidate keyboard-focus/390 px detail layout. Commit
`06181a90547685f3d7ba6c702c6f1a4ddef44060` adds browser tests for each
requested behavior:

- A failed next-page request keeps the first page visible; Retry sends the
  original opaque cursor and appends the next page on success.
- Changing the device query while the first scoped page is pending clears the
  old scope and the late result cannot replace the new scope.
- A failed detail device lookup leaves the stored device, rule, message,
  measurement, and time snapshot visible with the device-ID link.
- At 390 × 844, the detail heading receives focus, Tab reaches the device link,
  the snapshot uses one column, and the page has no horizontal overflow.

`corepack pnpm run test:browser` passed all 32 tests on that candidate. GitHub
Actions run 36515919848 passed all 14 PR checks on the same commit. A visible
Playwright Chromium pass verified the detail title and content at 1440 × 900
and 390 × 844, heading and Tab focus, one-column reflow, zero horizontal
overflow, and zero browser console/page errors. Screenshots were captured
outside the repository. The exact published head and all required checks are
recorded above. The project owner accepted this candidate for merge and will
perform that action manually; this plan is moved to `completed/` under that
acceptance. PR #19 remains open and unmerged at this documentation update.
