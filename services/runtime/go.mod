module github.com/kokou-egbewatt/NeuroMesh/services/runtime

go 1.26.3

replace (
	github.com/kokou-egbewatt/NeuroMesh/packages/config => ../../packages/config
	github.com/kokou-egbewatt/NeuroMesh/packages/logging => ../../packages/logging
	github.com/kokou-egbewatt/NeuroMesh/packages/middleware => ../../packages/middleware
	github.com/kokou-egbewatt/NeuroMesh/packages/utils => ../../packages/utils
	github.com/kokou-egbewatt/NeuroMesh/sdk/go => ../../sdk/go
)

require (
	github.com/kokou-egbewatt/NeuroMesh/packages/config v0.0.0-00010101000000-000000000000
	github.com/kokou-egbewatt/NeuroMesh/packages/logging v0.0.0-00010101000000-000000000000
	github.com/kokou-egbewatt/NeuroMesh/packages/utils v0.0.0-00010101000000-000000000000
	github.com/kokou-egbewatt/NeuroMesh/sdk/go v0.0.0-00010101000000-000000000000
	google.golang.org/grpc v1.84.0
)

require (
	golang.org/x/net v0.57.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/text v0.40.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260706201446-f0a921348800 // indirect
	google.golang.org/protobuf v1.36.12 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)
