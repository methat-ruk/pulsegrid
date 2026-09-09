# MVP-010 — Command Model and GraphQL API

Status: Planned

Intended PR: One command-domain PR

Milestone: M4 — Remote command loop

## Goal

Persist command intent and expose a strict asynchronous command lifecycle
through GraphQL before adding transport delivery.

## Why

Remote commands must remain meaningful through delay, retry, acknowledgement,
failure, and timeout rather than pretending to be synchronous device calls.

## Scope

- Define the MVP command type and payload boundary.
- Add command persistence and valid state transitions.
- Add tenant/device-scoped create and status GraphQL operations.
- Record correlation, creation, transition, and timeout metadata.
- Add state-machine, persistence, and contract tests.

## Out of Scope

- MQTT delivery, simulator acknowledgement, arbitrary command catalogs,
  scheduling, bulk commands, or production RBAC.

## Dependencies

- MVP-002.

## Architecture / Boundaries

The command module owns intent and lifecycle. GraphQL creates intent; transport
delivery is a later adapter and cannot invent state transitions.

## Implementation Direction

Use a small explicit state machine. Model impossible transitions as rejected
operations rather than silently coercing state.

## Validation

- Tests cover every allowed and rejected state transition.
- Duplicate request/idempotency behavior is explicit.
- Cross-tenant creation and reads are denied.
- Timeout fields and clock behavior are testable.

## Documentation Updates

- Document the command lifecycle and API behavior.
- Record idempotency and timeout decisions.

## Risks / Open Decisions

- Initial command catalog and payload validation.
- Idempotency-key source.
- Clock injection and timeout ownership.

## Done Criteria

The API creates and reads a tenant-scoped command whose lifecycle cannot enter
an invalid state, without claiming that it has reached a device.
