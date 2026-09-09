# MVP-009 — Alert Console

Status: Planned

Intended PR: One frontend-alert PR

Milestone: M3 — Rules and alerts

## Goal

Let an operator see active/recent alerts and trace each alert to its device,
rule, and triggering telemetry.

## Why

An alert is useful only when an operator can understand why it exists and where
to continue investigation.

## Scope

- Add alert list and alert detail/context presentation.
- Link alerts to device detail and triggering telemetry.
- Add severity/status presentation using icon, label, and color.
- Add loading, empty, error, and refresh behavior.
- Add component and browser tests.

## Out of Scope

- Notification delivery, acknowledgement workflow, escalation, bulk actions,
  or advanced filtering.

## Dependencies

- MVP-007 and MVP-008.

## Architecture / Boundaries

The console presents alert authority from the API and does not infer alert
matches from cached telemetry.

## Implementation Direction

Prioritize traceability over dashboard breadth. Reuse device and telemetry
navigation rather than adding a separate investigation subsystem.

## Validation

- Browser test shows a simulator-triggered alert and follows its context.
- Empty/error/stale states are visible.
- Status never relies on color alone.
- Tenant-scoped navigation cannot expose another tenant's identifiers.

## Documentation Updates

- Update UI reference only for reusable alert patterns.
- Document the limited MVP alert behavior.

## Risks / Open Decisions

- Active versus historical status semantics.
- Refresh behavior before realtime transport exists.

## Done Criteria

An operator can identify what happened, which device was affected, and which
measurement triggered the alert without relying on infrastructure logs.
