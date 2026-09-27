# internal

- `httpapi/`: the public HTTP surface: request IDs, size limits, validation, the JSON error shape, and the gRPC to HTTP status mapping.
- `router/`: resolves which `services/runtime` backend serves a model. `Static` today; round robin (#3), sticky (#29) and KV-cache aware (#50) routers plug in behind the same interface.

Auth (#6) and rate limiting (#7) land here as middleware.
