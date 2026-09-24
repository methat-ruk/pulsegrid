# MVP-009 — Alert Console

Status: Planned

Branch: `feat/mvp-009-alert-console`

Intended PR: One frontend-alert PR

Milestone: M3 — Rules and alerts

## Goal

Let an operator see recent alert occurrences and trace each one to its device,
rule, and stored triggering context.

## Why

An alert is useful only when an operator can understand why it exists and where
to continue investigation.

## Scope

- Add alert list and alert detail/context presentation.
- Link alerts to device detail and show the stored measurement/rule snapshot
  even after telemetry history pruning. Direct navigation to one telemetry
  row is not promised by MVP-008's GraphQL contract.
- Present occurrence time and triggering comparison in text as well as visual
  treatment; do not infer active/resolved status or severity.
- Add loading, empty, error, and refresh behavior.
- Add component and browser tests.

## Out of Scope

- Notification delivery, acknowledgement workflow, active/resolved lifecycle,
  severity, escalation, bulk actions, or advanced filtering.

## Dependencies

- MVP-007 and MVP-008.

## Architecture / Boundaries

The console presents immutable alert-occurrence authority from the API and
does not infer alert matches from cached telemetry. The `messageId` is a
logical trace reference; a pruned telemetry row must not make the alert detail
unusable.

## Implementation Direction

Prioritize traceability over dashboard breadth. Reuse device navigation and
the alert snapshot rather than adding a separate investigation subsystem.

## Validation

- Browser test shows a simulator-triggered alert and follows its stored
  context, including when the source history row is unavailable.
- Empty/error/stale states are visible.
- Triggering condition never relies on color alone.
- Tenant-scoped navigation cannot expose another tenant's identifiers.

## Documentation Updates

- Update UI reference only for reusable alert patterns.
- Document the limited MVP alert behavior.

## Risks / Open Decisions

- Refresh behavior before realtime transport exists.

## Done Criteria

An operator can identify what happened, which device was affected, and which
measurement/rule snapshot triggered the occurrence without relying on
infrastructure logs or a retained telemetry history row.
