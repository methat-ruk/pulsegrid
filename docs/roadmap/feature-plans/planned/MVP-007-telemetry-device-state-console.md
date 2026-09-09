# MVP-007 — Telemetry and Device-State Console

Status: Planned

Intended PR: One frontend-telemetry PR

Milestone: M2 — Telemetry and current state

## Goal

Show a device's current measurements, connectivity/last-seen state, and recent
telemetry in the console.

## Why

This is the first operator-visible proof that device input becomes useful
platform state.

## Scope

- Add current-state and recent-telemetry sections to device detail.
- Add an ECharts visualization only where it materially improves the time-series
  view.
- Define a bounded refresh strategy.
- Add loading, empty, stale, error, and retry states.
- Add accessible textual summaries for charted data.

## Out of Scope

- WebSocket/subscription infrastructure, fleet dashboards, maps, long-range
  analytics, or arbitrary metric builders.

## Dependencies

- MVP-003 and MVP-006.

## Architecture / Boundaries

The UI renders server-owned state. Staleness is explicit and must not be hidden
by cached visual state.

## Implementation Direction

Begin with bounded polling or explicit refresh unless measured freshness needs
justify a persistent realtime transport.

## Validation

- Component tests cover all visible async and stale states.
- Browser test observes simulator telemetry appear for the correct device.
- Chart has a readable non-visual summary.
- Responsive and keyboard behavior is verified.

## Documentation Updates

- Record refresh and chart decisions.
- Update the UI design system only for reusable telemetry patterns.

## Risks / Open Decisions

- Poll interval and cache invalidation.
- Exact connectivity/staleness threshold.
- Chart introduction depends on enough points to convey meaningful behavior.

## Done Criteria

An operator can distinguish current, stale, empty, and failed telemetry states
and inspect bounded recent values for the correct tenant-owned device.
