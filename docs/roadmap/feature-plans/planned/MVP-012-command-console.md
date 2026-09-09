# MVP-012 — Command Console

Status: Planned

Branch: `feat/mvp-012-command-console`

Intended PR: One frontend-command PR

Milestone: M4 — Remote command loop

## Goal

Let a controlled development operator issue the MVP command from device detail
and follow its asynchronous state to a terminal outcome.

## Why

The product loop is incomplete until device control and its uncertainty are
visible to the operator.

## Scope

- Add the command action and strictly validated input.
- Add appropriate confirmation for the command's effect.
- Display pending, dispatched, acknowledged, completed, failed, and timed-out
  states.
- Provide retry guidance without duplicating the original logical command.
- Add component and browser tests.

## Out of Scope

- Bulk commands, scheduling, custom command builders, production permissions,
  optimistic success, or fleet rollout UI.

## Dependencies

- MVP-003 and MVP-011.

## Architecture / Boundaries

The UI requests command intent and renders server-owned lifecycle state. It
must not treat MQTT publish or an HTTP response as device completion.

## Implementation Direction

Use explicit asynchronous UX states and bounded refresh. Destructive-looking
commands require clear intent, but no generic approval workflow is introduced.

## Validation

- Browser tests cover success, explicit failure, and timeout.
- Loading, retry, disabled, and validation states are accessible.
- The UI never presents pending or acknowledged as completed.
- Cross-tenant device/command access remains denied by the API.

## Documentation Updates

- Document the operator command journey and state language.
- Update UI reference only for reusable asynchronous-operation patterns.

## Risks / Open Decisions

- Confirmation copy and retry semantics.
- Refresh behavior before a realtime transport exists.

## Done Criteria

An operator can issue one supported command and accurately understand its
progress and terminal result without consulting logs.
