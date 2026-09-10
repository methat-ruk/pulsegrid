# FND-004 — Full-Stack Development Integration

Status: Planned

Branch: `feat/fnd-004-full-stack-development-integration`

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
- Connect the console to the narrow backend readiness response.
- Add visible connected, unavailable, and retry states without fake product
  data.
- Consume the FND-003 Playwright Test foundation for a full-stack smoke test
  using the same public HTTP boundary as the browser.
- Document how both applications run together in development and test modes.

## Out of Scope

- GraphQL domain schema, PostgreSQL, MQTT, authentication, production routing,
  Dockerfiles, or deployment infrastructure.

## Dependencies

- FND-003. It already requires FND-001 and FND-002 and owns the pinned browser
  test runner, browser installation, and CI execution boundary.

## Architecture / Boundaries

The frontend consumes a public backend boundary and never imports backend code
or server-only configuration. The readiness response is operational evidence,
not a product-domain API, and exposes no runtime or dependency versions.

## Implementation Direction

Prefer a same-origin Nuxt development proxy so FND-001 can keep CORS disabled.
If implementation evidence requires direct cross-origin browser access, define
an explicit development-only allowlist and test methods, headers, credentials,
and rejected origins. Keep production origin and routing decisions open.

## Validation

- A clean local run connects the browser console to the Go application.
- Playwright browser tests cover backend available, unavailable, and retry
  behavior through the public browser boundary.
- Test mode uses isolated ports/endpoints and cannot fall back to development.
- No server-only value appears in the browser bundle.

## Documentation Updates

- Update local-development commands and troubleshooting.
- Update development/test environment examples with only consumed URL/origin
  keys.
- Record the selected local proxy/origin decision.
- Record the Playwright web-server composition and the first full-stack journey
  command; keep later product journeys in their own feature plans.

## Risks / Open Decisions

- Nuxt proxy versus explicit API-origin configuration.
- Development CORS policy and production routing remain separate decisions.
- Whether the full-stack smoke starts both processes through Playwright
  `webServer` entries or uses a repository orchestration command, based on the
  final local workflow from FND-003.

## Done Criteria

A contributor can start both applications, observe accurate connection state in
the console, and run a repeatable browser-to-backend smoke test without any
product or infrastructure dependency.
