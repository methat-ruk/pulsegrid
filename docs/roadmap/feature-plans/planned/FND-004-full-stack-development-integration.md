# FND-004 — Full-Stack Development Integration

Status: Planned

Intended PR: One local-integration PR

Milestone: F0 — Executable repository foundation

## Goal

Prove that the Nuxt console can reach the Go/Fiber application through the
documented local-development configuration before product APIs are added.

## Why

Independent frontend and backend boot success does not prove that origins,
proxying, runtime configuration, errors, and the contributor workflow compose
correctly.

## Scope

- Define the development frontend-to-backend route and origin policy.
- Connect the console to a narrow backend health/version response.
- Add visible connected, unavailable, and retry states without fake product
  data.
- Add a full-stack smoke test using the same public HTTP boundary as the browser.
- Document how both applications run together in development and test modes.

## Out of Scope

- GraphQL domain schema, PostgreSQL, MQTT, authentication, production routing,
  Dockerfiles, or deployment infrastructure.

## Dependencies

- FND-001, FND-002, and FND-003.

## Architecture / Boundaries

The frontend consumes a public backend boundary and never imports backend code
or server-only configuration. The health/version response is operational
evidence, not a product-domain API.

## Implementation Direction

Use the simplest local proxy or explicit origin configuration supported by the
selected Nuxt run mode. Keep production origin and routing decisions open.

## Validation

- A clean local run connects the browser console to the Go application.
- Browser tests cover backend available, unavailable, and retry behavior.
- Test mode uses isolated ports/endpoints and cannot fall back to development.
- No server-only value appears in the browser bundle.

## Documentation Updates

- Update local-development commands and troubleshooting.
- Update development/test environment examples with only consumed URL/origin
  keys.
- Record the selected local proxy/origin decision.

## Risks / Open Decisions

- Nuxt proxy versus explicit API-origin configuration.
- Development CORS policy and production routing remain separate decisions.

## Done Criteria

A contributor can start both applications, observe accurate connection state in
the console, and run a repeatable browser-to-backend smoke test without any
product or infrastructure dependency.
