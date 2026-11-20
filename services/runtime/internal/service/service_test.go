package service

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	neuromeshv1 "github.com/kokou-egbewatt/NeuroMesh/sdk/go/gen/neuromesh/v1"
	"github.com/kokou-egbewatt/NeuroMesh/services/runtime/internal/backend"
)

func TestStatusMapsBackendErrors(t *testing.T) {
	for _, c := range []struct {
		err  error
		want codes.Code
	}{
		{fmt.Errorf("ollama at http://x: %w: refused", backend.ErrUnavailable), codes.Unavailable},
		{fmt.Errorf("ollama: %w: nope", backend.ErrModelNotFound), codes.NotFound},
		{fmt.Errorf("ollama: %w", backend.ErrInvalid), codes.InvalidArgument},
		{fmt.Errorf("vllm: 8 in flight, 0 queued: %w", backend.ErrSaturated), codes.ResourceExhausted},
		{context.DeadlineExceeded, codes.DeadlineExceeded},
		{context.Canceled, codes.Canceled},
		{errors.New("ollama: 500"), codes.Internal},
		{status.Error(codes.Aborted, "kept"), codes.Aborted},
	} {
		if got := status.Code(Status(c.err)); got != c.want {
			t.Errorf("%v: %s, want %s", c.err, got, c.want)
		}
	}
	if s, _ := status.FromError(Status(fmt.Errorf("ollama at http://ollama:11434: %w", backend.ErrUnavailable))); s.Message() == "" {
		t.Error("message dropped; it should name the backend for the gateway's log")
	}
}

type fake struct {
	tokens []string
	err    error
	got    backend.Request
}

func (*fake) Name() string { return "fake" }
func (f *fake) Complete(_ context.Context, r backend.Request) (backend.Response, error) {
	f.got = r
	return backend.Response{Output: "out", Usage: backend.Usage{PromptTokens: 1, CompletionTokens: 2}}, f.err
}

func (f *fake) Stream(_ context.Context, r backend.Request, emit func(string) error) (backend.Usage, error) {
	f.got = r
	for _, tok := range f.tokens {
		if err := emit(tok); err != nil {
			return backend.Usage{}, err
		}
	}
	return backend.Usage{CompletionTokens: int32(len(f.tokens))}, f.err //nolint:gosec // tiny
}

type sink struct {
	grpc.ServerStream
	chunks []*neuromeshv1.StreamChunk
}

func (s *sink) Context() context.Context { return context.Background() }
func (s *sink) Send(c *neuromeshv1.StreamChunk) error {
	s.chunks = append(s.chunks, c)
	return nil
}

func TestRoutePassesParamsAndEchoesRequestID(t *testing.T) {
	f := &fake{}
	r, err := New(f).Route(context.Background(), &neuromeshv1.RouteRequest{
		RequestId: "r1", Model: "m", Prompt: "p",
		Params: &neuromeshv1.GenerationParams{Temperature: 0.2, MaxTokens: 5, Stop: []string{"x"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if r.GetRequestId() != "r1" || r.GetOutput() != "out" || r.GetUsage().GetCompletionTokens() != 2 {
		t.Errorf("response %v", r)
	}
	if f.got.Model != "m" || f.got.Temperature != 0.2 || f.got.MaxTokens != 5 || len(f.got.Stop) != 1 {
		t.Errorf("backend got %+v", f.got)
	}
}

func TestStreamSendsTokensThenUsage(t *testing.T) {
	s := &sink{}
	if err := New(&fake{tokens: []string{"a", "b", "c"}}).StreamInference(&neuromeshv1.StreamRequest{RequestId: "s1"}, s); err != nil {
		t.Fatal(err)
	}
	if len(s.chunks) != 4 {
		t.Fatalf("%d chunks, want 3 tokens and a usage chunk", len(s.chunks))
	}
	for i, want := range []string{"a", "b", "c"} {
		if s.chunks[i].GetToken() != want || s.chunks[i].GetDone() || s.chunks[i].GetRequestId() != "s1" {
			t.Errorf("chunk %d = %v", i, s.chunks[i])
		}
	}
	last := s.chunks[3]
	if !last.GetDone() || last.GetUsage().GetCompletionTokens() != 3 {
		t.Errorf("last chunk %v", last)
	}
}

func TestStreamErrorIsAStatusWithNoUsageChunk(t *testing.T) {
	s := &sink{}
	err := New(&fake{tokens: []string{"a"}, err: fmt.Errorf("x: %w", backend.ErrUnavailable)}).StreamInference(&neuromeshv1.StreamRequest{}, s)
	if status.Code(err) != codes.Unavailable {
		t.Errorf("got %v", err)
	}
	if len(s.chunks) != 1 {
		t.Errorf("%d chunks, want only the token sent before the failure", len(s.chunks))
	}
}
