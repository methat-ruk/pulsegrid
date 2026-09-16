# PulseGrid local development

Status: Local repository workflow and merge-gate enforcement implemented;
FND-004 local full-stack readiness flow is implemented and validated.

This is the canonical guide for setting up and validating the repository. The
Go API and Nuxt console remain independently runnable, with an opt-in local
readiness adapter for the first full-stack development feedback loop.

## Requirements

- Node `24.20.0` from `.node-version`;
- pnpm `12.3.4` through Corepack;
- Go `1.27.1` from `.go-version`;
- Docker Compose with the pinned PostgreSQL image available locally.

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
loopback API origin and is consumed by the Nuxt same-origin readiness adapter;
it is never exposed through `runtimeConfig.public` or forwarded from the
browser. Do not add credentials, tokens, or other private service URLs to
frontend examples.

With both processes running, open `http://127.0.0.1:3000` (or the port shown by
Nuxt). The shell checks `GET /api/operational/ready` and offers a manual Retry
when the local API is stopped or starting. This adapter covers process
readiness only; it is not a product API or a generic proxy.

### Local PostgreSQL, seed, and development GraphQL

MVP-002 keeps database setup explicit. Copy the API example, set a disposable
password in the ignored file, and run migrations and seed before starting the
development-only GraphQL API:

```sh
cp apps/api/.env.development.example apps/api/.env.development
corepack pnpm run db:dev:up
corepack pnpm run db:dev:migrate
corepack pnpm run db:dev:seed
corepack pnpm run db:dev:status
corepack pnpm run dev:api
```

`PULSEGRID_IDENTITY_MODE=development` resolves only the seeded
`pulsegrid-dev` organization. The API fails before listening if PostgreSQL,
migrations, or seed data is unavailable. Use `PULSEGRID_IDENTITY_MODE=disabled`
for the health-only test/browser process; it does not open a database.

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
container without deleting the container or named volume. Use `db:dev:down`
when the container and Compose network should be removed; it also preserves
the named volume and its data. The isolated integration workflow owns a
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
the container and volume, and `pgdevdown` to remove the container and network
while keeping the volume. A persistent `pgtest` alias is not provided because
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

This smoke builds the API test binary and Nuxt test artifact, starts both in an
isolated process lifecycle, and covers planned-state rendering, readiness
success/unavailable/recovery, responsive reflow including 320px,
horizontal-overflow absence, keyboard navigation, page errors, and browser
console errors.

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
