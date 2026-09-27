# ADR-0001: Single polyglot monorepo, organized by delivery phase

- Status: Accepted
- Date: 2026-07-03
- Authors: Kokou Egbewatt

## Context

NeuroMesh spans four languages (Go for control plane/streaming/scheduling, Python for GPU workers, TypeScript for the dashboard/CLI-adjacent tooling, Rust for a future SDK), seven delivery phases (Runtime, Stream, Event Fabric, Scheduler, Insight, Agents, Cloud), and a large number of services that will need to evolve together (shared proto contracts, shared auth/tracing/logging packages, cross-service API changes).

We need to decide how source is organized before writing the first service: one repository for everything, or one repository per service/phase.

## Decision

Use a single monorepo (`neuromesh/`) containing all apps, services, packages, SDKs, infra, and docs, organized primarily by *architectural role* (`apps/`, `services/`, `packages/`, `sdk/`, `infra/`, ...) rather than by phase. Phase is tracked in documentation (the roadmap table in `docs/architecture/overview.md`) and in delivery order, not in directory structure. A service like `services/scheduler` lives at the same level whether it's a Phase 4 stub or fully built out.

Go modules are tied together with a root `go.work`; TypeScript packages with a root `pnpm-workspace.yaml`. Each service still gets a self-contained module (`go.mod`, `Dockerfile`, `Taskfile.yml`, own `README.md`) so it can, in principle, be extracted to its own repository later without restructuring.

## Alternatives considered

- **Polyrepo (one repo per service)**: better ownership isolation and independent CI, but this project is a single team building interdependent services and shared proto contracts. Cross-service changes (e.g., updating `proto/` and every consumer) would require coordinated multi-repo PRs from day one, which is pure overhead at this stage.
- **One repo per phase**: would mirror the roadmap but fights against the fact that later-phase services (e.g., `session-manager`, Phase 2) are consumed by earlier-phase services (`gateway`, Phase 1) as soon as they exist. Directory structure would need to be reshuffled as phases complete.

## Consequences

- A single `go.work` and `pnpm-workspace.yaml` must be kept in sync as new modules/packages are added. This is mechanical but low-cost.
- CI (`\.github/workflows/ci.yml`) builds/tests the whole Go workspace and the whole pnpm workspace on every push rather than per-service; this is acceptable at current scale and can be split into path-filtered jobs later if build times become a problem.
- Extracting any service to its own repository later is possible without renaming or restructuring, since each service is already self-contained.
