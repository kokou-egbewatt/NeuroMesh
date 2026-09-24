# dashboard

Operational UI for NeuroMesh: GPU monitoring, traces and lineage search, latency, sessions, agent runs, eval and cost dashboards, topology visualization.

Stack: Next.js, TypeScript, Tailwind, TanStack Query, gRPC-web. Built in Phase 7 (NeuroMesh Cloud); currently a placeholder package so the pnpm workspace resolves. It talks only to `apps/control-plane` and the observability query API, never to data-plane services ([ADR-0002](../../docs/adr/0002-control-plane-data-plane-split.md)).
