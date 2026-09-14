# MVP-001 — Tenant and Device Persistence

Status: In Progress

Branch: `feat/mvp-001-tenant-device-persistence`

Intended PR: One persistence-foundation PR. Implementation follows the
reviewed and approved plan; completion remains blocked on final validation and
post-implementation review.

Milestone: M1 — Device registry

Impact: Material Change (Tier 2): first durable data authority, tenant-scoped
repository boundary, local database service, and database-backed CI gate. No
existing/live data migration or production operation is authorized here.

## Goal

Create PostgreSQL authority for organizations and registered devices. A
controlled local organization can be created explicitly; a trusted internal
caller can create, list, and get its devices through a tenant-scoped repository.
Real-PostgreSQL tests must prove constraints, scoped reads, duplicate behavior,
and test/dev isolation. This is not an operator-facing provisioning journey.

The requirement comes from the [MVP boundary](../../../product/product-scope.md)
and [device-registry ownership](../../../architecture/system-architecture.md).
MVP-002 owns trusted development-identity mapping and GraphQL; MVP-003 owns the
console. Repository scoping is **not** production authentication/authorization
or proof that an arbitrary SQL user is tenant-isolated.

## Why

Provisioning and later telemetry/command flows need one trustworthy
device-to-organization authority. A real database boundary and test isolation
must exist before MVP-002 exposes product operations.

## Verified repository baseline (2026-09-14)

- `main`, `origin/main`, and this clean branch point to `cab5874`, which
  merged FND-004. The Go/Fiber process has only health endpoints; Nuxt has
  only the fixed local readiness adapter.
- `apps/api/` has config, HTTP server, and logging packages, but no database
  driver, pool, repository, migration, Compose service, or product API. Current
  Go config contains only used HTTP keys.
- Twelve independent CI jobs are required, including `api-test`, `api-race`,
  and `browser-smoke`. None supplies PostgreSQL; browser smoke starts the API
  without it.
- Environment policy requires separate test resources, ignored real
  `.env.*` files, and migration execution separate from API startup. Docker
  Compose is available locally, but no Compose file is tracked.

## Scope

1. Add local PostgreSQL 18.6 via Docker Compose, pinned to a reviewed image
   digest at implementation. Bind only to loopback; mount the development
   volume at the v18 image's supported `/var/lib/postgresql` path. Provide a
   separate disposable test service/profile and Compose project. CI may use a
   native PostgreSQL service container instead of Compose.
2. Add versioned SQL-only migrations with a pinned Goose runner/library. Keep migration
   execution explicit and separate from API startup. The initial migration
   creates only `organizations` and `devices`; it contains no seed data.
3. Add a PostgreSQL driver/pool and Go device-registry persistence boundary.
   Implement `CreateDevice`, `GetDevice`, and bounded `ListDevices` under an
   explicit trusted organization ID. Add only the organization lookup/creation
   needed by the guarded development seed and test fixtures; no public
   organization-management interface.
4. Add an idempotent development-only organization seed command. It refuses
   test, production, unknown, and non-local targets; it never logs credentials
   or runs implicitly during migration/API startup.
5. Add database configuration/examples only for migrations, seed, and repository
   tests. Reuse `PULSEGRID_ENV`/dotenv precedence. Require the database URL
   when those consumers run, but do not make the current health-only API
   require PostgreSQL before MVP-002 introduces a product consumer.
6. Add real-PostgreSQL integration tests and one independent required
   `api-db-integration` CI job. Preserve all existing checks and parallelism;
   do not hide integration skips inside `api-test` or `api-race`.

## Out of scope

- GraphQL/REST CRUD, resolvers, UI, production identity/RBAC, device
  credentials, MQTT, telemetry, commands, and production deployment.
- Runtime database readiness or mandatory API-to-database connection. Revisit
  in MVP-002 when the API actually consumes PostgreSQL; preserve the current
  FND-004 health contract until then.
- Organization/device deletion, transfer, update, lifecycle transitions,
  soft-delete/history/audit policy, arbitrary search, and bulk import.
- PostgreSQL RLS, per-tenant database roles, and production tenant-isolation
  topology. The private Go backend has no trusted per-session tenant context
  yet; repository scoping is the current enforcement boundary.
- Automatic migration at app startup, production migration execution, and
  destructive operations against a non-disposable database.

## Dependencies

FND-004 is merged at the verified baseline. MVP-002 must derive the trusted
organization ID from its development-identity adapter, never from a GraphQL
argument. Its current plan defers pagination: before exposing a GraphQL list,
MVP-002 must decide how to expose the repository continuation or an explicit
bounded-result contract, rather than silently truncating results. MVP-004/
MVP-005 may later resolve registered devices, but this PR does not define MQTT
or event identity. Existing API health and browser smoke must remain unchanged.
Compose, test commands, and environment examples are the coupled operational
surfaces.

## Architecture / Boundaries

The device registry owns device identity and organization association. Other
modules may reference its stable device ID but cannot modify ownership
directly. Stored rows and repository records are internal contracts, not
GraphQL, REST, or MQTT contracts.

### Domain and persistence decisions to approve

- `organizations`: database-generated UUID primary key, unique lowercase
  ASCII reference slug (1–63 characters), nonblank display name (1–200
  characters), and `created_at timestamptz`. The controlled seed uses slug
  `pulsegrid-dev`; do not hard-code its generated UUID in application code.
- `devices`: database-generated UUID primary key, non-null
  `organization_id` foreign key with delete restriction, nonblank opaque
  `device_key` (1–128 characters), nonblank display name (1–200
  characters), and `created_at timestamptz`. `device_key` is
  exact/case-sensitive, unique within an organization, has no surrounding
  whitespace, and is immutable in this slice. Display name is not identity.
  PostgreSQL 18 provides UUIDv7 defaults; return stored IDs rather than
  inventing them.
- Unknown organization on create fails. Duplicate
  `(organization_id, device_key)` maps the unique violation to a stable
  internal conflict; concurrent creates leave exactly one row, while the same
  key in another organization remains allowed. Do not upsert or silently
  change an existing device.
- Every read SQL predicate includes organization ID. Cross-organization
  `GetDevice` returns not found without disclosing another tenant's data.
  `ListDevices` uses deterministic `(created_at DESC, id DESC)` order,
  accepts page sizes 1–100, and returns keyset continuation; add the
  supporting tenant-first index, not speculative indexes. Reject
  invalid/missing organization IDs.
- Constraints protect nullability, nonblank/length bounds, tenant-local
  uniqueness, and the organization relationship. They complement repository
  scoping but do not authenticate callers. No ownership update/delete method
  exists, so this PR cannot claim a cross-tenant mutation authorization test.

These are narrow MVP modeling assumptions, not permanent product policy. A
global or case-insensitive key, transfer, deletion, or lifecycle requirement
would change the schema/contract and must be reviewed before implementation.

## Implementation Direction

Use direct parameterized pgx queries behind the registry repository rather
than introducing an ORM or SQL generator for two tables. Use SQL transactions
only where one observable operation spans multiple writes; the single-row
device create relies on database FK/unique constraints. A separate database
configuration loader is invoked by DB commands/tests; `cmd/api` does not
construct a pool in this PR. `PULSEGRID_DATABASE_URL` is the server-only
connection key; Compose receives a matching disposable local password from
an ignored file or process environment, never a tracked usable credential. The
production example keeps the key blank and documents secret-manager injection.
Commands ping the selected database before mutating it and fail within a
bounded timeout if unavailable.

### Implementation sequence

1. Pin PostgreSQL 18.6 and Goose/pgx versions. Add separate dev/test Compose
   resources and fail-closed database URL parsing. Development/test commands
   accept only `127.0.0.1:5432/pulsegrid_dev` or
   `127.0.0.1:15432/pulsegrid_test` respectively. CI maps its isolated
   service to the test port. Do not log full URLs; tracked examples contain
   no usable production credentials.
2. Add the initial transactional migration and guarded `status`/`up`
   commands. Test `up`, repeat `up`, and `down`/re-`up` **only** against
   disposable test data. Applied non-disposable migrations are immutable;
   recovery is a reviewed forward fix, not automatic `down`.
3. Implement a bounded pool owned by the database consumer/test lifecycle,
   not one pool per query. Repository operations take context, use SQL
   parameters, close rows, map expected conflict/not-found errors, and avoid
   leaking connection details.
4. Implement repository, idempotent seed, and two-organization fixtures.
   Exercise constraints, concurrent duplicate create, scoped reads, ordering,
   limits, invalid IDs, connection failure, and cancellation on real Postgres.
5. Update scripts/examples/guides. Review the actual code diff for domain,
   database, tenant-security, migration, and config/runtime effects; resolve
   findings; then run final local validation and CI on the same PR head. Move
   this plan to `completed/` only after the feature-plan lifecycle is met.

## Validation

Keep database tests in an explicit Go integration-test selection (for example,
a dedicated `integration` build tag) so ordinary `go test ./...` and its
race variant do not silently require Docker or skip a claimed database proof.
The dedicated command must fail if its test database is unavailable.

### CI and validation contract

| Guarantee | Evidence and gate |
| --- | --- |
| Existing API/Nuxt behavior unchanged | Keep all twelve required jobs independent and green, especially `api-test`, `api-race`, and `browser-smoke`; add no database prerequisite to them. |
| Schema and conflict semantics | New required `api-db-integration` job starts pinned PostgreSQL 18 with a health check, migrates, and tests FK/unique/check constraints, repeat migration, concurrent duplicate create, and error rollback. |
| Scoped reads and bounded lists | Same real-store job uses two organizations and covers cross-tenant get/list, empty/not-found, deterministic ordering, page boundary/cursor, and invalid organization ID. Mock-only tests do not prove this. |
| Test isolation and safe mutation | Config/command tests reject a development DB in test mode, remote/production seed targets, missing/invalid URL, and destructive operations; CI supplies only the disposable test database. |
| Resource failure | Focused tests cover connection failure, cancellation, and pool/row cleanup; existing Go static/race gates remain required. No API database-readiness claim is made. |

`api-db-integration` starts in parallel with all existing jobs, with its own
Go cache and PostgreSQL service; it does not wait for `api-test` or
`browser-smoke`. Measure cold/warm critical path against the roughly
two-minute feedback target. If startup is slower, improve setup/cache or
report the exception—never drop the real-store gate to make CI look fast.
Adding a required branch-protection context needs separately approved GitHub
settings work and verification on the final head. Until then, the new test is
not an enforced merge gate.

Local evidence after implementation: a warm disposable integration run
completed in approximately 9 seconds with the pinned image already cached;
the CI cold/warm measurement remains a PR validation item.

## Local workflow to document

- Keep `setup` for dependency installation and `check:fast` as no-database
  early feedback. Full pre-CI `check` should include the isolated database
  integration test and document its Docker prerequisite.
- Add/document root commands `db:dev:up`, `db:dev:migrate`,
  `db:dev:status`, `db:dev:seed`, `db:dev:stop`, and
  `api:test:integration` (or equally explicit names). The test command owns
  a unique disposable Compose project, refuses to attach to a process already
  using its port, and cleans up only that project's service/volume. Never use
  a broad `docker compose down -v` that can erase development data.
- From a clean checkout: create ignored local credentials, start development
  Postgres, migrate, seed, run integration tests against isolated test Postgres,
  then start the unchanged API/web processes. Explain failed-local-setup
  recovery without deleting non-disposable data.

## Documentation updates

Update `apps/api/README.md`, the local-development guide, environment
strategy, API env examples, and technology decisions with selected versions,
commands, local-only credential handling, data ownership, and lack of
production auth/RLS. Update roadmap/index only when lifecycle status changes.
No OpenAPI or browser contract is added.

## Risks / Open Decisions

- **Tenant authority:** a future caller could pass the wrong organization ID.
  MVP-002 must establish and test trusted identity-to-organization mapping;
  otherwise that PR is blocked. RLS is deferred without a trusted DB tenant
  session, not dismissed as unnecessary forever.
- **Identity assumption:** tenant-local, case-sensitive `device_key` is the
  smallest duplicate rule for the current journey. Different normalization
  or global uniqueness needs plan/schema review.
- **Data recovery:** the initial migration begins from empty data. Dev volume
  persists; test volume is disposable. Production/shared migration, backup,
  restore, and rollout are outside this approval. Recreate only a failed test
  project; inspect non-disposable migration status and forward-fix deliberately.
- **Alternatives:** a shared dev/test instance is cheaper but weakens the
  first persistence test boundary. A custom migration runner adds unnecessary
  migration-state/locking code. A pooler, ORM, RLS session policy, and product
  HTTP endpoint have no current consumer or authority.

## Engineering Improvement Review

- **Current scope:** tenant-filtered bounded reads, FK/uniqueness guarantees,
  guarded seed/migrations, real-store CI, and test isolation prevent credible
  integrity or false-validation failures at this new boundary.
- **Future enhancements:** production identity/RLS/topology and device
  lifecycle/deletion require their owning plans; measured pool/query tuning
  requires workload evidence.
- **Scope effect:** original persistence outcome becomes testable without
  adding a product API or changing FND-004 health behavior.

## Done Criteria

The reviewed schema migrates into fresh PostgreSQL 18 test data; dev seed is
repeatable; scoped repository operations persist/retrieve devices with stable
conflict/not-found and bounded list behavior; real-store tests prove
constraints, two-tenant reads, concurrency, disposable migration recovery,
and test/dev separation. Existing twelve required jobs and the newly protected
`api-db-integration` check pass on the same final PR head. Local instructions
reproduce the journey; post-implementation review and final validation pass;
no production identity, product API, or production database action is claimed.

## Selected technical references

- [PostgreSQL 18 UUIDv7](https://www.postgresql.org/docs/18/functions-uuid.html)
  and [supported release policy](https://www.postgresql.org/support/versioning/)
- [Docker Official PostgreSQL image and v18 volume layout](https://hub.docker.com/_/postgres)
- [Goose SQL migration annotations](https://pressly.github.io/goose/documentation/annotations/)
  and [CLI commands](https://pressly.github.io/goose/documentation/cli-commands/)
- [pgx driver and pool](https://github.com/jackc/pgx)
- [GitHub Actions PostgreSQL service containers](https://docs.github.com/en/actions/tutorials/use-containerized-services/create-postgresql-service-containers)
