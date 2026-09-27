// Package router resolves which runtime backend should serve a given model.
package router

import (
	"errors"

	neuromeshv1 "github.com/kokou-egbewatt/NeuroMesh/sdk/go/gen/neuromesh/v1"
)

// Router resolves which runtime backend should serve a given model.
//
// It returns the generated client interface rather than the SDK's concrete
// type, so handler tests inject a fake and later routers (#3 round robin, #29
// sticky, #50 KV-cache aware) wrap any mix of clients.
type Router interface {
	Client(model string) (neuromeshv1.InferenceServiceClient, error)
}

// Static always returns the same client, regardless of model.
type Static struct {
	client neuromeshv1.InferenceServiceClient
}

// NewStatic wraps a single client as a Router.
func NewStatic(client neuromeshv1.InferenceServiceClient) (*Static, error) {
	if client == nil {
		return nil, errors.New("router: static router needs a client")
	}
	return &Static{client: client}, nil
}

// Client implements Router.
func (s *Static) Client(string) (neuromeshv1.InferenceServiceClient, error) {
	return s.client, nil
}
