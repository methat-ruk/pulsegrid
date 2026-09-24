# PulseGrid local development

Status: Local repository workflow and merge-gate enforcement implemented;
FND-004 readiness, the MVP-003 device-registry browser journey, the MVP-005
local/test MQTT ingestion path, and the merged MVP-006 telemetry
persistence/current-state path are implemented and validated. The MVP-007
operator-visible telemetry console and review fixes were merged as `9ce9e7c`
(PR #16). Rules and alerts remain planned.

This is the canonical guide for setting up and validating the repository. The
Go API and Nuxt console remain independently runnable, with an opt-in local
readiness adapter for the first full-stack development feedback loop.

## Requirements

- Node `24.20.0` from `.node-version`;
- pnpm `12.3.4` through Corepack;
- Go `1.27.1` from `.go-version`;
- Docker Compose with the pinned PostgreSQL and Eclipse Mosquitto images
  available locally.

Verify the selected versions before setup:

```sh
node --version
corepack pnpm --version
go version
```

## First setup

From the repository root:

```sh
corepack pnpm run setup
```

This runs a frozen workspace install and downloads Go modules. It must work
from a clean checkout without another lockfile or an ignored local file.

Install the pinned local Chromium headless shell only when browser evidence is
needed:

```sh
corepack pnpm run setup:browser
```

CI installs the same pinned browser revision with the required Linux
dependencies. Browser binaries and generated application output are not
committed or cached as correctness inputs.

## Runtime modes

### Independent development processes

Run each application in its own terminal. Neither command starts the other
application or any database, broker, proxy, or full-stack dependency.

```sh
corepack pnpm run dev:api
corepack pnpm run dev:web
```

The API uses `PULSEGRID_ENV=development` and listens on
`http://127.0.0.1:8080` by default. The console uses its `.env.development`
contract; copy the reviewed examples first if the files do not exist:

```sh
cp apps/api/.env.development.example apps/api/.env.development
cp apps/web-console/.env.development.example apps/web-console/.env.development
```

`NUXT_BACKEND_ORIGIN` is a server-only development key. It points only to the
loopback API origin and is consumed by the Nuxt same-origin readiness and
GraphQL adapters; the adapters use only their fixed upstream paths. It is
never exposed through `runtimeConfig.public` or forwarded from the browser.
Do not add credentials, tokens, or other private service URLs to frontend
examples.

### Local dependency services in one command

After creating `apps/api/.env.development` with a disposable local database
password, start every development dependency together. The command loads the
database password for Compose, starts PostgreSQL and Mosquitto in parallel, and
waits for both health checks:

```sh
corepack pnpm run docker:dev:up
corepack pnpm run docker:dev:health
corepack pnpm run docker:dev:logs
corepack pnpm run docker:dev:stop
corepack pnpm run docker:dev:down
```

It does not run migrations or seed data. Run `db:dev:migrate` and
`db:dev:seed` before enabling the development GraphQL API. The service-scoped
stop/down commands remain available when only one dependency should change.
`docker:dev:up` uses Compose `up`, so it creates missing containers and starts
existing ones. `docker:dev:stop` stops both containers without removing them;
`docker:dev:down` removes both containers and their Compose network while
preserving the named PostgreSQL volume.

### Local MQTT broker, API consumer, and simulator

MVP-004 adds an ephemeral, loopback-only Mosquitto broker and a separate
one-shot device simulator. MVP-005 adds the opt-in API consumer in the same Go
process. The development broker uses `127.0.0.1:1883`; the isolated integration
broker uses `127.0.0.1:11883`. The broker has no named volume, retained
telemetry, dashboard, bridge, plugin, TLS, or production identity. Its Compose
lifecycle is service-scoped so PostgreSQL containers and volumes are not
removed:

```sh
corepack pnpm run mqtt:dev:up
corepack pnpm run mqtt:dev:health
corepack pnpm run mqtt:dev:logs
corepack pnpm run mqtt:dev:stop
corepack pnpm run mqtt:dev:down
```

To publish one observation, first register a device through the development
GraphQL/console journey and copy its canonical lowercase UUID into the ignored
`apps/api/.env.development` file. The simulator accepts only the exact local
development endpoint and fixed `pulsegrid-dev` tenant; it does not verify
registry membership or call the API:

```sh
cp apps/api/.env.development.example apps/api/.env.development
# Set PULSEGRID_DATABASE_URL and PULSEGRID_MQTT_DEVICE_ID in the ignored file.
corepack pnpm run docker:dev:up
corepack pnpm run mqtt:simulator
```

The command publishes MQTT 3.1.1 QoS 1 with `retain=false`, waits up to five
seconds for connect and PUBACK, then disconnects within a bounded quiesce
period. Missing broker, occupied port, invalid configuration, timeout, or
failed acknowledgement is a non-zero outcome. The output reports only the
topic, message ID, timestamp, QoS, and retain flag.

The API example enables `PULSEGRID_MQTT_INGESTION_MODE=development`, so start
the broker and database, migrate/seed, then start the API before publishing:

```sh
corepack pnpm run docker:dev:up
corepack pnpm run db:dev:migrate
corepack pnpm run db:dev:seed
corepack pnpm run dev:api
```

Check `GET /health/ready` for `200`/`ready`; the enabled API is ready only when
PostgreSQL is reachable, the required MVP-006 schema was validated before
listening, and its MQTT subscription is connected. A database below migration
005 fails startup with `database_schema_unavailable` rather than exposing a
partially usable telemetry API. Publish with
`mqtt:simulator`, query `deviceCurrentState` and `deviceTelemetry`, and inspect
the API log for `reason_code=telemetry_accepted`. The log exposes correlation
and registry IDs but never the raw payload or temperature. Invalid, retained,
unknown-device, wrong-tenant, oversized, and future-skewed messages are
rejected with stable reason codes. Exact replay does not add a history row or
advance `lastSeenAt`, including after the bounded history row has been pruned;
late observations remain queryable but cannot replace a newer
`(observedAt,messageId)` state. Stop with `Ctrl-C`; the API stops
admission, drains its bounded queue, and then closes the database pool.

Run the real API/broker/database integration evidence with a unique Compose
project. It registers a device through GraphQL, publishes with the real
simulator, verifies committed current state and bounded history through
GraphQL, checks strict rejection, exact replay, and late-observation semantics,
exercises retained input and readiness recovery across broker stop/start,
signals the API for drain, and removes only its own disposable resources on
success or failure:

```sh
corepack pnpm run mqtt:test:integration
```

If `127.0.0.1:1883` or `127.0.0.1:11883` is already occupied, stop the process
that owns that port or use the service-specific cleanup command. Do not use a
broad `docker compose down -v` because it can erase development PostgreSQL
data.

With both processes running, open `http://127.0.0.1:3000` (or the port shown by
Nuxt). The shell checks `GET /api/operational/ready` and offers a manual Retry
when the local API is stopped or starting. This adapter covers process
readiness only; it is not a product API or a generic proxy.

### MVP-007 telemetry console journey

The device detail route now consumes the existing MVP-006 GraphQL reads without
changing the API schema. Open a registered device at `/devices/:id` after the
local API and Nuxt console are running. The telemetry section initially shows an
explicit empty state. Publish with the one-shot `mqtt:simulator`, then activate
`Refresh telemetry`; the page shows the committed current value, observed and
received times, independent `lastSeenAt`, and newest-first history. A second
publish followed by another refresh makes the line chart eligible. `Load more
history` uses the opaque server cursor, while the local five-minute
`Recent signal`/`Stale signal` labels are derived only from `lastSeenAt`. The
page does not poll or label a device `Online`/`Offline`.

For the repeatable cross-boundary browser journey, use the repository command:

```sh
corepack pnpm run test:browser
```

The runner creates a unique Compose project with disposable PostgreSQL and the
existing `mqtt-test` Mosquitto service, runs migrations and seed, builds both
the API and device simulator, enables test-mode MQTT ingestion, starts the
Nuxt server, and removes only its own containers, volume, and network. It
asserts empty-to-current, explicit refresh, two-point chart/summary, the
controlled stale transition, populated history cards at 390px and 320px,
dense-chart time-label collision prevention at 320px, mobile overflow, the
existing registry/readiness journeys, and port-conflict cleanup behavior.
Headless assertions complement visible Browser inspection; they do not replace
it.

### Local PostgreSQL, seed, and development GraphQL

MVP-002 keeps database setup explicit. Copy the API example, set a disposable
password in the ignored file, and run migrations and seed before starting the
development-only GraphQL API:

```sh
cp apps/api/.env.development.example apps/api/.env.development
corepack pnpm run docker:dev:up
corepack pnpm run db:dev:migrate
corepack pnpm run db:dev:seed
corepack pnpm run db:dev:status
corepack pnpm run dev:api
```

`PULSEGRID_IDENTITY_MODE=development` resolves only the seeded
`pulsegrid-dev` organization. The API fails before listening if PostgreSQL,
migrations, or seed data is unavailable. The browser smoke uses the same
development identity mode against an isolated test database; the health-only
`disabled` mode remains available for tests that intentionally do not exercise
the product API.

### Migration preflight and recovery

Migration `004` makes the database whitespace rules match Go's Unicode
`strings.TrimSpace`. It intentionally does not rewrite existing rows. Before
applying it to a development volume that may contain data created by an older
checkout, run this read-only preflight from `psql`:

```sql
WITH whitespace(chars) AS (
  VALUES (U&'\0009\000A\000B\000C\000D\0020\0085\00A0\1680\2000\2001\2002\2003\2004\2005\2006\2007\2008\2009\200A\2028\2029\202F\205F\3000')
)
SELECT 'organizations' AS table_name, id, 'display_name' AS column_name,
       'length_or_unicode_whitespace' AS violation, display_name AS value
FROM organizations, whitespace
WHERE char_length(display_name) NOT BETWEEN 1 AND 200
   OR char_length(btrim(display_name, whitespace.chars)) = 0
UNION ALL
SELECT 'devices', id, 'device_key', 'length_or_unicode_whitespace', device_key
FROM devices, whitespace
WHERE char_length(device_key) NOT BETWEEN 1 AND 128
   OR device_key <> btrim(device_key, whitespace.chars)
UNION ALL
SELECT 'devices', id, 'display_name', 'length_or_unicode_whitespace', display_name
FROM devices, whitespace
WHERE char_length(display_name) NOT BETWEEN 1 AND 200
   OR char_length(btrim(display_name, whitespace.chars)) = 0;
```

An empty result is safe to continue with `corepack pnpm run db:dev:migrate`.
If rows are returned, stop before retrying the migration. Keep the rows for
review, choose an explicit valid replacement for each affected `device_key`
(1–128 characters with no surrounding Unicode whitespace; it is an identity
value), and choose a nonblank display name of at most 200 characters for
affected display-name rows. Apply those data changes only after confirming the
local data is disposable or obtaining the appropriate data-owner decision, then
rerun the migration and check its status. Do not use `migrate down`, delete a
volume, or run a broad Compose teardown as a migration-recovery shortcut.

The development service binds only to `127.0.0.1:5432`; `db:dev:stop` stops its
container without deleting the container or named volume. Use `db:dev:down` to
remove only the PostgreSQL container while preserving the named volume and its
data; it does not stop `mqtt-dev`. The isolated integration workflow owns a
unique Compose project and the test port `127.0.0.1:15432`:

```sh
corepack pnpm run api:test:integration
```

It fails when the test port is already in use, migrates the disposable
database twice, runs the tagged real-PostgreSQL tests, and removes only its
own container, volume, and network. Do not use a broad `docker compose down -v`
in this repository because it can erase development data.

To inspect development data, no host-side `psql` installation is needed—the
official PostgreSQL image includes the client:

```sh
corepack pnpm run db:dev:psql
```

The command resolves the Compose service instead of depending on a generated
container name. Pass normal `psql` arguments after `--`, for example
`corepack pnpm run db:dev:psql -- -c '\dt'`. If you use zsh and want shorter
commands from the repository root, add these optional functions to `~/.zshrc`:

```zsh
pgdevup() {
  corepack pnpm run db:dev:up
}

pgdevstop() {
  corepack pnpm run db:dev:stop
}

pgdevdown() {
  corepack pnpm run db:dev:down
}

pgdev() {
  corepack pnpm run db:dev:psql -- "$@"
}
```

Use `pgdevup` to start the persistent development database, `pgdev` or
`pgdev -c '\dt'` to inspect it, `pgdevstop` to stop the service while keeping
the container and volume, and `pgdevdown` to remove only the PostgreSQL
container while keeping the volume. A persistent `pgtest` alias is not provided because
`api:test:integration` deliberately creates a disposable database with a
random Compose project and removes it after the run.

The repository's Zed settings pass `-tags=integration` to `gopls`, so tagged
integration files remain navigable without changing the normal no-database Go
test command. Restart the Go language server after changing this setting.

### Isolated browser smoke

The browser command builds the console with `NUXT_APP_ENV=test`, starts that
fresh build on `127.0.0.1:4173`, and owns server teardown. It refuses to reuse
an existing process, so stop anything already listening on that port before
running it:

```sh
corepack pnpm run test:browser
```

This smoke owns an isolated PostgreSQL Compose project when
`PULSEGRID_DATABASE_URL` is not supplied, runs test migrations and the fixed
`pulsegrid-dev` seed, then builds the API test binary and Nuxt test artifact.
It starts both in an isolated process lifecycle and covers the device registry
through the real browser → Nuxt `/api/graphql` adapter → Go GraphQL →
PostgreSQL boundary: loading/empty and paginated list, create/detail,
duplicate conflict, malformed IDs, readiness success/unavailable/recovery,
responsive reflow including 320px, horizontal-overflow absence, keyboard and
mobile-menu focus behavior, page errors, and browser console errors. CI
provides its own pinned PostgreSQL service through `PULSEGRID_DATABASE_URL`,
while the local runner cleans up only the Compose project it created.

## Validation commands

| Command | Feedback boundary |
| --- | --- |
| `corepack pnpm run format` | Apply Go formatting and the existing frontend ESLint fix behavior |
| `corepack pnpm run format:check` | Check Go formatting and frontend stylistic lint without rewriting |
| `corepack pnpm run api:modernize` | Check pinned Go modernization analyzers across normal and `integration` build-tagged code |
| `corepack pnpm run api:staticcheck` | Run the pinned Staticcheck suite across normal and `integration` build-tagged code |
| `corepack pnpm run lint` | Go vet, frontend lint, and OpenAPI lint |
| `corepack pnpm run typecheck` | Nuxt/TypeScript typecheck |
| `corepack pnpm run browser:typecheck` | Typecheck Playwright fixtures/specs with the workspace TypeScript SDK |
| `corepack pnpm run test` | Go tests and frontend Vitest in explicit test mode |
| `corepack pnpm run test:race` | Go race suite |
| `corepack pnpm run build` | Go compilation and Nuxt production build |
| `corepack pnpm run openapi` | Lint, bundle, and static HTML rendering into ignored `.openapi/` |
| `corepack pnpm run audit` | Node production audit and reachable Go vulnerability scan |
| `corepack pnpm run api:generate:check` | Regenerate gqlgen artifacts and fail when committed GraphQL output is stale |
| `corepack pnpm run check:fast` | Fast pre-CI handoff: formatting, generated-contract drift, Go modernization/static analysis, lint, typecheck, and ordinary tests |
| `corepack pnpm run api:test:integration` | Isolated real-PostgreSQL migration, repository, constraint, and tenant-scope evidence |
| `corepack pnpm run mqtt:test:integration` | Isolated real-Mosquitto publish/subscribe, QoS 1, no-retain, restart, and cleanup evidence |
| `corepack pnpm run check` | Full pre-CI handoff, including race, build, OpenAPI, audits, database integration, and browser smoke |

The pre-commit hook runs only staged Go formatting, staged frontend ESLint,
staged OpenAPI lint, and the tracked environment-filename policy. Hooks are
convenience feedback and can be bypassed; CI remains authoritative.

Before opening or updating a pull request, run:

```sh
corepack pnpm run check
```

## Environment and secret policy

Tracked files may contain only reviewed `*.example` environment templates.
Real `.env` and `.env.*` files are ignored and must never contain production
credentials in source control. The repository policy check also rejects a
forced tracked real environment file, even when its contents do not look like
a secret.

Automated tests set `PULSEGRID_ENV=test` or `NUXT_APP_ENV=test` explicitly.
Production builds set `NUXT_APP_ENV=production` and use runtime-injected
configuration; they do not load development or test dotenv files.

## Troubleshooting

- If `corepack pnpm run setup` reports a lockfile or peer diagnostic, do not
  replace the lockfile. The current `@bomb.sh/tab`/`cac` peer mismatch is an
  upstream diagnostic and is not an authoritative gate.
- `corepack pnpm run node:audit` keeps low and informational advisories visible
  but blocks moderate, high, and critical production findings. The current
  lockfile has no production advisories; the scoped `fontless>esbuild` override
  keeps its transitive edge on the patched release until upstream widens its
  dependency range.
- `govulncheck` may list vulnerabilities in required Go modules that current
  code does not reach. They remain visible and are not reported as reachable
  application vulnerabilities.
- A Nuxt build may print one generated-dependency Rollup annotation warning.
  It does not originate in application source and does not fail the build.
- If the browser smoke cannot start, check port `4173`, run
  `corepack pnpm run setup:browser`, and remove only the generated
  `.output/`/`test-results/` directories if a stale local run is suspected.
- If a hook is unavailable after setup, run `corepack pnpm run prepare` and
  verify that Git's local `core.hooksPath` points to `.husky/_`. CI does not
  depend on the hook.

## Related guides

- [API guide](../../apps/api/README.md)
- [Web console guide](../../apps/web-console/README.md)
- [Environment strategy](environment-configuration.md)
- [FND-003 plan](../roadmap/feature-plans/completed/FND-003-repository-quality-and-local-workflow.md)
