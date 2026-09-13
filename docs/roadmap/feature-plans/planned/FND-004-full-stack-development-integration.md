# FND-004 — Full-Stack Development Integration

Status: Planned

Branch: `feat/fnd-004-full-stack-development-integration`

Intended PR: One local-integration PR

Milestone: F0 — Executable repository foundation

Impact: Material Change (Tier 2) because this PR adds a browser-visible HTTP
boundary, server-only runtime configuration, and cross-process CI evidence.

## Goal

Prove that the Nuxt console can reach the Go/Fiber process through one
same-origin local-development route, report that process's readiness honestly,
and recover visibly when the connection fails. This is not product readiness.

## Acceptance Boundary

A contributor can start both existing applications, see an accurate API-process
readiness state while the planned product state remains visible, and run a
repeatable browser-to-Go smoke in isolated test mode. The existing required CI
browser check must enforce the real cross-process success path; focused tests
must also protect unavailable, malformed, timeout, and retry behavior.

## Why

Independent frontend and backend boot success does not prove that browser
origin, Nuxt server routing, runtime configuration, failure mapping, and the
contributor workflow compose correctly.

## Plan Review Baseline

At the review baseline, `feat/fnd-004-full-stack-development-integration`,
`main`, and `origin/main` all point to `f27662f` (the merged FND-003 PR), and
the working tree has no FND-004 implementation diff.

- FND-001 already owns `GET /health/ready`, its OpenAPI wire contract, Go
  environment selection, loopback development port `8080`, and test port
  `18080`. It intentionally has no CORS or product API.
- FND-002 already owns the planned-state overview, Nuxt's default SSR mode,
  `NUXT_APP_ENV` startup validation, and an empty public runtime-config
  surface. It has no backend call or origin configuration.
- FND-003 already owns root setup/check commands, the pinned Playwright runner,
  Chromium installation, test-mode Nuxt build/server on `127.0.0.1:4173`, and
  twelve independent required CI jobs. `browser-smoke` is already required;
  its recorded warm critical-path time was about 56 seconds before this PR.
- Some existing status prose still describes FND-003 as unmerged or the
  frontend as absent. Git history and the codebase, not that stale prose,
  define this plan's implementation baseline. Correct relevant tracked prose
  when those documents are touched, without broad cleanup.

## Scope

- Add one narrow Nuxt server-owned, same-origin
  `GET /api/operational/ready` endpoint that queries only the existing Go
  `GET /health/ready` endpoint. Do not add a general-purpose API proxy.
- Add one private Nuxt runtime-config key, `NUXT_BACKEND_ORIGIN`, consumed by
  that server route in development and test. Add only reviewed, non-secret
  examples and validation.
- Add a small readiness presentation on the existing overview with initial
  checking, ready, unavailable, and manual retry states. Preserve a separate,
  truthful message that product data and workflows are not connected yet.
- Extend the FND-003 Playwright foundation to run a real browser → Nuxt → Go
  readiness journey, and add focused failure-path evidence at the appropriate
  boundary.
- Document the two-process development workflow and isolated test workflow.

## Out of Scope

- GraphQL/domain endpoints, PostgreSQL, MQTT, authentication, authorization,
  credentials, browser-public backend URLs, product metrics, polling, or
  realtime connectivity.
- Backend CORS, a generic `/api/**` proxy, production origin/routing policy,
  Dockerfiles, deployment, or branch-protection redesign.
- Replacing the FND-001 health contract, the FND-002 shell, the FND-003 test
  runner, or the twelve existing CI quality guarantees.

## Dependencies

FND-003 is merged and includes the FND-001/FND-002 prerequisites. No new
database, broker, service, package, or external provider is required.

## Architecture / Boundaries

```text
Browser (same origin, relative GET /api/operational/ready)
  → Nuxt server route (fixed GET, private NUXT_BACKEND_ORIGIN)
  → Go GET /health/ready (existing OpenAPI contract)
```

- The browser never imports Go code, reads the backend origin, or calls the Go
  listener directly. CORS stays disabled on Go. Nuxt's public runtime-config
  object stays empty; the browser uses a fixed relative path.
- The Nuxt route accepts no user-selected destination or path, forwards no
  browser cookies, authorization headers, or arbitrary request headers, and
  does not follow upstream redirects. It uses a short bounded timeout, a
  bounded response body, and `Cache-Control: no-store`.
- Only an upstream HTTP `200` with the expected `{ "status": "ready" }`
  response becomes HTTP `200` `{ "status": "ready" }` at the Nuxt boundary.
  Go `503/not_ready`, connection refusal, timeout, malformed/unexpected body,
  redirect, and other upstream errors become a safe HTTP `503`
  `{ "status": "unavailable" }`. Do not reflect the upstream body, URL,
  configuration, stack, or internal error into browser responses or logs.
- Readiness means only that the Go process currently accepts normal traffic.
  It is neither database/broker health nor an operator/product capability
  signal. A successful check can become stale immediately; no persistent
  “platform healthy” claim is made.
- Development requires an explicit loopback backend origin, normally
  `http://127.0.0.1:8080`. Test requires the isolated, explicit
  `http://127.0.0.1:18080` origin and must reject a missing or development
  origin rather than fall back to it. Validate absolute HTTP origin syntax,
  loopback host, expected mode/port, and absence of credentials, paths,
  query, or fragments at Nuxt startup. Process environment retains precedence
  over the selected local dotenv file.
- Production connectivity remains undecided. A production build must still
  succeed without this local backend key; the route must not silently use a
  development/test origin in production. Reject an explicitly supplied
  `NUXT_BACKEND_ORIGIN` in production rather than silently ignoring it, and
  return a safe unavailable response from this narrow route until a later
  plan defines production routing.

## Implementation Direction

1. Add the private runtime-config field and extend the existing Nitro startup
   validator with a small, separately testable origin parser. Keep the
   environment key out of `runtimeConfig.public` and avoid build-time
   `process.env` substitution for the upstream destination. Inject the test
   origin explicitly into every test-mode Nuxt startup, including `web:test`
   and the built-server browser command; do not rely on an ignored `.env.test`
   file or a development fallback.
2. Add the single GET-only Nuxt server route. Read runtime config per request,
   use only the fixed Go readiness path, validate the upstream status/body,
   and translate every expected failure into the stable safe response above.
   Keep the Go handler and its OpenAPI contract unchanged.
3. Add a focused overview component that checks on client mount so a Go outage
   cannot fail page SSR or cause duplicate hydration requests. Give every
   pending request a bounded exit; disable duplicate retries, ignore or cancel
   stale responses after retry/unmount, and announce state changes accessibly
   without relying on color alone. Preserve the existing planned-product
   message with copy adjusted only where “connected” would now be ambiguous.
4. Extend the root browser command to build a test-mode Nuxt artifact and a
   Go test binary in ignored output. Keep Playwright `webServer` responsible
   for the existing built Nuxt process on `127.0.0.1:4173`; use a narrowly
   scoped Playwright fixture to start/stop the direct Go binary on
   `127.0.0.1:18080` for each affected browser test. This deliberate fixture
   choice allows a real backend stop/restart within a browser journey. Wait
   for Go readiness, refuse existing listeners, and own child-process teardown
   even when an assertion fails. Do not use a `go run` wrapper that can leave
   a child listener behind.
5. Keep the real Go process and Nuxt route in the browser success and
   unavailable/retry/recovery tests: stop Go, observe the browser's unavailable
   state after a check, restart Go, and verify a manual retry becomes ready.
   Use focused Nuxt-route tests for controlled `503`, malformed response,
   redirect, and timeout cases that the normal Go process cannot generate
   reliably. Update the existing planned-state browser assertions for the
   new status without weakening their layout, keyboard, or console checks.
6. Update the environment examples, local and application guides, and relevant
   current-state prose. Review the complete diff and required evidence before
   marking the plan complete.

## CI / Parallelization Strategy

- Preserve all twelve independent FND-003 jobs and their required status. Do
  not add a shared setup job or make static, unit, audit, or OpenAPI checks
  depend on the browser journey.
- Extend only the existing required `browser-smoke` job with pinned Go setup
  and the real full-stack browser journey. Reuse its Node/pnpm store flow,
  Chromium installation, test-mode Nuxt build, one worker, retry/flaky policy,
  and failure diagnostics. The test fixture owns Go binary lifecycle within
  that job. This avoids another browser download, runner
  startup, and branch-protection context while keeping other jobs parallel.
- Keep `api-test`, `api-race`, `web-test`, `web-build`, security audits, and the
  remaining required jobs authoritative; focused route/config tests run in
  `web-test`, and the real cross-process journey runs in `browser-smoke`.
- Measure the first cold and warm PR runs against FND-003's approximately
  two-minute warm critical-path target. The 56-second baseline is historical,
  not a prediction for this larger job. If the new browser job exceeds that
  target, optimize redundant setup or revisit an independent, parallel,
  **required** full-stack job; never demote the cross-boundary evidence to
  non-blocking merely to meet the timing target.

## Local Workflow

1. From a clean checkout, run `corepack pnpm run setup`; install the pinned
   browser only when needed with `corepack pnpm run setup:browser`.
2. Copy the reviewed development examples for both applications. Run
   `corepack pnpm run dev:api` and `corepack pnpm run dev:web` in separate
   terminals; confirm Go readiness at `127.0.0.1:8080` and the overview's
   readiness state through the Nuxt origin. Stop Go and use Retry to confirm
   visible unavailability; restart Go and retry to confirm recovery.
3. Run focused Go/Nuxt checks while editing, then
   `corepack pnpm run test:browser` for the isolated test-mode cross-process
   smoke. Before a PR handoff run `corepack pnpm run check`, which must still
   include every required local gate. Hooks remain fast staged-file feedback.
4. Ports `4173` and `18080` belong to the test run. An occupied listener,
   missing test origin, failed startup, or leaked child process is a test
   failure, not permission to reuse a development service.

## Validation

| Guarantee | Required evidence |
| --- | --- |
| Origin and configuration isolation | Parser/startup tests for valid development/test origins, missing/invalid values, credentials/path/query/fragment, non-loopback host, test-to-development fallback rejection, and production with no local routing key |
| Narrow/safe HTTP route | GET-only route tests for `200/ready`, Go `503/not_ready`, refused connection, timeout, redirect, malformed or unexpected response, safe `503` mapping, no-store behavior, no arbitrary header/cookie forwarding, and no leaked upstream details |
| Browser-visible behavior | Component tests for checking/ready/unavailable/retry and stale-request handling; accessible label/live feedback; planned-product message remains truthful |
| Real full-stack composition | Playwright starts the built Nuxt test server; its fixture starts the real Go test binary; the browser requests the same-origin route; resulting ready state is backed by actual Go `/health/ready`, not a mocked network response |
| Degraded/recovery UI | Playwright stops the real Go child process, verifies the browser becomes unavailable after a check, restarts Go, and verifies manual retry → ready through the unchanged Nuxt route. Focused route tests separately cover malformed/slow/redirect responses. |
| Runtime and artifact safety | Development manual stop/restart check; test port-conflict/teardown check; production build remains green; backend-origin sentinel absent from HTML, hydration payload, and client assets |
| Regression and CI | Existing browser viewport/keyboard/console assertions remain meaningful; `check:fast`, `check`, all twelve required CI contexts, and cold/warm timing observations are reviewed on the final candidate |

## Documentation Updates

- Update the canonical local-development guide with two-process startup,
  expected API-only readiness meaning, browser-test lifecycle, ports, and
  troubleshooting. Remove stale FND-003 pre-merge wording in that touched file.
- Update the web-console guide and `.env.example`, `.env.development.example`,
  and `.env.test.example` with only the consumed private origin. Keep the
  production example free of a usable backend origin.
- Update the environment strategy's FND-004 key, precedence, isolation, and
  production deferral; record the same-origin route decision in the applicable
  architecture/technology decision source without implying production
  routing or GraphQL adoption.
- Update root README/current-state wording and the plan status only when the
  corresponding behavior is validated. Leave the ignored local project
  profile and unrelated historical FND-003 evidence out of this PR.

## Risks / Decisions and Recovery

- **Selected:** one narrow Nuxt server route with private runtime-config
  origin. A direct browser-to-Go URL would require CORS, expose a backend
  endpoint, and create a second browser origin policy for a single health
  probe. A static Nuxt `routeRules` proxy is a poor fit for a runtime-selected
  test origin and would risk baking local configuration into an artifact.
- **Accepted limitation:** the status is a point-in-time Go process readiness
  probe. It does not certify product dependencies, connectivity over time, or
  production routing.
- **Failure and recovery:** a missing Go process, not-ready response, malformed
  response, or timeout produces a bounded unavailable state. The contributor
  can start/fix Go and manually retry; no automatic retry storm or hidden
  fallback is introduced.
- **Rollback:** remove the Nuxt route/status and its private key, restore the
  prior overview/browser smoke, and retain the unchanged FND-001/FND-003
  foundations. No persistent data or public Go contract migration is needed.
- **Revisit trigger:** a real product API consumer or approved production
  routing requirement may need a separate API/BFF or proxy decision; do not
  generalize this operational probe into that future contract now.

## Engineering Improvement Review

- Current scope: bounded timeout, strict response validation, no-store
  semantics, honest async/retry states, test isolation, and real-boundary
  success evidence are tightly coupled to this new connection and prevent
  misleading status or unsafe routing.
- Future enhancements: product API integration and production routing only
  when their respective feature plans supply contracts and deployment context.
- Scope effect: unchanged; no new dependency or infrastructure is justified.

## Done Criteria

The overview shows the real Go process readiness without claiming product
availability; failure and manual recovery are visible and bounded; the
same-origin route and private/test configuration pass their negative tests;
the existing required `browser-smoke` job proves the browser-to-real-Go path;
all existing quality gates remain required and green on the final candidate;
and the local guide reproduces the journey from a clean checkout. Production
routing, product APIs, and infrastructure remain explicitly unimplemented.
