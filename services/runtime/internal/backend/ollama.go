package backend

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// Ollama generates through Ollama's /api/generate. Model names are Ollama tags, passed as they are.
type Ollama struct{ s httpServer }

const (
	maxBody = 16 << 20
	maxLine = 1 << 20
)

// NewOllama checks the URL and builds the client.
func NewOllama(cfg HTTPConfig) (*Ollama, error) {
	s, err := newHTTPServer(KindOllama, cfg)
	if err != nil {
		return nil, err
	}
	return &Ollama{s: s}, nil
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
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBody)).Decode(&r); err != nil {
		return Response{}, o.s.readErr(ctx, err)
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
	sc.Buffer(make([]byte, 0, 64<<10), maxLine)
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
		return Usage{}, o.s.readErr(ctx, err)
	}
	return Usage{}, o.s.cutOff()
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
	resp, err := o.s.post(ctx, "/api/generate", bytes.NewReader(b))
	if err != nil || resp.StatusCode == http.StatusOK {
		return resp, err
	}
	defer func() { _ = resp.Body.Close() }()
	var r ollamaReply
	_ = json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&r)
	msg := r.Error
	if msg == "" {
		msg = resp.Status
	}
	return nil, o.s.statusErr(resp.StatusCode, msg)
}
