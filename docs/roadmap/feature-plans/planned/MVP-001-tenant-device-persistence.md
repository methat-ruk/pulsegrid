# MVP-001 — Tenant and Device Persistence

Status: Planned

Intended PR: One persistence-foundation PR

Milestone: M1 — Device registry

## Goal

Create the first concrete PostgreSQL data boundary for organizations and
devices with tenant-scoped access enforced in the repository layer.

## Why

Provisioning and every later telemetry or command flow require a trustworthy
device-to-tenant authority.

## Scope

- Add PostgreSQL to local Docker Compose.
- Add migration tooling and organization/device tables.
- Add repository operations for create, list, and get under an explicit tenant.
- Seed or create a controlled development organization.
- Add integration-test database isolation and cleanup.

## Out of Scope

- Production authentication, RBAC, GraphQL, MongoDB, telemetry, or commands.

## Dependencies

- FND-001 and FND-003.

## Architecture / Boundaries

The device registry owns device identity and tenant association. Other modules
may reference a device identifier but may not change device ownership directly.

## Implementation Direction

Use PostgreSQL constraints and transactions for identity and ownership. Add
database configuration only in this PR and keep development/test databases
distinct.

## Validation

- Migration up/down or forward-recovery behavior is tested as selected.
- Repository integration tests cover create, duplicate identity, tenant-scoped
  list/get, and cross-tenant denial.
- Test configuration cannot connect to the development database.

## Documentation Updates

- Document PostgreSQL setup and migration commands.
- Add database keys to environment examples.
- Update architecture if data ownership changes.

## Risks / Open Decisions

- Migration tool.
- Identifier format and deletion/lifecycle semantics.
- Production tenant-isolation topology remains open.

## Done Criteria

Organizations and devices persist through migrations, and repository tests
prove that one tenant cannot read or mutate another tenant's device.
