# proto

gRPC service definitions: the source of truth for every SDK under `sdk/`. Linted with buf's STANDARD rules (`buf.yaml` at the repository root lists the one legacy exception for `runtime.proto`).

```sh
task proto:gen       # buf generate into sdk/go/gen (committed)
task proto:check     # lint, and fail if sdk/go/gen drifted from proto/
task proto:breaking  # fail on a breaking change against origin/main
```

Plugin versions are pinned in `scripts/install-tools.sh`; run `task tools:install` before generating.
