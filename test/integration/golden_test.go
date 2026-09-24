// Package integration boots the runtime and the gateway in-process on real
// loopback sockets over real TLS (HTTPS at the edge, mutual TLS between the
// two) and asserts the golden path with no mocks on the wire.
package integration

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kokou-egbewatt/NeuroMesh/packages/config"
	"github.com/kokou-egbewatt/NeuroMesh/packages/utils/certgen"
	sdk "github.com/kokou-egbewatt/NeuroMesh/sdk/go"
	neuromeshv1 "github.com/kokou-egbewatt/NeuroMesh/sdk/go/gen/neuromesh/v1"
	gateway "github.com/kokou-egbewatt/NeuroMesh/services/gateway/pkg/server"
	runtime "github.com/kokou-egbewatt/NeuroMesh/services/runtime/pkg/server"
)

var update = flag.Bool("update", false, "rewrite golden files")

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

type stack struct {
	gatewayURL  string
	runtimeAddr string
	caPEM       []byte
	certs       map[string]config.TLS
	stopRuntime func()
}

func issue(t *testing.T, ca *certgen.CA, dir, cn string) config.TLS {
	t.Helper()
	leaf, err := ca.Issue(cn, []string{cn, "localhost", "127.0.0.1"}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	d := filepath.Join(dir, cn)
	if err := certgen.WriteSecretLayout(d, leaf, ca.CertPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	return config.TLS{CertFile: filepath.Join(d, "tls.crt"), KeyFile: filepath.Join(d, "tls.key"), CAFile: filepath.Join(d, "ca.crt")}
}

func boot(t *testing.T) *stack {
	t.Helper()
	ca, err := certgen.NewCA("integration-ca", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	certs := map[string]config.TLS{"runtime": issue(t, ca, dir, "runtime"), "gateway": issue(t, ca, dir, "gateway")}

	rtTLS := certs["runtime"]
	rtTLS.AllowedClientNames = []string{"gateway"}
	rt, err := runtime.New(runtime.Config{Addr: "127.0.0.1:0", TLS: rtTLS, ShutdownTimeout: time.Second}, quiet)
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.Listen(context.Background()); err != nil {
		t.Fatal(err)
	}
	rtCtx, rtCancel := context.WithCancel(context.Background())
	rtDone := make(chan error, 1)
	go func() { rtDone <- rt.Serve(rtCtx) }()
	stopped := false
	stopRuntime := func() {
		if !stopped {
			stopped = true
			rtCancel()
			<-rtDone
		}
	}

	gwEdge := certs["gateway"]
	gw, err := gateway.New(gateway.Config{
		Addr:            "127.0.0.1:0",
		ShutdownTimeout: time.Second,
		TLS:             config.TLS{CertFile: gwEdge.CertFile, KeyFile: gwEdge.KeyFile},
		Runtime:         gateway.RuntimeConfig{Addr: rt.Addr().String(), ServerName: "runtime", TLS: certs["gateway"]},
	}, quiet)
	if err != nil {
		t.Fatal(err)
	}
	if err := gw.Listen(context.Background()); err != nil {
		t.Fatal(err)
	}
	gwCtx, gwCancel := context.WithCancel(context.Background())
	gwDone := make(chan error, 1)
	go func() { gwDone <- gw.Serve(gwCtx) }()

	t.Cleanup(func() {
		gwCancel()
		<-gwDone
		stopRuntime()
	})
	return &stack{
		gatewayURL:  "https://" + gw.Addr().String(),
		runtimeAddr: rt.Addr().String(),
		caPEM:       ca.CertPEM,
		certs:       certs,
		stopRuntime: stopRuntime,
	}
}

func (s *stack) httpsClient(t *testing.T) *http.Client {
	t.Helper()
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(s.caPEM)
	return &http.Client{
		Timeout:   10 * time.Second,
		Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS13, ServerName: "localhost"}},
	}
}

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
	s := boot(t)
	r := post(t, s.httpsClient(t), s.gatewayURL+"/v1/route", "golden-1", `{"model":"llama3","prompt":"hello there world"}`)
	if r.status != http.StatusOK {
		t.Fatalf("status %d: %s", r.status, r.body)
	}
	if r.tls == nil || r.tls.Version != tls.VersionTLS13 {
		t.Fatal("response did not come over TLS 1.3")
	}
	golden(t, "route.json", r.body)
}

func TestStreamInferenceOverMutualTLS(t *testing.T) {
	s := boot(t)
	clientTLS, err := s.certs["gateway"].ClientConfig("runtime")
	if err != nil {
		t.Fatal(err)
	}
	c, err := sdk.NewClient(s.runtimeAddr, sdk.WithTLS(clientTLS))
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
	s := boot(t)
	plain := strings.Replace(s.gatewayURL, "https://", "http://", 1)
	resp, err := (&http.Client{Timeout: 5 * time.Second}).Post(plain+"/v1/route", "application/json", strings.NewReader(`{"model":"m","prompt":"p"}`))
	if err == nil {
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode < 400 {
			t.Fatalf("plain HTTP was served with %d", resp.StatusCode)
		}
	}
}

func TestRuntimeDownIsA503WithoutInternals(t *testing.T) {
	s := boot(t)
	s.stopRuntime()
	r := post(t, s.httpsClient(t), s.gatewayURL+"/v1/route", "down-1", `{"model":"m","prompt":"p"}`)
	body := r.body
	if r.status != http.StatusServiceUnavailable {
		t.Fatalf("status %d: %s", r.status, body)
	}
	if strings.Contains(string(body), s.runtimeAddr) || strings.Contains(string(body), "dial") {
		t.Fatalf("response leaks upstream detail: %s", body)
	}
	if !strings.Contains(string(body), `"code":"backend_unavailable"`) || !strings.Contains(string(body), `"request_id":"down-1"`) {
		t.Fatalf("unexpected error body: %s", body)
	}
}

func TestOversizedBodyStopsAtTheEdge(t *testing.T) {
	s := boot(t)
	big := fmt.Sprintf(`{"model":"m","prompt":"%s"}`, strings.Repeat("a", 2<<20))
	r := post(t, s.httpsClient(t), s.gatewayURL+"/v1/route", "big-1", big)
	if r.status != http.StatusRequestEntityTooLarge {
		t.Fatalf("status %d: %s", r.status, r.body)
	}
}
