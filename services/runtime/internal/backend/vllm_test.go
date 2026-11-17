package backend

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func fakeVLLM(t *testing.T, h http.HandlerFunc) *VLLM {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	v, err := NewVLLM(HTTPConfig{BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func sse(w http.ResponseWriter, events ...string) {
	for _, e := range events {
		_, _ = fmt.Fprintf(w, "data: %s\n\n", e)
		w.(http.Flusher).Flush()
	}
}

func TestVLLMCompleteSendsChatAndReadsUsage(t *testing.T) {
	var got map[string]any
	v := fakeVLLM(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" || r.Method != http.MethodPost {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = w.Write([]byte(`{"choices":[{"index":0,"message":{"role":"assistant","content":"pong"}}],"usage":{"prompt_tokens":12,"completion_tokens":2}}`))
	})
	r, err := v.Complete(context.Background(), Request{Model: "Qwen/Qwen2.5-0.5B-Instruct", Prompt: "p", Temperature: 0.5, MaxTokens: 9, Stop: []string{"\n"}})
	if err != nil {
		t.Fatal(err)
	}
	if r.Output != "pong" || r.Usage != (Usage{PromptTokens: 12, CompletionTokens: 2}) {
		t.Errorf("got %+v", r)
	}
	if got["model"] != "Qwen/Qwen2.5-0.5B-Instruct" || got["stream"] != false || got["stream_options"] != nil {
		t.Errorf("request %v", got)
	}
	if fmt.Sprint(got["messages"]) != "[map[content:p role:user]]" {
		t.Errorf("messages %v", got["messages"])
	}
	if got["temperature"] != 0.5 || got["max_tokens"] != float64(9) || fmt.Sprint(got["stop"]) != "[\n]" {
		t.Errorf("params %v", got)
	}
}

func TestVLLMZeroParamsAreOmitted(t *testing.T) {
	var raw map[string]json.RawMessage
	v := fakeVLLM(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&raw)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"x"}}]}`))
	})
	if _, err := v.Complete(context.Background(), Request{Model: "m", Prompt: "p"}); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"temperature", "max_tokens", "stop"} {
		if _, ok := raw[k]; ok {
			t.Errorf("zero params sent %s %s", k, raw[k])
		}
	}
}

func TestVLLMStreamEmitsDeltasInOrderWithUsage(t *testing.T) {
	var got map[string]any
	v := fakeVLLM(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.Header().Set("Content-Type", "text/event-stream")
		sse(w,
			`{"choices":[{"index":0,"delta":{"role":"assistant","content":""}}]}`,
			`{"choices":[{"index":0,"delta":{"content":"one"}}]}`,
			`{"choices":[{"index":0,"delta":{"content":" two"}}]}`,
			`{"choices":[{"index":0,"delta":{"content":" three"},"finish_reason":"stop"}]}`,
			`{"choices":[],"usage":{"prompt_tokens":5,"completion_tokens":3}}`,
			`[DONE]`)
	})
	var toks []string
	u, err := v.Stream(context.Background(), Request{Model: "m", Prompt: "p"}, func(s string) error {
		toks = append(toks, s)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(toks, "|") != "one| two| three" || u != (Usage{PromptTokens: 5, CompletionTokens: 3}) {
		t.Errorf("tokens %q usage %+v", toks, u)
	}
	if got["stream"] != true || fmt.Sprint(got["stream_options"]) != "map[include_usage:true]" {
		t.Errorf("request %v", got)
	}
}

func TestVLLMStreamErrorEventAndCutOff(t *testing.T) {
	for _, ev := range []string{
		`{"error":{"message":"engine dead","type":"InternalServerError","code":500}}`,
		`{"object":"error","message":"engine dead","type":"InternalServerError","code":500}`,
	} {
		v := fakeVLLM(t, func(w http.ResponseWriter, _ *http.Request) {
			sse(w, `{"choices":[{"delta":{"content":"a"}}]}`, ev)
		})
		_, err := v.Stream(context.Background(), Request{Model: "m"}, func(string) error { return nil })
		if err == nil || !strings.Contains(err.Error(), "engine dead") {
			t.Errorf("error event %s: %v", ev, err)
		}
	}

	cut := fakeVLLM(t, func(w http.ResponseWriter, _ *http.Request) {
		sse(w, `{"choices":[{"delta":{"content":"a"}}]}`)
	})
	if _, err := cut.Stream(context.Background(), Request{Model: "m"}, func(string) error { return nil }); !errors.Is(err, ErrUnavailable) {
		t.Errorf("stream without [DONE]: %v, want ErrUnavailable", err)
	}
}

func TestVLLMStatusCodesMapToSentinels(t *testing.T) {
	for _, c := range []struct {
		code int
		body string
		want error
	}{
		{http.StatusNotFound, `{"error":{"message":"The model ` + "`nope`" + ` does not exist.","type":"NotFoundError","code":404}}`, ErrModelNotFound},
		{http.StatusNotFound, `{"object":"error","message":"The model ` + "`nope`" + ` does not exist.","type":"NotFoundError","code":404}`, ErrModelNotFound},
		{http.StatusBadRequest, `{"error":{"message":"This model's maximum context length is 2048 tokens.","type":"BadRequestError","code":400}}`, ErrInvalid},
		{http.StatusServiceUnavailable, ``, ErrUnavailable},
	} {
		v := fakeVLLM(t, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(c.code)
			_, _ = w.Write([]byte(c.body))
		})
		_, err := v.Complete(context.Background(), Request{Model: "nope"})
		if !errors.Is(err, c.want) {
			t.Errorf("%d %s: %v, want %v", c.code, c.body, err, c.want)
		}
		if c.body != "" && !strings.Contains(err.Error(), "does not exist") && !strings.Contains(err.Error(), "maximum context length") {
			t.Errorf("%d: message lost: %v", c.code, err)
		}
	}
	v := fakeVLLM(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusInternalServerError) })
	_, err := v.Complete(context.Background(), Request{Model: "m"})
	if err == nil || errors.Is(err, ErrUnavailable) || errors.Is(err, ErrModelNotFound) || errors.Is(err, ErrInvalid) {
		t.Errorf("500: %v, want an unclassified error", err)
	}
}

func TestVLLMUnreachableIsUnavailableNamingIt(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	_ = l.Close()
	v, err := NewVLLM(HTTPConfig{BaseURL: "http://" + addr, DialTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	_, err = v.Complete(context.Background(), Request{Model: "m"})
	if !errors.Is(err, ErrUnavailable) || !strings.Contains(err.Error(), "vllm") {
		t.Errorf("got %v", err)
	}
}

func TestVLLMCancelIsTheCallersError(t *testing.T) {
	v := fakeVLLM(t, func(w http.ResponseWriter, r *http.Request) {
		sse(w, `{"choices":[{"delta":{"content":"a"}}]}`)
		<-r.Context().Done()
	})
	ctx, cancel := context.WithCancel(context.Background())
	_, err := v.Stream(ctx, Request{Model: "m"}, func(string) error { cancel(); return nil })
	if !errors.Is(err, context.Canceled) {
		t.Errorf("got %v, want context.Canceled", err)
	}
}

func TestNewBuildsVLLM(t *testing.T) {
	b, err := New(Config{Kind: KindVLLM, VLLM: HTTPConfig{BaseURL: "http://vllm:8000"}})
	if err != nil || b.Name() != KindVLLM {
		t.Errorf("%v %v", b, err)
	}
	if _, err := New(Config{Kind: KindVLLM}); err == nil || !strings.Contains(err.Error(), "backend.vllm.base_url") {
		t.Errorf("vllm without a URL: %v", err)
	}
}
