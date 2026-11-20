// Package service is the runtime's InferenceService over a backend.Backend. It turns the backend's
// errors into gRPC codes, once for every adapter.
package service

import (
	"context"
	"errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	neuromeshv1 "github.com/kokou-egbewatt/NeuroMesh/sdk/go/gen/neuromesh/v1"
	"github.com/kokou-egbewatt/NeuroMesh/services/runtime/internal/backend"
)

// Server implements neuromeshv1.InferenceServiceServer.
type Server struct {
	neuromeshv1.UnimplementedInferenceServiceServer
	b backend.Backend
}

// New serves b.
func New(b backend.Backend) *Server { return &Server{b: b} }

func request(model, prompt string, p *neuromeshv1.GenerationParams) backend.Request {
	return backend.Request{
		Model:       model,
		Prompt:      prompt,
		Temperature: p.GetTemperature(),
		MaxTokens:   p.GetMaxTokens(),
		Stop:        p.GetStop(),
	}
}

func usage(u backend.Usage) *neuromeshv1.Usage {
	return &neuromeshv1.Usage{PromptTokens: u.PromptTokens, CompletionTokens: u.CompletionTokens}
}

// Route is one unary generation.
func (s *Server) Route(ctx context.Context, req *neuromeshv1.RouteRequest) (*neuromeshv1.RouteResponse, error) {
	r, err := s.b.Complete(ctx, request(req.GetModel(), req.GetPrompt(), req.GetParams()))
	if err != nil {
		return nil, Status(err)
	}
	return &neuromeshv1.RouteResponse{RequestId: req.GetRequestId(), Output: r.Output, Usage: usage(r.Usage)}, nil
}

// StreamInference sends a chunk per token, then a terminal usage chunk.
func (s *Server) StreamInference(req *neuromeshv1.StreamRequest, stream neuromeshv1.InferenceService_StreamInferenceServer) error {
	id := req.GetRequestId()
	u, err := s.b.Stream(stream.Context(), request(req.GetModel(), req.GetPrompt(), req.GetParams()), func(token string) error {
		return stream.Send(&neuromeshv1.StreamChunk{RequestId: id, Payload: &neuromeshv1.StreamChunk_Token{Token: token}})
	})
	if err != nil {
		return Status(err)
	}
	return stream.Send(&neuromeshv1.StreamChunk{RequestId: id, Payload: &neuromeshv1.StreamChunk_Usage{Usage: usage(u)}, Done: true})
}

// Status maps a backend error to a gRPC status. The message names the backend; the gateway logs it
// and never returns it to a client.
func Status(err error) error {
	if _, ok := status.FromError(err); ok {
		return err
	}
	code := codes.Internal
	switch {
	case errors.Is(err, backend.ErrUnavailable):
		code = codes.Unavailable
	case errors.Is(err, backend.ErrModelNotFound):
		code = codes.NotFound
	case errors.Is(err, backend.ErrInvalid):
		code = codes.InvalidArgument
	case errors.Is(err, backend.ErrSaturated):
		code = codes.ResourceExhausted
	case errors.Is(err, context.DeadlineExceeded):
		code = codes.DeadlineExceeded
	case errors.Is(err, context.Canceled):
		code = codes.Canceled
	}
	return status.Error(code, err.Error())
}
