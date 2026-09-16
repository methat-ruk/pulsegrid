# MVP-002 — Device GraphQL API

Status: Ready for review

Review state: Re-planned and reviewed against the repository, contracts, and
dependencies on 2026-09-16. Implementation was authorized after that review;
the implementation and review fixes are complete for this PR scope. The plan's
evidence map now distinguishes direct tests from code-reviewed lifecycle gaps;
default-page/final-page success and direct pool-close-order instrumentation
remain explicitly out of the completed evidence claim. The plan started from
branch head `1eaf7a2` and was committed before implementation as `63bfc26`.

Branch: `feat/mvp-002-device-graphql-api`

Intended PR: One API-slice PR

Milestone: M1 — Device registry

Impact: Material Change (Tier 2): this adds the first product API contract,
connects the running API process to tenant-scoped durable data, and introduces
a development-only identity boundary. It does not change a production security
boundary, migrate schema/data, or authorize deployment or production exposure.

## Goal

Expose a development-only GraphQL product API through which the controlled
development operator can create, list, and inspect devices belonging to the
seeded `pulsegrid-dev` organization. The server derives tenant authority from
an explicit runtime identity mode; no GraphQL input, header, cookie, or client
state may choose an organization.

The contract must be directly consumable by MVP-003, while preserving the
existing REST/OpenAPI health surface and making no production authentication,
authorization, or deployment claim.

## Why

The console needs a concrete, typed product boundary before client/cache
behavior and device screens can be selected. MVP-001 already provides the
durable repository, data constraints, and keyset continuation required by this
journey; this plan adds only the transport, identity composition, runtime
database ownership, and contract evidence needed to expose that capability
safely.

## Verified repository baseline (2026-09-16)

- `feat/mvp-002-device-graphql-api`, local `main`, and `origin/main` point to
  `1eaf7a2`, the accepted MVP-001 change; the working tree was clean at review.
- The Go module uses Go 1.27.1, Fiber v3.4.0, pgx v5.11.0, and PostgreSQL
  18.6. gqlgen is not present. gqlgen v0.17.95 is the current reviewed release,
  supports Go 1.27, and is the implementation pin unless a newer version is
  separately reviewed before the first dependency change.
- `registry.Repository` already validates and implements tenant-scoped
  `CreateDevice`, `GetDevice`, and `ListDevices`. Lists are bounded to 1–100 and
  ordered by `(created_at DESC, id DESC)` with a compound keyset cursor.
  Duplicate device keys are conflicts within one organization; cross-tenant
  detail reads return not found.
- The controlled organization is created explicitly by `cmd/db seed` with slug
  `pulsegrid-dev`; its database-generated UUID is not stable configuration.
  There is no read method that resolves an organization by slug yet.
- `cmd/api` currently loads only HTTP configuration and does not open
  PostgreSQL. `databaseconfig` is local/test-only and currently described as a
  database-command boundary. The existing production example contains a blank
  database URL and production database topology remains undecided.
- `httpserver.New` constructs the Fiber app and mounts only `/health/live` and
  `/health/ready`. Readiness currently represents listener lifecycle only and
  the OpenAPI enum contains only `starting` and `draining` not-ready reasons.
- The existing request-ID middleware stores the ID in Fiber locals, while
  gqlgen uses `net/http` contexts. Fiber v3.4.0 can host a `net/http.Handler`,
  but request identity, cancellation, and correlation require an explicit
  tested context bridge rather than implicit adaptation.
- CI has 13 independent jobs. `api-test`, `api-race`, and `browser-smoke` run
  without PostgreSQL; `api-db-integration` is the only real-PostgreSQL job.
  The browser fixture starts `cmd/api` in test mode without a database URL.
- The API index already reserves `apps/api/graph/schema/*.graphqls` as the SDL
  source of truth. The system-architecture current-state paragraph still says
  no database schema exists; the delivered implementation updates that
  paragraph to describe the development GraphQL surface.

## Scope

1. Pin gqlgen v0.17.95 and add a schema-first generation layout under
   `apps/api/graph/`, with SDL as the source of truth and committed generated
   artifacts. Add reproducible generate and generated-drift checks; generated
   files are never hand-edited.
2. Add the `/graphql` endpoint with only POST JSON operations. Do not add a
   playground, subscriptions, uploads, persisted queries, federation, GET
   execution, batching, or CORS. Enable introspection only while the explicit
   development identity mode is enabled in development/test.
3. Define the first device schema exactly as described in **GraphQL contract**:
   create, tenant-scoped detail, and bounded forward list operations with
   intentional nullability, ordering, opaque cursor, duplicate, and error
   behavior.
4. Add an explicit `PULSEGRID_IDENTITY_MODE` with values `disabled` and
   `development`. Development examples select `development`; ordinary test and
   production examples select `disabled`. Production rejects `development`.
   When disabled, `/graphql` is not mounted and the API remains health-only.
5. Implement a replaceable development-identity adapter that resolves the
   fixed `pulsegrid-dev` slug to its database-generated organization ID before
   listening, then places that immutable authority in request context. Startup
   fails safely and actionably if the database is unavailable, migrations are
   missing, or the seed organization does not exist. The API never seeds or
   migrates implicitly.
6. Make `cmd/api` own one bounded PostgreSQL pool only when development
   identity mode is enabled. Share it across requests, pass request deadlines
   and cancellation through gqlgen to pgx, drain HTTP work before closing the
   pool, and avoid logging connection URLs or credentials.
7. Extend HTTP-server composition through narrow options/interfaces for the
   GraphQL handler and required-dependency readiness. Keep GraphQL/resolver and
   registry packages out of the platform HTTP server.
8. When GraphQL is enabled, make `/health/ready` check PostgreSQL with a bounded
   probe and return the existing minimal not-ready shape with reason
   `dependency_unavailable` on failure. Liveness remains process-only. When
   GraphQL is disabled, readiness behavior remains listener-only.
9. Add resolver, HTTP contract, context-bridge, configuration, lifecycle,
   tenant-isolation, and real-PostgreSQL tests. Reuse the existing
   `api-db-integration` job for real-store GraphQL evidence while preserving the
   no-database `api-test`, `api-race`, and `browser-smoke` paths.
10. Update API, runtime/configuration, local-development, architecture, and
    technology documentation to match the implemented development-only
    contract and commands.

## Out of Scope

- Production authentication, authorization, RBAC, sessions, cookies, tokens,
  identity provider selection, audit history, or production tenant topology.
- A production GraphQL route or a mode that serves product data without a
  reviewed production identity adapter. Production remains health-only.
- Organization management, client-supplied tenant selection, device update or
  deletion, lifecycle state, credentials, bulk operations, search, filtering,
  or arbitrary sorting.
- Frontend integration, GraphQL client/cache selection, same-origin product
  proxying, UI routes, or browser device journeys; MVP-003 owns them.
- REST device CRUD, GraphQL subscriptions/WebSockets/SSE, MQTT, telemetry,
  commands, alerts, uploads, federation, DataLoader, or new service boundaries.
- Schema migrations, seed changes, RLS, new indexes, rate-limiting
  infrastructure, idempotency keys, production database configuration, or
  deployment changes.

## Dependencies

- MVP-001 is accepted in `main` at the verified baseline and remains the only
  feature dependency.
- Preserve MVP-001 rules: exact/case-sensitive tenant-local `device_key`,
  unchanged display-name validation, deterministic descending keyset order,
  page sizes 1–100, and not-found behavior for cross-tenant detail reads.
- Preserve the FND-001 operational REST/OpenAPI contract except for the
  additive readiness reason required when PostgreSQL becomes a live dependency.
- Preserve FND-004's narrow readiness adapter. It already maps every non-200
  backend response to generic unavailable and must not become a generic
  GraphQL proxy in this PR.
- The 13 existing CI jobs remain independent. No database prerequisite may be
  added to ordinary unit/race/browser jobs.

## Architecture / Boundaries

The request path is:

```text
untrusted POST /graphql
-> Fiber request/body/method limits and request ID
-> explicit Fiber-to-net/http context bridge
-> fixed development principal from server-owned context
-> gqlgen parsing, validation, complexity control, and resolver
-> tenant-scoped registry interface
-> parameterized pgx repository query
-> deliberate GraphQL DTO/error mapping
```

- SDL owns the public product contract. Generated gqlgen code implements that
  contract mechanically; resolvers remain transport adapters.
- The device-registry package continues to own validation, persistence rules,
  and tenant-scoped queries. GraphQL DTOs do not alias repository records, and
  persistence-only `OrganizationID` is never returned.
- A narrow resolver-facing registry interface supports unit/contract tests
  without adding a pass-through service layer. Add a slug lookup to the
  registry boundary only for startup identity resolution; it is not exposed in
  GraphQL.
- The identity adapter owns context construction and extraction. Resolvers
  fail closed when the principal is absent; they never read tenant authority
  from GraphQL variables, headers, or cookies.
- The platform HTTP server owns mounting, request context/correlation,
  readiness, and lifecycle only. It accepts an optional standard HTTP handler
  and dependency probe rather than importing generated GraphQL or device code.
- PostgreSQL remains the single MVP authority. This slice changes runtime
  consumption, not schema, ownership, or migration policy.

## GraphQL contract

SDL names may be split across files, but the public shape and semantics are:

```graphql
scalar Time

type Device {
  id: ID!
  deviceKey: String!
  displayName: String!
  createdAt: Time!
}

type DeviceEdge {
  cursor: String!
  node: Device!
}

type PageInfo {
  endCursor: String
  hasNextPage: Boolean!
}

type DeviceConnection {
  edges: [DeviceEdge!]!
  pageInfo: PageInfo!
}

input CreateDeviceInput {
  deviceKey: String!
  displayName: String!
}

type Query {
  device(id: ID!): Device
  devices(first: Int! = 20, after: String): DeviceConnection!
}

type Mutation {
  createDevice(input: CreateDeviceInput!): Device!
}
```

### Contract semantics

- Device IDs are returned in canonical UUID string form but remain opaque to
  consumers. An input ID must be a canonical value previously emitted by the
  API; malformed IDs are `BAD_USER_INPUT`.
- `device` returns `null` without an error for an absent or other-tenant ID so
  the contract does not reveal which case occurred. An unexpected resolver
  failure returns `device: null` plus a safe error; failures of non-null root
  fields (`devices` and `createDevice`) null the operation data as required by
  GraphQL null propagation.
- `devices.first` accepts 1–100. Results use repository order
  `(created_at DESC, id DESC)`. Empty results return `edges: []`,
  `hasNextPage: false`, and `endCursor: null`; a non-empty page returns the
  last edge cursor as `endCursor` whether or not another page exists.
- Every edge cursor is an opaque, versioned, bounded encoding of the existing
  repository continuation. Invalid version, structure, timestamp, UUID, size,
  or trailing data is `BAD_USER_INPUT`. Consumers may only store and replay the
  value; its encoding is not a client contract.
- Forward pagination is not a snapshot. Inserts newer than an already-issued
  cursor do not duplicate later pages and become visible when the consumer
  refreshes from the beginning.
- `Time` emits canonical UTC RFC 3339 Nano values. Repository values are mapped
  explicitly; GraphQL models do not expose organization IDs or database
  metadata.
- Create inputs are not silently trimmed, normalized, or case-folded. Existing
  registry validation remains authoritative: device keys are 1–128 Unicode
  code points with no surrounding Unicode whitespace; display names are
  1–200 code points and not whitespace-only.
- `createDevice` is intentionally not idempotent. A retry after an unknown
  outcome can receive `CONFLICT` because the first request committed; the
  client may refetch by list/detail. Idempotency keys require a separate need
  and persistence contract.
- Expected resolver errors have stable `extensions.code` values and safe
  messages: `BAD_USER_INPUT`, `CONFLICT`, and `INTERNAL_SERVER_ERROR`.
  Parsing and validation failures use `GRAPHQL_PARSE_FAILED` and
  `GRAPHQL_VALIDATION_FAILED`. Errors include the safe request ID when
  available, never raw pgx errors, SQL, configuration, stack traces, request
  bodies, or tenant existence detail.
- Requests require `Content-Type: application/json` and an empty, wildcard, or
  `application/graphql-response+json` `Accept` value. Configure the POST
  transport to return `application/graphql-response+json`; reject an explicitly
  incompatible response media type rather than exposing gqlgen's legacy
  status-code branch. Malformed JSON and parse/validation failures return HTTP
  400, while a parsed and validated operation returns HTTP 200 even when a
  resolver/domain error is present. Because Fiber mounts only POST at
  `/graphql`, other methods return the existing safe 405 transport envelope
  without executing GraphQL. Unsupported content type, oversized body, and
  disabled-route behavior are locked by HTTP contract tests rather than
  inferred from framework defaults.
- The first schema is unversioned and has no accepted external consumer yet.
  After MVP-003, evolve it additively; field removal, nullable-to-non-null
  changes, stricter arguments, error-code changes, or cursor incompatibility
  require explicit compatibility review rather than silent regeneration.

## Runtime and identity decisions

- `PULSEGRID_IDENTITY_MODE=development` is allowed only for
  `PULSEGRID_ENV=development|test`. It enables `/graphql`, requires a literal
  IPv4 loopback `PULSEGRID_HTTP_HOST` (`127.0.0.0/8`), the existing
  environment-correct local/test database URL, and the fixed `pulsegrid-dev`
  organization. Wildcard, hostname, non-loopback, and IPv6 binds are rejected;
  production rejects this mode even if a database URL exists.
- `PULSEGRID_IDENTITY_MODE=disabled` mounts no product API and opens no
  database. This keeps current health-only production and browser-smoke
  composition valid without pretending that it serves product traffic.
- Keep migration/seed target guards in `cmd/db`. Refactor shared local/test
  database parsing only as needed so both explicit DB commands and the enabled
  API consume one validated contract; do not weaken production or remote-host
  rejection.
- Startup order is configuration -> database pool/ping -> organization lookup
  -> GraphQL construction -> listener. Partial startup closes any opened pool.
- Each GraphQL request receives a bounded execution context shorter than the
  server write timeout. The Fiber request ID and development principal are
  copied deliberately into the `net/http`/gqlgen context and verified by a
  framework-boundary test.
- When enabled, readiness probes the required pool with a shorter bounded
  timeout, returns `dependency_unavailable` without topology detail during an
  outage, and recovers automatically when PostgreSQL recovers. Liveness stays
  `{"status":"ok"}` while the process is alive.
- Shutdown enters draining, stops new HTTP admission, allows accepted requests
  to finish within the existing shutdown bound, and closes the pool only after
  HTTP shutdown completes.

## Security / threat model

Protected assets are tenant-owned device records, tenant authority, database
credentials, and internal diagnostics. The only intended actor in this slice
is a controlled local/test operator; every HTTP client remains untrusted.

| Credible abuse case | Preventive control | Required evidence | Residual risk |
| --- | --- | --- | --- |
| Client forges an organization ID/header/cookie | No tenant field exists; fixed server principal is inserted before resolvers; every repository call receives that principal | Schema inspection plus spoofed-input/header and missing-principal tests | Development identity is not real authentication and must never be enabled in production |
| Client supplies another tenant's device ID | Repository query includes active organization; detail returns indistinguishable null | Real-PostgreSQL HTTP test with two organizations | Timing is not claimed to be indistinguishable |
| Client creates in another tenant or mass-assigns protected fields | Create input allowlists only device key/name; tenant comes from context | Unknown/protected field and cross-tenant persistence tests | No RBAC or audit trail in MVP |
| Aliases/fragments/repeated lists amplify work | Existing 64 KiB body limit, a 1,000-token parser limit, POST-only transport, strict page bound, no batching/subscriptions, and a weighted gqlgen complexity limit calibrated to allow one maximum-page canonical query but reject repeated amplification | Contract/security tests for oversized, over-token, nested, aliased, fragmented, and repeated operations | No per-client rate limit; acceptable only on loopback development/test surface |
| Duplicate/replayed create | Database tenant-local unique constraint and stable conflict mapping | Sequential and concurrent duplicate tests at the real store | Lost-success retries are not transparently idempotent |
| Failure leaks data, credentials, SQL, or stack detail | Central error presenter/recoverer, safe messages/codes, redacted structured logs, bounded request ID | Response/log leakage tests for repository error and panic | Local operators can still inspect their own process/database |
| Development product API is exposed in production | Production rejects development identity mode and disabled mode mounts no route | Production configuration/startup and `/graphql` absence tests | A future production identity design requires a new security review |

No cookies or browser credentials are introduced, GET execution is disabled,
and CORS remains disabled; CSRF middleware is therefore not added. MVP-003 must
use a reviewed same-origin server path or re-plan origin/credential behavior.

## Implementation Direction

1. Pin gqlgen, add `gqlgen.yml`, the SDL, explicit DTO/scalar/cursor mapping,
   and root/module generation commands. Keep generated execution/model files in
   dedicated paths and add a clean-tree generation-drift check to `api-static`
   and the composed local check.
2. Add identity-mode parsing and tests before runtime wiring. Keep disabled as
   the test/production default, reject invalid/production development mode,
   and update environment examples without adding secrets.
3. Add registry lookup by exact organization slug with unit/real-store
   evidence. Do not expose organization lookup or mutate seed state through
   GraphQL.
4. Implement cursor codec, explicit model mapping, narrow registry interface,
   resolvers, safe error presenter/recoverer, POST/response-media transport,
   introspection guard, parser token limit, and weighted complexity control.
   Unit-test these without PostgreSQL.
5. Refactor HTTP composition to accept optional product handler/readiness
   dependencies, propagate context explicitly across Fiber and `net/http`, and
   preserve existing health/error/request-ID behavior.
6. Compose the enabled runtime in `cmd/api`: open/ping one pool, resolve the
   development organization, construct GraphQL, serve, drain, then close.
   Keep disabled mode database-free and clean up partial startup failures.
7. Add real-PostgreSQL GraphQL integration tests in the existing integration
   suite, then update CI/scripts so that only `api-db-integration` enables the
   development identity and supplies PostgreSQL. Keep browser smoke explicitly
   disabled and database-free.
8. Update documentation and OpenAPI readiness enum. Review the complete diff
   for API, tenant security, database runtime, configuration/lifecycle,
   generated artifacts, and affected docs; fix findings; then run final
   validation on the reviewed result.

## Validation

### Guarantee-to-evidence map

| Guarantee | Evidence and gate |
| --- | --- |
| SDL and generated Go agree | Pinned generation command, generated-artifact drift check on a clean checkout, Go build/vet/static checks, and schema contract tests |
| Contract success and bounds | No-database HTTP/resolver tests cover create/detail/list mapping, explicit 1/100 bounds, empty detail, nullability, time/ID output, cursor round trip, and unknown fields; default-page and final-page success cases remain a documented evidence gap |
| Stable safe failures | Contract tests cover malformed ID/cursor, zero/over-limit page size, invalid device fields, duplicate conflict, not found, panic recovery, request ID, and safe public messages; dependency/cancellation and log-redaction claims remain code-review or runtime evidence rather than direct GraphQL contract assertions |
| Tenant authority cannot come from client input | Schema contains no organization field; tests send a spoofed tenant header and an unknown tenant input field and verify the fixed context principal; missing principal fails closed |
| Cross-tenant reads and writes are contained | Real-PostgreSQL HTTP tests use two organizations, other-tenant IDs, list/detail/create, and inspect stored ownership; mock-only tests do not satisfy this guarantee |
| Abuse is bounded | Tests cover POST-only behavior, unsupported request/response media types, 64 KiB body and 1,000-token parser limits, disabled batching/subscriptions, aliases/fragments/repeated selections, and complexity rejection while one canonical 100-item query remains allowed |
| Runtime database lifecycle is truthful | Config/composition tests cover disabled mode without DB, development-identity loopback binding, readiness dependency-ready/unavailable/recovered states, in-flight cancellation, and draining; startup failure classification is unit-tested, while partial-startup cleanup and pool-close ordering remain code-reviewed rather than directly instrumented |
| Existing operational and frontend behavior remains stable | Existing HTTP server, OpenAPI, API unit/race, Nuxt, and browser-smoke tests remain green; FND-004 still maps the new 503 readiness reason to generic unavailable |
| Dependency and generated-code risk is checked | `go mod verify`, build/vet/modernize/staticcheck, `govulncheck`, pinned gqlgen version review, and no unexpected generator/runtime dependency drift |

### CI and local gate placement

- Keep all 13 current CI jobs. Extend `api-static` with scoped generation drift
  detection and extend `api-db-integration` with the real-store GraphQL suite.
  Do not serialize unrelated jobs or create a second PostgreSQL service job
  without evidence that isolation requires it.
- `api-test` and `api-race` run GraphQL resolver/contract tests against a narrow
  fake registry with identity mode disabled at process level; they must not
  skip claimed real-database evidence.
- `api-db-integration` migrates the disposable test database, creates the
  controlled test organization explicitly, enables development identity, and
  exercises the real Fiber -> gqlgen -> registry -> PostgreSQL path.
- `browser-smoke` explicitly uses disabled identity mode and continues to start
  the health-only binary with no database. MVP-003 will own the first real
  browser-to-GraphQL journey.
- After implementation and review fixes, run targeted generation/config/
  resolver/HTTP/integration tests first, then the repository's final
  `check:fast`, integration, browser, generation-drift, and full `check`
  surfaces as applicable. Report failed, unavailable, skipped, and not-run
  checks separately; CI on the final PR head remains authoritative.

## Documentation Updates

- `apps/api/README.md`: development startup sequence (database up -> migrate ->
  seed -> API), identity mode, GraphQL curl examples, generate/test commands,
  readiness behavior, and production-disabled limitation.
- `docs/api/README.md`: mark the operator product API implemented for
  development/test, link SDL, document endpoint/method, operations, bounds,
  pagination/error semantics, and lack of production auth.
- `docs/project-setup/environment-configuration.md` and API env examples:
  document `PULSEGRID_IDENTITY_MODE`, expand database URL ownership from
  commands/tests to the enabled local API, and preserve blank production
  secret injection semantics.
- `docs/project-setup/local-development.md`: add the reproducible GraphQL
  journey and failure recovery without deleting the development volume.
- `docs/architecture/system-architecture.md`: correct the stale current-state
  paragraph and record the implemented API/BFF -> registry -> PostgreSQL path
  without changing the modular-monolith boundary.
- `docs/architecture/technology-decisions.md`: record the reviewed gqlgen pin,
  schema-first generation, POST-only development surface, and the trigger for
  revisiting transport/introspection/production identity.
- Update roadmap/index/status links only when the plan lifecycle actually
  changes; do not move this plan to `completed/` in the implementation commit
  until review and required evidence are complete.

## Risks / Open Decisions

No decision-changing design question remains for implementation. The following
risks are explicitly bounded:

- **Non-production identity:** fixed development authority is intentionally an
  unsafe substitute for user authentication. Production rejects it and the
  route is absent. Any production operator API, cookie/token use, RBAC, or CORS
  is a new material security decision.
- **Cursor compatibility:** cursors are versioned and opaque, but in-flight
  cursor compatibility must be preserved or deliberately migrated once a
  consumer exists. Until MVP-003, only server contract tests consume them.
- **Retry ambiguity:** create has durable unique/conflict semantics but no
  idempotency key. Add one only when a real retry requirement defines key
  ownership, lifetime, and stored outcome.
- **No rate limiter:** body, operation, complexity, list, database pool, and
  timeout bounds prevent obvious unbounded work on the loopback-only surface.
  Per-actor/IP limiting requires a real identity/exposure model and is
  production-hardening scope.
- **Dependency readiness cost:** PostgreSQL readiness probes must be bounded
  and use the existing shared pool. If measured probe pressure or latency is
  material, adjust the check/caching policy from evidence rather than hiding
  database unavailability.
- **Generated change volume:** generated gqlgen files are expected review
  artifacts. Keep handwritten and generated paths separate so review can focus
  on SDL/config and handwritten behavior while CI proves reproducibility.

## Alternatives considered

- **Client-supplied organization UUID or development header:** rejected because
  it makes untrusted input the tenant authority and directly contradicts
  MVP-001's handoff.
- **Store a generated organization UUID in tracked configuration:** rejected
  because IDs differ by database and would drift from the seeded authority.
  Exact server-side slug lookup is simpler and deterministic.
- **Enable GraphQL in every environment and call it development-only in docs:**
  rejected because documentation is not an enforcement boundary. Explicit mode
  validation and route absence fail closed.
- **Add production authentication/RBAC now:** rejected because the MVP is a
  non-production proof and identity-provider, role, session, and production
  topology decisions are intentionally Post-MVP.
- **Unbounded or truncated list:** rejected. MVP-001 already supplies keyset
  pagination, and silently dropping continuation would make the public
  contract incorrect.
- **Expose raw repository cursor fields or offset pagination:** rejected because
  it leaks persistence shape or discards the stable continuation already
  implemented.
- **Add a pass-through application service, ORM, DataLoader, REST mirror, or
  new CI job:** rejected because none protects a current requirement or
  boundary; the narrow registry interface and existing real-store gate are
  sufficient.

## Engineering Improvement Review

- **Current scope:** opaque keyset pagination, explicit fail-closed identity
  mode, safe error/correlation mapping, GraphQL amplification bounds, truthful
  database readiness, deterministic generation, and real-boundary tenant tests
  are tightly coupled to the first product API and prevent credible contract,
  isolation, runtime, or false-validation failures.
- **Future enhancements:** production identity/RBAC/audit/RLS, idempotency keys,
  rate limiting, client-cache policy, subscriptions, and query batching remain
  with their first proven consumer or production requirement.
- **Scope effect:** the PR remains one API slice with no schema migration,
  frontend implementation, deployment, or new runtime. The revised plan makes
  previously open pagination, identity, readiness, and error decisions
  explicit without expanding the product outcome.

## Rollback / containment

- Before merge, revert the PR or set identity mode to `disabled`; no migration
  or generated client exists to coordinate.
- After a local rollout, disabling the identity mode removes `/graphql` and
  database startup/readiness coupling while preserving existing health routes.
- Devices successfully created before rollback remain valid MVP-001 rows and
  require no rollback. This plan does not authorize destructive cleanup.
- If a contract flaw is found after MVP-003 begins, prefer an additive forward
  fix or explicit cursor/error compatibility handling rather than silently
  changing consumer-visible semantics.

## Done Criteria

The reviewed SDL and generated artifacts reproducibly expose POST-only
development create/detail/forward-list operations. Runtime identity resolves
the seeded organization without accepting tenant authority from the client;
real-PostgreSQL HTTP tests prove two-tenant isolation and repository behavior.
Input, cursor, nullability, error, request-ID, complexity, and retry semantics
match this plan. Enabled startup/readiness/shutdown own PostgreSQL truthfully;
disabled and production modes expose no product route. Existing 13 CI jobs
remain green with real-store evidence in `api-db-integration`, affected docs
and OpenAPI agree with runtime behavior, the implementation diff receives
post-implementation API/security/database/runtime review, fixes are revalidated,
and no production-auth or deployment claim is made.

## Selected technical references

- [gqlgen v0.17.95 release](https://github.com/99designs/gqlgen/releases/tag/v0.17.95)
  and [configuration](https://gqlgen.com/v0.17.95/config/)
- [gqlgen error handling](https://gqlgen.com/v0.17.95/reference/errors/),
  [query complexity](https://gqlgen.com/v0.17.95/reference/complexity/), and
  [introspection](https://gqlgen.com/v0.17.95/reference/introspection/)
- [Fiber v3 `net/http` adaptor](https://docs.gofiber.io/middleware/adaptor/)
