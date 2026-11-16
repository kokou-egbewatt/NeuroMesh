# Changelog

All notable changes to NeuroMesh are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).
Every change to code, contracts, infrastructure or CI adds a version here; `task ci:version-gate`
enforces it and `task ci:changelog` checks the file's structure.

## [Unreleased]

## [0.8.0] - Logs and Traces on the Local Cluster (2026-11-16)

**The rest of the observability profile: every pod's logs reach VictoriaLogs through the collector,
traces reach Tempo, and only the collector gateway may write to either.**
[#22](https://github.com/kokou-egbewatt/NeuroMesh/issues/22) stays open for its end-to-end smoke
test (one request's metric, trace and logs), which needs the services' instrumentation in #10.

### Added

- **`infra/helm/victoria-logs`:** VictoriaLogs 1.53 single, 7 days or 4 GiB on disk, with a Grafana
  datasource and `https://victorialogs.neuromesh.localhost/select/vmui/`.
- **`infra/helm/tempo`:** Tempo 2.9 single binary on local disk, 72 hours, with a Grafana datasource
  that links a span to its logs by `trace_id`.
- **`infra/helm/otel-collector`:** the contrib collector as an agent DaemonSet that reads
  `/var/log/pods` and parses the services' JSON, and a gateway that copies the Kubernetes names onto
  each record, redacts to the allow-list, and is the only writer to the stores.
- **`docs/observability/fields.yaml`:** the field allow-list, holding only fields the code emits
  today; the gateway's `allowed_keys` is rendered from its chart copy, and `task ci:charts` fails on
  drift.
- **NetworkPolicy** for VictoriaLogs, Tempo and the collector gateway: the stores admit the gateway as
  their only writer, plus Grafana and vmagent, and the gateway admits the agent and `neuromesh`.
- **`task obs:verify`:** a request's log line reaches VictoriaLogs redacted, a synthetic trace
  reaches Tempo, the stores refuse a writer in another namespace, and all three Grafana datasources
  are healthy.

### Changed

- **Grafana** installs the VictoriaLogs datasource plugin, pinned at 0.33.1.
- **`task obs:up`** installs the logs and traces half after the metrics half, and restarts the
  collectors so a changed pipeline loads; `task obs:access` lists the logs UI.
- **`task ci:charts`** reads every dependency repository of a chart, not only the first.
- **`docs/onboarding/local-cluster.md`** has the whole profile's footprint (530m and 1483 MiB
  requested, about 133m and 1695 MiB in use), the retention of each store, and the few seconds
  before NetworkPolicy recognises a new pod.

## [0.7.0] - The Services on the Local Cluster (2026-11-16)

**runtime and gateway run on the local cluster through Helm charts, reachable at
`https://gateway.neuromesh.localhost`, and the cluster has a baseline.**
[#14](https://github.com/kokou-egbewatt/NeuroMesh/issues/14)

### Added

- **`infra/helm/runtime` and `infra/helm/gateway`:** two runtime replicas behind a headless Service and
  one gateway, certificates from Secrets, grpc and HTTPS probes, measured requests, restricted Pod
  Security in a `neuromesh` namespace, a `VMServiceScrape` off until there are metrics (#61), and an
  HTTPRoute with a `BackendTLSPolicy` so the edge re-encrypts to the gateway.
- **`task svc:up|down|status` and `task image:runtime|gateway`** (`scripts/services.sh`): images built
  with Docker and imported into k3s's containerd, tagged with the commit; no registry.
- **`task test:cluster`:** the golden request through the cluster-deployed gateway, against the
  in-process fixture (`cluster` build tag).
- **`task bench:cluster`:** the driver as a Job inside the cluster (`benchmarks/inference/Dockerfile`),
  two runs and the 10% repeatability check. `bench -target` points the driver at any running
  gateway, and `BASELINE.md` has the cluster section.
- **`task ci:charts`** and a CI job: every chart lints, renders and validates with kubeconform. Helm
  4.3.1 and kubeconform 0.8.0 are pinned in `scripts/install-tools.sh`.

### Changed

- **The runtime serves `grpc.health.v1` in plaintext on `health_addr`** (`:8081` in the image) as well,
  for kubelet's grpc probe, which can't present a client certificate. It answers nothing else and
  reports NOT_SERVING while the runtime drains.
- **The edge keeps a client's `X-Request-Id`** (`PreserveOrGenerate`) instead of replacing it.
- **`docs/onboarding/local-cluster.md`** documents the services, and no longer lists a registry or a
  relay as missing.

## [0.6.0] - The Local Edge (2026-11-16)

**Every UI on the local cluster is `https://<name>.neuromesh.localhost` from Windows, with no
port-forward, relay or hosts file.** Settles the ingress question in
[#23](https://github.com/kokou-egbewatt/NeuroMesh/issues/23) (Envoy Gateway) and the Windows relay
in [#21](https://github.com/kokou-egbewatt/NeuroMesh/issues/21), which turned out not to be needed.

### Added

- **`task edge:up|down|status`** (`scripts/edge.sh`): Envoy Gateway v1.9.3 with the Gateway API, and
  `infra/helm/edge/`, one Gateway listening on 443 for `*.neuromesh.localhost`. The proxy runs on the
  node's network, so WSL2's localhost forwarding carries `127.0.0.1:443` on Windows to it, the same
  path the kubeconfig's `localhost:6443` takes. `edge:status` checks each routed hostname over HTTPS
  from the machine, against the dev CA.
- **HTTPRoutes for Grafana, VMSingle, vmalert and Alertmanager**, rendered only when the cluster has
  the Gateway API, so the metrics profile still installs without the edge. Grafana's `root_url` is
  its hostname, and `task obs:access` now prints real URLs.
- **An `edge` certificate** from the dev CA (`task certs:dev`) for the wildcard and each UI's name.

### Changed

- **`task certs:dev`** reports the certificates it actually wrote.
- **`docs/onboarding/local-cluster.md`** explains the hostnames: why no relay, why the proxy is root,
  the one-time certificate step, and how a chart opts in.

## [0.5.0] - Metrics on the Local Cluster (2026-11-16)

**The metrics half of the observability profile runs on the local cluster, and the GPU is in it.**
[#22](https://github.com/kokou-egbewatt/NeuroMesh/issues/22) stays open for logs, traces, the
collector and NetworkPolicy, and Grafana's hostname comes with the local edge.

### Added

- **`task obs:up|down|status|access|test-rules`** (`scripts/observability.sh`) install the profile on the
  `observability` namespace: the VictoriaMetrics CRDs first, then the stack, then dcgm-exporter.
  `obs:access` lists each UI's hostname, path and login, masked on the console and in full in
  `~/.neuromesh/access.md`; a hostname shows as reachable once an HTTPRoute serves it.
- **`infra/helm/victoria-metrics/`**, an umbrella over `victoria-metrics-k8s-stack` 0.97.1: VMSingle,
  vmagent, vmalert, Alertmanager, Grafana, node-exporter and kube-state-metrics, with k3s's absent
  control-plane scrapes and rules off, 45-day retention with a 5 GiB free-space floor, and an alert
  sink that writes each alert as a JSON line.
- **`infra/helm/dcgm-exporter/`**: dcgm-exporter as a GPU observer, collecting all 55 fields the
  exporter ships a definition for. On the RTX 5060 under WSL2, 27 report and the profiling fields
  don't.
- **Rules and dashboards as code:** `VMRule`s for dcgm-exporter down, GPU above 85 °C and a NeuroMesh
  service down, unit tested with `vmalert-tool` (`observability/victoria-metrics/tests/`); a
  platform status dashboard and a GPU dashboard.

### Changed

- **The GPU admission policy** allows one more shape: a GPU observer in `observability`, labelled
  `neuromesh.dev/gpu-observer: "true"`, owned by a DaemonSet and requesting no GPU, so it doesn't
  hold the card's one `nvidia.com/gpu`. `task cluster:status -- -probe` checks the exception and
  that a bare labelled pod, or the label elsewhere, is still rejected.
- **`docs/onboarding/local-cluster.md`** records the profile's footprint (380m and 1099 MiB requested,
  about 100m and 1182 MiB in use) and that dcgm-exporter's VRAM is below the desktop's noise.
- **The observability READMEs** describe what is deployed and where its files live.

## [0.4.0] - The Local Cluster (2026-10-29)

**The local cluster is one command, and the GPU in it is real: a pod gets it only by asking the
scheduler for it.** [#21](https://github.com/kokou-egbewatt/NeuroMesh/issues/21)

### Added

- **`task cluster:start|status|pause|resume|stop|delete`** (`tools/cluster`, shaped by
  `tools/cluster/cluster.yaml`) run a single-node k3s in the WSL2 distro. `start` provisions or
  reconciles and is a no-op the second time; `status` walks every layer from the Windows driver
  to the pods and, with `-probe`, runs a GPU pod; `pause` and `stop` keep the node, volumes and
  images; `delete` lists what it removes and needs `-yes`.
- **`infra/kubernetes/gpu/`**: the NVIDIA device plugin with `runtimeClassName: nvidia` (as
  published it runs and registers nothing on k3s) and a ValidatingAdmissionPolicy that rejects a
  GPU request without the RuntimeClass, and the RuntimeClass without a GPU request.
- **A keepalive Scheduled Task** that loops a hidden `wsl.exe` session, so the cluster survives
  closing every terminal and comes back by itself about 9 s after the distro is terminated.
- **[Local cluster](docs/onboarding/local-cluster.md)**: commands, what each keeps, a GPU pod's
  two required lines, the measurements behind each choice, the reference machine's figures.

### Changed

- **`deployments/local/README.md`** no longer says kind and the others cannot schedule the GPU
  under WSL2. They can; k3s was chosen because it is faster, lighter and runs GPU pods
  unprivileged ([infinity-bench #22](https://github.com/kokou-egbewatt/infinity-bench/issues/22)).

## [0.3.0] - The Stub-Path Baseline (2026-09-28)

**Before the stub gives way to a real backend, a number for what the platform itself costs.**
Issues #1, #6, #7 and #10 each add work to the hot path; this release records the path without
them, so each of those changes can say what it cost
([#13](https://github.com/kokou-egbewatt/NeuroMesh/issues/13)).

### Added

- **`benchmarks/inference`**, a workspace module with a closed-loop load driver (fixed request
  counts, at least 1000 per worker, discarded warmup, exact percentiles over every sample) that
  measures `/v1/route` over HTTPS at 1, 8 and 64 concurrent clients and `StreamInference` over
  mutual TLS at 1 and 8, reporting p50, p95, p99, req/s, time to first chunk and chunks/s.
  `TestSmoke` runs every case at a tiny count in CI; `BenchmarkRoute` and `BenchmarkStream` expose
  the same cases to `go test -bench`. The driver has no dependencies outside the standard library
  and cpuid, so `go work sync` adds nothing to the service modules.
- **[`BASELINE.md`](benchmarks/inference/BASELINE.md)** with the reference numbers, the environment
  they came from and the protocol for recording the next run.
- **`task bench`, `bench:repeat` and `bench:docker`.** `bench:repeat` and `bench:docker` run the
  matrix twice and fail if any p50 moves by 10% or more; `bench:docker` runs in a Linux container,
  the reference environment.
- **`BenchmarkRouteHandler`**, the gateway handler alone against a fake client: about 7 µs and 50
  allocations of a 340 µs round trip.

### Changed

- **The integration harness is a package.** `test/integration/harness` boots both services for the
  golden tests and the benchmarks alike, so both measure the same wiring.
- `task test:unit` skips `benchmarks/` along with `test/`, since both open sockets.

### Fixed

- **Sub-millisecond timing on Windows.** Go's monotonic clock there advances about every 0.5 ms,
  so a request measured with `time.Since` reads 0. The driver reads QueryPerformanceCounter on
  Windows and refuses to run on a clock coarser than 1 µs.

## [0.2.0] - Local Developer Tooling (2026-09-24)

**The scaffold worked, and using it took two terminals, a curl flag nobody remembered, and a CI
run to find out that a file had CRLF line endings.** This release is the daily loop.

### Added

- **`task dev`** runs the runtime and the gateway together with live reload (air), prefixed
  output, and dev certificates minted on first run. A change in a service, a shared package or
  the SDK is served within seconds.
- **`task call:route`, `call:health` and `call:runtime`** call the local stack over TLS. The last
  one calls the runtime directly with `buf curl`, presenting the gateway's certificate, so the
  runtime is reachable without reflection.
- **`task certs:trust` and `certs:untrust`** add the dev CA to the current Windows user's trust
  store, so a browser and plain curl accept `https://localhost:8443`.
- **`task doctor`** checks Go, the pinned tools, Node and pnpm, Docker, act, certificates, hooks,
  `origin/main` and line endings, and says what to run for each gap.
- **`task go:test:docker`** runs the tests in a Linux container with the race detector, for
  machines without cgo.
- **`task test:unit`, `test:golden` and `cover:html`** for the fast loop, golden updates and a
  coverage report.
- **Git hooks** (`lefthook.yml`, `task hooks:install`): line endings, Go formatting, doc links,
  changelog and buf lint before a commit; the version gate and unit tests before a push.
- **`task ci:line-endings`** in the docs job, which now holds line endings and links only: act copies the working tree, not the commit, so a
  CRLF file on disk breaks it even when the commit is clean.
- **`task release:next`** adds the next changelog version and sets `package.json` to match.
- **`task adr:new` and `rfc:new`** create the next numbered ADR or RFC in the house format.
- **Shared VS Code config**: recommended extensions, launch configs for both services and the
  current test package, and a compound that starts both.
- **A version control job** (`task ci:version`, `ci/scripts/version-control.sh`) runs first in
  `ci.yml`, and the Go, proto and web jobs wait for it. It runs the changelog integrity check and
  the version gate, then writes the version, its release status, the commit, the Go version and
  that version's changelog entry to the job summary, even when a check fails.
- **The docs job shows the latest changelog updates** (`[Unreleased]` and the newest release) in
  its summary.
- **Images carry the version.** `task images:build` tags both images with the `package.json`
  version and sets `org.opencontainers.image.version`; the smoke test fails if the label
  disagrees, and writes an images table (version, size, user) under the version header.
- **gofumpt and goimports** through golangci-lint, with `task go:fmt`.
- **`-config` flag and `log_format`** on both services. Relative paths in a config resolve
  against the config file's directory, so a service runs from any working directory. Local
  configs log text; everything else stays JSON.

### Changed

- Test output prints each package's share of the workspace and one merged coverage total,
  instead of repeating the full package list per line.
- **Actions run on Node 24** (checkout v7, setup-go v7, setup-node v7, pnpm/action-setup v6), and
  jobs are pinned to `ubuntu-24.04`, so the `ubuntu-latest` move to Ubuntu 26 on 2026-10-19 lands as
  a deliberate change rather than a silent one.
- **No Pulumi.** `infra/pulumi` is removed; Terraform is the only infrastructure-as-code tool.
- **Builds use Go 1.26.8**, pinned as `toolchain go1.26.8` in `go.work` and in both Dockerfiles,
  while the `go` lines stay at 1.26.3, the minimum the code needs. setup-go v7 sets
  `GOTOOLCHAIN=local`, which showed that buf v1.73.0 needs Go 1.26.7: CI had been quietly
  downloading a newer toolchain than the repository declared. Keeping the minimum at 1.26.3 lets
  gopls load the workspace on an installed 1.26.3, and setup-go reads the `toolchain` line.

## [0.1.0] - The Phase 1 Scaffold (2026-09-24)

**The first commit of the platform itself.** Before this, `main` held a README and a license. This
release is the baseline every Phase 1 issue lands on: two services that talk to each other over
mutual TLS, a gateway that validates what it accepts, tests that boot both, and CI that runs the
same way on GitHub, under act and in a terminal
([#94](https://github.com/kokou-egbewatt/NeuroMesh/issues/94)).

### Added

- **The monorepo layout** ([ADR-0001](docs/adr/0001-monorepo-structure-and-phasing.md)):
  `apps/`, `services/`, `packages/`, `sdk/`, `proto/`, `infra/`, `deployments/`, `observability/`,
  `benchmarks/`, `test/`, `tools/`, `docs/`, tied together by `go.work` and `pnpm-workspace.yaml`.
  Every Go module builds on its own through `replace` directives, and `task deps:check` fails
  when a module is untidy or disagrees with its siblings on a shared version.
- **The inference contract**, `proto/neuromesh/v1/runtime.proto`: unary `Route` and
  server-streaming `StreamInference`, with generation parameters and token usage. Linted by buf;
  the generated Go code in `sdk/go/gen` is committed and `task proto:check` fails when it drifts.
- **`services/runtime`**: the gRPC `InferenceService` with a stub backend until a real adapter
  lands, the gRPC health service, a message size limit, reflection off by default, and bounded
  shutdown (health flips to NOT_SERVING, calls drain for `shutdown_timeout`, the rest are cut).
- **`services/gateway`**: the HTTPS data-plane entrypoint. `/healthz` and `POST /v1/route`, a
  body limit (413), required and bounded `model` and `prompt` (400 with the field and a code),
  one JSON error shape carrying the request ID, `X-Request-Id` on every response, header and
  idle timeouts, and the runtime's status mapped to HTTP without returning its message. Routing
  goes through the `internal/router` seam later routers plug into.
- **The control plane and data plane split** ([ADR-0002](docs/adr/0002-control-plane-data-plane-split.md)):
  the gateway never calls a management service in the request path.
- **TLS on every hop.** HTTPS at the edge, TLS 1.3 minimum. Mutual TLS from gateway to runtime,
  with `tls.allowed_client_names` restricting which certificate common names may call: chain
  verification says the caller is a NeuroMesh service, the list says which one. Plaintext needs
  `tls.insecure: true` and logs a warning.
- **Certificates in the cert-manager Secret layout** (`tls.crt`, `tls.key`, `ca.crt`). On a
  cluster cert-manager mints them; `task certs:dev` mints a throwaway set for runs outside one,
  and the tests mint their own with the same code (`packages/utils/certgen`).
- **`sdk/go`**: a typed client that requires an explicit transport (`WithTLS` or `WithInsecure`),
  applies a default deadline to unary calls, and retries `Unavailable` only. `ResourceExhausted`
  is never retried: it means the request is too large or the backend is saturated.
- **Shared packages**: `packages/config` (YAML with unknown keys rejected, env overrides, the TLS
  section), `packages/logging` (JSON slog at the configured level), `packages/middleware` (gRPC
  deadline and retry interceptors), `packages/utils/certgen`.
- **Tests**: gateway handler table tests with no sockets, runtime transport tests (plaintext dial,
  foreign CA, disallowed caller, expired certificate, oversized message, shutdown, reflection),
  TLS and config tests, and `test/integration`, which boots both services in-process over real
  TLS and checks golden responses.
- **Images** for both services on `distroless/static-debian12:nonroot`, running as uid 65532 and
  built without protoc. `task images:smoke` runs them together and serves `/v1/route` over HTTPS.
- **CI**: `ci.yml` (Go, proto, web), `docs.yml` (links, changelog, version gate) and `images.yml`
  (build and smoke test), each a thin wrapper over `task ci:*`. `.actrc` and `.act/` make
  `act pull_request -W .github/workflows/<file>.yml` run with no flags
  ([Local CI](docs/onboarding/local-ci.md)).
- **Docs gates**: every relative Markdown link resolves inside the repository, this file is
  structurally sound and matches `package.json`, and code changes carry a new version here.
- **Pinned tooling** in `scripts/install-tools.sh`: task, buf, protoc-gen-go, protoc-gen-go-grpc
  and golangci-lint, used by CI, act and the Taskfile alike.
- **Design docs**: the [architecture overview](docs/architecture/overview.md) with the seven-phase
  roadmap and the local k3s and VictoriaMetrics substrate, and
  [RFC-0001](docs/rfc/0001-neuromesh-runtime.md) for the runtime. The delivery backlog lives in
  GitHub issues, one milestone per phase.
- **Local only.** The local k3s cluster is the one environment; `deployments/dev` and the
  remote `infra/` directories are placeholders until
  [#90](https://github.com/kokou-egbewatt/NeuroMesh/issues/90), and nothing in CI deploys anywhere.
