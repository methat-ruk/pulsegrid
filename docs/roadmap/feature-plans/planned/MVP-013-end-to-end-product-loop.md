# MVP-013 — End-to-End Product Loop

Status: Planned

Intended PR: One MVP-acceptance PR

Milestone: M5 — MVP acceptance

## Goal

Make the complete PulseGrid MVP loop repeatable and verifiable from a clean
local checkout.

## Why

Individually working screens, handlers, and dependencies do not prove that the
device-to-operator-to-device journey works as a coherent product.

## Scope

- Add a repeatable end-to-end scenario for provisioning a device, receiving
  telemetry, creating an alert, sending a command, and observing its result.
- Validate selected degraded paths, including malformed telemetry, duplicate
  input, explicit command failure, and timeout.
- Confirm tenant isolation with at least two test tenants/fixtures.
- Verify clean setup, environment separation, and documented commands.
- Reconcile README, product scope, architecture, roadmap, and technology status
  with the implemented reality.

## Out of Scope

- Production deployment, load certification, Kafka, Kubernetes, full security
  readiness, or new product capability.

## Dependencies

- MVP-009 and MVP-012.

## Architecture / Boundaries

This plan validates existing boundaries and does not add a new platform layer.
Failures found during validation are fixed in the owning module or re-planned
if the correction changes scope materially.

## Implementation Direction

Use deterministic fixtures and the same public/device boundaries used by the
product. Do not bypass MQTT, GraphQL, or persistence to manufacture a passing
test.

## Validation

- Clean local setup and all documented checks pass.
- The complete primary product loop passes through real boundaries.
- Selected degraded-path and tenant-isolation scenarios pass.
- No test or development configuration can target production endpoints.
- Results, skipped checks, limitations, and residual risk are recorded.

## Documentation Updates

- Mark only verified capabilities as implemented.
- Update roadmap and plan statuses.
- Add the validated developer/demo workflow.
- Keep Post-MVP technologies labeled conditional or deferred.

## Risks / Open Decisions

- End-to-end runtime and fixture isolation may expose missing lifecycle
  ownership in earlier plans.
- MVP acceptance does not imply production readiness.

## Done Criteria

From a clean checkout, a contributor can follow documented commands to execute
and verify the complete MVP loop, including named failure paths, without any
distributed-infrastructure dependency.
