# syntax=docker/dockerfile:1
# Build context: the repository root.
#   docker build -f services/gateway/Dockerfile --build-arg VERSION=0.2.0 -t neuromesh-gateway .
# VERSION (package.json's version; `task images:build` passes it) becomes the
# org.opencontainers.image.version label.
#
# Generated gRPC code is committed, so the build needs no protoc. GOWORK=off
# builds the module through its own replace directives, the same way
# `task deps:check` proves it can.
FROM golang:1.26.8 AS build
ENV CGO_ENABLED=0 GOWORK=off GOFLAGS=-trimpath
WORKDIR /src
COPY packages/ packages/
COPY sdk/go/ sdk/go/
COPY services/gateway/ services/gateway/
WORKDIR /src/services/gateway
RUN --mount=type=cache,target=/go/pkg/mod     --mount=type=cache,target=/root/.cache/go-build     go build -ldflags="-s -w" -o /out/gateway ./cmd/server

# static: nothing needs libc. nonroot: uid 65532.
FROM gcr.io/distroless/static-debian12:nonroot
ARG VERSION=dev
LABEL org.opencontainers.image.title="neuromesh-gateway" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.source="https://github.com/kokou-egbewatt/NeuroMesh" \
      org.opencontainers.image.licenses="MIT"
COPY --from=build /out/gateway /gateway
COPY services/gateway/configs/config.container.yaml /configs/config.yaml
ENV CONFIG_PATH=/configs/config.yaml
USER nonroot:nonroot
EXPOSE 8443
# /certs holds tls.crt, tls.key and ca.crt: a cert-manager Secret on a cluster,
# `docker cp` of `task certs:dev` output in `task images:smoke`.
ENTRYPOINT ["/gateway"]
