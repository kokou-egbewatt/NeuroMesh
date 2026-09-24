<!-- markdownlint-disable MD033 -->

<h1 align="center">NeuroMesh</h1>

<p align="center">
  <i>AI runtime operating system for realtime and distributed GenAI: inference routing, streaming, GPU scheduling, observability, evaluation and durable agents on one platform.</i><br><br>

<!-- CI Badges -->

<a href="https://github.com/kokou-egbewatt/NeuroMesh/actions/workflows/ci.yml">
    <img src="https://github.com/kokou-egbewatt/NeuroMesh/actions/workflows/ci.yml/badge.svg" alt="CI">
  </a>

<a href="https://github.com/kokou-egbewatt/NeuroMesh/actions/workflows/docs.yml">
    <img src="https://github.com/kokou-egbewatt/NeuroMesh/actions/workflows/docs.yml/badge.svg" alt="Docs">
  </a>

<a href="https://github.com/kokou-egbewatt/NeuroMesh/actions/workflows/images.yml">
    <img src="https://github.com/kokou-egbewatt/NeuroMesh/actions/workflows/images.yml/badge.svg" alt="Images">
  </a>

<!-- License Badge -->

<a href="https://github.com/kokou-egbewatt/NeuroMesh/blob/main/LICENSE">
    <img src="https://img.shields.io/badge/license-MIT-blue" alt="License: MIT">
  </a>
</p>

## About

**NeuroMesh** is a Kubernetes-native runtime mesh for AI workloads: one platform to deploy a model, route traffic to it, stream responses, observe and evaluate what it does, autoscale GPUs, run agents reliably, and replay failures. Think Kubernetes plus Datadog plus Envoy plus Temporal, for inference and agents.

It is built in seven phases, each a GitHub milestone:

| Phase | Component | Problem solved |
| --- | --- | --- |
| 1 | [NeuroMesh Runtime](https://github.com/kokou-egbewatt/NeuroMesh/milestone/1) | Unified inference gateway |
| 2 | [NeuroMesh Stream](https://github.com/kokou-egbewatt/NeuroMesh/milestone/2) | Realtime AI streaming |
| 3 | [NeuroMesh Event Fabric](https://github.com/kokou-egbewatt/NeuroMesh/milestone/3) | Inference overload and reliability |
| 4 | [NeuroMesh Scheduler](https://github.com/kokou-egbewatt/NeuroMesh/milestone/4) | GPU waste |
| 5 | [NeuroMesh Insight](https://github.com/kokou-egbewatt/NeuroMesh/milestone/5) | Debugging AI systems |
| 6 | [NeuroMesh Agents](https://github.com/kokou-egbewatt/NeuroMesh/milestone/6) | Unreliable, stateless agents |
| 7 | [NeuroMesh Cloud](https://github.com/kokou-egbewatt/NeuroMesh/milestone/7) | Operating AI infra |

## Features

- 🔐 TLS on every hop: HTTPS at the gateway edge, mutual TLS from gateway to runtime with a caller allow-list on the certificate common name
- 🪪 Certificates in the cert-manager Secret layout (`tls.crt`, `tls.key`, `ca.crt`): cert-manager mints them on a cluster, `task certs:dev` outside one
- 🚦 A data-plane gateway that validates and bounds every request and answers in one JSON error shape, never leaking upstream detail
- 🧭 Control plane and data plane split by rule: the gateway never calls a management service in the request path ([ADR-0002](docs/adr/0002-control-plane-data-plane-split.md))
- 📜 One gRPC contract in `proto/`, linted by buf, with committed generated code checked for drift
- 🧩 A polyglot monorepo where every Go module builds on its own ([ADR-0001](docs/adr/0001-monorepo-structure-and-phasing.md))
- 🐳 Distroless images running as uid 65532, smoke-tested end to end over TLS
- ⚙️ CI as thin wrappers over `task ci:*`: GitHub-hosted runners, act and a terminal run the same commands
- 📝 Docs gates: every relative Markdown link resolves, the changelog is structurally sound, and code changes carry a version

## Getting Started

Prerequisites: Go 1.26+, Node 22.13+, [pnpm](https://pnpm.io), [go-task](https://taskfile.dev), Docker. On Windows, run `task` from Git Bash.

```sh
task tools:install    # pinned task, buf, protoc plugins, golangci-lint, air, lefthook
task doctor           # what this machine is missing
task hooks:install    # fast gates before commit and push
task dev              # runtime and gateway with live reload, over TLS
task call:route       # POST /v1/route through the gateway
task ci               # every CI job, locally
```

The full loop (debugging, tests, hooks, releases) is in [Dev loop](docs/onboarding/dev-loop.md).

## 📚 Documentation

- 📐 [Architecture Overview](docs/architecture/overview.md): phase roadmap, service responsibility map, local substrate
- 🔁 [Dev loop](docs/onboarding/dev-loop.md): live reload, calling the stack, debugging, tests, hooks, releases
- 🧪 [Local CI](docs/onboarding/local-ci.md): running the workflows with act, and each gate on its own
- 🧾 [ADRs](docs/adr/) and [RFCs](docs/rfc/): every decision, with the alternatives that lost
- 📝 [Changelog](CHANGELOG.md): release notes and version history
- 🗂️ [Backlog](https://github.com/kokou-egbewatt/NeuroMesh/issues): one milestone per phase, dependencies by issue number

### Quick Links

- [🚦 Gateway](services/gateway/README.md)
- [🧠 Runtime](services/runtime/README.md)
- [📦 Go SDK](sdk/go/README.md)
- [📜 Proto contract](proto/README.md)
- [📜 RFC-0001: NeuroMesh Runtime](docs/rfc/0001-neuromesh-runtime.md)
- [🧭 ADR-0002: Control plane and data plane split](docs/adr/0002-control-plane-data-plane-split.md)
- [🖥️ Local environment](deployments/local/README.md)
- [📊 Observability stack](observability/victoria-metrics/README.md)

## Repository Layout

| Path | Holds |
| --- | --- |
| `apps/` | Operator surfaces: control plane API, dashboard, CLI, admin console |
| `services/` | Platform services: runtime, gateway, and the later-phase services |
| `packages/` | Shared Go libraries (config and TLS, logging, middleware, utils) |
| `sdk/` | Client SDKs (Go today; Python and TypeScript in Phase 7) |
| `proto/` | gRPC contracts, source of truth for every SDK |
| `infra/`, `deployments/`, `observability/` | Charts, environments, the observability stack |
| `benchmarks/`, `test/` | Benchmarks, integration, chaos and load tests |
| `ci/`, `scripts/` | CI gate scripts and the pinned tool installer |
| `docs/` | Architecture, ADRs, RFCs, runbooks, onboarding |

______________________________________________________________________

© 2026 Kokou M. Egbewatt. Released under the [MIT License](LICENSE).
