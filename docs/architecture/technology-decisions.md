# PulseGrid Technology Decisions

Status: Planning source of truth

## Purpose

This document records whether a technology is selected for the immediate
delivery path, retained as a conditional target, or still open. It prevents a
technology named in the project vision from being mistaken for an implemented
or immediately required dependency.

## Decision states

- **Selected — Foundation**: expected in the executable repository foundation.
- **Selected — MVP**: introduced by a concrete MVP feature plan.
- **Conditional target**: remains part of the project direction but requires a
  stated adoption trigger.
- **Open**: the project has not selected an exact product, topology, or
  mechanism.

## Decision register

| Technology or approach | State | Current rationale or adoption trigger |
| --- | --- | --- |
| Go 1.27 | Selected — Foundation | Current supported Go release for the initial module; pin the toolchain in `go.mod` |
| goenv | Selected — Foundation | Developer tool that follows the repository `.go-version`; runtime builds remain pinned by the Go module |
| Node.js 24 LTS | Selected — Foundation | Frontend runtime selected by `.node-version` and the root `engines.node` range |
| Corepack with pnpm 12 | Selected — Foundation | Root `packageManager` pins one package-manager version for the JavaScript workspace |
| Fiber v3 | Selected — Foundation | HTTP runtime for the initial Go application |
| REST and OpenAPI 3.1.0 | Selected — Foundation | Operational HTTP contract for health and future operational endpoints; do not create domain REST CRUD without a concrete consumer |
| Redocly CLI | Selected — Foundation | Lint, bundle, and render OpenAPI contracts in the repository workflow once the pnpm/CI foundation exists |
| Vue 3 and Nuxt 4 | Selected — Foundation | Web console framework using the Nuxt 4 `app/` directory structure |
| TypeScript | Selected — Foundation | Frontend static typing |
| pnpm workspace | Selected — Foundation | One root lockfile and explicit workspace for JavaScript tooling and the Nuxt application |
| Tailwind CSS v4 and Nuxt UI | Selected — Foundation | UI implementation foundation governed by the UI design system |
| Iconify | Selected — Foundation | Consistent icon source for the console |
| Nuxt rendering mode | Selected — Foundation | Nuxt default SSR remains the simplest initial runtime; revisit only when authentication or a proven browser-only dependency requires it |
| Nuxt ESLint flat config | Selected — Foundation | Application-owned linting and stylistic checks without a second formatter |
| Vitest and Nuxt Test Utils | Selected — Foundation | Focused unit and Nuxt component/runtime evidence before repository-wide CI composition |
| Manrope and Noto Sans Thai delivery | Selected — Foundation | Package-managed local font assets avoid third-party font requests |
| Light theme | Selected — Foundation | Only approved PulseGrid light tokens exist; dark-theme work remains deferred |
| Native browser `fetch` with a feature-scoped typed GraphQL client | Selected — MVP | MVP-003 needs one bounded device journey; avoid a cache/SSR/client-runtime dependency until shared cache, polling, or schema-scale evidence justifies it |
| Apache ECharts | Selected — MVP | Added when the telemetry console has a concrete chart requirement |
| GraphQL and gqlgen | Selected — MVP | Concrete device and operator API boundary; MVP-002 pins gqlgen `v0.17.95` and keeps the SDL as source of truth |
| PostgreSQL 18.6 | Selected — MVP | Transactional authority for the MVP-001 organization/device registry; local and CI targets are pinned and isolated |
| pgx v5 | Selected — MVP | Direct parameterized Go PostgreSQL driver and bounded pool for the registry boundary |
| Goose v3 SQL migrations | Selected — MVP | Explicit versioned SQL migrations with session locking; migration execution remains outside API startup |
| MQTT 3.1.1 | Selected — MVP | MVP-004 selects QoS 1, non-retained telemetry over loopback TCP for the first producer fixture; duplicate and untrusted-input handling remain with MVP-005/MVP-006 |
| Eclipse Mosquitto 2.1.2 | Selected — MVP | Reviewed for the MVP-004 local/test broker and pinned by multi-platform image digest; production broker product/topology remains open |
| Eclipse Paho MQTT Go client v1.5.1 | Selected — MVP | Reviewed for the separate MVP-004 simulator command and reusable by the later Go ingestion boundary without a generic messaging abstraction |
| Docker Compose | Selected — MVP | Local PostgreSQL and the reviewed loopback-only MQTT development/test services are implemented as isolated dependency runtimes |
| Backend/frontend Dockerfiles | Conditional target | Add when a containerized run, CI, or deployment target will build and exercise the images |
| MongoDB | Conditional target | Adopt only when heterogeneous profile data and queries justify separate authority |
| Dedicated time-series storage | Open | Select from measured volume, retention, aggregation, and query patterns |
| Redis | Conditional target | Adopt for a concrete ephemeral, cache, coordination, or idempotency use case |
| Apache Kafka | Conditional target | Adopt for a concrete durable fan-out, replay, or independent-consumer flow |
| AsyncAPI | Conditional target | Introduce with the first concrete MQTT or Kafka producer/consumer message flow |
| Kafka consumer groups | Conditional target | Introduce with a Kafka workload that requires parallel consumption |
| gRPC and Protocol Buffers | Conditional target | Adopt when independently deployed services need a synchronous typed contract |
| OpenTelemetry | Conditional target | Adopt when cross-process request/event diagnosis is required |
| Prometheus and Grafana | Conditional target | Adopt when an operated runtime has metrics and dashboard questions to answer |
| Kubernetes and Helm | Conditional target | Adopt when deployment, availability, or scaling requirements justify orchestration |
| KEDA | Conditional target | Adopt only after Kafka lag is an established scaling signal |
| GitHub Actions | Selected — Foundation | Authoritative repository validation boundary |
| Husky and lint-staged | Selected — Foundation | Fast staged-file feedback after frontend/repository files exist |

## Repository layout decision

Foundation uses application-owned roots inside `apps/`:

```text
apps/
├── api/          # Go module and Fiber application
└── web-console/  # Nuxt application and pnpm workspace package
```

The Go module path is
`github.com/methat-ruk/pulsegrid/apps/api`. The repository does not add
`services/`, `workers/`, shared `packages/`, or `go.work` until a second real
consumer or independently owned runtime requires one.

## Decisions intentionally left open

- Polling, GraphQL subscriptions, SSE, or WebSocket transport for live UI state.
- Production identity provider and RBAC model.
- Production MQTT broker topology, device identity/credential lifecycle, TLS,
  authorization, and durable-session policy. MVP-004 owns only the reviewed
  loopback local/test Mosquitto fixture and telemetry-v1 contract.
- Kafka topic, partition, schema-registry, retry, and replay design.
- Production database topology and tenant-isolation strategy.
- Telemetry retention period and specialized storage engine.
- Observability backend, SLOs, and exact metric names.
- Kubernetes cluster and stateful-service topology.

Each decision should be made in the first feature plan whose acceptance criteria
depend on it. Decisions that are expensive to reverse should receive a durable
decision record at that time rather than a speculative record now.

## MVP-004 local MQTT decision

MVP-004 implements an intentionally non-production transport fixture:

- official `eclipse-mosquitto:2.1.2-alpine`, pinned by multi-platform digest
  `sha256:772e7b27d51cf2547a399fffa93ffa0075420fcc1eae84c4b422c72233c3ebb6`;
- separate development/test Compose services published only on loopback, with
  no broker persistence, retained telemetry, dashboard, plugin, bridge, TLS,
  or shared credentials;
- MQTT 3.1.1, QoS 1, `retain=false`, clean simulator sessions, and the
  versioned topic/payload contract owned by the
  [MVP-004 plan](../roadmap/feature-plans/planned/MVP-004-mqtt-local-runtime-and-simulator.md);
- Eclipse Paho MQTT Go client v1.5.1 inside a separate simulator command in the
  existing Go module, with no registry, GraphQL, database, or HTTP dependency.

Mosquitto is preferred over a feature-rich broker because the current job is a
single local producer/consumer fixture, not broker-side rules, dashboards,
clustering, or production operations. A separate Go module and MQTT.js package
were rejected because they add toolchain and validation surfaces without
improving the current runtime boundary.

The image review on 2026-09-18 found provenance/SBOM attestations plus
0 Critical and 2 High Docker Scout findings on linux/amd64 and linux/arm64,
both from
`cJSON 1.7.19-r1` with no fixed package. The advisories require JSON Patch/apply
or JSON comparison call paths. Review of image-provenance source commit
`5b74cce8a4fe2a73b57df6c703bfde2cfd535d60` found no reference to
`cJSONUtils_ApplyPatches*`, `cJSON_Compare`, or `cJSON_Utils` anywhere in the
source tree. The findings are therefore not affected for this exact artifact,
not an accepted security exception. Implementation and final validation must
re-scan, reopen the disposition if digest/provenance/advisory/call graph
changes, and stop if a finding becomes Critical/reachable or loopback
containment changes.

The implementation-admission re-scan on 2026-09-18 reproduced the same result
on linux/arm64 and linux/amd64: 0 Critical, 2 High cJSON findings, with no
fixed package. The pinned image and source call-path disposition therefore
remain unchanged and must be revisited if the artifact or advisory changes.

Revisit the selection when production identity/TLS/authorization is planned, a
non-loopback or shared environment is required, command delivery proves
different session/delivery needs, a patched image changes the risk decision,
or broker operations become an owned production capability. The feature plan
and current repository state own the implementation status; this record
preserves the selected boundary and its revisit triggers.

## Foundation selection evidence

- [Go release history](https://go.dev/doc/devel/release) identifies Go 1.27 as
  the current supported release line selected for FND-001.
- [Fiber documentation](https://docs.gofiber.io/) documents the v3 module and
  its Go version requirement.
- [Nuxt 4 directory structure](https://nuxt.com/docs/4.x/directory-structure)
  defines the `app/` application root selected for FND-002.
- [pnpm workspace documentation](https://pnpm.io/workspaces) defines the root
  `pnpm-workspace.yaml` contract selected for the JavaScript workspace.

## Adoption rules

Technology is introduced only when:

1. a current product, reliability, security, scale, or operational requirement
   names the need;
2. the responsible boundary and owner are clear;
3. failure and recovery behavior are defined;
4. the feature plan contains proportional validation;
5. the dependency and operating cost are justified against a simpler option.

## Related documents

- [API documentation](../api/README.md)
- [System architecture](system-architecture.md)
- [Product scope](../product/product-scope.md)
- [Roadmap](../roadmap/roadmap.md)
