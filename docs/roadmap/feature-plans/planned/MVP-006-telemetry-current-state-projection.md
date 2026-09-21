# MVP-006 — Telemetry and Current-State Projection

Status: Planned; depends on the implemented MVP-005 local/test ingestion boundary

Branch: `feat/mvp-006-telemetry-current-state-projection`

Intended PR: One backend-state PR

Milestone: M2 — Telemetry and current state

## Goal

Persist bounded recent telemetry and maintain a deterministic latest-device
state from accepted ingestion inputs.

## Why

Operators need evidence of recent behavior and a current state; raw MQTT
receipt alone is not product value.

## Scope

- Add PostgreSQL telemetry and current-state schema/migrations.
- Persist accepted telemetry with a stable idempotency identifier.
- Project latest accepted values and last-seen state.
- Define duplicate and out-of-order timestamp behavior.
- Add GraphQL read queries for bounded recent telemetry and current state.

## Out of Scope

- Permanent high-volume storage choice, Kafka, aggregation, downsampling,
  retention automation, charts, or rules.

## Dependencies

- MVP-005.

## Architecture / Boundaries

MVP-005 hands this plan a validated, tenant-resolved `AcceptedTelemetry` value:
`IngestionID`, producer `MessageID`, registry-authoritative `OrganizationID` and
`DeviceID`, `ObservedAt`, server `ReceivedAt`, finite
`TemperatureCelsius`, and transport-only `MQTTDuplicate`. The topic tenant is
not an authority and must not be persisted as a substitute for the registry
organization.

Telemetry history is the accepted-input record; current state is a derived read
model with a named writer. The device registry remains authoritative for device
ownership. MVP-005's diagnostic handoff is bounded and non-durable: queue drops,
registry/consumer failures, process restarts, and transport acknowledgement do
not produce a durable record. This plan owns the transactional persistence and
logical `MessageID` idempotency needed to close that gap.

## Implementation Direction

Use PostgreSQL for the bounded MVP dataset. Make ordering and deduplication
rules explicit before optimizing storage. Persist `MessageID` as the logical
idempotency key, retain `IngestionID` and `ReceivedAt` as delivery diagnostics,
and define how late/out-of-order `ObservedAt` values affect current state.

## Validation

- Integration tests cover normal, duplicate, late, and cross-tenant inputs.
- Projection tests prove a late event cannot incorrectly replace newer state.
- GraphQL tests prove tenant scope and bounded result size.
- Migration and restart behavior preserve accepted state.

## Documentation Updates

- Document MVP retention bounds and timestamp semantics.
- Update data ownership and database commands.

## Risks / Open Decisions

- Exact retention/count bound and timestamp-skew policy.
- Idempotency key supplied by device versus derived at ingestion.
- Specialized telemetry storage remains deferred.

## Done Criteria

Accepted telemetry is deduplicated, tenant scoped, queryable within a documented
bound, and produces deterministic current state across restart and late input.
