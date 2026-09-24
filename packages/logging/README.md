# logging

`logging.New(w, service, level)` returns the JSON `slog` logger every service uses: one record per line on stdout, tagged with `service`, level from `log_level` (`debug`, `info`, `warn`, `error`). `forbidigo` in `.golangci.yml` rejects the standard `log` package outside `tools/`.
