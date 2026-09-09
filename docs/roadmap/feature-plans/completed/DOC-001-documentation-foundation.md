# DOC-001 — Documentation Foundation

Status: Complete

Branch: `main` (delivered in initial repository commit `32a3e95`)

Intended PR: One documentation-only PR

Milestone: D0 — Documentation foundation

## Goal

Turn the repository documentation into a concise entry point backed by clear,
non-overlapping sources of truth and dependency-ordered feature plans.

## Why

The original README combines product scope, architecture, technology choices,
failure handling, development intent, and roadmap detail. Empty roadmap files
leave no authoritative MVP or delivery boundary.

## Scope

- Refactor the root README into a GitHub entry point.
- Define product scope and the Foundation/MVP/Post-MVP boundary.
- Define current, MVP, and target architecture without claiming implementation.
- Record technology states and adoption triggers.
- Define development, test, and production configuration policy.
- Add Mermaid views for the MVP product loop, runtime architecture,
  architecture evolution, and roadmap dependencies.
- Populate the roadmap, feature-plan index, and Foundation/MVP plans.
- Restore the thin PulseGrid project-profile routing adapter.

## Out of Scope

- Application or infrastructure implementation.
- Actual `.env` files, credentials, CI workflows, or container definitions.
- Final API, event, database, broker, deployment, or observability contracts.
- Post-MVP feature plans.

## Dependencies

None.

## Architecture / Boundaries

Public project documentation is canonical. `references/pulsegrid/profile.md`
is a routing adapter only and must link to, not duplicate, public sources.

## Implementation Direction

Use one product document, one system-architecture document, one technology
decision register, one environment policy, one roadmap, and PR-sized plans.
Keep event, data, reliability, and observability in the architecture document
until one concern has enough independent implementation and lifecycle to split.

## Validation

- Verify all local Markdown links resolve.
- Verify README statements distinguish current state from target direction.
- Verify roadmap dependencies are acyclic and every Foundation/MVP plan is
  linked.
- Verify Mermaid blocks are present and structurally closed in each intended
  document; render verification remains a review step when a Mermaid renderer
  is available.
- Verify every feature plan contains the required sections.
- Verify no application code, infrastructure, or real environment file is
  introduced.

## Documentation Updates

All work in this plan is documentation.

## Risks / Open Decisions

- The initial documentation was delivered directly on `main` before the
  one-plan/one-branch convention was established.
- Application layout was intentionally deferred to Foundation plan review and
  is now recorded in the technology decision register.

## Done Criteria

- README is a concise entry point.
- Document ownership and cross-links are explicit.
- MVP proves one complete product loop without requiring distributed topology.
- Foundation/MVP plans are independently reviewable and dependency ordered.
- Environment separation and secret handling are explicit.

## Completion Evidence

- All local links resolve across 28 Markdown files.
- Nineteen feature plans contain every required planning section.
- Lifecycle placement is consistent: one completed plan and eighteen
  planned/deferred plans.
- Mermaid CLI 11.17.0 rendered all twelve repository diagrams successfully.
- No application code, Dockerfile, Docker Compose configuration, or real
  `.env` file was introduced.
