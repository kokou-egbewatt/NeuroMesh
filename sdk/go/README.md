# sdk/go

Go client for the NeuroMesh `InferenceService`. `gen/` holds the stubs `task proto:gen` generates from `proto/`; they are committed, and `task proto:check` fails if they drift.

```go
tlsCfg, err := config.TLS{CertFile: "tls.crt", KeyFile: "tls.key", CAFile: "ca.crt"}.ClientConfig("runtime")
client, err := sdk.NewClient("runtime:50051", sdk.WithTLS(tlsCfg))
```

`NewClient` needs an explicit transport: `WithTLS` (mutual TLS when the config carries a certificate) or `WithInsecure` for a local plaintext run. Unary calls get a 10 s default deadline and up to three attempts on `Unavailable` only; `ResourceExhausted` is never retried. Streams are never retried.
