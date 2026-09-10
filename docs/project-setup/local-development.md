# PulseGrid local development

Status: Local repository workflow implemented; the first GitHub CI run passed,
while merge-gate enforcement remains pending FND-003 approval.

This is the canonical guide for setting up and validating the repository. The
Go API and Nuxt console remain independently runnable until FND-004 introduces
the first full-stack development mode.

## Requirements

- Node `24.20.0` from `.node-version`;
- pnpm `12.3.4` through Corepack;
- Go `1.27.1` from `.go-version`.

Verify the selected versions before setup:

```sh
node --version
corepack pnpm --version
go version
```

## First setup

From the repository root:

```sh
corepack pnpm run setup
```

This runs a frozen workspace install and downloads Go modules. It must work
from a clean checkout without another lockfile or an ignored local file.

Install the pinned local Chromium headless shell only when browser evidence is
needed:

```sh
corepack pnpm run setup:browser
```

CI installs the same pinned browser revision with the required Linux
dependencies. Browser binaries and generated application output are not
committed or cached as correctness inputs.

## Runtime modes

### Independent development processes

Run each application in its own terminal. Neither command starts the other
application or any database, broker, proxy, or full-stack dependency.

```sh
corepack pnpm run dev:api
corepack pnpm run dev:web
```

The API uses `PULSEGRID_ENV=development` and its existing local listener. The
console uses its existing `.env.development` contract; copy the reviewed
example first if the file does not exist:

```sh
cp apps/api/.env.development.example apps/api/.env.development
cp apps/web-console/.env.development.example apps/web-console/.env.development
```

Do not add backend URLs, credentials, tokens, or browser-public runtime keys to
these foundation examples. FND-004 owns the first frontend-to-backend routing
key.

### Isolated browser smoke

The browser command builds the console with `NUXT_APP_ENV=test`, starts that
fresh build on `127.0.0.1:4173`, and owns server teardown. It refuses to reuse
an existing process, so stop anything already listening on that port before
running it:

```sh
corepack pnpm run test:browser
```

This smoke covers planned-state rendering, responsive reflow including 320px,
horizontal-overflow absence, keyboard navigation, page errors, and browser
console errors. It does not claim a backend journey.

## Validation commands

| Command | Feedback boundary |
| --- | --- |
| `corepack pnpm run format` | Apply Go formatting and the existing frontend ESLint fix behavior |
| `corepack pnpm run format:check` | Check Go formatting and frontend stylistic lint without rewriting |
| `corepack pnpm run lint` | Go vet, frontend lint, and OpenAPI lint |
| `corepack pnpm run typecheck` | Nuxt/TypeScript typecheck |
| `corepack pnpm run test` | Go tests and frontend Vitest in explicit test mode |
| `corepack pnpm run test:race` | Go race suite |
| `corepack pnpm run build` | Go compilation and Nuxt production build |
| `corepack pnpm run openapi` | Lint, bundle, and static HTML rendering into ignored `.openapi/` |
| `corepack pnpm run audit` | Node production audit and reachable Go vulnerability scan |
| `corepack pnpm run check:fast` | Fast pre-CI handoff: formatting, lint, typecheck, and ordinary tests |
| `corepack pnpm run check` | Full pre-CI handoff, including race, build, OpenAPI, audits, and browser smoke |

The pre-commit hook runs only staged Go formatting, staged frontend ESLint,
staged OpenAPI lint, and the tracked environment-filename policy. Hooks are
convenience feedback and can be bypassed; CI remains authoritative.

Before opening or updating a pull request, run:

```sh
corepack pnpm run check
```

## Environment and secret policy

Tracked files may contain only reviewed `*.example` environment templates.
Real `.env` and `.env.*` files are ignored and must never contain production
credentials in source control. The repository policy check also rejects a
forced tracked real environment file, even when its contents do not look like
a secret.

Automated tests set `PULSEGRID_ENV=test` or `NUXT_APP_ENV=test` explicitly.
Production builds set `NUXT_APP_ENV=production` and use runtime-injected
configuration; they do not load development or test dotenv files.

## Troubleshooting

- If `corepack pnpm run setup` reports a lockfile or peer diagnostic, do not
  replace the lockfile. The current `@bomb.sh/tab`/`cac` peer mismatch is an
  upstream diagnostic and is not an authoritative gate.
- `corepack pnpm run node:audit` keeps low and informational advisories visible
  but blocks moderate, high, and critical production findings. The current
  low `esbuild` advisory is documented for follow-up.
- `govulncheck` may list vulnerabilities in required Go modules that current
  code does not reach. They remain visible and are not reported as reachable
  application vulnerabilities.
- A Nuxt build may print one generated-dependency Rollup annotation warning.
  It does not originate in application source and does not fail the build.
- If the browser smoke cannot start, check port `4173`, run
  `corepack pnpm run setup:browser`, and remove only the generated
  `.output/`/`test-results/` directories if a stale local run is suspected.
- If a hook is unavailable after setup, run `corepack pnpm run prepare` and
  verify that Git's local `core.hooksPath` points to `.husky/_`. CI does not
  depend on the hook.

## Related guides

- [API guide](../../apps/api/README.md)
- [Web console guide](../../apps/web-console/README.md)
- [Environment strategy](environment-configuration.md)
- [FND-003 plan](../roadmap/feature-plans/planned/FND-003-repository-quality-and-local-workflow.md)
