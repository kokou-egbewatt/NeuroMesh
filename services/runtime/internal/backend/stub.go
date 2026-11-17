package backend

import (
	"context"
	"strings"
)

// Stub answers with a fixed string per model, so the gateway, the SDK, the tests and the #13
// baseline have something deterministic to call. Its output is the one the golden files record.
type Stub struct{}

// Name is "stub".
func (Stub) Name() string { return KindStub }

// wordCount stands in for tokenization.
func wordCount(s string) int32 {
	return int32(min(len(strings.Fields(s)), 1<<31-1)) //nolint:gosec // bounded above
}

// Complete returns "stub response for model <model>".
func (Stub) Complete(_ context.Context, req Request) (Response, error) {
	out := "stub response for model " + req.Model
	return Response{Output: out, Usage: Usage{PromptTokens: wordCount(req.Prompt), CompletionTokens: wordCount(out)}}, nil
}

// Stream emits one token, "stub token for <model>".
func (Stub) Stream(_ context.Context, req Request, emit func(string) error) (Usage, error) {
	token := "stub token for " + req.Model
	if err := emit(token); err != nil {
		return Usage{}, err
	}
	return Usage{PromptTokens: wordCount(req.Prompt), CompletionTokens: wordCount(token)}, nil
}
