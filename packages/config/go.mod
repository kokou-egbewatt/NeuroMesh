module github.com/kokou-egbewatt/NeuroMesh/packages/config

go 1.26.3

replace github.com/kokou-egbewatt/NeuroMesh/packages/utils => ../utils

require (
	github.com/kokou-egbewatt/NeuroMesh/packages/utils v0.0.0-00010101000000-000000000000
	gopkg.in/yaml.v3 v3.0.1
)

require (
	github.com/kr/pretty v0.3.1 // indirect
	github.com/rogpeppe/go-internal v1.14.1 // indirect
	gopkg.in/check.v1 v1.0.0-20201130134442-10cb98267c6c // indirect
)
