# PulseGrid API

This directory owns the Go/Fiber API process. It exposes lifecycle and health
contracts in every mode, the development-only GraphQL device contract when the
fixed development identity is explicitly enabled, and the opt-in local/test
MVP-005 MQTT telemetry consumer.

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

The development example also enables
`PULSEGRID_MQTT_INGESTION_MODE=development`. Keep the loopback Mosquitto
service running before `dev:api`; the API connects and subscribes before it
starts serving, and `/health/ready` remains unavailable if PostgreSQL or MQTT
is down. Set the mode to `disabled` in the ignored environment file when a
health-only or GraphQL-only run is desired.

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

## Local MQTT simulator and ingestion (MVP-004/MVP-005)

The `device-simulator` is a separate one-shot Go process. It publishes one
versioned telemetry observation over MQTT and does not import or call the API,
GraphQL, registry, database, or migration packages. Register a device through
the development console first, then copy its canonical lowercase UUID into the
ignored API environment file:

```sh
cp apps/api/.env.development.example apps/api/.env.development
# Set PULSEGRID_DATABASE_URL to a disposable local password as documented above.
# Set PULSEGRID_MQTT_DEVICE_ID to the UUID returned by the device journey.
corepack pnpm run docker:dev:up
corepack pnpm run mqtt:simulator
```

The development broker listens only on `127.0.0.1:1883`. The simulator accepts
only that exact development endpoint, the fixed `pulsegrid-dev` tenant slug,
and a finite temperature value. It uses MQTT 3.1.1, QoS 1, `retain=false`, a
clean session, five-second connect/publish bounds, and a bounded disconnect.
Successful output reports the topic, logical message ID, timestamp, QoS, and
retain flag; it does not print the payload or environment values. Any missing
broker, invalid configuration, timeout, or failed acknowledgement exits
non-zero without attempting registry or application acceptance.

MVP-005 consumes the same topic in the API process. It validates the exact
topic/payload contract, rejects retained/QoS-0/oversized/future/malformed input,
resolves the device through PostgreSQL, and logs
`reason_code=telemetry_accepted` only after the diagnostic consumer succeeds.
The handoff is bounded and non-durable: PUBACK and queue admission are not
persistence, duplicate `messageId` values are not deduplicated, and failures
are not retried by this slice. Persistence and durable idempotency are owned by
MVP-006.

Stop or remove only the MQTT service; these commands do not remove PostgreSQL
containers or volumes:

```sh
corepack pnpm run mqtt:dev:health
corepack pnpm run mqtt:dev:logs
corepack pnpm run mqtt:dev:stop
corepack pnpm run mqtt:dev:down
```

The isolated real-broker evidence uses port `127.0.0.1:11883`, a unique
Compose project, a real API process and simulator, strict rejection cases,
broker restart/readiness recovery, and owned cleanup:

```sh
corepack pnpm run mqtt:test:integration
```

Broker acknowledgement proves transport delivery to Mosquitto only. The
integration command also proves the MVP-005 validation and diagnostic
acceptance boundary. It does not claim persistence or display; those belong to
MVP-006 and later plans.

## Local PostgreSQL (MVP-001)

The disabled/health-only API does not open PostgreSQL. Development GraphQL
opens it only after explicit setup below. Create the
ignored development environment file once and replace `CHANGE_ME` with a
disposable local password. Run these commands from the repository root:

```sh
cp apps/api/.env.development.example apps/api/.env.development
corepack pnpm run docker:dev:up
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

`docker:dev:up` starts the loopback PostgreSQL 18.6 and Mosquitto Compose
services together and waits for both health checks. It does not run migrations
or seed data. Compose creates missing containers and starts existing ones. The
development volume is persistent; stopping the PostgreSQL service does not
delete it:

```sh
corepack pnpm run db:dev:stop
```

To remove only the development PostgreSQL container while keeping the named
volume and its data, run:

```sh
corepack pnpm run db:dev:down
```

To stop or remove the complete local dependency stack, use the aggregate
commands. `docker:dev:down` does not remove the named PostgreSQL volume:

```sh
corepack pnpm run docker:dev:health
corepack pnpm run docker:dev:logs
corepack pnpm run docker:dev:stop
corepack pnpm run docker:dev:down
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
