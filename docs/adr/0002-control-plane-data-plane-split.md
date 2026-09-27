# ADR-0002: Explicit control-plane / data-plane split

- Status: Accepted
- Date: 2026-07-03
- Authors: Kokou Egbewatt

## Context

`apps/control-plane` and `services/gateway` currently look identical from their directory names and descriptions alone: both mention "routing," both sit between users and the rest of the system. `apps/control-plane/README.md` even lists "inference routing" as one of its responsibilities, which overlaps with what `services/gateway` actually does at request time. As more services and contributors are added, this ambiguity will get worse, not better, and the likely failure mode is someone adding a slow, deploy-time code path (a database write, a policy check against an external system) into the gateway's hot path because "control-plane" and "gateway" were never clearly separated.

This is the same distinction Envoy/Istio make between the control plane (Istio's Pilot/istiod: computes desired configuration) and the data plane (Envoy: proxies live traffic using that configuration, never calls back into the control plane synchronously per request). NeuroMesh should adopt the same split, explicitly, before more code is written against either service.

## Decision

`services/gateway` is the **data plane**. It sits in the hot path of every inference request and its only job is to get that request to the right backend and back as fast as possible: request-time auth token validation (against locally cached keys/policies), rate limit and quota enforcement (against locally cached counters/limits), routing (via `internal/router`), and streaming session proxying. The gateway never makes a synchronous call to `apps/control-plane` while handling a request. Anything it needs at request time is read from a local cache, not fetched live.

`apps/control-plane` is the **control plane**. It owns everything that is slow, infrequent, or stateful in a management sense: model deployment, tenant and RBAC configuration, routing policy configuration (which model versions exist, canary weights, per-tenant limits), and session bookkeeping metadata. It is the API the dashboard and CLI talk to. It computes desired state; it does not proxy inference traffic itself.

Configuration flows one direction: control-plane produces it, gateway (and eventually runtime, scheduler) consumes it, asynchronously. The exact distribution mechanism (polling a shared store, an internal push API, etc.) is left to a follow-up ADR once `apps/control-plane` is actually built; this ADR only fixes the boundary and the rule that the gateway must not call it synchronously per request.

## Alternatives considered

- **Merge gateway and control-plane into one service**: rejected. It mixes latency-sensitive hot-path code with slower management APIs (deploy workflows, tenancy CRUD), and the two need to scale independently: the data plane scales with inference traffic volume, the control plane scales with admin operation volume, which is orders of magnitude lower.
- **Leave the boundary implicit**: rejected, since it is exactly what caused the ambiguity this ADR is fixing. An unwritten rule gets violated the first time someone is in a hurry.

## Consequences

- `apps/control-plane/README.md` and `services/gateway/README.md` are updated to state this split explicitly and no longer list "inference routing" as a control-plane responsibility.
- `docs/architecture/overview.md`'s service responsibility map is updated to match.
- Code review rule going forward: any PR that adds a synchronous call from `services/gateway` to `apps/control-plane` (or any other management-plane service) in a request-handling path should be rejected or redesigned to read from a cache instead.
- Follow-up ADR needed once `apps/control-plane` exists: how configuration actually gets from control-plane to gateway (push vs. poll, shared store vs. streaming config API).
