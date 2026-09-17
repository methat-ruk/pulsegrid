# PulseGrid API

This directory owns the Go/Fiber API process. It exposes lifecycle and health
contracts in every mode, plus the development-only GraphQL device contract
when the fixed development identity is explicitly enabled.

## Requirements

The repository pins Go 1.27.1 in the root `.go-version`. With `goenv` initialized
in the shell, entering this repository automatically selects that version.

## Run locally

From the repository root, use the canonical composed workflow:

```sh
corepack pnpm run dev:api
```

The native command remains available from this directory:

```sh
PULSEGRID_ENV=development go run ./cmd/api
```

The default listener is `127.0.0.1:8080`. When development identity is enabled,
`PULSEGRID_HTTP_HOST` must remain a literal IPv4 loopback address in `127.0.0.0/8`;
wildcard, non-loopback, hostname, and IPv6 binds are rejected. To use a local
dotenv file, copy the reviewed example and keep the environment selector explicit:

```sh
cp .env.development.example .env.development
PULSEGRID_ENV=development go run ./cmd/api
```

Development mode requires the seeded `pulsegrid-dev` organization and enables
`POST /graphql`. Run migrations and seed before starting the API:

```sh
corepack pnpm run db:dev:up
corepack pnpm run db:dev:migrate
corepack pnpm run db:dev:seed
corepack pnpm run dev:api
```

The GraphQL transport accepts JSON POST requests only. Test and production
examples default to `PULSEGRID_IDENTITY_MODE=disabled`, which keeps the API
health-only and does not open PostgreSQL. The repository browser smoke
explicitly overrides test mode to `development` against its isolated migrated
and seeded test database so the real console journey can cross the GraphQL
boundary.

Check the lifecycle endpoints:

```sh
curl -i http://127.0.0.1:8080/health/live
curl -i http://127.0.0.1:8080/health/ready
```

The machine-readable operational contract and response semantics are in the
[API documentation index](../../docs/api/README.md) and
[OpenAPI contract](api/openapi/operational.yaml). The development-only
GraphQL device contract is documented in the API index; production product
APIs are not implemented by this foundation.

## Local PostgreSQL (MVP-001)

The disabled/health-only API does not open PostgreSQL. Development GraphQL
opens it only after explicit setup below. Create the
ignored development environment file once and replace `CHANGE_ME` with a
disposable local password. Run these commands from the repository root:

```sh
cp apps/api/.env.development.example apps/api/.env.development
corepack pnpm run db:dev:up
corepack pnpm run db:dev:migrate
corepack pnpm run db:dev:seed
corepack pnpm run db:dev:status
```

If `db:dev:migrate` is upgrading a development volume created by an older
checkout, follow the [migration preflight and recovery procedure](../../docs/project-setup/local-development.md#migration-preflight-and-recovery)
before retrying after a constraint failure. It covers the length and Unicode
whitespace rules from migrations `003` and `004`. The preflight is read-only
and the migration never rewrites existing device keys or display names
implicitly.

`db:dev:up` starts only the loopback PostgreSQL 18.6 Compose service and
waits for its health check. The development volume is persistent; stopping the
service does not delete it:

```sh
corepack pnpm run db:dev:stop
```

To remove the development container and Compose network while keeping the
named volume and its data, run:

```sh
corepack pnpm run db:dev:down
```

Use `db:dev:up` to recreate the service from the existing volume. Do not add
`-v` to this command unless intentionally deleting the development database.

To inspect the development tables, use the `psql` client already included in
the PostgreSQL container; installing `psql` on the host is not required:

```sh
corepack pnpm run db:dev:up
corepack pnpm run db:dev:psql
```

The command resolves the Compose service instead of depending on a generated
container name. Pass normal `psql` arguments after `--`, for example
`corepack pnpm run db:dev:psql -- -c '\dt'`. Inside `psql`, useful commands are
`\dt`, `\d organizations`, `\d devices`, and for example:

```sql
SELECT * FROM organizations;
SELECT id, organization_id, device_key, display_name, created_at
FROM devices
ORDER BY created_at DESC, id DESC;
```

Exit with `\q`. A persistent `pgtest` alias is intentionally not provided:
the integration command creates a disposable database with a random Compose
project and removes it after the run.

The integration command owns a separate disposable Compose project on port
15432, applies migrations twice, verifies a disposable `down`/`up` recovery,
executes the real-PostgreSQL tests, and removes only that test project and
volume. It refuses to attach to an existing process on the test port:

```sh
corepack pnpm run api:test:integration
```

The repository boundary remains internal. GraphQL uses the fixed development
organization resolved at startup; it does not accept a tenant identifier from
the request. Readiness checks PostgreSQL only while GraphQL is enabled, while
liveness remains process-only.

## Test

Repository-wide setup, fast checks, full pre-CI validation, and dependency
audits are documented in [the local-development guide](../../docs/project-setup/local-development.md).

```sh
PULSEGRID_ENV=test go test ./...
PULSEGRID_ENV=test go vet ./...
gofmt -d $(find . -name '*.go' -type f)
```

Test mode uses isolated defaults and never loads a development dotenv file.
Production mode reads process-injected values only; do not copy a production
file containing credentials into the repository.
