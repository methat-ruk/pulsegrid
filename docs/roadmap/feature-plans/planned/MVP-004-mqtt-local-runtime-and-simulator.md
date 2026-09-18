# MVP-004 — MQTT Local Runtime and Simulator

Status: Planned

Review state: Plan reviewed and revised on 2026-09-18. No implementation has
started. The repository baseline, broker and client dependencies, MQTT
contract, local/test isolation, security limits, validation gates, rollback,
and lifecycle closeout are explicit. Implementation must stop and re-plan if
it needs production exposure, credentials, a different topic/payload contract,
broker persistence, an application consumer, or a broader runtime boundary.
Post-revision verdict: the plan conforms to the current project architecture
and is ready for implementation. Pinned image identity plus advisory re-scan
is the first dependency admission gate; adding the new MQTT CI context to
GitHub branch protection remains a separately approved pre-merge action.

Branch: `feat/mvp-004-mqtt-local-runtime-and-simulator`

Intended PR: One device-transport fixture PR

Milestone: M2 — Telemetry and current state

Impact: Material Change (Tier 2): this adds a pinned broker image, two local
Compose services, a new MQTT client dependency and command process, and the
first versioned device message contract consumed by later feature plans. It
does not change production topology, application startup, persistence,
GraphQL, tenant authority, or platform-side telemetry behavior.

## Goal

Provide a reproducible development/test MQTT broker and a one-shot device
simulator that publishes one documented, versioned temperature message for a
device ID copied from the existing registered-device journey.

The simulator proves the producer and transport fixture only. It must not
claim that PulseGrid has subscribed, validated, accepted, stored, or displayed
the message.

## Acceptance Boundary

This PR is complete when a contributor can:

1. register a device through the existing development GraphQL/console journey;
2. copy the returned device UUID into the ignored simulator configuration;
3. start the development broker with the documented command and observe MQTT
   readiness;
4. run the simulator once and receive a QoS 1 publish acknowledgement; and
5. use the isolated integration command to observe the exact topic and JSON
   payload through a real broker, including proof that it was not retained.

The required runtime evidence crosses this boundary:

```text
host-run device-simulator process
-> MQTT 3.1.1 over loopback TCP
-> pinned Eclipse Mosquitto container
-> independent test subscriber
```

It deliberately does not cross into the Go API, GraphQL, the registry
repository, PostgreSQL, or telemetry ingestion. A real registered device ID is
a documented operator-supplied precondition; MVP-004 does not verify
registration because doing so would bypass or prematurely implement MVP-005.

## Why

MVP-005 needs a real producer, a stable message contract, and a failure-capable
transport before it can design the untrusted ingestion boundary. A loopback
broker and one-shot simulator are the smallest independently testable slice
that provides those prerequisites without adding a platform consumer or a
generic messaging abstraction.

## Verified Repository Baseline (2026-09-18)

- `feat/mvp-004-mqtt-local-runtime-and-simulator`, local `main`, and
  `origin/main` all pointed to `848b792`; the branch was zero commits
  ahead/behind `main` and the worktree was clean before this plan revision.
- MVP-001 through MVP-003 are merged. The seeded development tenant slug is
  `pulsegrid-dev`; devices have globally unique UUIDs plus tenant-local,
  unrestricted-text device keys. Device keys therefore cannot safely become
  MQTT topic levels because they may contain MQTT separators or wildcard
  characters.
- `compose.yaml` currently contains only pinned PostgreSQL 18.6 development
  and test services. Both publish only to loopback, use distinct profiles and
  ports, and keep destructive volume removal out of routine lifecycle commands.
- The selected local toolchain is Node 24.20.0, pnpm 12.3.4, Go 1.27.1, Docker
  Engine 29.7.2 on Docker Desktop/aarch64, and Docker Compose 5.4.0.
- No MQTT image, client library, simulator command, broker configuration,
  message schema, MQTT environment key, or MQTT validation job exists.
- `corepack pnpm run check:fast` passed on the baseline: Go formatting,
  generated GraphQL drift, modernization, Staticcheck, vet, frontend lint and
  typecheck, OpenAPI lint, browser-fixture typecheck, Go tests, and 44 frontend
  tests. `pnpm audit --prod` reported no production advisories, and
  `go -C apps/api mod verify` passed.
- GitHub branch protection currently requires 13 named contexts with strict
  up-to-date checks. No existing context owns an MQTT runtime, so the broker
  smoke requires a new independent job rather than making API unit, database,
  frontend, or browser jobs depend on it. Making that new context required is
  a separate external branch-protection mutation and needs explicit approval
  after the workflow job exists.
- The official `eclipse-mosquitto:2.1.2-alpine` multi-platform manifest digest
  resolved to
  `sha256:772e7b27d51cf2547a399fffa93ffa0075420fcc1eae84c4b422c72233c3ebb6`.
  Docker Scout found 0 Critical and 2 High findings on both linux/arm64 and
  linux/amd64, both in `cJSON 1.7.19-r1` with no fixed package available. The
  advisories require `cJSONUtils_ApplyPatches*` or `cJSON_Compare`. Review of
  the image-provenance source commit
  `5b74cce8a4fe2a73b57df6c703bfde2cfd535d60` found no reference to those APIs
  anywhere in the source tree. The findings are therefore not affected for the
  pinned artifact, not silently accepted; the image must be rescanned and the
  disposition reopened if its digest, provenance, advisory, or call graph
  changes.

## Scope

1. Add `mqtt-dev` and `mqtt-test` Compose services under the existing `dev`
   and `test` profiles. Both use the same reviewed Mosquitto configuration,
   run without persistent broker storage, and publish distinct ports only on
   `127.0.0.1` (`1883` for development and `11883` for test).
2. Pin the official Eclipse Mosquitto 2.1.2 Alpine image by the reviewed
   multi-platform digest. Do not use a floating tag in the resolved Compose
   model.
3. Configure one MQTT/TCP listener, anonymous local-only access, no dashboard,
   WebSocket, bridge, plugin, TLS, or production-like identity claim,
   `persistence false`, stdout logging, a 16 KiB publish-payload limit, and a
   bounded MQTT-level health check. The container must not receive a host
   directory, Docker socket, privileged mode, added capability, or a named data
   volume. Run as the image's non-root Mosquitto identity with a read-only root
   filesystem and only an explicit tmpfs if runtime evidence proves one is
   required.
4. Add a separate `device-simulator` command process inside the existing Go
   module. Its runtime dependency direction ends at the MQTT client and broker;
   it must not import or call registry, GraphQL, database, HTTP-server, or
   migration code.
5. Add the latest reviewed stable Eclipse Paho MQTT 3.1/3.1.1 Go client,
   `github.com/eclipse/paho.mqtt.golang v1.5.1`, as the simulator's only new
   direct code dependency. Pin it in `go.mod`/`go.sum`, inspect its transitive
   closure, and run the existing Go vulnerability gate after admission.
6. Implement one-shot simulator configuration for development/test only:
   `PULSEGRID_ENV`, `PULSEGRID_MQTT_BROKER_URL`,
   `PULSEGRID_MQTT_TENANT_SLUG`, `PULSEGRID_MQTT_DEVICE_ID`, and
   `PULSEGRID_SIMULATOR_TEMPERATURE_CELSIUS`. Reject production, non-loopback
   hosts, unsupported schemes, userinfo, path/query/fragment, wrong fixed
   tenant slug, malformed UUIDs, and non-finite temperature values.
7. Add explicit root commands for broker development lifecycle, one-shot
   publish, and isolated MQTT integration validation. Stop/down commands must
   target only MQTT services or the unique test Compose project and must not
   remove PostgreSQL volumes.
8. Add unit and real-broker integration evidence, an independent CI job, the
   documentation updates below, post-implementation review, final validation,
   and lifecycle closeout.

## Out of Scope

- Any API subscriber, telemetry validation/acceptance, registry lookup,
  persistence, current-state projection, rules, alerting, console telemetry,
  or application readiness dependency on MQTT.
- Production broker selection/topology, public or non-loopback listeners, TLS,
  production device credentials, credential rotation, ACL policy, certificates,
  secret managers, HA, clustering, bridge configuration, or durable sessions.
- Kafka, AsyncAPI, schema registries, generic event/message-bus abstractions,
  WebSockets, retained current-state messages, offline queues, replay, or
  exactly-once claims.
- Simulator command handling, acknowledgement/result topics, long-running
  reconnect loops, high-volume/device-fleet generation, fault injection, or
  load/performance testing.
- Backend/frontend Dockerfiles, application containers, production deployment,
  or changes to database, GraphQL, OpenAPI, frontend behavior, and browser
  tests.
- Treating a known local password as production security. This plan selects a
  narrower loopback-only anonymous fixture instead of adding a credential
  mechanism that would be usable but not meaningfully secret.

## Dependencies

- MVP-001 supplies tenant/device persistence and UUID identity.
- MVP-002/MVP-003 supply the development-only registration journey from which
  a contributor obtains a real device UUID; their API and UI contracts remain
  unchanged.
- FND-003 supplies root validation, repository policy, GitHub Actions, and the
  single-lockfile/toolchain rules that the new dependency and CI job must keep.
- Docker Engine/Compose is the selected local dependency runtime. The broker
  image is a runtime/delivery dependency; Eclipse Paho is a direct Go runtime
  dependency of the simulator process.

## Architecture / Boundaries

### Runtime and ownership

```text
Operator registers device through existing journey
-> copies opaque device UUID into ignored local simulator config
-> device-simulator constructs one contract-valid message
-> device-simulator publishes over MQTT to loopback broker
-> broker acknowledges QoS 1 delivery to the publisher
-> independent test subscriber observes the message
```

- The simulator owns fixture configuration, topic construction, message ID and
  timestamp generation, JSON serialization, bounded connect/publish lifecycle,
  and human-readable success/failure output.
- Mosquitto owns only local MQTT transport and broker-level acknowledgement.
- The device registry remains authoritative for registration. The simulator
  receives an opaque UUID and cannot query, create, or mutate registry data.
- MVP-005 will own untrusted transport validation, tenant/device resolution,
  duplicate handling, and application acceptance. Broker acknowledgement is
  not application acceptance.
- Code colocation in the existing Go module is a repository/tooling choice,
  not a logical or runtime merge. The simulator runs as a separate command and
  has an explicit no-import boundary from application/domain packages.

### Local/test isolation and trust

- Host-run development clients use `mqtt://127.0.0.1:1883`; isolated tests use
  `mqtt://127.0.0.1:11883`. Container-internal service names are not exposed as
  host configuration.
- Test commands own a unique Compose project, refuse a conflicting test port,
  wait for broker health, and remove only resources they created on success,
  failure, signal, or timeout.
- Anonymous access is accepted only because the listener is published on
  loopback, the simulator rejects non-loopback URLs and production mode, there
  is no persistent broker state, and the fixture carries no secret or private
  production data. This is containment, not authentication.
- A future non-loopback listener, production mode, shared broker, credential,
  TLS requirement, tenant authorization rule, or containerized application
  caller is a security-boundary change and requires re-planning before code
  continues.

### MQTT telemetry v1 contract

- Protocol: MQTT 3.1.1 over TCP.
- Topic:
  `pulsegrid/v1/tenants/{tenantSlug}/devices/{deviceId}/telemetry`.
- For MVP-004, `tenantSlug` is exactly `pulsegrid-dev`; `deviceId` is the
  canonical lowercase UUID returned by the registry. Neither value may contain
  MQTT wildcards or an extra topic level.
- Publish QoS: 1. Duplicate delivery is possible and expected.
- Retain: false. Telemetry is an observation, not broker-owned current state.
- Session: clean session; the one-shot simulator has no durable subscription or
  offline queue. Automatic reconnect is disabled.
- Payload media type: UTF-8 JSON, serialized below 1 KiB; broker rejects
  publish payloads larger than 16 KiB. Unknown/extra fields are not part of v1.

```json
{
  "schemaVersion": 1,
  "messageId": "5edacace-70a7-4a8f-846d-3f4d60c56f3a",
  "observedAt": "2026-09-18T04:00:00Z",
  "temperatureCelsius": 23.5
}
```

- `schemaVersion` is the integer literal `1`.
- `messageId` is a new canonical UUID per logical observation and is the input
  for later duplicate detection; QoS packet identifiers are transport details
  and are not logical telemetry identity.
- `observedAt` is a UTC RFC 3339 timestamp generated when the simulator creates
  the observation. It is device-observed time, not broker or ingestion time.
- `temperatureCelsius` is a finite JSON number. Product acceptance bounds and
  late/out-of-order handling belong to MVP-005/MVP-006, not the broker.
- Topic identifiers and payload are untrusted when MVP-005 consumes them. The
  contract does not grant tenant/device authority.

## Security / Threat Model

Protected assets in this slice are local-host availability, the boundary
between local/test and production configuration, device/tenant identifiers,
diagnostic output, and the Docker host. The intended actors are a local
contributor and the isolated CI harness. A malicious/compromised local process
or container is credible; an internet or production client is outside the
authorized surface and must not gain a listener through this change.

| Credible abuse or failure path | Preventive control | Required evidence | Remaining risk |
| --- | --- | --- | --- |
| A non-local client reaches the development broker | Publish the host port on literal `127.0.0.1`; simulator accepts only the exact environment-specific loopback URL; production mode is rejected | Resolved Compose inspection, host listener inspection, negative configuration tests, and forbidden-path connection attempt | A user with local/Docker-host access can still reach or reconfigure local services; this fixture is not a sandbox |
| A local client spoofs another tenant/device or publishes malformed telemetry | Broker grants no authority; simulator constructs only the fixed topic and validates canonical UUID/tenant; MVP-005 must treat all broker input as hostile | Topic/config tests and boundary review showing no registry/application trust is inferred | Anonymous local clients can publish arbitrary bytes; safe because MVP-004 has no application consumer, and later ingestion must validate independently |
| Cheap input causes memory/CPU exhaustion | Loopback-only exposure, 16 KiB broker payload cap, no dashboard/plugin path, bounded simulator timeouts, and isolated test lifecycle | Oversized-publish rejection, timeout/failure evidence, and broker-log inspection | No per-client rate limit or authenticated quota; required before any shared/non-local environment |
| Broker URL, credentials, or internals leak through output | Userinfo and external URLs rejected; no broker credential exists; errors/logs report safe reason and identifiers without dumping environment values or payload internals | Unit tests and log/output inspection for invalid configuration and connection failure | A local operator can inspect their own process and Docker configuration |
| Known image component flaw is reachable | Exact image digest, no JSON dashboard/security plugin, smallest listener surface, pinned-source reachability review, repeated Scout scans | Source/config call-path disposition for both cJSON findings on amd64/arm64 | If not-affected cannot be proved, image adoption is blocked and the plan must change |
| Cleanup affects PostgreSQL or another developer stack | No broker volume; service-scoped dev commands; unique test Compose project; never use broad `down -v` | Success/failure/signal cleanup tests and resource inventory before/after | Manually issued broad Docker commands remain outside repository safeguards |
| QoS retry creates duplicate logical telemetry | Unique `messageId`; no exactly-once claim; subscriber evidence tolerates transport duplicates only when payload identity is unchanged | Duplicate-aware integration assertion and contract review | Deduplication/acceptance remains unimplemented until MVP-005/MVP-006 |

No secret, authorization decision, protected production data, or remote
listener is introduced. Loopback containment, payload bounding, fail-fast
configuration, least-privilege container settings, and safe diagnostics are
baseline requirements, not deferred hardening.

## Implementation Direction

1. Change status to `In progress` only in the first implementation commit.
   Add the pinned broker services/config and narrow MQTT lifecycle commands;
   verify the fully resolved Compose model before starting containers.
2. Add isolated simulator configuration and pure topic/payload construction
   with table-driven tests before adding network behavior. Keep configuration
   failure messages actionable but free of environment values and URLs.
3. Add Eclipse Paho v1.5.1 and implement a bounded one-shot MQTT 3.1.1 client:
   unique client ID, clean session, 5-second connect timeout, 5-second publish
   timeout, QoS 1, retain false, wait for PUBACK, bounded disconnect, no
   automatic reconnect, and non-zero exit on any unknown outcome.
4. Add development commands and reviewed example values. The device UUID stays
   blank/placeholder in tracked examples and is set only in ignored local
   configuration or the test process environment.
5. Add an isolated integration runner that starts a subscriber before publish,
   exercises the command over the real broker, validates the exact topic and
   JSON shape, proves a later subscriber receives no retained message, checks
   broker restart/recovery, and always performs owned cleanup.
6. Add a separate `mqtt-integration` CI job rather than coupling MQTT to API
   unit/database, frontend, OpenAPI, or browser jobs. Compose/image download
   failure must fail this job explicitly. After the job has passed on the PR,
   request explicit approval to add its exact context to `main` branch
   protection, then read back the rule before calling it authoritative.
7. Review the working-tree-inclusive implementation for contract drift,
   simulator/application boundary, dependency scope, loopback exposure,
   Compose lifecycle, failure handling, logs, and documentation. Resolve
   findings before final validation.
8. Run final validation on the reviewed result, then perform the lifecycle
   closeout. Do not move the plan or update milestone status before the
   implementation, review, required evidence, and accepted merge state exist.

## Validation

### Guarantee-to-evidence map

| Guarantee | Required evidence |
| --- | --- |
| Compose topology is isolated and reproducible | `docker compose config` for dev/test profiles; exact service/profile/port/image/config/health model inspection; development and unique-project test startup/health/stop/down evidence |
| Broker is local-only and ephemeral | Host reachability on the selected loopback ports, negative inspection for wildcard host publication, no named broker volume, restart/recreate behavior, and no retained message after publish |
| Simulator cannot target production/external brokers | Unit tests reject production, non-loopback host, unsupported scheme, userinfo, path/query/fragment, wrong tenant, malformed UUID, and invalid temperature; integration uses only the isolated test endpoint |
| Telemetry v1 is deterministic and versioned | Table-driven topic/payload tests assert exact topic levels, canonical UUIDs, schema version, UTC timestamp, field set, finite temperature, JSON validity, and serialized-size bound |
| Publish semantics are honest | Real subscriber is ready before one-shot publish; simulator receives PUBACK at QoS 1; observed message matches contract; a later subscriber receives no retained message; evidence never claims exactly-once or application acceptance |
| Failure and recovery are bounded | Missing broker, connect timeout, publish timeout, interrupted process, broker stop/restart, occupied test port, and cleanup-on-failure tests produce non-zero actionable outcomes without hanging or deleting unrelated resources |
| Runtime boundary remains external-device shaped | Diff/import review plus tests show the simulator uses no registry, GraphQL, database, HTTP, or migration package and performs no application-side registration check |
| Dependencies are intentional | Image tag+manifest digest/provenance/SBOM inspection, Docker Scout on linux/amd64 and linux/arm64, Paho module/transitive diff, `go mod verify`, `govulncheck`, and rollback/removal path review |
| Existing repository behavior remains stable | Targeted tests during implementation, post-implementation review/fixes, then `corepack pnpm run check`, the new MQTT job, and all 13 currently required GitHub checks on the exact final head; after explicit approval, branch-protection readback proves the MQTT context is also required |

### Gate placement and evidence limits

- Unit tests remain in the ordinary Go test/race/static gates and do not need a
  broker.
- `mqtt-integration` owns real Mosquitto startup, publish/subscribe, retain,
  restart, and cleanup evidence. It is an independent CI job; it becomes a
  required context only after the separately approved branch-protection update
  and readback.
- The full local `check` command includes the isolated MQTT integration command
  before the existing database/browser runtime checks. Each runtime remains
  independently diagnosable.
- Docker health proves the listener can complete a bounded MQTT operation. It
  does not prove the message contract, application ingestion, registration,
  production readiness, or security.
- Re-run the image scan at implementation admission and final validation. The
  two current cJSON High findings are closed as not affected by pinned-source
  evidence showing no vulnerable API reference. Reopen the disposition if the
  digest, provenance source, advisory, or call graph changes. A Critical or
  newly reachable finding, loss of loopback containment, or absence of
  provenance/SBOM is a stop/re-plan condition, not a skipped pass.
- Report passed, failed, skipped, unavailable, and not-run checks separately.
  An unavailable Docker daemon leaves runtime validation incomplete.

## Documentation Updates

During implementation:

- `apps/api/README.md`: document the simulator command, its strict local/test
  configuration, registered-device precondition, MQTT-only boundary, output,
  timeouts, and failure recovery.
- `docs/api/README.md`: change the MQTT telemetry producer/contract state from
  wholly planned to producer fixture implemented/platform consumer planned;
  document the topic, payload, QoS, retain, identity, and non-acceptance rules.
- `docs/project-setup/local-development.md`: add broker start/health/log/stop/
  down and one-shot publish workflows, dev/test ports, port-conflict recovery,
  and safe cleanup without touching PostgreSQL volumes.
- `docs/project-setup/environment-configuration.md`: add the five consumed
  simulator keys, exact environment/loopback validation, test isolation, and
  explicit production rejection; do not introduce unused API broker keys.
- `docs/architecture/system-architecture.md`: update current state only after
  implementation, record the separate simulator process and broker boundary,
  and preserve ingestion/application ownership for MVP-005.
- `docs/architecture/technology-decisions.md`: retain the reviewed broker,
  protocol, QoS, retain, session, dependency, and residual-CVE decision; mark
  implementation state accurately and keep production broker identity/
  topology open.
- `README.md`: after acceptance, report local MQTT runtime/simulator as
  implemented while telemetry ingestion/current state and production identity
  remain planned/deferred.
- Root scripts and the CI workflow become executable project contracts and
  must be reflected in the local validation-command table.

### Final lifecycle closeout (only after implementation and acceptance)

- Record the reviewed final candidate identity and disposition of material
  findings.
- Record focused MQTT evidence, final `corepack pnpm run check`, dependency
  scan/audit results, and every required CI context on the exact head.
- After explicit approval, add `mqtt-integration` to strict `main` branch
  protection and record a readback showing all prior contexts remain required.
- Move this plan from `planned/` to `completed/` and update every inbound link
  in the feature-plan index and roadmap in the same closeout change.
- Mark MVP-004 complete and M2 `In progress`; do not mark M2 complete because
  MVP-005 through MVP-007 remain required.
- Reconcile README/current-state, technology decision, environment, API,
  architecture, and local-development claims with the implemented behavior.
- Re-run repository-policy and link/search checks after the move. No link may
  retain the old planned path or describe the broker/simulator as pending.

## Risks / Open Decisions

No decision-changing design question remains for MVP-004 implementation. One
external approval remains before merge readiness: adding `mqtt-integration` to
GitHub branch protection after the context exists and passes. The remaining
risks are explicit and bounded:

- **Known image findings:** the selected official image currently has two High
  cJSON advisories and no patched package. The vulnerable JSON Patch/compare
  APIs are absent from the exact image-provenance source commit, so the current
  findings are not affected rather than accepted. This is still not a clean
  package scan: re-scan and stop/re-plan if image identity, advisory scope, or
  reachability changes before final acceptance.
- **Development trust only:** loopback-only anonymous access prevents casual
  external exposure but is not authentication or tenant authorization. Any
  widened listener or shared environment requires a new security decision.
- **QoS 1 duplicates:** broker acknowledgement means at-least-once transport to
  the broker, not unique application processing. `messageId` exists so MVP-005
  and MVP-006 can define duplicate handling.
- **Operator-supplied registration identity:** the simulator validates UUID
  syntax, not registry membership. That preserves the external-device boundary
  but means MVP-004 alone cannot prove the device is accepted by PulseGrid.
- **Shared Go module:** the simulator avoids a second module and wider CI/tool
  changes, but colocation could invite accidental imports. Import/diff review
  is a required boundary check; a real independent ownership/deployment need is
  the revisit trigger.
- **Fixed local ports:** deterministic ports make docs and test isolation clear
  but can collide. Commands must fail clearly before startup and never reuse an
  unrelated process.
- **Merge-gate authority:** a workflow job is not a required status check by
  declaration alone. Until the separately approved branch-protection mutation
  is read back, the new MQTT job is evidence but not merge authority; the prior
  13 contexts must remain unchanged.

## Alternatives Considered

- **Do nothing / let MVP-005 create its own fixture:** rejected because the
  producer, contract, and transport failures would be inseparable from
  ingestion, making the next PR broader and its evidence less diagnostic.
- **Use Mosquitto CLI only as the simulator:** rejected because a shell-shaped
  one-off publish is harder to validate, extend for MVP-011, and keep behind a
  typed configuration/contract boundary. The CLI may still serve the bounded
  broker health check.
- **Create a separate Go module/application now:** rejected because it would
  require a second module lifecycle and broaden setup/static/audit/CI work for
  a small development fixture. A separate process inside the existing module
  preserves the runtime boundary with less change amplification.
- **Use MQTT.js in a new pnpm package:** rejected because it adds a second
  simulator toolchain surface and a larger transitive runtime closure while
  the existing Go module and official Eclipse Paho client can satisfy the same
  one-shot requirement and be reused by MVP-005.
- **Use EMQX or a feature-rich broker:** rejected because clustering,
  dashboards, rule engines, and broker-side product features are unnecessary
  for one loopback producer/consumer fixture and add operational/security
  surface.
- **Add tracked username/password fixtures:** rejected because reusable local
  credentials would not provide a meaningful production security model and
  would add generation/injection/logging complexity. Loopback publication,
  production rejection, no persistence, and no sensitive data are the smaller
  honest containment boundary.
- **QoS 0 or retained telemetry:** QoS 0 provides no broker acknowledgement for
  the smoke guarantee. Retained telemetry can make a later subscriber observe
  stale data as if it were a new observation. QoS 1 with `retain=false` fits
  the failure model while making duplicates explicit.

## Engineering Improvement Review

- **Current scope:** exact topic/payload semantics, logical message identity,
  QoS/retain/session decisions, loopback and production-negative controls,
  bounded timeouts, payload cap, isolated test lifecycle, independent CI,
  dependency provenance/vulnerability review, and lifecycle closeout are
  tightly coupled to a trustworthy producer fixture and prevent false success,
  accidental exposure, or an unusable handoff to MVP-005.
- **Future enhancements:** production device identity, TLS/ACL/credential
  lifecycle, broker persistence/HA, command/response behavior, high-volume
  simulation, AsyncAPI, application ingestion, and richer telemetry wait for
  their owning feature or adoption trigger.
- **Scope effect:** the plan remains one broker/simulator PR. It resolves the
  original open decisions and adds required evidence without adding a platform
  consumer, data change, production surface, or second application module.

## Rollback / Containment

- Before merge, revert the PR. The existing API, database, GraphQL, frontend,
  and stored devices are unchanged.
- Stop or remove only `mqtt-dev`/`mqtt-test` or the integration runner's unique
  Compose project. There is no broker volume or migration to recover.
- Remove the simulator command and Paho module entries together; verify the Go
  module and lock/dependency state after removal.
- If the broker is unhealthy or a dependency finding becomes reachable, stop
  the MQTT services and keep MVP-004 incomplete. Existing health, device
  registry, database, and console workflows continue independently.
- Prefer a forward pin to a patched official image with the same verified
  contract. A different broker product, protocol version, topic/payload shape,
  security model, or non-local topology requires re-planning rather than a
  silent substitution.

## Done Criteria

The reviewed final candidate provides isolated `mqtt-dev` and `mqtt-test`
services from a pinned official broker image, a separate one-shot Go simulator,
and the exact MQTT telemetry v1 contract above. Development/test configuration
is fail-fast and loopback-only; production and external endpoints are rejected;
the broker keeps no persistent or retained telemetry state; and the simulator
waits for a bounded QoS 1 acknowledgement without calling any PulseGrid
application boundary.

Unit evidence proves configuration and contract behavior. Real-broker evidence
proves startup/readiness, publish/subscribe, exact topic/payload, QoS 1 outcome,
retain=false, broker restart, failure handling, and owned cleanup. Dependency
evidence records the image digest/provenance/SBOM and current vulnerability
disposition plus the Paho module/transitive audit. The working-tree-inclusive
implementation is reviewed and fixed before the final full local check and all
currently required CI contexts plus the MQTT job pass on the same head. After
explicit approval, branch-protection readback confirms the MQTT context is
required without dropping any prior context.

Documentation accurately distinguishes transport publication from application
acceptance. The completed plan and inbound links are moved together; M2 becomes
`In progress`, not `Complete`; and no production broker, identity, ingestion,
storage, or end-to-end product claim is made.

## Selected Technical References

- [Eclipse Mosquitto 2.1.2 release](https://mosquitto.org/blog/2026/02/version-2-1-2-released/)
  and [official Docker image](https://hub.docker.com/_/eclipse-mosquitto)
- [Eclipse Mosquitto configuration](https://www.mosquitto.org/man/mosquitto-conf-5.html)
- [Eclipse Paho MQTT Go client v1.5.1](https://github.com/eclipse-paho/paho.mqtt.golang/releases/tag/v1.5.1)
- [OASIS MQTT 3.1.1 specification](https://docs.oasis-open.org/mqtt/mqtt/v3.1.1/mqtt-v3.1.1.html)
- [CVE-2026-67215](https://github.com/advisories/GHSA-5q3m-r3x7-8phg)
  and [CVE-2026-67216](https://www.cve.org/CVERecord?id=CVE-2026-67216)
