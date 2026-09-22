# MVP-007 — Telemetry and Device-State Console

Status: Planned

Review state: Reconciled on 2026-09-22 against merged MVP-003 and MVP-006,
the current Nuxt device-detail route and feature-scoped GraphQL client, the
delivered telemetry GraphQL contract, current package/lock state, the browser
runner and CI job, the UI design system, and the M2 acceptance boundary. This
revision closes the refresh, staleness, chart, async-state, browser-runtime,
dependency, rollback, and documentation decisions needed before implementation.

Branch: `feat/mvp-007-telemetry-device-state-console`

Intended PR: One frontend-telemetry PR

Milestone: M2 — Telemetry and current state

Impact: Material Change (Tier 2). This introduces the first browser-visible
tenant-scoped telemetry journey, one production frontend dependency, time-based
presentation policy, and a real browser-to-MQTT-to-PostgreSQL journey. It
reuses the existing development identity, same-origin GraphQL adapter, Go API,
PostgreSQL projection, Mosquitto fixture, and Nuxt runtime. It does not change
the public GraphQL schema, persistence model, production identity, deployment
topology, or production exposure.

## Goal

Extend the existing device-detail page so an operator can inspect the latest
temperature, understand when the device was last observed by the platform,
distinguish recent, stale, empty, and failed telemetry states, and explore the
bounded recent temperature history.

The console must describe evidence it actually has. Telemetry recency is a
signal-status heuristic, not proof that an MQTT connection is currently open;
the UI must not label the device `Online` or `Offline` in this slice.

## Acceptance Boundary

This PR is complete when a contributor can:

1. start the existing isolated PostgreSQL and Mosquitto test dependencies,
   migrate and seed the database, and run the built API and Nuxt console;
2. create or select a device, open `/devices/:id`, and see an explicit empty
   telemetry state without losing the existing device identity details;
3. publish the existing telemetry-v1 message with the real device simulator,
   use the page's explicit Refresh action, and see the committed current
   temperature, selected observation time, independent last-seen time, and
   recent history for that device;
4. publish a second observation and see a chronological temperature chart plus
   the newest-first textual history after refresh;
5. load older history through the existing opaque cursor without duplicate
   rows or a second frontend source of truth;
6. distinguish `Recent signal`, `Stale signal`, `No telemetry`, loading,
   refresh-in-progress, history-pagination failure, and telemetry-read failure;
7. use the telemetry journey with keyboard-only navigation and at desktop,
   mobile, 320 px reflow, and reduced-motion conditions without horizontal
   page overflow or a chart-only information dependency; and
8. pass focused frontend tests, the real automated browser journey, required
   repository gates, and a visible Browser inspection of the implemented page.

The required end-to-end path is:

```text
device simulator
-> loopback Mosquitto test broker
-> existing MQTT ingestion and PostgreSQL projection
-> existing tenant-scoped GraphQL current-state/history reads
-> fixed same-origin Nuxt /api/graphql adapter
-> /devices/:id telemetry region
-> visible current state, recency status, history, and chart/summary
```

Mocked component tests do not satisfy this cross-boundary claim. The automated
browser journey and visible Browser QA must use the real built Nuxt server,
real Go API, disposable PostgreSQL data, and real simulator/broker path.

## Why

MVP-004 through MVP-006 prove that a simulated device observation can be
validated, committed, retained, and queried. This slice makes that result
operator-visible and completes M2 without introducing realtime infrastructure
before a freshness requirement justifies it.

## Verified Repository Baseline (2026-09-22)

- The branch `feat/mvp-007-telemetry-device-state-console`, local `main`,
  locally known `origin/main`, and remote `main` all point to merged MVP-006
  commit `8c7a0f5`. No remote MVP-007 branch exists yet. The worktree was clean
  before this documentation-only review.
- MVP-003 already owns `/devices/:id`, a route-local request/cancellation
  pattern, safe loading/not-found/error/retry states, and the feature-scoped
  native-`fetch` client in `app/features/devices/device-graphql.ts`.
- Browser requests use only same-origin `POST /api/graphql`, omit credentials,
  use `no-store`, follow a six-second client timeout, and cannot choose an
  upstream or tenant. The Nuxt adapter keeps the backend origin private and
  fixed to `/graphql`.
- MVP-006 is merged. `deviceCurrentState(deviceId)` is nullable;
  `deviceTelemetry(deviceId, first, after)` defaults to 50, accepts 1–100,
  orders by `observedAt DESC, messageId DESC`, and uses an opaque
  telemetry-specific cursor. The selected current measurement and independent
  `lastSeenAt` have different semantics.
- Telemetry history is bounded to 1,000 stored observations per device while a
  separate append-only identity authority preserves replay/conflict semantics.
  The UI does not own either bound or expose internal sequence/ingestion data.
- The simulator is deliberately one-shot. No heartbeat interval, device
  session authority, or production connectivity contract exists, so an
  `Online`/`Offline` claim would be unsupported.
- The current automated browser runner owns disposable PostgreSQL, migrations,
  seed, built Nuxt and API processes, and cleanup, but it does not start the
  existing `mqtt-test` service, enable API ingestion, or build/run the device
  simulator. MVP-007 must extend this owner rather than create a competing
  browser harness.
- The web manifest has no chart dependency. The resolved frontend graph uses
  Node 24.20.0, pnpm 12.3.4, Nuxt 4.5.2, Vue 3.5.42, Nuxt UI 4.11.1,
  TypeScript 5.9.3, Vitest 5.0.0, and Playwright 1.63.0. The current production
  dependency audit reports no advisories.
- Apache ECharts 6.1.0 is the current registry release reviewed for this plan.
  It is Apache-2.0, ships TypeScript declarations, depends directly on
  `zrender` 6.1.0 and `tslib` 2.3.0, and supports tree-shakeable imports plus
  an explicit SVG renderer and ARIA component. Admission must be rechecked on
  the implementation candidate; inspection is not installation or
  compatibility proof.
- Existing docs still contained several pre-merge MVP-006 status sentences.
  This plan-review change corrects those current-state claims without claiming
  any MVP-007 behavior is implemented.

## Scope

1. Extend the existing device-detail page with independently recoverable
   telemetry state while preserving current device identity, not-found, route
   change, and focus behavior.
2. Extend the feature-scoped GraphQL client with exact TypeScript shapes, one
   first-page operation that requests current state plus history, and one
   history-only continuation operation. Do not change the GraphQL SDL or
   generated Go code.
3. Show the current temperature, observation time, receive time, `lastSeenAt`,
   and an explicitly qualified recency status derived only from `lastSeenAt`.
4. Show recent telemetry newest-first in semantic text/table or compact-card
   form, with a guarded `Load more` action using the server cursor.
5. Add a temperature line chart only when at least two history points are
   loaded. Keep a visible non-chart summary and the textual history as the
   accessible, inspectable representation.
6. Add explicit manual Refresh. A successful refresh replaces current state
   and resets history to the newest first page; it does not replay an old
   cursor or preserve older loaded pages behind a changed head page.
7. Cover initial loading, empty, current/recent, stale, refresh pending,
   telemetry error/retry, load-more pending/error/retry, and route/unmount
   cancellation without hiding the already loaded device details.
8. Add the direct ECharts production dependency, its intentional lockfile
   closure, the smallest client-only chart component, and dependency evidence.
9. Extend the existing browser runner and browser tests to include the existing
   test broker, ingestion-enabled API, and real simulator publish while
   preserving bounded startup, teardown, port ownership, and CI diagnostics.
10. Complete the documentation and lifecycle closeout defined below only after
    implementation, self-review, required validation, and merge state justify
    each claim.

## Out of Scope

- GraphQL SDL/resolver/generated-code changes, backend projection changes,
  database migrations, retention changes, or new API fields.
- Automatic polling, background refresh, GraphQL subscriptions, SSE,
  WebSockets, retained MQTT state, or a generic realtime abstraction.
- A global store, normalized/server-state cache, persistent browser cache,
  local/session storage, optimistic telemetry, or automatic request retry.
- A definitive `Online`/`Offline` connection claim, device heartbeat contract,
  fleet health aggregation, maps, alerts, commands, arbitrary metrics, units,
  filters, time ranges, aggregation, export, or long-range analytics.
- A chart wrapper/plugin, custom chart framework, canvas snapshot contract,
  visual-regression platform, or committed browser screenshots/reports.
- Production identity, cookies/tokens, CORS, CSRF changes, public backend
  origin, tenant selector, production broker, deployment, or production
  readiness.

## Dependencies

- MVP-003 supplies the device-detail route, shell/layout, design tokens,
  same-origin GraphQL adapter, native-fetch client policy, request cancellation,
  and PostgreSQL-backed browser runner.
- MVP-006 supplies the unchanged tenant-scoped current-state/history GraphQL
  contract, independent `lastSeenAt`, cursor/page bounds, ordering, retention,
  replay semantics, and the MQTT-to-GraphQL integration path.
- The repository supplies the pinned Node/pnpm/Nuxt/Vue/TypeScript/Vitest/
  Playwright toolchain, `mqtt-test` Compose service, one-shot simulator, and CI
  browser job.
- The UI design system supplies telemetry chart colors, semantic status rules,
  cards/tables, responsive behavior, and accessibility expectations.

The only expected new third-party package is direct production dependency
`echarts` at manifest range `^6.1.0`, with the root lockfile resolving the
reviewed 6.1.0 release. Do not add `vue-echarts`, another chart library, date
library, polling library, GraphQL client, or state manager. A different package,
major version, lifecycle script requirement, native binary, runtime/engine
change, or material transitive/advisory finding is a re-plan condition.

## Architecture / Boundaries

### Ownership and dependency direction

```text
/devices/:id page
├── existing device identity state and route outcome
├── DeviceTelemetryPanel (telemetry request and visible UX state)
│   └── TelemetryTemperatureChart.client (client-only presentation)
└── devices/device-graphql client
    -> existing same-origin Nuxt GraphQL adapter
    -> existing Go GraphQL API
    -> existing telemetry projection read authority
```

- PostgreSQL/MVP-006 remains the telemetry and current-state authority. The
  browser owns only presentation state, loaded-page state, the recency clock,
  and pending/error state.
- The server-selected organization remains authoritative. Browser operations
  contain only `deviceId`, `first`, and the opaque `after` cursor; no tenant,
  organization, upstream, topic, or storage identifier is accepted.
- Keep telemetry DTOs and operations beside the existing device feature client.
  Do not introduce a global API client, store, composable hierarchy, or shared
  package for one route consumer.
- Mount one focused `DeviceTelemetryPanel` only after device resolution; it
  owns telemetry fetch/refresh/pagination and visible section state, while the
  route keeps device identity/not-found authority. This is a concrete
  responsibility split, not a generic component framework.
- Keep chart lifecycle in a focused `.client.vue` component so ECharts is
  excluded from server execution. The component owns `init`, option updates,
  resize, and `dispose`; it owns no fetching, pagination, staleness, or domain
  authority.
- Device identity loads through the existing route behavior first. Only a
  tenant-visible device starts the telemetry request. Once identity is visible,
  telemetry loading or failure is section-local so the useful device details
  and back navigation remain available.

### Frontend data and request lifecycle

- Add `DeviceTelemetryOverview` as the first-page GraphQL document returning
  both `deviceCurrentState` and `deviceTelemetry`, plus
  `DeviceTelemetryPage` as the history-only continuation document. Use
  `first: 50` for each history request and pass the server cursor unchanged for
  continuation. `Load more` must not silently replace current state with a
  value fetched outside the refresh lifecycle.
- Initial telemetry load starts after the existing device query returns a
  visible device. A route-ID change or unmount aborts both relevant requests
  and prevents a late result from overwriting the new route.
- Initial success replaces telemetry state. `Load more` appends only the next
  page and is disabled while pending or when `hasNextPage` is false. A page
  failure preserves already loaded points and its cursor for explicit retry.
- Manual Refresh aborts any telemetry request, starts from `after: null`, and
  replaces current/history/page info only after the new first page succeeds.
  While refreshing, preserve previously rendered data and expose a non-blocking
  `Refreshing telemetry…` status. On failure, preserve the prior successful
  data, show a retryable refresh error, and never claim it was updated.
- Keep `cache: no-store`, `credentials: omit`, the existing bounded client
  timeout, no automatic retry, and no persistent cache. One pending guard plus
  abort/sequence checks prevents duplicate or stale updates.
- No automatic network polling is introduced. This matches the one-shot
  simulator and avoids pretending a freshness SLA exists. A future measured
  requirement for unattended live updates can select bounded polling or a
  realtime transport in a separate review.

### Signal-status and time semantics

- Status derives exclusively from the server's `lastSeenAt`, never the selected
  measurement's `observedAt` or `receivedAt`.
- `No telemetry`: current state is `null` and history is empty.
- `Recent signal`: valid `lastSeenAt` age is less than or equal to five minutes.
- `Stale signal`: valid `lastSeenAt` age is greater than five minutes.
- The five-minute threshold is a reversible MVP presentation policy, not a
  stored health rule, heartbeat SLA, alert threshold, or connectivity proof.
  Display qualifying text such as `Recent signal`/`Stale signal`, never an
  unqualified `Online`/`Offline` label.
- Recompute displayed relative age and the threshold crossing from the local
  clock every 30 seconds without issuing a network request. Clamp a negative
  age to zero for display and retain the absolute timestamp. Invalid contract
  timestamps resolve to explicit unknown text/state instead of `Invalid Date`.
- A first-page response where current-state and history presence disagree
  violates the delivered projection invariant. Render an honest degraded
  `Telemetry state is inconsistent` message while keeping any returned history
  inspectable; do not manufacture a current value from the first history row
  or hide an authoritative current value because history is unexpectedly empty.

### Presentation and chart decision

- Current-state card: temperature in °C, selected observation time, selected
  receive time, independent last-seen time, and icon + text + semantic color
  status. Color is never the only status signal.
- History: newest-first semantic table on wider layouts and readable stacked
  entries/cards on narrow layouts, retaining temperature, observed time, and
  received time. Message IDs may be available as secondary technical identity
  but must not dominate the operator task.
- Chart: one temperature line series over loaded observations, sorted into
  `(observedAt, messageId)` ascending order for display. Zero points use the
  empty state; one point uses textual/current presentation only; two or more
  points render the chart.
- Use direct tree-shakeable imports from ECharts core with only the line chart,
  needed axes/grid/tooltip/ARIA components, and the SVG renderer. Disable or
  reduce nonessential animation, honor reduced motion, use existing PulseGrid
  chart tokens, and keep tooltips supplemental.
- Keep a visible text summary near the chart describing loaded point count,
  temperature range, and newest observation. Register ECharts ARIA support,
  but do not treat generated chart ARIA as a replacement for the visible
  summary and semantic history.
- No visualization is rendered until the client is mounted. Reserve useful
  chart space only when a chart is eligible, observe its container for size
  changes, and dispose observers/chart instances on unmount.

## Security and Failure Model

| Failure or abuse path | Required behavior and control | Evidence | Remaining risk |
| --- | --- | --- | --- |
| Client attempts another tenant/upstream | Operation accepts no tenant/upstream; existing same-origin adapter and Go principal remain authoritative | Client operation inspection, existing proxy tests, existing two-tenant API integration | Development identity is not production auth |
| Telemetry API unavailable, slow, malformed, or returns GraphQL errors | Bounded timeout/cancellation; safe public copy; section-local error and explicit retry; no raw response/SQL/internal URL rendered | Client and Nuxt tests plus browser failure/recovery exercise | No automatic recovery until the operator retries |
| Refresh or route change races an older response | Abort controller, request sequence, pending guard, replace-on-success | Deterministic deferred-promise/fake-timer tests | Network completion may still occur server-side; reads have no side effect |
| Pagination fails after data loaded | Preserve current data/cursor, show inline error, allow exact explicit retry, prevent duplicate append | Component tests | No deduplication beyond cursor/request guard is needed for one route owner |
| Browser clock is skewed | Keep absolute server time visible; clamp negative display age; describe status as a heuristic, not connectivity fact | Clock-boundary tests | A badly wrong client clock can misclassify recency |
| Current/history presence disagrees | Show degraded inconsistency message, retain any authoritative values, and never infer current authority from the first history row | Component contract-anomaly tests in both directions | Root cause still requires backend diagnosis |
| Chart dependency or lifecycle fails | Text/table remain the useful representation; client-only init, safe cleanup, no dynamic remote assets | Component lifecycle, production build, visible Browser console/DOM checks | A browser-specific renderer defect may affect only the chart |
| Large loaded history causes poor layout | Server hard cap 1,000, manual 50-row pagination, no background accumulation, SVG chart uses only loaded points | Dense-data component/browser check and build artifact inspection | 1,000 DOM rows are not a long-range analytics design |

No new authentication, authorization, cookie, credential, redirect, CORS,
upload, rich HTML, public runtime configuration, or production boundary is
introduced. Render API values through normal Vue text binding; do not use raw
HTML. Any change to those facts requires security and plan re-review.

## Decisions and Alternatives

### Selected

- Extend the existing device-detail route instead of creating a telemetry
  route or fleet dashboard.
- Keep device and telemetry request state independently recoverable, with the
  telemetry request beginning only after a visible device is established.
- Use explicit manual Refresh and a local 30-second recency tick; no polling.
- Use a five-minute `Recent signal`/`Stale signal` UI threshold while avoiding
  `Online`/`Offline` claims.
- Use the delivered GraphQL current/history fields unchanged, page by 50, and
  reset to the first page after refresh.
- Add direct ECharts 6.1 with tree-shakeable SVG/ARIA imports and no Vue wrapper;
  render only for at least two points.
- Extend the existing browser harness with its already selected MQTT fixture
  and simulator rather than duplicate runtime orchestration.

### Rejected for this slice

- **Do nothing / text only:** textual history is necessary but a small line
  chart materially improves trend recognition once multiple points exist; the
  text remains the accessible authority.
- **Custom SVG chart:** avoids a package but creates chart scaling, axes,
  tooltip, resize, and accessibility code that the already selected ECharts
  capability owns. Direct modular imports keep the dependency bounded.
- **`vue-echarts` wrapper:** adds another version/peer/lifecycle owner for one
  focused component without reducing the material lifecycle work.
- **Automatic polling:** no publish cadence or freshness SLA justifies ongoing
  requests, and the one-shot simulator makes explicit Refresh more truthful.
- **Subscription/SSE/WebSocket:** no server transport exists and the current
  requirement does not justify a new bidirectional/realtime boundary.
- **Call telemetry in parallel before device visibility is known:** saves one
  local/test request latency but complicates not-found/route cancellation and
  performs a query the page may not need.
- **Infer current state from the first history row:** violates the server's
  independent projection authority and hides an inconsistent response.
- **Global cache/store or generated GraphQL client:** one route, no mutation
  invalidation, and explicit refresh do not justify those dependencies or
  ownership layers.

### Re-plan triggers

- a GraphQL schema/resolver/generated artifact, database, retention rule,
  backend production behavior, public configuration, or identity boundary must
  change;
- an automatic refresh, subscription, more-than-one-metric, unit conversion,
  time-range/filter/aggregation, or fleet-level view becomes required;
- ECharts admission reveals a material advisory, incompatible runtime/build,
  unexpected install script/native artifact, unacceptable bundle impact, or a
  need for another chart package;
- the existing browser runner cannot safely own the test broker/simulator
  lifecycle without a new CI topology or destructive/shared resource behavior;
- response semantics cannot distinguish empty, missing device, and dependency
  failure without an API change; or
- production auth/exposure, cookies/tokens, tenant selection, or user-sensitive
  telemetry becomes part of the requested journey.

## Risks / Open Decisions

- The five-minute recency threshold is an accepted, reversible MVP assumption,
  not measured connectivity policy. Its visible wording and revisit trigger
  contain the risk of overstating what `lastSeenAt` proves.
- ECharts compatibility, final transitive closure, advisory state, and bundle
  cost remain implementation evidence. A material surprise triggers re-plan;
  dependency installation alone is not acceptance.
- The browser runner is expected to compose its existing PostgreSQL lifecycle
  with the existing Mosquitto test fixture. Ownership and cleanup must be proven
  on both local and CI paths before that harness change is accepted.
- Repeated `Load more` can render up to the server's 1,000-row history cap.
  Dense-data browser evidence must confirm this bounded MVP remains usable;
  virtualization is deferred unless measured behavior disproves that choice.
- No blocking product or architecture decision remains for implementation.

## Implementation Direction

1. Change status to `In progress` with the first implementation change. Add
   telemetry DTOs, the current+history operation, pagination function, and
   strict response/error tests to the existing feature client.
2. Mount a focused telemetry panel from `/devices/:id` after successful device
   resolution. Implement abort/sequence/pending guards, first-page load,
   refresh replacement, load-more append/retry, and the 30-second local recency
   tick before styling or chart work.
3. Add current-state, empty/degraded/error, refresh, and recent-history UI using
   existing tokens and semantic markup. Preserve device identity and
   navigation when telemetry alone fails.
4. Add `echarts@^6.1.0` through pnpm and inspect the manifest/lock diff,
   transitive closure, lifecycle metadata, license, runtime composition, build
   output, and production audit. Add no wrapper.
5. Implement the focused client-only SVG chart with modular imports, resize and
   disposal, reduced-motion behavior, ECharts ARIA, and the visible textual
   summary. Render it only for two or more valid points.
6. Extend Nuxt/component tests for the full state machine, clock boundaries,
   pagination, race/cancellation, accessible content, responsive structures,
   and chart eligibility/lifecycle.
7. Extend the existing browser runner so it safely owns or uses PostgreSQL and
   separately owns the existing `mqtt-test` Compose service, enables ingestion
   on the API, builds the simulator alongside the API, and cleans up only its
   own processes/project on success, failure, or interruption.
8. Add the real browser journey: create a unique device, confirm empty state,
   publish with the simulator, Refresh, assert current/history, publish a second
   point, Refresh, assert chart/summary, exercise stale time with a controlled
   browser clock, and verify no console/page errors or horizontal overflow.
9. Run targeted checks, reconcile plan versus actual diff, perform author
   self-review and in-scope fixes, then run final validation on the reviewed
   head. Open the PR only after the local candidate is ready; independent
   review and exact-head CI remain separate gates.
10. Update only the closeout documentation whose claims are supported by the
    final implementation and evidence. Move this plan to `completed/` and mark
    M2 complete only after merge/acceptance.

## Expected File Boundary

Expected new/changed surfaces include:

- `apps/web-console/package.json` and root `pnpm-lock.yaml`;
- `apps/web-console/app/features/devices/device-graphql.ts` and focused tests;
- `apps/web-console/app/pages/devices/[id].vue`, one focused
  `app/components/DeviceTelemetryPanel.vue`, and one focused
  `app/components/TelemetryTemperatureChart.client.vue`;
- `apps/web-console/app/assets/css/main.css` for telemetry/card/history/chart
  layout using existing tokens;
- `apps/web-console/test/unit/` and `apps/web-console/test/nuxt/` telemetry
  client/component/route tests;
- `tests/browser/`, `scripts/build-api-test.mjs` or a narrowly renamed/replaced
  test-binary builder, `scripts/test-browser.mjs`, and possibly the existing
  browser CI step when required to express the broker lifecycle;
- frontend/local-development, architecture/technology, root status, roadmap,
  and feature-plan closeout documentation listed below.

No API schema/resolver/generated Go, database migration, projection/ingestion
implementation, Compose service definition/image, environment example,
AsyncAPI, OpenAPI, production configuration, or new CI job is expected. The
existing browser job may receive only the configuration/lifecycle changes
needed to consume the existing test broker. A material diff outside this
boundary requires re-review before proceeding.

## Validation

### Static, dependency, and artifact evidence

- Inspect the manifest and lockfile so only direct `echarts` plus its expected
  transitive closure enters the production graph; verify no unintended
  workspace/package-family upgrades, native binary, or build-script policy
  change.
- Run frontend lint, Nuxt/TypeScript typecheck, browser-fixture typecheck,
  focused Vitest/Nuxt tests, production build, repository policy, and the
  project production dependency audit. Inspect the built client artifact to
  confirm ECharts is client-only and no private backend origin/configuration is
  exposed.
- Compare build output before/after enough to report the chart dependency cost;
  no arbitrary size budget is invented, but an unexpectedly broad full-library
  bundle is a failure of the modular-import decision.

### Client and component evidence

- Client tests prove exact operation/variable shape, `first: 50`, opaque cursor
  pass-through, DTO parsing/error behavior, `no-store`, credentials omitted,
  timeout, external abort, and no automatic retry.
- Route/component tests cover device success then telemetry loading; empty;
  one point; two-plus points; recent at just before and exactly five minutes;
  stale just after five minutes; `lastSeenAt` rather than observation time;
  negative/invalid client-clock cases; either direction of current/history
  presence inconsistency; first-load failure/retry; refresh
  pending/success/failure with old data
  preserved; load-more append/end/error/retry; duplicate pending guard; route
  change/unmount and late-response suppression.
- Accessibility assertions cover headings/landmarks, semantic current/history
  labels, live/pending/error feedback, icon-plus-text status, keyboard actions,
  visible chart summary, and absence of chart-only information.
- Chart tests cover the zero/one/two-point admission rule, chronological input,
  modular option construction, update, resize, and disposal without relying on
  pixel snapshots in happy-dom.

### Automated real-browser evidence

- Extend `corepack pnpm run test:browser`; do not add a second browser command
  for the product journey. The runner must use unique test identity/data,
  bounded readiness waits, isolated ports, the existing pinned PostgreSQL and
  Mosquitto artifacts, and cleanup it can prove it owns.
- The Playwright journey must cross real simulator -> MQTT -> ingestion ->
  PostgreSQL -> GraphQL -> Nuxt -> browser behavior. It proves empty-to-current,
  explicit refresh, a second point/chart/summary, correct device scoping, and a
  controlled stale transition without sleeping five minutes.
- Retain existing readiness restart and registry regressions. Verify the
  telemetry route at desktop, 390 px mobile, and 320 px reflow where behavior
  changes; assert no unintended page-level horizontal scrolling, keyboard
  reachability, reduced motion, and no relevant page/console errors.
- Headless browser automation proves only its assertions; it does not replace
  the visible Browser inspection below.

### Required visible Browser inspection

After the candidate passes focused automation, start the built local/test stack
with isolated data and open the implemented `/devices/:id` route in a visible
Browser. The target flow is:

```text
device detail opens empty
-> simulator publishes telemetry
-> operator activates Refresh
-> current value/history appear
-> second publish + Refresh
-> chart, summary, and latest value update
```

Record page URL/title, meaningful nonblank DOM, absence of a framework error
overlay, relevant console warnings/errors, and screenshot evidence. Exercise
Refresh and Load more or its terminal disabled/absent state by keyboard. Inspect
desktop 1440x900 and mobile 390x844, plus 320 px/zoom when needed to close
reflow risk. Confirm chart resize, no clipping/overlap/scroll trap, visible
focus, readable empty/error/current/stale states, and that the text summary and
history remain usable without interpreting the chart. Keep screenshots and
temporary Browser artifacts outside the repository.

Visible Browser evidence is required for completion because this is a
page-level frontend feature. If the Browser surface is unavailable, report the
gate as unavailable and do not relabel headless Playwright as equivalent
interactive visual evidence.

### Final gates and evidence identity

- During implementation run the smallest affected lint/typecheck/test/browser
  checks after each material slice.
- After plan-to-actual reconciliation and author self-review/fixes, run
  `corepack pnpm run check:fast`, then `corepack pnpm run check` on the final
  local candidate. Required full-stack, audit, build, and browser evidence is
  incomplete when skipped.
- Inspect required GitHub checks on the exact PR head. Any post-review code,
  dependency, test, executable configuration, or documentation change that can
  invalidate the reviewed claim requires proportionate re-review/retest.
- Report passed, failed, unavailable, skipped, and not-run checks separately.
  A green build or a screenshot alone does not prove the product journey.

## Documentation Updates and Closeout

After implementation and evidence agree, update:

- `apps/web-console/README.md` with telemetry detail behavior, manual refresh,
  ECharts ownership, and browser evidence;
- `docs/project-setup/local-development.md` with the exact local
  migrate/start/broker/simulator/Refresh journey and browser runner's MQTT
  lifecycle;
- `docs/architecture/system-architecture.md` with the implemented console
  consumer boundary and the fact that recency is a UI heuristic over
  `lastSeenAt`, not connection authority;
- `docs/architecture/technology-decisions.md` with direct modular ECharts and
  manual refresh as the selected MVP-007 choices while leaving persistent
  realtime transport conditional;
- `README.md` with implemented telemetry projection/console status only after
  those outcomes are true;
- `docs/api/README.md` only if the implemented console consumer/testing map
  adds useful human testing guidance; do not restate an unchanged GraphQL
  contract merely to create doc churn;
- `docs/design/ui-design/ui-design-system.md` only if implementation creates a
  reusable telemetry component/token/state pattern not already owned there;
- this plan, the feature-plan index, and roadmap lifecycle/status, moving the
  plan to `completed/` and M2 to `Complete` only after merge or equivalent
  acceptance.

No API/AsyncAPI/OpenAPI/environment-contract update is expected. If actual
implementation changes those owners, stop and re-plan rather than silently
expanding closeout prose.

## Rollback / Containment

- Before merge, revert the route/client/chart/test-harness changes and remove
  `echarts` plus its lockfile closure. The existing device-detail route and
  backend remain independently usable.
- If only the chart fails during implementation, keep text/current/history
  behavior only if the plan and acceptance boundary are explicitly revised;
  do not leave a half-initialized hidden chart dependency.
- Runtime containment is route-local: a telemetry read failure preserves
  device identity and exposes Retry. No database, API, broker, or production
  state needs rollback.
- Browser-test failure cleanup must stop owned child processes and remove only
  its unique Compose project/volumes while preserving the primary failure.
- No destructive schema/data rollback is part of this PR.

## Engineering Improvement Review

- **Current scope:** independent telemetry UX state, truthful recency labels,
  manual refresh with race protection, bounded pagination, accessible text plus
  conditional visualization, direct modular dependency admission, real
  simulator-to-browser proof, visible Browser QA, and exact documentation
  closeout. These are tightly coupled to correctness, usability,
  accessibility, maintainability, and honest completion of the first telemetry
  console.
- **Future enhancements:** measured auto-refresh/realtime transport; a real
  heartbeat/connectivity contract; multi-metric/unit metadata; time-range and
  aggregation queries; fleet health; long-range storage/performance work;
  production identity/RBAC. Each has an independent product, API, data,
  security, or operational trigger and is not authorized by this plan.
- **Scope effect:** remains one frontend-focused PR using existing backend and
  runtime dependencies, plus one direct chart package and a bounded extension
  to the existing browser test lifecycle.

## Final Plan Review (2026-09-22)

### Findings resolved by this revision

- **Blocker:** refresh was undecided. The plan now selects explicit manual
  Refresh, replace-on-success, no automatic retry/polling, and exact race/
  failure behavior.
- **Blocker:** connectivity language exceeded available authority. The plan now
  uses a five-minute `Recent signal`/`Stale signal` heuristic derived only from
  `lastSeenAt` and expressly rejects `Online`/`Offline` claims.
- **Blocker:** nullable current state versus empty/stale behavior was ambiguous.
  Empty, stale, and invariant-anomaly outcomes now have distinct rendering.
- **Major:** chart admission, dependency, SSR/lifecycle, accessibility, and
  ordering were unresolved. The plan now selects direct modular ECharts 6.1,
  SVG/client-only lifecycle, a two-point minimum, chronological chart order,
  and text/table alternatives.
- **Major:** the original browser sentence could pass without MQTT ingestion or
  a visible browser inspection. The revised evidence crosses the real
  simulator path and separately requires a visible Browser QA session.
- **Major:** request ownership, pagination refresh semantics, stale-response
  handling, and telemetry-section recovery were unspecified. They are now
  route-local and testable without changing the existing API/client boundary.
- **Major:** dependency admission, rollback, test-resource ownership, exact
  documentation closeout, and re-plan triggers were missing. Each now has a
  named owner and evidence gate.

### Remaining assumptions and confidence

- The five-minute threshold is an explicit reversible product assumption
  because no heartbeat/publish cadence exists. It is intentionally described
  as signal recency and has a revisit trigger; it does not block this MVP proof.
- ECharts 6.1.0 compatibility and final transitive/advisory status remain to be
  proven after installation, build, and audit. The package is admitted in
  principle, not pre-certified.
- The existing GitHub-hosted browser job is expected to run the existing
  Compose MQTT service. If that lifecycle cannot be isolated without a new CI
  topology, implementation must stop at the re-plan trigger.
- Confidence is high that the current API and repository boundaries can support
  the slice without backend changes; rendered/browser and installed-dependency
  behavior remain implementation evidence, not planning facts.

## Done Criteria

An operator can open a tenant-scoped device detail, distinguish no telemetry,
recent signal, stale signal, refresh/failure, and pagination states, inspect
the current temperature and bounded newest-first history, and use a meaningful
line chart plus a visible non-visual summary when multiple points exist.

The journey uses no client-selected tenant, no persistent cache or automatic
polling, no unsupported online/offline claim, and no backend contract change.
Focused tests, the real simulator-to-browser automated journey, visible Browser
inspection, full required local gates, exact-head CI, plan-to-actual
reconciliation, author self-review, and required independent review are
complete. Documentation matches the implementation, and M2 lifecycle status
changes only after merge or equivalent acceptance.

## Dependency References Reviewed

- [Apache ECharts import and tree-shaking guidance](https://echarts.apache.org/handbook/en/basics/import/)
- [Apache ECharts accessibility guidance](https://echarts.apache.org/handbook/en/best-practices/aria/)
- [npm registry entry for ECharts](https://www.npmjs.com/package/echarts)
