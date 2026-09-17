# MVP-003 — Device Registry Console

Status: Planned

Review state: Re-planned and reviewed against repository head `c25c160` on
2026-09-17. No implementation has started. The branch and `main` are aligned,
the working tree was clean before this plan update, and the existing fast gate
passed at the reviewed baseline.

Branch: `feat/mvp-003-device-registry-console`

Intended PR: One frontend-journey PR

Milestone: M1 — Device registry

Impact: Material Change (Tier 2): this introduces the first browser-visible
tenant-scoped product data and mutation journey, a same-origin Nuxt-to-GraphQL
transport boundary, a frontend data-client policy, and a real
browser-to-Nuxt-to-Go-to-PostgreSQL test path. It does not change the GraphQL
schema, persistence schema, tenant-authority decision, production identity,
deployment exposure, or production runtime.

## Goal

Let a controlled development operator list, provision, and inspect devices for
the fixed `pulsegrid-dev` organization through the web console, using the
implemented MVP-002 GraphQL contract without making browser state or a request
field authoritative for tenant selection.

The journey must remain explicitly development/test-only. Production continues
to have no product API or production identity claim.

## Acceptance Boundary

This PR is complete when a contributor can follow the documented local startup
sequence, open the console, navigate to `Devices`, observe loading/empty/error
and paginated success states, create one valid device without duplicate
submission, reach its detail route, and find the new device again from a fresh
list query.

The required browser evidence must cross the real boundary:

```text
browser
-> fixed same-origin POST /api/graphql
-> Nuxt server adapter
-> Go POST /graphql
-> fixed development principal
-> registry repository
-> disposable PostgreSQL
```

Component-only or mocked-network tests do not satisfy this end-to-end claim.

## Why

MVP-001 and MVP-002 already own durable tenant-scoped device behavior and the
public GraphQL contract. This plan adds the first operator-visible consumer and
closes M1 without changing domain rules or inventing production identity.

The slice also establishes the smallest reusable console boundary needed by
the later telemetry and command views: stable device routes, explicit async
states, and a fixed same-origin GraphQL transport.

## Verified Repository Baseline (2026-09-17)

- `feat/mvp-003-device-registry-console`, local `main`, and `origin/main` all
  point to `c25c160`; the branch is zero commits ahead/behind `main` and has no
  implementation diff.
- MVP-001 and MVP-002 are merged. The SDL exposes only `devices(first, after)`,
  `device(id)`, and `createDevice(input)` with opaque IDs/cursors, descending
  `(created_at, id)` order, page sizes 1–100, and stable safe error codes.
- Go serves `POST /graphql` only when the development identity is enabled on a
  literal IPv4 loopback listener. The request cannot select a tenant through
  variables, headers, cookies, or browser state. Production and disabled modes
  mount no GraphQL route.
- The Nuxt console currently has only `/`, a desktop Overview navigation item,
  a mobile header with no navigation control, and a planned-state message that
  says product data is not connected. The first second destination therefore
  also owns the already-deferred mobile-menu behavior from FND-002.
- The only Nuxt backend boundary is the fixed same-origin readiness route. It
  uses private `NUXT_BACKEND_ORIGIN`, forwards no browser authority or arbitrary
  path, and is not a product proxy.
- The browser fixture starts the API with identity disabled and no database.
  `browser-smoke` therefore cannot currently prove the device journey and must
  gain isolated PostgreSQL, migration, seed, and enabled-API lifecycle without
  weakening its existing readiness/recovery coverage.
- The resolved frontend baseline is Node 24.20.0, pnpm 12.3.4, Nuxt 4.5.2,
  Vue 3.5.42, Nuxt UI 4.11.1, Nitro 2.13.4, Vitest 5.0.0, and Playwright
  1.63.0. The production Node dependency audit reports no advisories.
- The repository has 13 independent CI jobs. The reviewed baseline passed
  `corepack pnpm run check:fast`: GraphQL generation drift, Go format/vet/
  modernize/staticcheck/tests, OpenAPI lint, frontend lint/typecheck/tests, and
  browser fixture typecheck. Frontend tests passed 27/27.
- The root README is stale: it still reports Foundation in progress and says
  database and product APIs have not started. MVP-003 closeout must correct
  that tracked public status rather than repeating it in another source.

## Scope

1. Add the fixed same-origin GraphQL transport described below. Reuse the
   existing private `NUXT_BACKEND_ORIGIN`; do not expose the Go origin through
   public runtime config, add CORS, or accept a client-selected upstream path.
2. Add a small feature-scoped typed GraphQL client over native `fetch` for the
   three committed operations. Add no GraphQL client, code-generation, global
   store, or normalized-cache dependency in this slice.
3. Add stable routes:
   - `/devices` for the initial bounded list and explicit `Load more` action;
   - `/devices/new` for provisioning;
   - `/devices/:id` for detail, safe not-found, error, and retry behavior.
4. Add `Devices` to desktop/tablet navigation and implement the first mobile
   navigation menu using the existing Nuxt UI foundation. The menu has a
   visible `Menu` label plus icon, scrim, Escape dismissal, focus containment,
   and focus restoration; active-route semantics must follow the current URL
   rather than remain hard-coded to Overview.
5. Replace the obsolete overview planned-state copy with a truthful entry point
   to the implemented device registry while leaving telemetry, alerts, and
   commands visibly unclaimed. Preserve the existing process-readiness card as
   a separate operational signal.
6. Implement the list, create, and detail presentation using existing PulseGrid
   tokens and Nuxt UI primitives. Desktop/tablet use a semantic table; mobile
   uses a prioritized card/list representation rather than horizontal table
   scrolling.
7. Cover loading, empty, paginated success, not found, validation, duplicate
   conflict, dependency/transport failure, retry, mutation pending, and
   post-create success states. Preserve form input after recoverable failure
   and prevent duplicate submit/load-more actions while pending.
8. Extend focused frontend tests and the existing browser-smoke surface. The
   browser journey must use the real GraphQL API and disposable PostgreSQL;
   mocks remain limited to deterministic component/adapter failure cases.
9. Complete the documentation and lifecycle closeout described below after
   implementation review and required validation succeed.

## Out of Scope

- GraphQL SDL/resolver changes, new device fields, device update/delete,
  lifecycle state, bulk actions, search, filtering, arbitrary sorting, total
  counts, or backward pagination.
- Production authentication, authorization, RBAC, sessions, cookies, tokens,
  CSRF policy, production product routing, deployment, or a production backend
  origin.
- Client-supplied organization or tenant selection, organization management,
  protected-field mass assignment, or any frontend authorization decision.
- Optimistic creation, automatic mutation retry, offline queueing, persisted
  client cache, local/session storage, global state, Graphcache, subscriptions,
  polling, or background refresh.
- A generic Nuxt reverse proxy, arbitrary upstream paths/headers, backend CORS,
  REST device CRUD, schema/code generation for TypeScript, or a shared package.
- Telemetry, status/connectivity, charts, maps, alerts, commands, device groups,
  fake fleet metrics, or sample operational data.
- New database migrations, seed semantics, indexes, API rate limiting, API
  observability infrastructure, or changes to the 13-job required-check model.

## Dependencies

- FND-002 supplies the SSR shell, tokens, responsive layout, Nuxt UI, and the
  mobile-navigation decision deferred until a second real route exists.
- FND-003 supplies Vitest, Playwright, root checks, hooks, and required CI.
- FND-004 supplies `NUXT_BACKEND_ORIGIN`, the fixed same-origin readiness
  pattern, browser process ownership, and the readiness recovery journey that
  must remain green.
- MVP-001 supplies migrations, seed organization, repository validation,
  uniqueness, and tenant-scoped persistence.
- MVP-002 supplies the unchanged GraphQL schema, development identity,
  loopback restriction, safe errors, request/complexity limits, and runtime
  database readiness.
- Preserve exact server semantics: device keys are case-sensitive and 1–128
  Unicode code points with no surrounding Unicode whitespace; display names
  are 1–200 code points and not whitespace-only; inputs are not silently
  trimmed, normalized, or case-folded.

## Architecture / Boundaries

### Request and authority flow

```text
untrusted browser input
-> feature-scoped typed operation and local UI state
-> fixed same-origin POST /api/graphql
-> Nuxt request/response size, media-type, timeout, and no-store boundary
-> fixed private NUXT_BACKEND_ORIGIN + /graphql
-> Go GraphQL transport and fixed development principal
-> tenant-scoped registry repository
-> PostgreSQL
```

- Nuxt is a transport adapter, not a second domain API. It does not interpret
  tenant authority, rewrite GraphQL operations, add business validation, or
  turn device behavior into parallel REST endpoints.
- The browser never receives the backend origin and never sends tenant,
  organization, database, credential, or trusted-principal state.
- The adapter accepts only JSON `POST` at one fixed path, forwards only the
  bounded request body with explicit GraphQL request/response media types, and
  follows no redirects. It forwards no cookies, authorization headers, origin,
  arbitrary browser headers, or client-selected destination.
- The standard development console command must bind Nuxt explicitly to
  `127.0.0.1`. Test preview already binds to `127.0.0.1`; production has no
  backend origin and the product adapter returns a safe unavailable response
  without contacting an upstream. This prevents the Nuxt adapter from
  accidentally widening Go's loopback-only development surface.
- Keep `runtimeConfig.public` empty. Build/artifact checks must prove that the
  backend-origin sentinel is absent from HTML, hydration payloads, and client
  bundles.
- The list, form draft, pagination continuation, and request status stay at the
  route/feature boundary. No global store or duplicate source of truth is
  introduced.
- Nuxt SSR renders the shell and route structure only; device requests start
  after hydration. No tenant-scoped product response is serialized into the
  Nuxt payload in this development-only slice.

### Same-origin GraphQL transport contract

- Browser endpoint: `POST /api/graphql` with `Content-Type: application/json`
  and `Accept: application/graphql-response+json`.
- Upstream endpoint: the exact configured loopback origin plus `/graphql`.
  No incoming value can alter origin, scheme, port, path, or method.
- Bound the incoming body to the API's existing 64 KiB limit before forwarding,
  including chunked requests. Bound the upstream response to 256 KiB, which
  covers the current maximum device response while preventing an unbounded
  proxy buffer.
- Use a 5-second Nuxt upstream timeout and propagate cancellation; the browser
  client has a 6-second timeout and ignores late responses after retry or route
  unmount. Do not automatically retry queries or mutations.
- Preserve valid upstream status, GraphQL JSON body, and
  `application/graphql-response+json` content type. Set `Cache-Control:
  no-store` on every adapter response.
- Connection refusal, timeout, redirect, oversized/malformed response, wrong
  upstream media type, missing production origin, and other adapter failures
  return HTTP 503 with `application/graphql-response+json` and the fixed safe
  error `device service is unavailable` / `SERVICE_UNAVAILABLE`, without
  upstream URL, body, stack, configuration, SQL, or credential detail. The UI
  maps this code to retryable dependency failure rather than rendering raw
  messages.
- Reject an unsupported browser request media type with HTTP 415 and an
  oversized request with HTTP 413 using a safe GraphQL error envelope. Forward
  bounded malformed JSON so the Go GraphQL contract remains the authority for
  its existing HTTP 400 parse behavior.
- The adapter does not weaken Go's existing method, body, parser-token,
  complexity, pagination, validation, or tenant-isolation controls.

## Frontend Data Client and Cache Decision

- Use a feature-scoped TypeScript client built on native `fetch`; it owns the
  fixed relative URL, POST/headers, timeout/cancellation, GraphQL envelope
  decoding, safe error-code mapping, and exact types for the three operations.
- Add no runtime dependency. The current journey does not need shared
  cross-route data, background refresh, deduplication, optimistic rollback, or
  normalized entity updates, so a server-state library would add more cache
  and SSR lifecycle than the requirement needs.
- Cache policy is deliberately **no persistent client cache**. Set request and
  response `no-store`; each route entry performs a fresh query. A loaded list
  page owns only its current edges and opaque continuation in memory.
- After successful create, navigate to the canonical detail route returned by
  the mutation. Returning to `/devices` performs a fresh initial query, so no
  hidden invalidation or manual normalized-cache update is required.
- Do not optimistically insert a device. `createDevice` is non-idempotent and a
  lost response can make a retry return `CONFLICT`; the UI preserves input,
  never auto-retries, and offers a clear path to refresh the list.
- Revisit a maintained GraphQL/server-state client and generated operation
  types when a later plan proves shared cache keys, cross-route invalidation,
  polling/subscriptions, optimistic rollback, or enough schema surface that
  manual operation types become a recurring drift risk.

Reviewed alternatives:

- `@urql/vue` 2.1.1 with `@urql/core` 6.0.3 is compatible with Vue 3, but its
  typical exchange/cache and SSR lifecycle are unnecessary here, and the core
  GET preference would need explicit override for MVP-002's POST-only contract.
- `graphql-request` 7.4.0 is small but adds its own runtime plus a GraphQL 14–16
  peer dependency, while the upstream project is transitioning to Graffle.
  It does not protect enough additional behavior to justify adoption for three
  fixed operations.
- Direct browser-to-Go requests would require a public backend origin and CORS,
  contradict the reviewed same-origin boundary, and weaken local/test routing
  isolation.

## User and Route Contract

### Device list — `/devices`

- Request `devices(first: 20, after: null)` on route entry and show a bounded
  loading state that always exits to success, empty, or error.
- Render device key, display name, and created time. Treat IDs and cursors as
  opaque. Do not infer status, type, location, or tenant from absent fields.
- When `hasNextPage` is true, enable one explicit `Load more` control using the
  returned `endCursor`. Append the next page in server order, guard duplicate
  clicks, and reject/ignore a late response after retry or navigation.
- A refresh/retry restarts from the first page rather than replaying a stale
  continuation. Empty is distinct from failure and offers the create action.

### Provision device — `/devices/new`

- Render persistent labels and associated guidance for `deviceKey` and
  `displayName`; placeholders are examples only.
- Client validation counts Unicode code points and uses a Unicode-whitespace
  predicate aligned with the API: device keys reject surrounding whitespace;
  display names reject empty/all-whitespace values; both enforce their exact
  maximum length. Validation never changes the submitted string. The API
  remains authoritative and the client must handle `BAD_USER_INPUT` even after
  local validation passes.
- During submit, preserve the draft, show pending feedback, and prevent a
  second submit. Do not retry automatically.
- Map `CONFLICT` to a field/form error that explains the device key already
  exists in the current development organization. Map unknown/internal or
  transport failures to safe form-level recovery without exposing raw details.
- On success, navigate to `/devices/{returned-id}` and focus/announce the
  destination heading or success context. The returned server record, not the
  draft, is the displayed authority.

### Device detail — `/devices/:id`

- Query the route ID as an opaque GraphQL variable. Render only the contract
  fields: device key, display name, ID, and created time.
- `device: null` becomes one indistinguishable not-found state with a link back
  to the list. A malformed ID/`BAD_USER_INPUT` becomes safe invalid/not-found
  guidance; it must not reveal tenant existence.
- Unexpected/transport errors retain navigation and expose an explicit retry.
  No stale detail is persisted across IDs or tenant/identity changes.

## Security / Threat Model

Protected assets are tenant-owned device records, mutation authority, the
fixed development principal, backend/database configuration, and internal
failure details. Intended actors are a controlled local/test operator and the
test harness; every browser request remains untrusted.

| Credible abuse case | Preventive control | Required evidence | Residual risk |
| --- | --- | --- | --- |
| Remote user reaches the loopback-only Go API through a broadly bound Nuxt server | Canonical dev and test commands bind Nuxt to `127.0.0.1`; product adapter is unavailable in production | Config/startup tests plus browser/artifact checks; inspect canonical command and production behavior | A contributor can deliberately bypass the canonical local command; that is not production authorization |
| Client selects another upstream, tenant, or organization | Fixed private origin/path; no tenant field or trusted header; Go principal remains server-owned | Adapter tests with spoofed path/header/tenant values plus existing real-store tenant tests | Development identity is not authentication and remains local/test-only |
| Browser cookies or future credentials are forwarded accidentally | Browser request uses `credentials: omit`; adapter forwards an explicit header allowlist and no cookies/auth | Adapter request-capture test | Any future cookie/token identity requires a new auth/CSRF review |
| Oversized or expensive GraphQL input exhausts Nuxt or Go | 64 KiB request bound before buffering/forwarding, 256 KiB response bound, timeouts, Go token/complexity/page limits | Chunked/content-length body tests, oversized response test, existing GraphQL abuse tests | No actor/IP rate limit; acceptable only for the loopback development surface |
| Create is replayed or submitted twice | Pending guard, no automatic mutation retry, database uniqueness/conflict mapping | Component and real browser duplicate/conflict evidence | A lost successful response remains ambiguous; refresh/list is the recovery path |
| Error or rendered data leaks internals or unsafe content | Vue text interpolation, contract field allowlist, safe code-based copy, no raw upstream body/logging | Malformed/upstream error tests and browser console inspection | Local operators can inspect their own process and database |
| Cached or late data appears under the wrong route/scope | No persistent cache, route-local state, abort/ignore stale responses, fresh query on route entry | Component tests for cancellation/route change and browser navigation evidence | Later shared caching or production identity requires cache scoping review |

No cookie, token, browser storage, CORS, rich HTML, upload, redirect, or new
production trust boundary is introduced. Frontend checks remain usability only;
Go and PostgreSQL remain the tenant and integrity enforcement boundaries.

## Implementation Direction

1. Update the plan status to `In progress` in the first implementation commit.
   Add the fixed Nuxt GraphQL adapter, shared bounded-body/response helpers only
   where they remain destination-specific, and tests for method/media/size/
   timeout/redirect/error/configuration behavior.
2. Make the canonical Nuxt development command explicitly loopback-bound and
   preserve test preview's fixed listener. Add artifact tests that keep the
   private backend origin out of browser output.
3. Add the typed feature client and three operation documents with explicit
   no-cache, timeout, error, cancellation, and no-credentials behavior. Keep
   operation types and error mapping beside the device feature.
4. Refactor navigation from hard-coded Overview state to route-aware items;
   add Devices and the accessible mobile menu without exposing unimplemented
   destinations.
5. Implement list and pagination first, then detail/not-found, then create and
   post-create navigation. Reuse focused components only when list/detail/form
   responsibilities are clearer than one page component.
6. Replace stale overview copy with an honest registry entry point. Do not add
   invented counts or fleet health.
7. Extend the browser harness with isolated PostgreSQL, migration, seed, and
   enabled API startup. Local runs create and remove only a unique disposable
   Compose project. The CI `browser-smoke` job declares its own pinned
   PostgreSQL service and passes the isolated test URL; the runner migrates and
   seeds it but never tears down that externally owned service.
8. Add the real device browser journey with retry-safe unique device keys.
   Keep empty and controlled adapter failure permutations in focused tests so
   browser retries do not depend on mutable global ordering.
9. Review the working-tree-inclusive diff for frontend behavior,
   accessibility, same-origin/security boundary, runtime/test lifecycle,
   dependency scope, and documentation accuracy. Fix findings, then run final
   validation on the reviewed head.
10. Perform the closeout updates only after required evidence and review pass;
    then move this plan to `completed/` and update all inbound links in the
    same final closeout change.

## Validation

### Guarantee-to-evidence map

| Guarantee | Required evidence |
| --- | --- |
| Same-origin adapter is fixed, bounded, and safe | Server unit/integration tests for POST/JSON only, fixed upstream URL, no forwarded cookie/auth/tenant headers, 64 KiB including chunked input, 256 KiB response, redirect refusal, timeout/cancellation, media type, `no-store`, safe unavailable mapping, and production with no upstream call |
| Client matches MVP-002 transport | Focused tests assert relative `/api/graphql`, POST, exact request/accept headers, credentials omitted, no-store, operation/variable shapes, GraphQL error-code mapping, timeout, abort, and malformed response behavior |
| List and pagination are correct | Component tests cover initial loading, empty, first-page success, append order, `hasNextPage`, terminal page, retry reset, duplicate load-more guard, and stale response suppression; real browser exercises at least one continuation against PostgreSQL |
| Create behavior is truthful | Component tests cover Unicode-aware validation, no trimming/normalization, pending/duplicate guard, `BAD_USER_INPUT`, `CONFLICT`, transport/internal failure with preserved input, and success navigation using the returned ID; real browser proves persistence and fresh-list visibility |
| Detail is safe and useful | Component/route tests cover success, null/not-found, malformed ID mapping, unexpected error/retry, and route-ID change cancellation; browser proves created-device detail from the real API |
| Navigation and accessibility work | Nuxt/component tests plus Playwright for active route, desktop collapse, mobile menu open/dismiss/Escape/focus restoration, headings, labels/error association, keyboard-only create/list/detail path, 320px reflow, and no unintended horizontal scrolling |
| Tenant authority remains server-owned | Schema/client operation inspection shows no organization input; adapter spoof tests prove no trusted headers are forwarded; existing two-tenant real-store GraphQL tests remain green; browser uses only the fixed seeded organization |
| Existing readiness and shell behavior remains stable | Existing readiness stop/restart/recovery, overview, responsive, console-error, production build, and artifact leakage assertions remain meaningful with the database-enabled browser API |
| Real full-stack journey and lifecycle are honest | Browser runner applies migrations and seed to an isolated test database, starts enabled Go and built Nuxt, performs create/list/detail through the browser, handles API stop/restart, and verifies owned process/container cleanup on success and failure |
| Dependency and repository policy remain sound | Lockfile review, `node:audit`, lint/typecheck/build, repository policy, and all applicable existing CI contexts pass; no unplanned runtime dependency appears |

### Project gate placement

- Keep all 13 required CI jobs independent. Do not make frontend lint,
  typecheck, unit, build, audit, API unit/race, or OpenAPI jobs depend on
  PostgreSQL.
- `web-test` owns components, feature client, form/state logic, navigation, and
  deterministic Nuxt adapter tests without PostgreSQL.
- `api-db-integration` continues to own backend tenant/persistence/GraphQL
  integration. Do not duplicate its complete backend matrix in browser tests.
- `browser-smoke` gains the smallest PostgreSQL/migrate/seed setup required to
  prove the cross-boundary operator journey while retaining readiness and
  responsive/keyboard coverage. It remains one worker and one required status.
- Use unique device keys derived from the browser test/run identity so a retry
  cannot silently depend on the failed attempt's mutation. Do not assert a
  globally empty database in retryable E2E tests; prove empty state in focused
  frontend tests.
- During implementation run targeted adapter/client/component/browser checks
  first. After post-implementation review and fixes, run
  `corepack pnpm run check` on the final candidate and require all 13 GitHub
  checks on the pushed head. Report passed, failed, skipped, unavailable, and
  not-run evidence separately.

## Documentation Updates

During implementation:

- `apps/web-console/README.md`: replace the planned-shell claim with the device
  journey, fixed GraphQL adapter, no-cache client policy, local prerequisites,
  and development/test-only limitation.
- `docs/project-setup/local-development.md`: document the complete database ->
  migrate -> seed -> API -> console workflow and the database-owning browser
  test lifecycle/recovery.
- `docs/project-setup/environment-configuration.md`: expand the existing
  private `NUXT_BACKEND_ORIGIN` ownership from readiness-only to the two fixed
  readiness/GraphQL adapters while retaining loopback and production rejection.
- `docs/api/README.md`: add the console same-origin GraphQL adapter to the
  surface map and describe that it preserves the Go GraphQL contract without
  becoming a parallel product API.
- `docs/architecture/system-architecture.md`: update current state and the web
  console boundary for the implemented development device journey.
- `docs/architecture/technology-decisions.md`: record native fetch plus fixed
  same-origin POST and no persistent client cache as the MVP-003 selection;
  remove the GraphQL-client/cache item from open decisions and record the
  revisit trigger.
- `README.md`: correct stale Foundation/database/GraphQL status and describe M1
  accurately without claiming production readiness.
- Update the UI design system only if implementation introduces a reusable
  pattern not already covered by its navigation, table, form, responsive, and
  accessibility rules.

### Final lifecycle closeout

After implementation review and required evidence:

1. Record the final candidate commit, review disposition, and evidence summary
   in this plan.
2. Set the plan to `Complete` only when it is accepted for merge with required
   checks green; move it from `planned/` to `completed/`.
3. Update the feature-plan index and every inbound plan/roadmap link to the
   completed path in the same commit.
4. Mark M1 `Complete` in the roadmap and classify MVP-001, MVP-002, and MVP-003
   as complete in its dependency graph. Do not change M2 or later milestone
   status.
5. Re-run link/repository-policy checks after the move so closeout cannot leave
   stale `planned/MVP-003...` references.

## Risks / Open Decisions

No decision-changing design question remains for implementation. The remaining
risks are bounded:

- **Development identity:** the fixed organization is deliberately not real
  authentication. Canonical commands and production behavior must keep the
  entire journey local/test-only.
- **Proxy exposure:** the same-origin adapter can widen the Go loopback surface
  if Nuxt is broadly bound. Explicit loopback binding and production failure
  are completion requirements, not optional hardening.
- **Manual operation types:** small handwritten operation/result types can drift
  from SDL. Real contract/browser tests are required now; generated types are a
  revisit when operation volume makes drift recurring.
- **No cache:** navigation refetches by design. This trades extra local requests
  for simple freshness and tenant scoping. Introduce a cache only with a
  concrete later consumer and invalidation model.
- **Non-idempotent create:** a lost success can surface as conflict on retry.
  The UI must not promise transparent retry; fresh list/detail is the recovery
  path.
- **Browser test cost:** adding PostgreSQL lengthens the required browser job.
  Preserve independent CI jobs and measure the resulting critical path before
  considering a different required-job split.

## Alternatives Considered

- **Do nothing / keep the planned shell:** rejected because M1 has no
  operator-visible outcome and MVP-002 would have no real consumer.
- **Use the existing readiness route as a generic proxy:** rejected because it
  has a fixed operational GET contract. Product GraphQL receives its own fixed
  route and transport rules.
- **Add REST-shaped Nuxt device endpoints:** rejected because they duplicate
  the GraphQL product contract and create a second error/pagination API.
- **Use a full GraphQL cache or global store:** rejected because current state
  is route-local and a fresh-query policy is simpler and safer for the fixed
  development identity.
- **Use optimistic creation or automatic retries:** rejected because create is
  non-idempotent and an unknown result cannot be represented truthfully as
  success or safe retry.
- **Reuse the health-only browser API:** rejected because it would mock away
  PostgreSQL, identity, GraphQL, and persistence—the boundaries this feature
  must prove.

## Engineering Improvement Review

- **Current scope:** fixed same-origin product transport, explicit loopback
  containment, route-local no-cache server state, bounded pagination,
  duplicate-submit protection, truthful mutation uncertainty, accessible
  mobile navigation, real-database browser evidence, and lifecycle closeout
  are tightly coupled to the first device journey and prevent credible
  security, usability, stale-state, or false-evidence failures.
- **Future enhancements:** production identity/RBAC/CSRF/audit, generated
  operation types, maintained server-state caching, search/filtering, optimistic
  updates, polling/subscriptions, and frontend observability wait for their
  first proven consumer or production requirement.
- **Scope effect:** the plan remains one frontend journey PR. It expands the
  originally vague client/proxy/test/documentation bullets into required
  behavior without changing the GraphQL schema, database, production runtime,
  or milestone outcome.

## Rollback / Containment

- Before merge, revert the PR; the backend contract and stored data remain
  unchanged.
- If local UI integration is unsafe or broken, stop the Nuxt process or run the
  API with identity disabled. The GraphQL route and database coupling then
  disappear while health endpoints remain available.
- Devices successfully created through the console are valid MVP-001 rows and
  must not be deleted automatically during rollback. Development data cleanup
  remains an explicit data-owner action.
- If a consumer/API mismatch is discovered, prefer a frontend correction or an
  additive GraphQL forward fix. Do not silently change cursors, error codes,
  nullability, or tenant behavior.
- A scope change involving production identity, cookies/tokens, public network
  exposure, schema modification, or a persistent shared cache requires
  re-planning and a new approval boundary before implementation continues.

## Done Criteria

The reviewed final candidate exposes `/devices`, `/devices/new`, and
`/devices/:id` with route-aware desktop/mobile navigation; the list, create,
detail, pagination, validation, conflict, not-found, loading, empty, pending,
error, retry, and success states are accessible and responsive. The browser
uses only a fixed same-origin POST adapter, no tenant or backend authority is
client-controlled, no private origin reaches browser artifacts, and no
persistent frontend cache or new GraphQL dependency is introduced.

Focused tests prove transport bounds and safe failures, component behavior,
stale-request handling, form recovery, and navigation. The required Playwright
journey proves create -> detail -> fresh list through built Nuxt, enabled Go,
the fixed development principal, and disposable PostgreSQL while existing
readiness recovery and shell regressions remain green. The final reviewed head
passes the repository's full check and all 13 required CI contexts.

Documentation agrees with implemented behavior; the stale root status is
corrected; the plan is moved to `completed/`; all inbound links are updated;
and roadmap M1 is marked complete without making production-authentication,
deployment, telemetry, or later-milestone claims.

## Selected Technical References

- [Nuxt 4 server routes](https://nuxt.com/docs/4.x/directory-structure/server)
  and [runtime configuration](https://nuxt.com/docs/4.x/guide/going-further/runtime-config)
- [GraphQL over HTTP specification](https://github.com/graphql/graphql-over-http/blob/main/spec/GraphQLOverHTTP.md)
- [urql architecture](https://github.com/urql-graphql/urql/blob/main/docs/architecture.md)
  (evaluated but not selected for this slice)
