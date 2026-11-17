package backend

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// OllamaConfig points the runtime at an Ollama server.
type OllamaConfig struct {
	// BaseURL is the server, e.g. http://ollama:11434.
	BaseURL string
	// DialTimeout bounds connecting; an unreachable server is Unavailable within it.
	DialTimeout time.Duration
	// ResponseHeaderTimeout bounds the wait for the first byte of an answer. A unary generation
	// answers only once it is complete, so this also caps how long one may run, model load included.
	ResponseHeaderTimeout time.Duration
}

// Ollama generates through Ollama's /api/generate. Model names are Ollama tags, passed as they are.
type Ollama struct {
	base   string
	client *http.Client
}

const (
	maxOllamaBody = 16 << 20
	maxOllamaLine = 1 << 20
)

// NewOllama checks the URL and builds the client. It does not contact the server: the runtime boots
// whether or not Ollama is up, and each request reports Unavailable until it is.
func NewOllama(cfg OllamaConfig) (*Ollama, error) {
	u, err := url.Parse(cfg.BaseURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("backend.ollama.base_url %q: want http(s)://host:port", cfg.BaseURL)
	}
	if cfg.DialTimeout <= 0 {
		cfg.DialTimeout = 3 * time.Second
	}
	if cfg.ResponseHeaderTimeout <= 0 {
		cfg.ResponseHeaderTimeout = 60 * time.Second
	}
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.DialContext = (&net.Dialer{Timeout: cfg.DialTimeout, KeepAlive: 30 * time.Second}).DialContext
	t.ResponseHeaderTimeout = cfg.ResponseHeaderTimeout
	t.MaxIdleConnsPerHost = 64
	return &Ollama{base: strings.TrimRight(u.String(), "/"), client: &http.Client{Transport: t}}, nil
}

// Name is "ollama".
func (*Ollama) Name() string { return KindOllama }

type ollamaOptions struct {
	Temperature *float32 `json:"temperature,omitempty"`
	NumPredict  int32    `json:"num_predict,omitempty"`
	Stop        []string `json:"stop,omitempty"`
}

type ollamaRequest struct {
	Model   string         `json:"model"`
	Prompt  string         `json:"prompt"`
	Stream  bool           `json:"stream"`
	Options *ollamaOptions `json:"options,omitempty"`
}

// ollamaReply is a whole answer, or one line of a streamed one; Error is set instead on failure.
type ollamaReply struct {
	Response        string `json:"response"`
	Done            bool   `json:"done"`
	PromptEvalCount int32  `json:"prompt_eval_count"`
	EvalCount       int32  `json:"eval_count"`
	Error           string `json:"error"`
}

func (r ollamaReply) usage() Usage {
	return Usage{PromptTokens: r.PromptEvalCount, CompletionTokens: r.EvalCount}
}

// Complete asks for the whole answer at once.
func (o *Ollama) Complete(ctx context.Context, req Request) (Response, error) {
	resp, err := o.generate(ctx, req, false)
	if err != nil {
		return Response{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	var r ollamaReply
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxOllamaBody)).Decode(&r); err != nil {
		return Response{}, o.readErr(ctx, err)
	}
	if r.Error != "" {
		return Response{}, fmt.Errorf("ollama: %s", r.Error)
	}
	return Response{Output: r.Response, Usage: r.usage()}, nil
}

// Stream reads Ollama's newline-delimited replies, one token each, until the one marked done.
func (o *Ollama) Stream(ctx context.Context, req Request, emit func(string) error) (Usage, error) {
	resp, err := o.generate(ctx, req, true)
	if err != nil {
		return Usage{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 64<<10), maxOllamaLine)
	for sc.Scan() {
		var r ollamaReply
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			return Usage{}, fmt.Errorf("ollama: unreadable stream line: %w", err)
		}
		if r.Error != "" {
			return Usage{}, fmt.Errorf("ollama: %s", r.Error)
		}
		if r.Response != "" {
			if err := emit(r.Response); err != nil {
				return Usage{}, err
			}
		}
		if r.Done {
			return r.usage(), nil
		}
	}
	if err := sc.Err(); err != nil {
		return Usage{}, o.readErr(ctx, err)
	}
	return Usage{}, fmt.Errorf("ollama at %s: %w: stream ended before done", o.base, ErrUnavailable)
}

func (o *Ollama) generate(ctx context.Context, req Request, stream bool) (*http.Response, error) {
	body := ollamaRequest{Model: req.Model, Prompt: req.Prompt, Stream: stream}
	if req.Temperature != 0 || req.MaxTokens != 0 || len(req.Stop) > 0 {
		opts := &ollamaOptions{NumPredict: req.MaxTokens, Stop: req.Stop}
		if req.Temperature != 0 {
			opts.Temperature = &req.Temperature
		}
		body.Options = opts
	}
	b, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	hr, err := http.NewRequestWithContext(ctx, http.MethodPost, o.base+"/api/generate", bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	hr.Header.Set("Content-Type", "application/json")
	resp, err := o.client.Do(hr)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("ollama at %s: %w: %w", o.base, ErrUnavailable, err)
	}
	if resp.StatusCode == http.StatusOK {
		return resp, nil
	}
	defer func() { _ = resp.Body.Close() }()
	var r ollamaReply
	_ = json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&r)
	msg := r.Error
	if msg == "" {
		msg = resp.Status
	}
	switch resp.StatusCode {
	case http.StatusNotFound:
		return nil, fmt.Errorf("ollama: %w: %s", ErrModelNotFound, msg)
	case http.StatusBadRequest:
		return nil, fmt.Errorf("ollama: %w: %s", ErrInvalid, msg)
	case http.StatusServiceUnavailable, http.StatusBadGateway:
		return nil, fmt.Errorf("ollama at %s: %w: %s", o.base, ErrUnavailable, msg)
	}
	return nil, fmt.Errorf("ollama: %d: %s", resp.StatusCode, msg)
}

// readErr is a failure partway through an answer: the caller's cancellation if that is what it was,
// otherwise the server going away.
func (o *Ollama) readErr(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) {
		return fmt.Errorf("ollama at %s: %w: answer cut off", o.base, ErrUnavailable)
	}
	return fmt.Errorf("ollama at %s: %w: %w", o.base, ErrUnavailable, err)
}
