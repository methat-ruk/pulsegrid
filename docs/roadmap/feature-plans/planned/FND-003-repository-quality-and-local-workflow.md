# FND-003 — Repository Quality and Local Workflow

Status: Planned

Branch: `chore/fnd-003-repository-quality-and-local-workflow`

Intended PR: One repository-tooling PR

Milestone: F0 — Executable repository foundation

## Goal

Provide one documented local workflow and an authoritative CI boundary for the
Go and Nuxt foundations.

## Why

Independent application commands are insufficient if contributors cannot run
consistent repository-level checks or if CI does not enforce them.

## Scope

- Add repository-level commands for setup, run, format, lint, typecheck, and
  test without hiding native tool output.
- Add GitHub Actions for backend and frontend checks.
- Add Husky and lint-staged for fast staged-file checks.
- Finalize `.gitignore` rules for real `.env` files and allow reviewed example
  files only.
- Supply explicit test-mode configuration in CI.
- Create the canonical local-development document.

## Out of Scope

- PostgreSQL, MQTT, integration environments, release/deployment pipelines, or
  production secrets.

## Dependencies

- FND-001 and FND-002.

## Architecture / Boundaries

Local hooks optimize feedback; CI remains authoritative. Repository commands
compose application-native tools rather than replacing their contracts.

## Implementation Direction

Keep commands non-interactive and cross-project behavior explicit. Cache only
safe dependency/build inputs. Make test environment selection visible in CI.

## Validation

- Clean-checkout setup and documented commands succeed.
- CI runs the same material checks as local commands.
- A deliberately failing check blocks CI and the relevant hook.
- Test jobs cannot target development resource names or endpoints.
- No tracked file contains a real secret or usable production credential.

## Documentation Updates

- Create `docs/project-setup/local-development.md`.
- Update environment examples and the environment strategy if implementation
  changes the planned precedence.
- Link development commands from the root README.

## Risks / Open Decisions

- Root task runner or script mechanism.
- Node dependency-cache strategy.
- Whether hooks should run tests or only fast static checks.

## Done Criteria

A new contributor can set up, run, and validate both applications using the
documented workflow, while CI independently enforces the required checks.
