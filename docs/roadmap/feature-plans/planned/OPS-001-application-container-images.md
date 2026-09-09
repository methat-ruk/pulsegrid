# OPS-001 — Application Container Images

Status: Deferred

Intended PR: One application-packaging PR

Milestone: P5 — Runtime orchestration

## Goal

Create reproducible production-mode container images for the Go backend and
Nuxt frontend when a containerized run or deployment target exists.

## Why

Dockerfiles have ongoing build, security, runtime, and patching ownership. They
should be introduced when an image is actually built and exercised, not as
unused Foundation scaffolding.

## Scope

- Add separate backend and frontend Dockerfiles with reproducible multi-stage
  builds.
- Run application processes as non-root users with minimal runtime contents.
- Inject runtime configuration without copying real `.env` files or secrets.
- Define health checks and graceful termination behavior against implemented
  application contracts.
- Add local/CI image build and container smoke validation.

## Out of Scope

- Image registry publication, production deployment, Kubernetes, Helm, KEDA,
  database/broker images, or secret-manager selection.

## Dependencies

- MVP-013 and a confirmed containerized run or deployment requirement.

## Architecture / Boundaries

Each image packages one existing application boundary. Containerization must
not create a service split, change data ownership, or bake environment-specific
state into the image.

## Implementation Direction

Choose base images and frontend serving mode from the implemented Go and Nuxt
runtimes. Pin and update dependencies through the repository's normal
maintenance path.

## Validation

- Both images build reproducibly from a clean checkout.
- Containers run as non-root and start in production mode.
- Health, shutdown, and browser-to-backend smoke checks pass in containers.
- Image layers contain no `.env` files, source credentials, or test fixtures
  with secrets.
- A selected image vulnerability check reports its evidence and limitations.

## Documentation Updates

- Add image build/run commands and runtime-configuration contract.
- Update technology decisions and roadmap status when the adoption trigger is
  approved.

## Risks / Open Decisions

- Containerized frontend serving mode and base images.
- Multi-architecture build requirement.
- Registry, provenance, signing, and release policy are future decisions.

## Done Criteria

Backend and frontend production-mode images build and run through documented
commands with validated health, shutdown, configuration injection, and no
embedded secrets.
