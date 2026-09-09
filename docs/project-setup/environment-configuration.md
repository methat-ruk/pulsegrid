# Environment and Configuration Strategy

Status: Planning source of truth

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

The Go application uses `PULSEGRID_ENV`. The Nuxt application will select its
native server-only and `NUXT_PUBLIC_*` keys in FND-002; it must map to the same
three logical environments without exposing backend configuration.

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

## Delivery sequence

1. This documentation foundation defines the policy and plan.
2. The Go foundation implements typed development/test/production validation
   for the variables it actually uses. (FND-001 complete.)
3. The Nuxt foundation implements public/private runtime configuration and
   browser-exposure tests.
4. Repository CI supplies the `test` environment explicitly and verifies that
   test configuration cannot target development resources.
5. PostgreSQL and MQTT plans add their variables and example values when the
   dependencies are introduced.
6. Production hardening defines the deployment secret provider, rotation,
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

- Exact Nuxt server-only and public key names; FND-002 owns this decision.
- Secret manager and deployment injection mechanism.
- Per-test database strategy and MQTT namespace isolation.

## Related documents

- [System architecture](../architecture/system-architecture.md)
- [Technology decisions](../architecture/technology-decisions.md)
- [Roadmap](../roadmap/roadmap.md)
