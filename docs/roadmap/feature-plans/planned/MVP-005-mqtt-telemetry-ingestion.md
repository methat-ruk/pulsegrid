# MVP-005 — MQTT Telemetry Ingestion

Status: Planned

Intended PR: One ingestion-boundary PR

Milestone: M2 — Telemetry and current state

## Goal

Subscribe to the MVP telemetry topic, validate input, and resolve each accepted
message to a registered tenant-owned device.

## Why

The product cannot trust device identifiers, payloads, or timestamps without an
explicit ingestion boundary.

## Scope

- Add broker connection and subscription lifecycle to the Go application.
- Parse and strictly validate the versioned telemetry payload.
- Resolve device identity and tenant ownership through the device registry.
- Assign ingestion and correlation metadata.
- Expose a narrow accepted-telemetry application interface for the next plan.
- Add malformed, unknown-device, reconnect, and shutdown tests.

## Out of Scope

- Telemetry persistence, current-state projection, rules, Kafka, generic event
  frameworks, or production broker authentication.

## Dependencies

- MVP-002 and MVP-004.

## Architecture / Boundaries

MQTT input is untrusted. Ingestion owns transport validation and device
resolution but does not own telemetry storage or rule behavior.

## Implementation Direction

Use a direct narrow interface instead of a speculative broker abstraction.
Define duplicate and timestamp inputs explicitly for downstream handling.

## Validation

- Integration tests consume valid messages from the local broker.
- Invalid version, shape, range, and unknown device are rejected observably.
- Reconnect is bounded and shutdown does not abandon an accepted callback.
- Logs include identifiers without payload secrets.

## Documentation Updates

- Document accepted/rejected payload behavior.
- Update MQTT configuration and local workflow.

## Risks / Open Decisions

- Broker client library and reconnect policy.
- Maximum payload size and timestamp-skew bounds.
- Device identity trust remains development-only.

## Done Criteria

The ingestion boundary emits a validated, tenant-resolved input for valid MQTT
messages and produces clear evidence for rejected or interrupted inputs.
