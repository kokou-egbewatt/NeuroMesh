# agent-runtime

Durable agent execution: workflows, checkpoints, retries, memory, orchestration. Every agent step is an inference call through `services/gateway` plus tool calls; the runtime adds durability around the existing path rather than a parallel one. The execution engine (Temporal vs. building on the Phase 3 event fabric) is decided by an ADR with a prototype on each option; nothing here presupposes it. (Phase 6)
