// Package stub is the placeholder InferenceService backend until #1 lands a
// real adapter. It echoes a fixed string per model so the gateway, the SDK and
// the tests have something deterministic to call.
package stub

import (
	"context"
	"strings"

	neuromeshv1 "github.com/kokou-egbewatt/NeuroMesh/sdk/go/gen/neuromesh/v1"
)

// Server implements neuromeshv1.InferenceServiceServer with canned output.
type Server struct {
	neuromeshv1.UnimplementedInferenceServiceServer
}

// wordCount stands in for tokenization until a backend reports real counts.
func wordCount(s string) int32 {
	return int32(min(len(strings.Fields(s)), 1<<31-1)) //nolint:gosec // bounded above
}

// Route returns "stub response for model <model>".
func (Server) Route(_ context.Context, req *neuromeshv1.RouteRequest) (*neuromeshv1.RouteResponse, error) {
	output := "stub response for model " + req.GetModel()
	return &neuromeshv1.RouteResponse{
		RequestId: req.GetRequestId(),
		Output:    output,
		Usage: &neuromeshv1.Usage{
			PromptTokens:     wordCount(req.GetPrompt()),
			CompletionTokens: wordCount(output),
		},
	}, nil
}

// StreamInference sends one token chunk, then a terminal usage chunk.
func (Server) StreamInference(req *neuromeshv1.StreamRequest, stream neuromeshv1.InferenceService_StreamInferenceServer) error {
	token := "stub token for " + req.GetModel()
	if err := stream.Send(&neuromeshv1.StreamChunk{
		RequestId: req.GetRequestId(),
		Payload:   &neuromeshv1.StreamChunk_Token{Token: token},
	}); err != nil {
		return err
	}
	return stream.Send(&neuromeshv1.StreamChunk{
		RequestId: req.GetRequestId(),
		Payload: &neuromeshv1.StreamChunk_Usage{
			Usage: &neuromeshv1.Usage{
				PromptTokens:     wordCount(req.GetPrompt()),
				CompletionTokens: wordCount(token),
			},
		},
		Done: true,
	})
}
