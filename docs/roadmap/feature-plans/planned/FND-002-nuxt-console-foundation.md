# FND-002 — Nuxt Console Foundation

Status: Planned

Branch: `feat/fnd-002-nuxt-console-foundation`

Intended PR: One frontend-foundation PR

Milestone: F0 — Executable repository foundation

## Goal

Create the first runnable Vue/Nuxt operations-console shell using the existing
UI design system.

## Why

MVP operator journeys need a stable application shell, visual tokens,
navigation, runtime configuration, and frontend validation boundary.

## Scope

- Establish `apps/web-console` as a pnpm workspace package, with one root
  `pnpm-workspace.yaml` and `pnpm-lock.yaml`.
- Add Nuxt 4 using its `app/` directory structure, Vue 3, TypeScript, Tailwind
  CSS v4, Nuxt UI, and Iconify.
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

Use the UI design system as the visual source of truth and pnpm as the only
Node package manager. Keep Nuxt's default rendering mode unless a concrete
client-only constraint appears during implementation. Expose only deliberately
public `NUXT_PUBLIC_*` runtime values to browser code.

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

- Whether an authenticated console requirement later justifies changing the
  default Nuxt rendering mode.
- Font delivery and dependency policy.

## Done Criteria

The console runs locally, renders the PulseGrid shell without fabricated
product status, validates browser-visible configuration, and passes documented
checks.
