# PulseGrid API

This directory owns the initial Go/Fiber API process. It currently exposes
only lifecycle and health contracts; product APIs are introduced by later
feature plans.

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

The default listener is `127.0.0.1:8080`. To use a local dotenv file, copy the
reviewed example and keep the environment selector explicit:

```sh
cp .env.development.example .env.development
PULSEGRID_ENV=development go run ./cmd/api
```

Check the lifecycle endpoints:

```sh
curl -i http://127.0.0.1:8080/health/live
curl -i http://127.0.0.1:8080/health/ready
```

The machine-readable operational contract and response semantics are in the
[API documentation index](../../docs/api/README.md) and
[OpenAPI contract](api/openapi/operational.yaml). Product-domain APIs are not
implemented by this foundation.

## Local PostgreSQL (MVP-001)

The health-only API does not open PostgreSQL. Database access is explicit
through the migration, seed, and integration-test commands below. Create the
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
before retrying after a constraint failure. The preflight is read-only and
the migration never rewrites existing device keys or display names implicitly.

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

No product REST/GraphQL endpoint or runtime database readiness contract is
introduced by MVP-001. The repository boundary is an internal trusted caller
surface until a later feature establishes identity-to-organization mapping.

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
