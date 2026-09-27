# sdk/typescript

TypeScript client SDK for NeuroMesh, generated from `proto/` in Phase 7: inference calls go to `services/gateway`, management calls to `apps/control-plane`, never blurred into one endpoint. `apps/dashboard` uses only the control-plane and observability clients ([ADR-0002](../../docs/adr/0002-control-plane-data-plane-split.md)). Placeholder package so the pnpm workspace resolves.
