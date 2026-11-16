# syntax=docker/dockerfile:1
# The benchmark driver as a pod on the cluster (#14): `task bench:cluster` runs it as a Job against
# the gateway Service, so the measured path is gateway and runtime over Kubernetes networking, not
# WSL's localhost relay. Build context is the repo root:
#
#   docker build -f benchmarks/inference/Dockerfile -t neuromesh-bench:<tag> .
#
# GOWORK=off builds the module through its own replace directives, as the service images do.
# Alpine rather than distroless: run-in-cluster.sh records two runs and compares them in one pod.
FROM golang:1.26.8 AS build
ENV CGO_ENABLED=0 GOWORK=off GOFLAGS=-trimpath
WORKDIR /src
COPY packages/ packages/
COPY sdk/go/ sdk/go/
COPY services/gateway/ services/gateway/
COPY services/runtime/ services/runtime/
COPY test/integration/ test/integration/
COPY benchmarks/inference/ benchmarks/inference/
WORKDIR /src/benchmarks/inference
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    go build -ldflags="-s -w" -o /out/bench ./cmd/bench

FROM alpine:3.22
COPY --from=build /out/bench /usr/local/bin/bench
COPY benchmarks/inference/run-in-cluster.sh /usr/local/bin/run-in-cluster.sh
USER 65532:65532
ENTRYPOINT ["/bin/sh", "/usr/local/bin/run-in-cluster.sh"]
