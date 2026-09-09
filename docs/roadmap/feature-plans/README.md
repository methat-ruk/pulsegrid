# PulseGrid Feature Plans

Status: Planning source of truth

## Purpose

This directory owns reviewable PR-level plans. Each plan describes one intended
pull request with a coherent outcome, explicit exclusions, dependencies,
validation, documentation work, and done criteria.

Plans are separated by lifecycle so repository browsing immediately shows what
has and has not been completed:

- `planned/` contains Proposed, Planned, In progress, Ready for review, Blocked,
  and Deferred work;
- `completed/` contains only plans whose outcome and required evidence are
  complete.

The `Status` field provides the finer-grained state while directory placement
provides the completed/not-completed split. Moving a plan to `completed/` and
updating inbound links are part of the same completion change.

## Status definitions

- **Proposed**: scope is being reviewed and is not approved for implementation.
- **Planned**: approved and ready to be selected for implementation.
- **In progress**: implementation has started.
- **Ready for review**: implementation and author review are complete.
- **Complete**: merged or otherwise accepted with required evidence.
- **Blocked**: a named decision or external change prevents progress.
- **Deferred**: intentionally outside the current delivery horizon.

## Required plan structure

Every feature plan must contain:

```text
Status
Intended PR
Milestone
Goal
Why
Scope
Out of Scope
Dependencies
Architecture / Boundaries
Implementation Direction
Validation
Documentation Updates
Risks / Open Decisions
Done Criteria
```

The plan must describe an observable or enabling outcome. “Implement the
platform foundation” is not an acceptable PR boundary.

## Plan index

### Completed

- [DOC-001 — Documentation foundation](completed/DOC-001-documentation-foundation.md)

### Planned — Foundation

- [FND-001 — Go API foundation](planned/FND-001-go-api-foundation.md)
- [FND-002 — Nuxt console foundation](planned/FND-002-nuxt-console-foundation.md)
- [FND-003 — Repository quality and local workflow](planned/FND-003-repository-quality-and-local-workflow.md)
- [FND-004 — Full-stack development integration](planned/FND-004-full-stack-development-integration.md)

### MVP — Device registry

- [MVP-001 — Tenant and device persistence](planned/MVP-001-tenant-device-persistence.md)
- [MVP-002 — Device GraphQL API](planned/MVP-002-device-graphql-api.md)
- [MVP-003 — Device registry console](planned/MVP-003-device-registry-console.md)

### MVP — Telemetry and current state

- [MVP-004 — MQTT local runtime and simulator](planned/MVP-004-mqtt-local-runtime-and-simulator.md)
- [MVP-005 — MQTT telemetry ingestion](planned/MVP-005-mqtt-telemetry-ingestion.md)
- [MVP-006 — Telemetry and current-state projection](planned/MVP-006-telemetry-current-state-projection.md)
- [MVP-007 — Telemetry and device-state console](planned/MVP-007-telemetry-device-state-console.md)

### MVP — Rules and alerts

- [MVP-008 — Threshold rule and alert backend](planned/MVP-008-threshold-rule-alert-backend.md)
- [MVP-009 — Alert console](planned/MVP-009-alert-console.md)

### MVP — Remote commands

- [MVP-010 — Command model and GraphQL API](planned/MVP-010-command-model-graphql-api.md)
- [MVP-011 — MQTT command delivery and acknowledgement](planned/MVP-011-mqtt-command-delivery-acknowledgement.md)
- [MVP-012 — Command console](planned/MVP-012-command-console.md)

### MVP acceptance

- [MVP-013 — End-to-end product loop](planned/MVP-013-end-to-end-product-loop.md)

### Deferred packaging

- [OPS-001 — Application container images](planned/OPS-001-application-container-images.md)

## Concern-to-plan map

| Concern | Owning plan or plans | Why it is placed there |
| --- | --- | --- |
| Go project and backend foundation | FND-001 | Creates the first runnable backend process and its configuration/tests |
| Vue/Nuxt project and frontend foundation | FND-002 | Creates the console shell and browser-safe configuration boundary |
| Repository commands, lint, tests, Git hooks, and CI | FND-003 | Composes real backend/frontend checks after both applications exist |
| Frontend-to-backend local connection | FND-004 | Proves HTTP/proxy/origin and failure behavior before domain APIs |
| Development, test, and production configuration | FND-001 to FND-004, then each consuming feature | Keys are added with their consumers; FND-003 enforces repository/CI policy |
| PostgreSQL and migrations | MVP-001 | Added with the first concrete organization/device data authority |
| GraphQL/gqlgen | MVP-002 | Added with the first concrete device API contract |
| Frontend-to-GraphQL product integration | MVP-003 | Connects the first real operator journey instead of a placeholder API |
| PostgreSQL Docker Compose service | MVP-001 | Added when persistence has an immediate consumer |
| MQTT Docker Compose service and simulator | MVP-004 | Added when the first device transport flow exists |
| Backend and frontend Dockerfiles | OPS-001 | Deferred until an image has a real local/CI/deployment consumer |
| Kafka, MongoDB, Redis, gRPC, Kubernetes, Helm, KEDA | Post-MVP adoption plans | Added only after their documented trigger is satisfied |

## Planning rules

- A plan should map to one reviewable and mergeable PR.
- Infrastructure is added in the first plan with an immediate consumer.
- A runtime or service boundary requires a concrete scaling, failure,
  reliability, or ownership reason.
- New configuration keys must update the environment examples and the
  [environment strategy](../../project-setup/environment-configuration.md).
- Tests and documentation are part of the plan, not cleanup after the feature.
- When a decision becomes expensive to reverse, record it durably in the same
  PR and link it from the plan.
