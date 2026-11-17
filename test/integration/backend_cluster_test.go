//go:build cluster

// The runtime on a real backend: Ollama (#1, `task ollama:up`) or vLLM (#105, `task vllm:up`). The
// tests through the gateway need the cluster's runtime on that backend (`BACKEND=<kind> task svc:up`);
// task test:cluster says which in NEUROMESH_BACKEND, and on the stub they skip. The streaming test
// boots a runtime in this process pointed at the backend's edge hostname, because the gateway has no
// streaming route yet (#4).
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

// realBackend is a model server chart on the cluster.
type realBackend struct {
	kind  string // backend.kind, the Deployment, the Service and the edge hostname's first label
	model string // the model its chart provides
}

var realBackends = map[string]realBackend{
	"ollama": {kind: "ollama", model: "qwen2.5:0.5b"},
	"vllm":   {kind: "vllm", model: "Qwen/Qwen2.5-0.5B-Instruct"},
}

// needRealBackend is the backend the cluster's runtime is on, or skips on the stub.
func needRealBackend(t *testing.T) realBackend {
	t.Helper()
	b, ok := realBackends[os.Getenv("NEUROMESH_BACKEND")]
	if !ok {
		t.Skipf("the cluster's runtime is on %q: task ollama:up or vllm:up, then BACKEND=<kind> task svc:up", os.Getenv("NEUROMESH_BACKEND"))
	}
	return b
}

func TestClusterBackendRouteIsModelOutput(t *testing.T) {
	b := needRealBackend(t)
	c, gatewayURL := clusterClient(t)
	c.Timeout = 90 * time.Second // the first request may load the model
	r := post(t, c, gatewayURL+"/v1/route", b.kind+"-pong",
		`{"model":"`+b.model+`","prompt":"reply with exactly: pong","max_tokens":16}`)
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

func TestClusterBackendUnknownModelIsNotFound(t *testing.T) {
	b := needRealBackend(t)
	c, gatewayURL := clusterClient(t)
	r := post(t, c, gatewayURL+"/v1/route", b.kind+"-nomodel", `{"model":"no-such-model:0b","prompt":"hi"}`)
	if r.status != http.StatusNotFound || !strings.Contains(string(r.body), `"code":"model_not_found"`) {
		t.Fatalf("status %d: %s, want 404 model_not_found", r.status, r.body)
	}
}

// A prompt past vLLM's max_model_len (4096 tokens in the chart) is the caller's fault: 400, not 502.
func TestClusterVLLMPromptPastContextIsInvalid(t *testing.T) {
	b := needRealBackend(t)
	if b.kind != "vllm" {
		t.Skip("vLLM's context limit; Ollama truncates instead")
	}
	c, gatewayURL := clusterClient(t)
	prompt := strings.Repeat("word ", 6000)
	r := post(t, c, gatewayURL+"/v1/route", "vllm-toolong", `{"model":"`+b.model+`","prompt":"`+prompt+`"}`)
	if r.status != http.StatusBadRequest || !strings.Contains(string(r.body), `"code":"invalid_request"`) {
		t.Fatalf("status %d: %s, want 400 invalid_request", r.status, r.body)
	}
}

// edgeProxy forwards to https://<kind>.neuromesh.localhost, trusting the dev CA, so the in-process
// runtime reaches the cluster's backend without the CA in the system store.
func edgeProxy(t *testing.T, kind string) string {
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
	target, _ := url.Parse("https://" + kind + ".neuromesh.localhost")
	p := httputil.NewSingleHostReverseProxy(target)
	p.Transport = &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}}
	p.FlushInterval = -1
	director := p.Director
	p.Director = func(r *http.Request) { director(r); r.Host = target.Host }
	srv := httptest.NewServer(p)
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestClusterBackendStreamsTokensInOrder(t *testing.T) {
	b := needRealBackend(t)
	cfg := runtime.BackendConfig{Kind: b.kind}
	server := runtime.HTTPBackend{BaseURL: edgeProxy(t, b.kind)}
	if b.kind == "vllm" {
		cfg.VLLM = server
	} else {
		cfg.Ollama = server
	}
	s := harness.BootWith(t, harness.Options{Backend: cfg})
	c, err := s.SDKClient()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	stream, err := c.StreamInference(ctx, &neuromeshv1.StreamRequest{
		RequestId: b.kind + "-stream", Model: b.model, Prompt: "count from one to ten in words, separated by spaces",
		// Near greedy: vLLM otherwise samples with the model's own defaults (0.7) and the
		// answer varies run to run. 0 means unset in the proto.
		Params: &neuromeshv1.GenerationParams{MaxTokens: 32, Temperature: 0.01},
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
	// In generation order: one before two before three, as words or digits.
	text := strings.ToLower(strings.Join(tokens, ""))
	ordered := func(a, b, c string) bool {
		i, j, k := strings.Index(text, a), strings.Index(text, b), strings.Index(text, c)
		return i >= 0 && j > i && k > j
	}
	if !ordered("one", "two", "three") && !ordered("1", "2", "3") {
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

// With the backend scaled to zero, a request is a 503 within the runtime's dial timeout: no hang and
// no stub answer. The backend is scaled back up and ready before the test returns; vLLM takes longer
// to load than Ollama.
func TestClusterBackendScaledToZeroIsUnavailable(t *testing.T) {
	b := needRealBackend(t)
	kubectl(t, "scale", "deploy/"+b.kind, "--replicas=0")
	t.Cleanup(func() {
		kubectl(t, "scale", "deploy/"+b.kind, "--replicas=1")
		kubectl(t, "rollout", "status", "deploy/"+b.kind, "--timeout=10m")
	})
	kubectl(t, "wait", "--for=delete", "pod", "-l", "app.kubernetes.io/name="+b.kind, "--timeout=2m")

	c, gatewayURL := clusterClient(t)
	start := time.Now()
	r := post(t, c, gatewayURL+"/v1/route", b.kind+"-down", `{"model":"`+b.model+`","prompt":"hi"}`)
	took := time.Since(start)
	if r.status != http.StatusServiceUnavailable || !strings.Contains(string(r.body), `"code":"backend_unavailable"`) {
		t.Fatalf("status %d: %s, want 503 backend_unavailable", r.status, r.body)
	}
	// The default dial timeout is 3s; refused connections answer at once.
	if took > 4*time.Second {
		t.Errorf("took %v, past the 3s dial timeout", took)
	}
}

// routeModel is a model the cluster's runtime serves: any name on the stub, the chart's on a real
// backend.
func routeModel() string {
	if b, ok := realBackends[os.Getenv("NEUROMESH_BACKEND")]; ok {
		return b.model
	}
	return "llama3"
}
