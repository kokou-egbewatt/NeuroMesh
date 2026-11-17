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

func fakeOllama(t *testing.T, h http.HandlerFunc) *Ollama {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	o, err := NewOllama(HTTPConfig{BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	return o
}

func TestOllamaCompleteSendsOptionsAndReadsUsage(t *testing.T) {
	var got map[string]any
	o := fakeOllama(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/generate" || r.Method != http.MethodPost {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = w.Write([]byte(`{"model":"m","response":"pong","done":true,"prompt_eval_count":7,"eval_count":2}`))
	})
	r, err := o.Complete(context.Background(), Request{Model: "qwen2.5:0.5b", Prompt: "p", Temperature: 0.5, MaxTokens: 9, Stop: []string{"\n"}})
	if err != nil {
		t.Fatal(err)
	}
	if r.Output != "pong" || r.Usage != (Usage{PromptTokens: 7, CompletionTokens: 2}) {
		t.Errorf("got %+v", r)
	}
	if got["model"] != "qwen2.5:0.5b" || got["stream"] != false {
		t.Errorf("request %v", got)
	}
	opts, _ := got["options"].(map[string]any)
	if opts["temperature"] != 0.5 || opts["num_predict"] != float64(9) || fmt.Sprint(opts["stop"]) != "[\n]" {
		t.Errorf("options %v", opts)
	}
}

func TestOllamaZeroParamsSendNoOptions(t *testing.T) {
	var raw map[string]json.RawMessage
	o := fakeOllama(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&raw)
		_, _ = w.Write([]byte(`{"response":"x","done":true}`))
	})
	if _, err := o.Complete(context.Background(), Request{Model: "m", Prompt: "p"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := raw["options"]; ok {
		t.Errorf("zero params sent options %s", raw["options"])
	}
}

func TestOllamaStreamEmitsTokensInOrder(t *testing.T) {
	o := fakeOllama(t, func(w http.ResponseWriter, _ *http.Request) {
		for _, tok := range []string{"one", " two", " three"} {
			_, _ = fmt.Fprintf(w, `{"response":%q,"done":false}`+"\n", tok)
			w.(http.Flusher).Flush()
		}
		_, _ = w.Write([]byte(`{"response":"","done":true,"prompt_eval_count":3,"eval_count":3}` + "\n"))
	})
	var toks []string
	u, err := o.Stream(context.Background(), Request{Model: "m", Prompt: "p"}, func(s string) error {
		toks = append(toks, s)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(toks, "|") != "one| two| three" || u.CompletionTokens != 3 {
		t.Errorf("tokens %q usage %+v", toks, u)
	}
}

func TestOllamaStreamErrorLineAndCutOff(t *testing.T) {
	o := fakeOllama(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"response":"a","done":false}` + "\n"))
		_, _ = w.Write([]byte(`{"error":"model runner crashed"}` + "\n"))
	})
	_, err := o.Stream(context.Background(), Request{Model: "m"}, func(string) error { return nil })
	if err == nil || !strings.Contains(err.Error(), "model runner crashed") {
		t.Errorf("error line: %v", err)
	}

	cut := fakeOllama(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"response":"a","done":false}` + "\n"))
	})
	if _, err := cut.Stream(context.Background(), Request{Model: "m"}, func(string) error { return nil }); !errors.Is(err, ErrUnavailable) {
		t.Errorf("stream without done: %v, want ErrUnavailable", err)
	}
}

func TestOllamaStatusCodesMapToSentinels(t *testing.T) {
	for _, c := range []struct {
		code int
		want error
	}{
		{http.StatusNotFound, ErrModelNotFound},
		{http.StatusBadRequest, ErrInvalid},
		{http.StatusServiceUnavailable, ErrUnavailable},
	} {
		o := fakeOllama(t, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(c.code)
			_, _ = w.Write([]byte(`{"error":"model \"nope\" not found, try pulling it first"}`))
		})
		_, err := o.Complete(context.Background(), Request{Model: "nope"})
		if !errors.Is(err, c.want) {
			t.Errorf("%d: %v, want %v", c.code, err, c.want)
		}
	}
	o := fakeOllama(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusInternalServerError) })
	_, err := o.Complete(context.Background(), Request{Model: "m"})
	if err == nil || errors.Is(err, ErrUnavailable) || errors.Is(err, ErrModelNotFound) || errors.Is(err, ErrInvalid) {
		t.Errorf("500: %v, want an unclassified error", err)
	}
}

// Nothing listening is Unavailable at once, naming the backend, with no fallback to the stub.
func TestOllamaUnreachableIsUnavailableQuickly(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	_ = l.Close()
	o, err := NewOllama(HTTPConfig{BaseURL: "http://" + addr, DialTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	_, err = o.Complete(context.Background(), Request{Model: "m"})
	if !errors.Is(err, ErrUnavailable) || !strings.Contains(err.Error(), "ollama") {
		t.Errorf("got %v", err)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("took %v, past the 1s dial timeout", d)
	}
}

func TestOllamaCancelIsTheCallersError(t *testing.T) {
	o := fakeOllama(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"response":"a","done":false}` + "\n"))
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	})
	ctx, cancel := context.WithCancel(context.Background())
	_, err := o.Stream(ctx, Request{Model: "m"}, func(string) error { cancel(); return nil })
	if !errors.Is(err, context.Canceled) {
		t.Errorf("got %v, want context.Canceled", err)
	}
}

func TestNewRejectsUnknownKindAndBadURL(t *testing.T) {
	if _, err := New(Config{Kind: "triton"}); err == nil {
		t.Error("unknown kind accepted")
	}
	if _, err := New(Config{Kind: KindOllama, Ollama: HTTPConfig{BaseURL: "ollama:11434"}}); err == nil {
		t.Error("URL without scheme accepted")
	}
	if b, err := New(Config{}); err != nil || b.Name() != KindStub {
		t.Errorf("empty kind: %v %v", b, err)
	}
}
