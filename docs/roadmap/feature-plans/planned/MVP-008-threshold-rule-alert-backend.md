# MVP-008 — Threshold Rule and Alert Backend

Status: Planned

Intended PR: One backend-condition PR

Milestone: M3 — Rules and alerts

## Goal

Evaluate one deliberately limited threshold-rule model against accepted
telemetry and persist an alert with its triggering context.

## Why

Detecting an actionable condition is the bridge between raw measurements and
the operator workflow PulseGrid is intended to support.

## Scope

- Add tenant/device-scoped threshold-rule persistence.
- Support one metric, comparison operator, threshold, and enabled state.
- Evaluate rules after accepted telemetry is stored.
- Persist an alert linked to rule, device, and triggering telemetry.
- Add GraphQL operations needed to configure rules and read alerts.
- Prevent duplicate alerts for the same rule/input pair.

## Out of Scope

- General expression language, windows/aggregations, alert notifications,
  escalation policy, Kafka consumer, or complex alert lifecycle.

## Dependencies

- MVP-006.

## Architecture / Boundaries

Rules own condition definitions and evaluation. Alerts own the recorded match.
Neither boundary may rewrite telemetry or device ownership.

## Implementation Direction

Use an explicit small domain model and direct application call. Do not build a
plugin engine or generic rule DSL for the MVP.

## Validation

- Unit tests cover operators, boundaries, disabled rules, and invalid values.
- Integration tests cover tenant isolation and duplicate input.
- Failure tests prove telemetry remains inspectable when rule evaluation fails.
- GraphQL contracts expose triggering context without leaking another tenant.

## Documentation Updates

- Document the exact MVP rule limitation and alert semantics.
- Update architecture only if the processing boundary changes.

## Risks / Open Decisions

- Alert repeat/suppression behavior beyond one input.
- Numeric precision and unit compatibility.
- Recovery strategy if evaluation fails after telemetry commits.

## Done Criteria

A matching accepted telemetry input creates exactly one tenant-scoped alert
with traceable triggering context, while non-matches and failures are explicit.
