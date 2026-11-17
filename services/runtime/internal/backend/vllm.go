package backend

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// VLLM generates through vLLM's OpenAI-compatible /v1/chat/completions, the prompt as one user
// message so an instruct model gets its chat template, as Ollama's /api/generate applies it. Model
// names are vLLM's served model names, passed as they are.
type VLLM struct{ s httpServer }

// NewVLLM checks the URL and builds the client.
func NewVLLM(cfg HTTPConfig) (*VLLM, error) {
	s, err := newHTTPServer(KindVLLM, cfg)
	if err != nil {
		return nil, err
	}
	return &VLLM{s: s}, nil
}

// Name is "vllm".
func (*VLLM) Name() string { return KindVLLM }

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type streamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

type vllmRequest struct {
	Model         string         `json:"model"`
	Messages      []chatMessage  `json:"messages"`
	Stream        bool           `json:"stream"`
	StreamOptions *streamOptions `json:"stream_options,omitempty"`
	Temperature   *float32       `json:"temperature,omitempty"`
	MaxTokens     int32          `json:"max_tokens,omitempty"`
	Stop          []string       `json:"stop,omitempty"`
}

type vllmUsage struct {
	PromptTokens     int32 `json:"prompt_tokens"`
	CompletionTokens int32 `json:"completion_tokens"`
}

// vllmReply is a whole answer, one event of a streamed one, or an error. vLLM has written errors
// both flat ({"object":"error","message":...}) and nested ({"error":{"message":...}}).
type vllmReply struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
	} `json:"choices"`
	Usage   *vllmUsage `json:"usage"`
	Object  string     `json:"object"`
	Message string     `json:"message"`
	Error   *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (r vllmReply) err() string {
	if r.Error != nil {
		return r.Error.Message
	}
	if r.Object == "error" {
		return r.Message
	}
	return ""
}

func (r vllmReply) usage() Usage {
	if r.Usage == nil {
		return Usage{}
	}
	return Usage{PromptTokens: r.Usage.PromptTokens, CompletionTokens: r.Usage.CompletionTokens}
}

// Complete asks for the whole answer at once.
func (v *VLLM) Complete(ctx context.Context, req Request) (Response, error) {
	resp, err := v.chat(ctx, req, false)
	if err != nil {
		return Response{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	var r vllmReply
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBody)).Decode(&r); err != nil {
		return Response{}, v.s.readErr(ctx, err)
	}
	if msg := r.err(); msg != "" {
		return Response{}, fmt.Errorf("vllm: %s", msg)
	}
	if len(r.Choices) == 0 {
		return Response{}, fmt.Errorf("vllm: answer has no choices")
	}
	return Response{Output: r.Choices[0].Message.Content, Usage: r.usage()}, nil
}

// Stream reads vLLM's server-sent events, one delta each, until [DONE]. The usage arrives in the
// last event before it, with no choices, because the request asks for include_usage.
func (v *VLLM) Stream(ctx context.Context, req Request, emit func(string) error) (Usage, error) {
	resp, err := v.chat(ctx, req, true)
	if err != nil {
		return Usage{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	var u Usage
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 64<<10), maxLine)
	for sc.Scan() {
		data, ok := strings.CutPrefix(sc.Text(), "data:")
		if !ok {
			continue // blank separators, comments, event names
		}
		data = strings.TrimSpace(data)
		if data == "[DONE]" {
			return u, nil
		}
		var r vllmReply
		if err := json.Unmarshal([]byte(data), &r); err != nil {
			return Usage{}, fmt.Errorf("vllm: unreadable stream event: %w", err)
		}
		if msg := r.err(); msg != "" {
			return Usage{}, fmt.Errorf("vllm: %s", msg)
		}
		if r.Usage != nil {
			u = r.usage()
		}
		for _, c := range r.Choices {
			if c.Delta.Content == "" {
				continue
			}
			if err := emit(c.Delta.Content); err != nil {
				return Usage{}, err
			}
		}
	}
	if err := sc.Err(); err != nil {
		return Usage{}, v.s.readErr(ctx, err)
	}
	return Usage{}, v.s.cutOff()
}

func (v *VLLM) chat(ctx context.Context, req Request, stream bool) (*http.Response, error) {
	body := vllmRequest{
		Model:     req.Model,
		Messages:  []chatMessage{{Role: "user", Content: req.Prompt}},
		Stream:    stream,
		MaxTokens: req.MaxTokens,
		Stop:      req.Stop,
	}
	if stream {
		body.StreamOptions = &streamOptions{IncludeUsage: true}
	}
	if req.Temperature != 0 {
		body.Temperature = &req.Temperature
	}
	b, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	resp, err := v.s.post(ctx, "/v1/chat/completions", bytes.NewReader(b))
	if err != nil || resp.StatusCode == http.StatusOK {
		return resp, err
	}
	defer func() { _ = resp.Body.Close() }()
	var r vllmReply
	_ = json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&r)
	msg := r.err()
	if msg == "" {
		msg = resp.Status
	}
	return nil, v.s.statusErr(resp.StatusCode, msg)
}
