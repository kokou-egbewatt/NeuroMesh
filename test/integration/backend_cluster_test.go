//go:build cluster

// The runtime on its first real backend (#1), against the Ollama chart (`task ollama:up`). The tests
// through the gateway need the cluster's runtime on Ollama (`BACKEND=ollama task svc:up`); task
// test:cluster says which backend it is in NEUROMESH_BACKEND. The streaming test boots a runtime in
// this process pointed at https://ollama.neuromesh.localhost, because the gateway has no streaming
// route yet (#4).
package integration

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	neuromeshv1 "github.com/kokou-egbewatt/NeuroMesh/sdk/go/gen/neuromesh/v1"
	runtime "github.com/kokou-egbewatt/NeuroMesh/services/runtime/pkg/server"
	"github.com/kokou-egbewatt/NeuroMesh/test/integration/harness"
)

// The model the Ollama chart pulls.
const ollamaModel = "qwen2.5:0.5b"

func needOllamaRuntime(t *testing.T) {
	t.Helper()
	if b := os.Getenv("NEUROMESH_BACKEND"); b != "ollama" {
		t.Skipf("the cluster's runtime is on %q: task ollama:up, then BACKEND=ollama task svc:up", b)
	}
}

func TestClusterOllamaRouteIsModelOutput(t *testing.T) {
	needOllamaRuntime(t)
	c, gatewayURL := clusterClient(t)
	c.Timeout = 90 * time.Second // the first request loads the model
	r := post(t, c, gatewayURL+"/v1/route", "ollama-pong",
		`{"model":"`+ollamaModel+`","prompt":"reply with exactly: pong","max_tokens":16}`)
	if r.status != http.StatusOK {
		t.Fatalf("status %d: %s", r.status, r.body)
	}
	var body struct {
		Output           string `json:"output"`
		CompletionTokens int    `json:"completion_tokens"`
	}
	if err := json.Unmarshal(r.body, &body); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(body.Output, "stub response") || !strings.Contains(strings.ToLower(body.Output), "pong") {
		t.Fatalf("output %q, want the model saying pong", body.Output)
	}
	if body.CompletionTokens == 0 {
		t.Errorf("no completion tokens reported: %s", r.body)
	}
}

func TestClusterOllamaUnknownModelIsNotFound(t *testing.T) {
	needOllamaRuntime(t)
	c, gatewayURL := clusterClient(t)
	r := post(t, c, gatewayURL+"/v1/route", "ollama-nomodel", `{"model":"no-such-model:0b","prompt":"hi"}`)
	if r.status != http.StatusNotFound || !strings.Contains(string(r.body), `"code":"model_not_found"`) {
		t.Fatalf("status %d: %s, want 404 model_not_found", r.status, r.body)
	}
}

// ollamaProxy forwards to https://ollama.neuromesh.localhost, trusting the dev CA, so the in-process
// runtime reaches the cluster's Ollama without the CA in the system store.
func ollamaProxy(t *testing.T) string {
	t.Helper()
	caPath := os.Getenv("NEUROMESH_CA")
	if caPath == "" {
		caPath = "../../certs/dev/ca.crt"
	}
	pem, err := os.ReadFile(caPath)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(pem)
	target, _ := url.Parse("https://ollama.neuromesh.localhost")
	p := httputil.NewSingleHostReverseProxy(target)
	p.Transport = &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}}
	p.FlushInterval = -1
	director := p.Director
	p.Director = func(r *http.Request) { director(r); r.Host = target.Host }
	srv := httptest.NewServer(p)
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestClusterOllamaStreamsTokensInOrder(t *testing.T) {
	needOllamaRuntime(t)
	s := harness.BootWith(t, harness.Options{Backend: runtime.BackendConfig{
		Kind: "ollama", Ollama: runtime.OllamaConfig{BaseURL: ollamaProxy(t)},
	}})
	c, err := s.SDKClient()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	stream, err := c.StreamInference(ctx, &neuromeshv1.StreamRequest{
		RequestId: "ollama-stream", Model: ollamaModel, Prompt: "count from one to ten in words, separated by spaces",
		Params: &neuromeshv1.GenerationParams{MaxTokens: 32},
	})
	if err != nil {
		t.Fatal(err)
	}
	var tokens []string
	var last *neuromeshv1.StreamChunk
	for {
		chunk, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if last != nil && last.GetDone() {
			t.Fatal("a chunk after the done chunk")
		}
		if tok := chunk.GetToken(); tok != "" {
			tokens = append(tokens, tok)
		}
		last = chunk
	}
	if len(tokens) < 2 {
		t.Fatalf("%d token chunks, want at least 2: %q", len(tokens), tokens)
	}
	if last == nil || !last.GetDone() || last.GetUsage().GetCompletionTokens() == 0 {
		t.Fatalf("last chunk %v, want done with usage", last)
	}
	// In generation order: the words come out as a sentence, one before two before three.
	text := strings.ToLower(strings.Join(tokens, ""))
	one, two, three := strings.Index(text, "one"), strings.Index(text, "two"), strings.Index(text, "three")
	if one < 0 || two < one || three < two {
		t.Errorf("tokens out of order: %q", text)
	}
}

func kubectl(t *testing.T, args ...string) string {
	t.Helper()
	ctx := os.Getenv("NEUROMESH_CONTEXT")
	if ctx == "" {
		ctx = "gpu-lab"
	}
	out, err := exec.Command("kubectl", append([]string{"--context", ctx, "-n", "neuromesh"}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("kubectl %v: %v\n%s", args, err, out)
	}
	return string(out)
}

// With Ollama scaled to zero, a request is a 503 within the runtime's dial timeout: no hang and no
// stub answer. Ollama is scaled back up and ready before the test returns.
func TestClusterOllamaScaledToZeroIsUnavailable(t *testing.T) {
	needOllamaRuntime(t)
	kubectl(t, "scale", "deploy/ollama", "--replicas=0")
	t.Cleanup(func() {
		kubectl(t, "scale", "deploy/ollama", "--replicas=1")
		kubectl(t, "rollout", "status", "deploy/ollama", "--timeout=5m")
	})
	kubectl(t, "wait", "--for=delete", "pod", "-l", "app.kubernetes.io/name=ollama", "--timeout=2m")

	c, gatewayURL := clusterClient(t)
	start := time.Now()
	r := post(t, c, gatewayURL+"/v1/route", "ollama-down", `{"model":"`+ollamaModel+`","prompt":"hi"}`)
	took := time.Since(start)
	if r.status != http.StatusServiceUnavailable || !strings.Contains(string(r.body), `"code":"backend_unavailable"`) {
		t.Fatalf("status %d: %s, want 503 backend_unavailable", r.status, r.body)
	}
	// The default dial timeout is 3s; refused connections answer at once.
	if took > 4*time.Second {
		t.Errorf("took %v, past the 3s dial timeout", took)
	}
}

// routeModel is a model the cluster's runtime serves: any name on the stub, the pulled one on Ollama.
func routeModel() string {
	if os.Getenv("NEUROMESH_BACKEND") == "ollama" {
		return ollamaModel
	}
	return "llama3"
}
