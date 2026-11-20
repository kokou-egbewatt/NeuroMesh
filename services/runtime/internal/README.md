# internal

- `admit/`: the cap on requests in flight and the bounded queue in front of a `Backend` (#2), itself a `Backend`.
- `backend/`: the `Backend` interface (#1), the stub, and the Ollama and vLLM (#105) adapters over a shared HTTP client.
- `service/`: `InferenceService` over a `Backend`, with the error-to-gRPC-code mapping every adapter shares.
