module github.com/kokou-egbewatt/NeuroMesh/sdk/go

go 1.26.8

replace github.com/kokou-egbewatt/NeuroMesh/packages/middleware => ../../packages/middleware

require (
	github.com/kokou-egbewatt/NeuroMesh/packages/middleware v0.0.0-00010101000000-000000000000
	google.golang.org/grpc v1.84.0
	google.golang.org/protobuf v1.36.12
)

require (
	github.com/grpc-ecosystem/go-grpc-middleware/v2 v2.3.4 // indirect
	golang.org/x/net v0.57.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/text v0.40.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260706201446-f0a921348800 // indirect
)
