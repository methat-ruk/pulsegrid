# Environment and Configuration Strategy

Status: Configuration contract and validation source of truth

## Purpose

This document owns how PulseGrid separates development, test, and production
configuration. It defines the contract before implementation without creating
unused environment variables or committing secrets.

## Principles

- Configuration keys are added only with the feature that consumes them.
- Secret values never enter tracked `.env` files or example files.
- Production configuration is injected by the runtime or secret manager, not
  read from a deployed `.env.production` file.
- Test configuration is isolated from development resources and cannot silently
  fall back to them.
- Applications fail fast when required configuration is absent or invalid.
- Browser-exposed values are explicitly separated from server-only values.

## Environment names

Applications will recognize exactly these logical environments:

| Environment | Purpose | Persistent external data allowed |
| --- | --- | --- |
| `development` | Local interactive development | Local disposable dependencies only |
| `test` | Automated unit, integration, and end-to-end validation | Isolated test resources only |
| `production` | Deployed production behavior and production-mode smoke tests | Explicitly configured production resources |

The Go application uses `PULSEGRID_ENV`. The Nuxt application uses the
server-only `NUXT_APP_ENV` key implemented by FND-002; it maps to the same
three logical environments. FND-004 adds a private `NUXT_BACKEND_ORIGIN` for
the local readiness adapter; the public runtime-config object remains empty.

The repository root `.go-version` pins the Go toolchain for goenv. This is a
developer-tool selection only; the API module remains the source of truth for
the required Go language version.

## Planned file contract

Each application owns its examples at its application root. Tracked files may
include:

```text
.env.example
.env.development.example
.env.test.example
.env.production.example
```

Their responsibilities are:

- `.env.example`: canonical inventory of supported keys, descriptions, and
  safe placeholders;
- `.env.development.example`: safe local-development values or overrides;
- `.env.test.example`: deterministic, non-secret test values and isolation
  requirements;
- `.env.production.example`: names and validation expectations only, with no
  usable credentials or secret values.

Untracked local files may include:

```text
.env.development
.env.test
.env.production
```

`.env.production` is allowed only for an ignored local production-mode smoke
test. A real production deployment must use runtime-injected environment
variables and a managed secret mechanism.

Before any local `.env.*` file is introduced, `.gitignore` must ignore all real
environment files while explicitly allowing only reviewed `*.example` files.

## Configuration precedence

The intended precedence, from highest to lowest, is:

1. process environment injected by the test runner or deployment runtime;
2. the explicitly selected local `.env.<environment>` file in development or
   test only;
3. safe non-secret code defaults.

Production must not automatically load development or test files. Missing
required production configuration is a startup error.

## Environment behavior

### Development

- Uses local endpoints and disposable credentials.
- May load `.env.development` explicitly.
- Uses developer-readable logs without exposing secrets.
- Adds PostgreSQL or MQTT settings only when their feature plans introduce
  those dependencies.

### Test

- Uses deterministic defaults and isolated database/broker namespaces.
- May load `.env.test` under the test runner.
- Must never use the development database, broker namespace, or production
  endpoint as fallback.
- Integration and end-to-end runs own setup and cleanup of their resources.
- CI supplies or generates non-secret test values explicitly.

### Production

- Uses process-injected configuration and a managed secret source.
- Rejects development defaults, debug behavior, and missing required values.
  Wildcard bind hosts are allowed only when explicitly injected by an operator
  and the deployment network policy intentionally scopes the listener; they are
  never a code default.
- Separates migration execution from application startup unless a later
  deployment decision explicitly proves another model safe.
- Logs configuration validation results without logging values.

## Frontend and backend separation

Backend configuration may contain server-only credentials and connection
details. Frontend configuration may expose only values intentionally safe for a
browser. Nuxt public-runtime keys must never be used for database credentials,
broker credentials, signing material, or private service endpoints.

Shared variable names do not imply shared files. Each application should own
and validate the smallest configuration surface it consumes.

### FND-001 Go keys

FND-001 introduces only these backend keys under `apps/api/`:

| Key | Development/test behavior | Production behavior |
| --- | --- | --- |
| `PULSEGRID_ENV` | Required enum: `development` or `test` | Required value: `production` |
| `PULSEGRID_HTTP_HOST` | Defaults to `127.0.0.1` | Required and explicitly injected |
| `PULSEGRID_HTTP_PORT` | Safe local/test default; isolated in tests | Required and explicitly injected |
| `PULSEGRID_LOG_LEVEL` | Validated safe default | Validated; no debug default |
| `PULSEGRID_SHUTDOWN_TIMEOUT` | Validated bounded duration | Required or an explicitly documented safe default |

No database, broker, token, or credential key is introduced until its consumer
plan begins.

### MVP-001 PostgreSQL key

MVP-001 adds one server-only database key for migration, seed, repository
integration-test commands, and the MVP-002 development GraphQL startup. The
disabled/health-only API path does not require or open it.

| Key | Development/test behavior | Production behavior |
| --- | --- | --- |
| PULSEGRID_DATABASE_URL | Development points to loopback pulsegrid_dev; test points to the isolated loopback pulsegrid_test port and must never fall back to development | Injected by the deployment secret manager when a production database consumer exists; the tracked production example keeps it blank |

### MVP-002 GraphQL identity mode

MVP-002 adds an explicit server-only identity mode. The safe default is
`disabled`; development GraphQL is enabled only with the local/test value
`development`, a literal IPv4 loopback `PULSEGRID_HTTP_HOST` (`127.0.0.0/8`),
and a seeded `pulsegrid-dev` organization. Wildcard, hostname, non-loopback,
and IPv6 binds are rejected in this mode.

| Key | Development/test behavior | Production behavior |
| --- | --- | --- |
| `PULSEGRID_IDENTITY_MODE` | `disabled` keeps the API health-only; `development` is allowed only with the isolated local/test database and fixed seed mapping | `disabled` only; `development` is rejected |

### FND-002 Nuxt keys

FND-002 introduces one server-only application-environment key:

| Key | Development/test behavior | Production behavior |
| --- | --- | --- |
| `NUXT_APP_ENV` | Exact enum value; development may load `.env.development`, while tests inject `test` deterministically | Exact value `production` is injected by the runtime; development and test files are not auto-loaded |

### FND-004 Nuxt readiness key

| Key | Development | Test | Production |
| --- | --- | --- | --- |
| `NUXT_BACKEND_ORIGIN` | Required loopback origin `http://127.0.0.1:8080` when using the local readiness adapter | Required isolated loopback origin `http://127.0.0.1:18080`; never falls back to development | Must be absent; startup rejects it |

The key is private Nitro runtime configuration. The readiness adapter calls
only the fixed backend path `/health/ready`, accepts only the exact `200`
`{"status":"ready"}` response, bounds the response and timeout, and maps all
other outcomes to a generic unavailable state. MVP-003 also uses the same
private origin for a fixed `/graphql` product adapter. That adapter accepts
only JSON requests, forwards no browser authority or arbitrary path, bounds
request/response bytes and upstream time, preserves valid GraphQL status/body
responses, and maps upstream transport/media failures to a safe unavailable
GraphQL envelope. Neither route is a general proxy or a production identity
boundary.

Nuxt validates these keys during Nitro startup. `runtimeConfig.public` remains
empty, so the browser receives no API origin, credential, or other private
runtime value.

### MVP-004 local MQTT simulator keys

MVP-004 adds five server-side simulator keys. They are consumed by the
standalone `device-simulator` process, not by the API or browser console:

| Key | Development | Test | Production |
| --- | --- | --- | --- |
| `PULSEGRID_ENV` | Exact value `development` | Exact value `test` | Rejected by the simulator |
| `PULSEGRID_MQTT_BROKER_URL` | Exact `mqtt://127.0.0.1:1883` | Exact `mqtt://127.0.0.1:11883` | Rejected; no production broker is selected |
| `PULSEGRID_MQTT_TENANT_SLUG` | Exact `pulsegrid-dev` | Exact `pulsegrid-dev` | Rejected with simulator production mode |
| `PULSEGRID_MQTT_DEVICE_ID` | Canonical lowercase UUID copied from the registered-device journey | Canonical lowercase UUID supplied by the isolated test process | Rejected with simulator production mode |
| `PULSEGRID_SIMULATOR_TEMPERATURE_CELSIUS` | Required finite JSON number | Required finite JSON number | Rejected with simulator production mode |

The simulator rejects non-loopback hosts, unsupported MQTT schemes, URL
userinfo, paths, queries, fragments, wrong tenant slugs, malformed or nil
UUIDs, uppercase UUID forms, and non-finite temperatures. Process values have
precedence over the selected `.env.<environment>` file. The tracked examples
leave the device UUID blank; a contributor supplies it only in an ignored local
file or the test process environment. These keys do not make the API depend on
MQTT, and production identity/TLS/authorization remain a later decision.

### MVP-005 API MQTT ingestion keys

MVP-005 adds two server-only API keys. The broker URL is intentionally shared
with the standalone simulator, but the API does not consume the simulator's
tenant/device/temperature keys; it derives authority from the topic and the
PostgreSQL registry.

| Key | Development | Test | Production |
| --- | --- | --- | --- |
| `PULSEGRID_MQTT_INGESTION_MODE` | `disabled` by default; exact `development` enables the local consumer | `disabled` for ordinary tests; the owned MQTT integration runner injects `development` | `disabled` only; enabled mode is rejected |
| `PULSEGRID_MQTT_BROKER_URL` | Required only when enabled; exact `mqtt://127.0.0.1:1883` | Required only when enabled; exact `mqtt://127.0.0.1:11883` | No broker URL is accepted while ingestion is disabled |

Enabled ingestion requires the existing database configuration because device
authority is resolved before acceptance. The API rejects credentials, alternate
schemes, non-loopback hosts, URL paths/queries/fragments, and environment-port
crossover. A broker disconnect makes readiness return the generic
`dependency_unavailable` state while bounded reconnect and resubscription run;
liveness remains process-only. Application acceptance is a diagnostic,
non-durable handoff until MVP-006 adds persistence and logical `messageId`
idempotency.

## Delivery sequence

1. This documentation foundation defines the policy and plan.
2. The Go foundation implements typed development/test/production validation
   for the variables it actually uses. (FND-001 complete.)
3. The Nuxt foundation implements and validates the server-only
   `NUXT_APP_ENV` boundary; its public runtime configuration remains empty.
4. FND-004 adds and validates the private loopback readiness origin and proves
   test isolation with a real Go process in browser smoke.
5. Repository CI supplies the `test` environment explicitly and verifies that
   test configuration cannot target development resources.
6. MVP-001 adds PostgreSQL configuration and example values with the first
   persistence consumer; MVP-002 adds the development-only GraphQL identity
   mode and runtime database readiness; MVP-003 consumes the existing private
   origin through a fixed same-origin adapter and does not add a browser-facing
   origin or credential; MVP-004 adds and validates the local MQTT simulator
   configuration; MVP-005 adds the explicit API ingestion mode and shared
   loopback broker URL.
7. Production hardening defines the deployment secret provider, rotation,
   access controls, and production-mode smoke validation.

## Validation requirements

- Example files contain no secret-looking or usable production values.
- Required keys fail fast with actionable messages.
- Invalid URLs, durations, ports, and enum values are rejected.
- Test runs prove they do not use development resource names or endpoints.
- Frontend build output is checked for accidental server-only values.
- Production-mode startup refuses missing required configuration and does not
  load development files.

## Open decisions

- Secret manager and deployment injection mechanism.
- Per-test database strategy and MQTT namespace isolation.

## Related documents

- [System architecture](../architecture/system-architecture.md)
- [Technology decisions](../architecture/technology-decisions.md)
- [Roadmap](../roadmap/roadmap.md)
