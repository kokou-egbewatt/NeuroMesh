# pkg

- `server/`: builds and runs the runtime gRPC server from its `Config` (`New`, `Listen`, `Serve`). `cmd/server` wires it to config and signals; `test/integration` boots it in-process.
