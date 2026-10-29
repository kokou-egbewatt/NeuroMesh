// Package inference benchmarks the stub path through the gateway and the
// runtime, booted in-process by the integration harness over the same TLS and
// mutual TLS a deployment uses. cmd/bench records the baseline in
// BASELINE.md; the Benchmark functions expose the same scenarios to go test.
package inference

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"google.golang.org/grpc"

	"github.com/kokou-egbewatt/NeuroMesh/benchmarks/inference/driver"
	neuromeshv1 "github.com/kokou-egbewatt/NeuroMesh/sdk/go/gen/neuromesh/v1"
	"github.com/kokou-egbewatt/NeuroMesh/test/integration/harness"
)

// Model and Prompt are the fixed request the baseline measures: the same one
// the integration golden file records.
const (
	Model  = "llama3"
	Prompt = "hello there world"
)

// requestTimeout bounds one request so a hung call fails the run instead of
// stalling it.
const requestTimeout = 10 * time.Second

// Scenario is one measurable operation plus what it holds open.
type Scenario struct {
	Name  string
	Op    driver.Op
	Close func()
}

// Route posts to the gateway's /v1/route over HTTPS: the full unary path, edge
// validation, the HTTP to gRPC hop over mutual TLS, the runtime and back.
// The transport keeps one idle connection per worker so the measured phase
// reuses warm TLS connections instead of handshaking.
func Route(s *harness.Stack, concurrency int) Scenario {
	tr := s.Transport(concurrency)
	c := &http.Client{Transport: tr, Timeout: requestTimeout}
	url := s.GatewayURL + "/v1/route"
	body := fmt.Appendf(nil, `{"model":%q,"prompt":%q}`, Model, Prompt)
	op := func(ctx context.Context) (driver.Sample, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			return driver.Sample{}, err
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := c.Do(req)
		if err != nil {
			return driver.Sample{}, err
		}
		defer func() { _ = resp.Body.Close() }()
		// Read to the end so the connection goes back to the pool.
		if _, err := io.Copy(io.Discard, resp.Body); err != nil {
			return driver.Sample{}, err
		}
		if resp.StatusCode != http.StatusOK {
			return driver.Sample{}, fmt.Errorf("route: status %d", resp.StatusCode)
		}
		return driver.Sample{}, nil
	}
	return Scenario{Name: "route", Op: op, Close: tr.CloseIdleConnections}
}

// Stream calls StreamInference on the runtime directly through the Go SDK,
// presenting the gateway's client certificate: time to first chunk and the
// whole stream. The gateway gains a streaming endpoint in #4; until then this
// is the only streaming surface.
func Stream(s *harness.Stack) (Scenario, error) {
	c, err := s.SDKClient()
	if err != nil {
		return Scenario{}, err
	}
	op := func(ctx context.Context) (driver.Sample, error) {
		ctx, cancel := context.WithTimeout(ctx, requestTimeout)
		defer cancel()
		t0 := driver.Now()
		stream, err := c.StreamInference(ctx, &neuromeshv1.StreamRequest{Model: Model, Prompt: Prompt})
		if err != nil {
			return driver.Sample{}, err
		}
		return drain(stream, t0)
	}
	return Scenario{Name: "stream", Op: op, Close: func() { _ = c.Close() }}, nil
}

// drain reads a stream to the end, timing the first chunk from t0, a driver.Now reading.
func drain(stream grpc.ServerStreamingClient[neuromeshv1.StreamChunk], t0 int64) (driver.Sample, error) {
	var sample driver.Sample
	for {
		chunk, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return driver.Sample{}, err
		}
		if sample.Chunks == 0 {
			sample.FirstChunk = driver.Since(t0)
		}
		sample.Chunks++
		if e := chunk.GetError(); e != "" {
			return driver.Sample{}, fmt.Errorf("stream: runtime error chunk: %s", e)
		}
	}
	if sample.Chunks == 0 {
		return driver.Sample{}, errors.New("stream: no chunks")
	}
	return sample, nil
}
