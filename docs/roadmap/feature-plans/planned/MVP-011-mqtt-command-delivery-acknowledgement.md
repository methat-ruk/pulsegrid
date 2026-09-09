# MVP-011 — MQTT Command Delivery and Acknowledgement

Status: Planned

Intended PR: One command-transport PR

Milestone: M4 — Remote command loop

## Goal

Deliver persisted commands to the simulator over MQTT and translate device
acknowledgement, result, failure, and silence into valid command states.

## Why

This closes the backend device-control loop and proves the asynchronous state
model against a real transport boundary.

## Scope

- Define versioned command and response topics/payloads.
- Dispatch pending commands to the registered simulated device.
- Extend the simulator to ACK and return success or explicit failure.
- Apply acknowledgement/result transitions idempotently.
- Mark commands timed out after the documented bound.
- Add reconnect, duplicate response, and restart tests.

## Out of Scope

- Kafka, fleet/bulk commands, offline queues without bounds, production device
  credentials, or exactly-once claims.

## Dependencies

- MVP-004 and MVP-010.

## Architecture / Boundaries

The MQTT adapter carries command and response messages. The command module
remains authoritative for transition validity and timeout state.

## Implementation Direction

Use at-least-once-safe identifiers and idempotent transition handling. Bound
retry and timeout work; do not add a generic message bus abstraction.

## Validation

- Integration tests cover dispatch, ACK, completion, explicit failure, timeout,
  duplicate response, broker reconnect, and process restart.
- No duplicate logical command is created by retry.
- Logs correlate command, device, tenant, and MQTT interaction.

## Documentation Updates

- Document command/response contracts and local simulator controls.
- Update MQTT environment examples and failure semantics.

## Risks / Open Decisions

- QoS and retained-message policy.
- Retry schedule and ownership of timeout scanning.
- Device response ordering and late response after timeout.

## Done Criteria

Each test command reaches an accurate acknowledged, completed, failed, or timed
out state under the documented delivery semantics.
