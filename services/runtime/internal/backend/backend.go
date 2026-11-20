// Package backend is what the runtime's InferenceService calls to generate text (#1): the stub, which
// answers with a fixed string, Ollama, and vLLM (#105). Adapters return the sentinel errors below; the service
// maps them to gRPC codes, so every adapter shares one mapping.
package backend

import (
	"context"
	"errors"
	"fmt"
)

// Request is one generation. Zero values mean the backend's default.
type Request struct {
	Model       string
	Prompt      string
	Temperature float32
	MaxTokens   int32
	Stop        []string
}

// Usage is the token count the backend reports.
type Usage struct {
	PromptTokens     int32
	CompletionTokens int32
}

// Response is a finished generation.
type Response struct {
	Output string
	Usage  Usage
}

// Backend generates text.
type Backend interface {
	// Name identifies the backend in errors and logs.
	Name() string
	Complete(ctx context.Context, req Request) (Response, error)
	// Stream calls emit with each token in generation order and returns the usage once the
	// generation ends. An error from emit stops the generation and is returned.
	Stream(ctx context.Context, req Request, emit func(token string) error) (Usage, error)
}

var (
	// ErrUnavailable: the backend can't be reached, or says it can't serve now.
	ErrUnavailable = errors.New("backend unavailable")
	// ErrModelNotFound: the backend doesn't have the requested model.
	ErrModelNotFound = errors.New("model not found")
	// ErrInvalid: the backend rejected the request itself.
	ErrInvalid = errors.New("invalid request")
	// ErrSaturated: the runtime's cap on the backend is full, queue included (#2).
	ErrSaturated = errors.New("backend saturated")
)

// Kinds a runtime config may name.
const (
	KindStub   = "stub"
	KindOllama = "ollama"
	KindVLLM   = "vllm"
)

// Config picks a backend and holds each HTTP backend's server; only the chosen one is read.
type Config struct {
	Kind   string
	Ollama HTTPConfig
	VLLM   HTTPConfig
}

// New builds the backend cfg.Kind names. An empty kind is the stub.
func New(cfg Config) (Backend, error) {
	switch cfg.Kind {
	case "", KindStub:
		return Stub{}, nil
	case KindOllama:
		return NewOllama(cfg.Ollama)
	case KindVLLM:
		return NewVLLM(cfg.VLLM)
	}
	return nil, fmt.Errorf("backend.kind %q: want %s, %s or %s", cfg.Kind, KindStub, KindOllama, KindVLLM)
}
