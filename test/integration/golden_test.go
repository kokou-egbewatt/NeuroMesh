// Package integration boots the runtime and the gateway in-process through
// the harness package (real loopback sockets, HTTPS at the edge, mutual TLS
// between the two) and asserts the golden path with no mocks on the wire.
package integration

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	neuromeshv1 "github.com/kokou-egbewatt/NeuroMesh/sdk/go/gen/neuromesh/v1"
	"github.com/kokou-egbewatt/NeuroMesh/test/integration/harness"
)

var update = flag.Bool("update", false, "rewrite golden files")

// result is what the tests need from a response, read and closed inside post.
type result struct {
	status int
	tls    *tls.ConnectionState
	body   []byte
}

func post(t *testing.T, c *http.Client, url, requestID, body string) result {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-Id", requestID)
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return result{status: resp.StatusCode, tls: resp.TLS, body: b}
}

func golden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.WriteFile(path, got, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run with -update to create it)", err)
	}
	if !bytes.Equal(bytes.TrimSpace(got), bytes.TrimSpace(want)) {
		t.Fatalf("%s mismatch\n got: %s\nwant: %s", name, got, want)
	}
}

func TestGoldenRouteOverHTTPSAndMutualTLS(t *testing.T) {
	s := harness.Boot(t)
	r := post(t, s.HTTPSClient(10*time.Second), s.GatewayURL+"/v1/route", "golden-1", `{"model":"llama3","prompt":"hello there world"}`)
	if r.status != http.StatusOK {
		t.Fatalf("status %d: %s", r.status, r.body)
	}
	if r.tls == nil || r.tls.Version != tls.VersionTLS13 {
		t.Fatal("response did not come over TLS 1.3")
	}
	golden(t, "route.json", r.body)
}

func TestStreamInferenceOverMutualTLS(t *testing.T) {
	s := harness.Boot(t)
	c, err := s.SDKClient()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	stream, err := c.StreamInference(ctx, &neuromeshv1.StreamRequest{RequestId: "golden-stream", Model: "llama3", Prompt: "hello there world"})
	if err != nil {
		t.Fatal(err)
	}
	var chunks []string
	for {
		chunk, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		b, _ := json.Marshal(map[string]any{"token": chunk.GetToken(), "usage": chunk.GetUsage(), "done": chunk.GetDone()})
		chunks = append(chunks, string(b))
	}
	golden(t, "stream.jsonl", []byte(strings.Join(chunks, "\n")))
}

func TestPlainHTTPToTheGatewayIsRefused(t *testing.T) {
	s := harness.Boot(t)
	plain := strings.Replace(s.GatewayURL, "https://", "http://", 1)
	resp, err := (&http.Client{Timeout: 5 * time.Second}).Post(plain+"/v1/route", "application/json", strings.NewReader(`{"model":"m","prompt":"p"}`))
	if err == nil {
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode < 400 {
			t.Fatalf("plain HTTP was served with %d", resp.StatusCode)
		}
	}
}

func TestRuntimeDownIsA503WithoutInternals(t *testing.T) {
	s := harness.Boot(t)
	s.StopRuntime()
	r := post(t, s.HTTPSClient(10*time.Second), s.GatewayURL+"/v1/route", "down-1", `{"model":"m","prompt":"p"}`)
	body := r.body
	if r.status != http.StatusServiceUnavailable {
		t.Fatalf("status %d: %s", r.status, body)
	}
	if strings.Contains(string(body), s.RuntimeAddr) || strings.Contains(string(body), "dial") {
		t.Fatalf("response leaks upstream detail: %s", body)
	}
	if !strings.Contains(string(body), `"code":"backend_unavailable"`) || !strings.Contains(string(body), `"request_id":"down-1"`) {
		t.Fatalf("unexpected error body: %s", body)
	}
}

func TestOversizedBodyStopsAtTheEdge(t *testing.T) {
	s := harness.Boot(t)
	big := fmt.Sprintf(`{"model":"m","prompt":"%s"}`, strings.Repeat("a", 2<<20))
	r := post(t, s.HTTPSClient(10*time.Second), s.GatewayURL+"/v1/route", "big-1", big)
	if r.status != http.StatusRequestEntityTooLarge {
		t.Fatalf("status %d: %s", r.status, r.body)
	}
}
