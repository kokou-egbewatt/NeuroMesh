# Dev loop

Everything here runs locally. On Windows, run `task` from Git Bash. How the pieces fit together is in [How the tooling works](tooling.md).

## First time

```sh
task tools:install    # pinned task, buf, protoc plugins, golangci-lint, air, lefthook
task doctor           # what is missing on this machine, and what to run for each gap
task hooks:install    # git hooks: fast gates before commit and push
task certs:trust      # Windows only, optional: trust the dev CA in browsers and curl
```

## Every day

```sh
task dev              # runtime and gateway with live reload, over TLS
task call:route       # POST /v1/route through the gateway
task call:route -- '{"model":"phi3","prompt":"hi"}'
task call:runtime     # the runtime directly, over mutual TLS, as the gateway
task call:health
```

`task dev` mints certificates into `certs/dev` on first run, then runs both services under [air](https://github.com/air-verse/air). A change in the service, in `packages/` or in `sdk/go` rebuilds and restarts it within a few seconds. Logs are text locally (`log_format: text` in `services/*/configs/config.yaml`); every other config logs JSON for the collector.

The services read `-config` (or `CONFIG_PATH`), and relative paths inside a config resolve against the config file's own directory, so they run the same from the repository root, the service directory or a debugger.

## Debugging

`.vscode/launch.json` has a configuration for each service, one for the test package of the open file, and a `runtime + gateway` compound that mints certificates first. Install the recommended extensions when VS Code offers them.

## Tests

| Task | Runs |
| --- | --- |
| `task test:unit` | Everything except `test/integration`, in seconds |
| `task go:test` | Everything, with merged coverage in `coverage.out` |
| `task go:test:docker` | Everything in a Linux container with `-race`, the way CI runs it |
| `task test:golden` | Rewrites `test/integration/testdata` after an intended response change |
| `task cover:html` | `go:test`, then `coverage.html` |

Windows without gcc cannot run the race detector, so `task go:test` warns and runs without it; `task go:test:docker` is the one to trust before a push that touches concurrency.

## Formatting and hooks

`task go:fmt` applies gofumpt and goimports. `task go:lint` fails on unformatted code, and the pre-commit hook checks staged Go files before they land. The hooks in `lefthook.yml` run in seconds:

- **pre-commit**: line endings, Go formatting, doc links, changelog integrity, buf lint (each only when matching files are staged).
- **pre-push**: the version gate and the unit tests.

`git commit --no-verify` skips them when you mean it; CI runs the full versions regardless.

## Versions and docs

```sh
task release:next -- minor "Title"   # next CHANGELOG version plus package.json, together
task adr:new -- "Title"              # docs/adr/NNNN-title.md in the house format
task rfc:new -- "Title"              # docs/rfc/NNNN-title.md
```

Any change to code, contracts, infrastructure or CI needs a new changelog version; `task ci:version-gate` and the pre-push hook enforce it.
