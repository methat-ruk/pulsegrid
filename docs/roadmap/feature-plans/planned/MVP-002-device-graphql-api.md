# MVP-002 — Device GraphQL API

Status: Planned

Intended PR: One API-slice PR

Milestone: M1 — Device registry

## Goal

Expose device provisioning, list, and detail through a typed GraphQL contract.

## Why

The console needs a concrete API boundary before choosing client behavior or
building product screens.

## Scope

- Add gqlgen and the first domain schema.
- Add create-device mutation and tenant-scoped list/detail queries.
- Map a controlled development identity/context to one tenant.
- Validate input and translate domain failures into stable GraphQL errors.
- Add resolver and contract tests.

## Out of Scope

- Production authentication/RBAC, pagination at unproven scale, telemetry,
  subscriptions, alerts, or commands.

## Dependencies

- MVP-001.

## Architecture / Boundaries

GraphQL composes the device-registry application boundary; resolvers do not own
persistence rules or accept tenant authority directly from untrusted input.

## Implementation Direction

Start with the smallest schema that supports the device journey. Keep the
development identity adapter replaceable and clearly non-production.

## Validation

- Schema/resolver tests for normal and invalid input.
- Cross-tenant access is denied independently of requested identifiers.
- Error contracts and nullability match actual behavior.
- Generated artifacts are reproducible.

## Documentation Updates

- Document GraphQL generation/test commands.
- Record the development identity limitation.
- Update technology decisions with any client-independent GraphQL decisions.

## Risks / Open Decisions

- Development identity mechanism.
- Identifier exposure and error-shape conventions.
- Pagination is deferred until list behavior requires it.

## Done Criteria

The tested GraphQL contract provisions and reads devices only within the active
development tenant and contains no production-auth claim.
