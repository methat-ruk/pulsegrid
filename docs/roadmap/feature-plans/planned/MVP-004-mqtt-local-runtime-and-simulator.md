# MVP-004 — MQTT Local Runtime and Simulator

Status: Planned

Intended PR: One device-transport fixture PR

Milestone: M2 — Telemetry and current state

## Goal

Provide a reproducible local MQTT broker and simulator that can publish one
versioned telemetry payload for a registered device.

## Why

Telemetry ingestion needs a concrete producer and transport before platform
consumers or event abstractions are designed.

## Scope

- Select and add an MQTT broker to local Docker Compose.
- Add a small device simulator with explicit development configuration.
- Define one telemetry topic and payload version for the MVP flow.
- Add local-only broker credentials or an equally bounded development trust
  mechanism.
- Add broker readiness and simulator smoke checks.

## Out of Scope

- Production device credential lifecycle, ingestion processing, Kafka, command
  delivery, high-volume simulation, or broker clustering.

## Dependencies

- MVP-001 and FND-003.

## Architecture / Boundaries

The simulator represents an external device and must use the MQTT boundary; it
must not call application repositories or GraphQL to fake telemetry delivery.

## Implementation Direction

Choose the broker in this PR. Keep the payload minimal, versioned, and tied to
one real telemetry flow. Add MQTT variables only when consumed.

## Validation

- Clean local startup reaches broker readiness.
- Simulator publishes the documented payload to the documented topic.
- Invalid configuration fails clearly.
- Development/test broker namespaces cannot target production endpoints.

## Documentation Updates

- Document broker/simulator commands and telemetry example.
- Add MQTT keys to development/test example files.
- Record broker, topic, and QoS decisions.

## Risks / Open Decisions

- Broker product, QoS, retained-message behavior, and topic namespace.
- Development credential approach is not a production security model.

## Done Criteria

A registered simulated device can reproducibly publish one documented MQTT
telemetry message without any platform-side ingestion claim.
