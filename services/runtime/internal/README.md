# internal

- `backend/`: the `Backend` interface (#1), the stub, and the Ollama and vLLM (#105) adapters over a shared HTTP client. Batching (#2) builds on the interface.
- `service/`: `InferenceService` over a `Backend`, with the error-to-gRPC-code mapping every adapter shares.
