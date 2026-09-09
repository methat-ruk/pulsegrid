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
| Fiber v3 | Selected — Foundation | HTTP runtime for the initial Go application |
| Vue 3 and Nuxt 4 | Selected — Foundation | Web console framework using the Nuxt 4 `app/` directory structure |
| TypeScript | Selected — Foundation | Frontend static typing |
| pnpm workspace | Selected — Foundation | One root lockfile and explicit workspace for JavaScript tooling and the Nuxt application |
| Tailwind CSS v4 and Nuxt UI | Selected — Foundation | UI implementation foundation governed by the UI design system |
| Iconify | Selected — Foundation | Consistent icon source for the console |
| Apache ECharts | Selected — MVP | Added when the telemetry console has a concrete chart requirement |
| GraphQL and gqlgen | Selected — MVP | Concrete device and operator API boundary |
| PostgreSQL | Selected — MVP | Transactional authority and bounded MVP telemetry/state storage |
| MQTT | Selected — MVP | Device telemetry and command transport required by the product loop |
| Docker Compose | Selected — MVP | Local PostgreSQL and MQTT dependencies when those features begin |
| Backend/frontend Dockerfiles | Conditional target | Add when a containerized run, CI, or deployment target will build and exercise the images |
| MongoDB | Conditional target | Adopt only when heterogeneous profile data and queries justify separate authority |
| Dedicated time-series storage | Open | Select from measured volume, retention, aggregation, and query patterns |
| Redis | Conditional target | Adopt for a concrete ephemeral, cache, coordination, or idempotency use case |
| Apache Kafka | Conditional target | Adopt for a concrete durable fan-out, replay, or independent-consumer flow |
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

- Nuxt rendering mode for the authenticated operations console.
- GraphQL client and client-cache policy.
- Polling, GraphQL subscriptions, SSE, or WebSocket transport for live UI state.
- Production identity provider and RBAC model.
- MQTT broker product, QoS policy, session behavior, and topic namespace.
- Kafka topic, partition, schema-registry, retry, and replay design.
- Production database topology and tenant-isolation strategy.
- Telemetry retention period and specialized storage engine.
- Observability backend, SLOs, and exact metric names.
- Kubernetes cluster and stateful-service topology.

Each decision should be made in the first feature plan whose acceptance criteria
depend on it. Decisions that are expensive to reverse should receive a durable
decision record at that time rather than a speculative record now.

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

- [System architecture](system-architecture.md)
- [Product scope](../product/product-scope.md)
- [Roadmap](../roadmap/roadmap.md)
