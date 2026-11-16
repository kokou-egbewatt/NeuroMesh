module github.com/kokou-egbewatt/NeuroMesh/benchmarks/inference

go 1.26.3

replace (
	github.com/kokou-egbewatt/NeuroMesh/packages/config => ../../packages/config
	github.com/kokou-egbewatt/NeuroMesh/packages/logging => ../../packages/logging
	github.com/kokou-egbewatt/NeuroMesh/packages/middleware => ../../packages/middleware
	github.com/kokou-egbewatt/NeuroMesh/packages/telemetry => ../../packages/telemetry
	github.com/kokou-egbewatt/NeuroMesh/packages/tracing => ../../packages/tracing
	github.com/kokou-egbewatt/NeuroMesh/packages/utils => ../../packages/utils
	github.com/kokou-egbewatt/NeuroMesh/sdk/go => ../../sdk/go
	github.com/kokou-egbewatt/NeuroMesh/services/gateway => ../../services/gateway
	github.com/kokou-egbewatt/NeuroMesh/services/runtime => ../../services/runtime
	github.com/kokou-egbewatt/NeuroMesh/test/integration => ../../test/integration
)

require (
	github.com/klauspost/cpuid/v2 v2.4.0
	github.com/kokou-egbewatt/NeuroMesh/packages/logging v0.0.0-00010101000000-000000000000
	github.com/kokou-egbewatt/NeuroMesh/packages/tracing v0.0.0-00010101000000-000000000000
	github.com/kokou-egbewatt/NeuroMesh/sdk/go v0.0.0-00010101000000-000000000000
	github.com/kokou-egbewatt/NeuroMesh/test/integration v0.0.0-00010101000000-000000000000
	go.opentelemetry.io/otel v1.47.0
	go.opentelemetry.io/otel/sdk v1.47.0
	google.golang.org/grpc v1.84.0
)

require (
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/cenkalti/backoff/v5 v5.0.3 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/felixge/httpsnoop v1.1.0 // indirect
	github.com/go-logr/logr v1.4.4 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/grpc-ecosystem/go-grpc-middleware/v2 v2.3.4 // indirect
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.30.0 // indirect
	github.com/kokou-egbewatt/NeuroMesh/packages/config v0.0.0-00010101000000-000000000000 // indirect
	github.com/kokou-egbewatt/NeuroMesh/packages/middleware v0.0.0-00010101000000-000000000000 // indirect
	github.com/kokou-egbewatt/NeuroMesh/packages/telemetry v0.0.0-00010101000000-000000000000 // indirect
	github.com/kokou-egbewatt/NeuroMesh/packages/utils v0.0.0-00010101000000-000000000000 // indirect
	github.com/kokou-egbewatt/NeuroMesh/services/gateway v0.0.0-00010101000000-000000000000 // indirect
	github.com/kokou-egbewatt/NeuroMesh/services/runtime v0.0.0-00010101000000-000000000000 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/prometheus/client_golang v1.25.0 // indirect
	github.com/prometheus/client_model v0.6.3 // indirect
	github.com/prometheus/common v0.72.0 // indirect
	github.com/prometheus/procfs v0.22.0 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc v0.72.0 // indirect
	go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.72.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace v1.47.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc v1.47.0 // indirect
	go.opentelemetry.io/otel/log v1.47.0 // indirect
	go.opentelemetry.io/otel/metric v1.47.0 // indirect
	go.opentelemetry.io/otel/trace v1.47.0 // indirect
	go.opentelemetry.io/proto/otlp v1.11.0 // indirect
	golang.org/x/net v0.59.0 // indirect
	golang.org/x/sys v0.48.0 // indirect
	golang.org/x/text v0.42.0 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20260928230214-8a89bd6388cc // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260928230214-8a89bd6388cc // indirect
	google.golang.org/protobuf v1.36.12 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)
