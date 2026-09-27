# control-plane

Management API: model deployment, tenancy and RBAC configuration, routing policy configuration (model versions, canary weights, per-tenant limits), and session bookkeeping metadata. Stack: Go, gRPC, ConnectRPC, Kubernetes client-go.

This is the control plane, not the data plane. It computes desired state; it does not proxy inference traffic itself, `services/gateway` does that. See [ADR-0002](../../docs/adr/0002-control-plane-data-plane-split.md) for the split and the rule that `services/gateway` must not call this service synchronously while handling a request.
