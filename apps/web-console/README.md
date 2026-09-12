# PulseGrid Web Console

This directory owns the initial Vue/Nuxt console shell. It currently renders a
truthful planned state and does not connect to a backend or expose product
domain behavior.

## Requirements

- Node 24.20.0, selected through the repository `.node-version`
- Corepack with the repository-pinned pnpm version

From the repository root, install the workspace dependencies with:

```sh
corepack pnpm install
```

The composed setup, local run modes, fast checks, full pre-CI validation, and
browser smoke are documented in [the local-development guide](../../docs/project-setup/local-development.md).

## Environment

The console requires `NUXT_APP_ENV` with one of these values:

- `development` for local interactive work
- `test` for deterministic automated validation
- `production` for production-mode preview checks

For local development, copy the reviewed example and run the app:

```sh
cp apps/web-console/.env.development.example apps/web-console/.env.development
corepack pnpm --filter @pulsegrid/web-console dev
```

Production configuration is injected by the process. A deployed Nuxt server
does not read `.env` files after build:

```sh
NUXT_APP_ENV=production corepack pnpm --filter @pulsegrid/web-console build
NUXT_APP_ENV=production corepack pnpm --filter @pulsegrid/web-console preview
```

Do not add backend URLs, credentials, tokens, or browser-public runtime keys to
this foundation. The first API routing key belongs to FND-004.

## Checks

Run from the repository root:

```sh
corepack pnpm --filter @pulsegrid/web-console lint
corepack pnpm --filter @pulsegrid/web-console typecheck
corepack pnpm --filter @pulsegrid/web-console test
corepack pnpm --filter @pulsegrid/web-console build
```

Browser verification must cover the planned-state route at desktop, tablet,
mobile, and 320px widths, including keyboard focus and reduced-motion behavior.
The repository-root `corepack pnpm run test:browser` command owns the isolated
test-mode build and server lifecycle for that evidence.

## Known diagnostics and warnings

- `@theme` in `app/assets/css/main.css` is a valid Tailwind CSS v4 directive.
  Open the repository root in Zed. The repository `.zed/settings.json` selects
  Zed's `tailwindcss-intellisense-css` language server for CSS and disables the
  default CSS language server, so Zed understands Tailwind-specific at-rules
  without hiding unrelated CSS diagnostics. Restart or reopen the Zed
  workspace after changing editor settings if a stale diagnostic remains.
- Nuxt Test Utils and Vitest imports require the workspace TypeScript SDK. Run
  `corepack pnpm install` before treating those imports as unresolved. Zed's
  TypeScript language server should resolve them from the installed workspace
  package graph.
- Nuxt build may print one Rollup `@__NO_SIDE_EFFECTS__` annotation warning
  from generated dependency code. It does not originate in application source
  and does not fail the build.
- `pnpm peers check` may report the transitive `@bomb.sh/tab`/`cac` peer
  advisory. It is outside the FND-002 runtime boundary and is tracked for the
  repository quality/maintenance milestone.
- On hosts with a low file-descriptor limit, `nuxt dev` can report `EMFILE`
  while starting its watcher. This is a local watcher-capacity limitation;
  production build and preview checks remain the authoritative runtime smoke
  path until the repository workflow adds a host-specific mitigation.
