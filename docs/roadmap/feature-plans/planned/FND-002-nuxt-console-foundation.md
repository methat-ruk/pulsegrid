# FND-002 — Nuxt Console Foundation

Status: Planned

Intended PR: One frontend-foundation PR

Milestone: F0 — Executable repository foundation

## Goal

Create the first runnable Vue/Nuxt operations-console shell using the existing
UI design system.

## Why

MVP operator journeys need a stable application shell, visual tokens,
navigation, runtime configuration, and frontend validation boundary.

## Scope

- Establish the frontend application and selected package-manager workspace.
- Add Nuxt, Vue, TypeScript, Tailwind CSS v4, Nuxt UI, and Iconify.
- Implement semantic design tokens and the minimal responsive application shell.
- Add a neutral planned-state page rather than fake operational data.
- Add lint, typecheck, and component/smoke-test commands.
- Define private versus browser-public runtime configuration.

## Out of Scope

- Device, telemetry, alert, command, map, or chart product screens.
- GraphQL client selection.
- Authentication or production hosting.

## Dependencies

- DOC-001.

## Architecture / Boundaries

The console owns presentation and interaction state. It must not become an
authority for tenant, device, alert, or command rules.

## Implementation Direction

Use the UI design system as the visual source of truth. Choose rendering mode
and package manager in this PR based on the console's actual local and testing
needs. Expose only deliberately public Nuxt runtime values to browser code.

## Validation

- Lint, typecheck, unit/component tests, and production build pass.
- Browser verification at desktop, tablet, and mobile widths.
- Keyboard focus and initial accessibility checks pass.
- Build output contains no server-only configuration values.

## Documentation Updates

- Record package-manager and frontend commands.
- Add only consumed frontend keys to the environment examples.
- Update technology decisions for choices made in this PR.

## Risks / Open Decisions

- Package manager and workspace configuration.
- SSR versus client-rendered console behavior.
- Font delivery and dependency policy.

## Done Criteria

The console runs locally, renders the PulseGrid shell without fabricated
product status, validates browser-visible configuration, and passes documented
checks.
