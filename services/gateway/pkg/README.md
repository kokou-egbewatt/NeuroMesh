# pkg

- `server/`: builds the gateway (HTTPS listener plus the mutual TLS client to the runtime) from its `Config` (`New`, `Listen`, `Serve`). `cmd/server` wires it to config and signals; `test/integration` boots it in-process.
