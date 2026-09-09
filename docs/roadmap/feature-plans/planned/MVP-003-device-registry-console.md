# MVP-003 — Device Registry Console

Status: Planned

Intended PR: One frontend-journey PR

Milestone: M1 — Device registry

## Goal

Let a controlled development operator provision, list, and inspect devices in
the web console.

## Why

The first operator-visible journey validates that the product and GraphQL
boundaries compose coherently.

## Scope

- Select and configure the GraphQL client.
- Add device list, create form, and device detail views.
- Add loading, empty, error, success, and validation states.
- Follow the UI design system and responsive behavior.
- Add component and browser journey tests.

## Out of Scope

- Telemetry, maps, fleet grouping, bulk actions, production auth, or advanced
  filtering/search.

## Dependencies

- FND-002, FND-004, and MVP-002.

## Architecture / Boundaries

The UI owns form and presentation state. The GraphQL API remains authoritative
for device and tenant rules.

## Implementation Direction

Prefer direct server-state handling over introducing a broad client store.
Choose cache behavior for this journey and make invalidation after provisioning
explicit.

## Validation

- Component tests cover visible async and validation states.
- Browser test covers create, list refresh, and detail navigation.
- Responsive and keyboard behavior is verified.
- No tenant identifier is treated as client-side authority.

## Documentation Updates

- Record the selected GraphQL client and cache policy.
- Update UI documentation only if a reusable pattern is introduced.

## Risks / Open Decisions

- GraphQL client choice.
- Route structure and refetch/cache invalidation behavior.

## Done Criteria

An operator completes the device journey with accessible state feedback, and
tests prove the visible result against the real GraphQL contract.
