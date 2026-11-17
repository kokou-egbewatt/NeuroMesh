//go:build cluster

// The golden path through the cluster-deployed services (#14): the same request and golden file as
// TestGoldenRouteOverHTTPSAndMutualTLS, sent to the gateway behind the local edge instead of the
// in-process harness. `task test:cluster` runs it after `task svc:up`.
//
//	NEUROMESH_GATEWAY_URL  default https://gateway.neuromesh.localhost
//	NEUROMESH_CA           default ../../certs/dev/ca.crt (the dev CA the edge's certificate is from)
package integration

import (
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

func clusterClient(t *testing.T) (*http.Client, string) {
	t.Helper()
	url := os.Getenv("NEUROMESH_GATEWAY_URL")
	if url == "" {
		url = "https://gateway.neuromesh.localhost"
	}
	caPath := os.Getenv("NEUROMESH_CA")
	if caPath == "" {
		caPath = "../../certs/dev/ca.crt"
	}
	pem, err := os.ReadFile(caPath)
	if err != nil {
		t.Fatalf("%v (task certs:dev, or set NEUROMESH_CA)", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pem) {
		t.Fatalf("no certificate in %s", caPath)
	}
	return &http.Client{
		Timeout:   10 * time.Second,
		Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}},
	}, url
}

func TestClusterGoldenRoute(t *testing.T) {
	if b := os.Getenv("NEUROMESH_BACKEND"); b != "" && b != "stub" {
		t.Skipf("the golden file is the stub's answer; the runtime is on %s", b)
	}
	c, url := clusterClient(t)
	r := post(t, c, url+"/v1/route", "golden-1", `{"model":"llama3","prompt":"hello there world"}`)
	if r.status != http.StatusOK {
		t.Fatalf("status %d: %s", r.status, r.body)
	}
	golden(t, "route.json", r.body)
}

func TestClusterHealthz(t *testing.T) {
	c, url := clusterClient(t)
	resp, err := c.Get(url + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("healthz %d", resp.StatusCode)
	}
}

// One request across the observability stack (#10, #22): its log lines in VictoriaLogs from both
// services, joined on one trace_id; that trace in Tempo, through Grafana; and its count in
// VictoriaMetrics through /prometheus/api/v1/query. Needs `task obs:up` and `task svc:up`, and
// GRAFANA_PASSWORD (task test:cluster reads it from the cluster).
func TestClusterOneRequestAcrossTheStack(t *testing.T) {
	c, gatewayURL := clusterClient(t)
	pw := os.Getenv("GRAFANA_PASSWORD")
	if pw == "" {
		t.Skip("GRAFANA_PASSWORD unset: run through task test:cluster")
	}
	domain := os.Getenv("NEUROMESH_DOMAIN")
	if domain == "" {
		domain = "neuromesh.localhost"
	}
	c.Timeout = 90 * time.Second // on Ollama, the first request may load the model
	rid := fmt.Sprintf("stack-%d", time.Now().UnixNano())
	// A sampled traceparent of our own: the services sample 10% of new traces but always follow a
	// parent's decision, so this request is traced, under a trace ID known in advance.
	var tid [16]byte
	var sid [8]byte
	_, _ = rand.Read(tid[:])
	_, _ = rand.Read(sid[:])
	want := hex.EncodeToString(tid[:])
	req, _ := http.NewRequest(http.MethodPost, gatewayURL+"/v1/route", strings.NewReader(`{"model":"`+routeModel()+`","prompt":"hello there world","max_tokens":8}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-Id", rid)
	req.Header.Set("traceparent", "00-"+want+"-"+hex.EncodeToString(sid[:])+"-01")
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}

	// The gateway's access line, found by request_id, names the trace.
	logs := func(query string) []map[string]any {
		u := "https://victorialogs." + domain + "/select/logsql/query?query=" + url.QueryEscape(query)
		resp, err := c.Get(u)
		if err != nil {
			return nil
		}
		defer func() { _ = resp.Body.Close() }()
		var out []map[string]any
		dec := json.NewDecoder(resp.Body)
		for {
			var line map[string]any
			if dec.Decode(&line) != nil {
				return out
			}
			out = append(out, line)
		}
	}
	var traceID string
	eventually(t, 90*time.Second, "the gateway's log line with trace_id in VictoriaLogs", func() bool {
		for _, l := range logs(`request_id:"` + rid + `" service:gateway`) {
			if id, _ := l["trace_id"].(string); id != "" {
				traceID = id
				return true
			}
		}
		return false
	})
	if traceID != want {
		t.Fatalf("the gateway logged trace %s, want the client's %s", traceID, want)
	}
	eventually(t, 90*time.Second, "a runtime log line on the same trace", func() bool {
		return len(logs(`trace_id:"`+traceID+`" service:runtime`)) > 0
	})

	eventually(t, 90*time.Second, "the trace in Tempo through Grafana", func() bool {
		req, _ := http.NewRequest(http.MethodGet, "https://grafana."+domain+"/api/datasources/proxy/uid/tempo/api/traces/"+traceID, nil)
		req.SetBasicAuth("admin", pw)
		resp, err := c.Do(req)
		if err != nil {
			return false
		}
		defer func() { _ = resp.Body.Close() }()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode == http.StatusOK && strings.Contains(string(b), "runtime") && strings.Contains(string(b), "gateway")
	})

	eventually(t, 120*time.Second, "the request count in VictoriaMetrics", func() bool {
		q := `sum(neuromesh_requests_total{service="gateway",rpc="/v1/route",code="200"})`
		resp, err := c.Get("https://vmsingle." + domain + "/prometheus/api/v1/query?query=" + url.QueryEscape(q))
		if err != nil {
			return false
		}
		defer func() { _ = resp.Body.Close() }()
		var body struct {
			Data struct {
				Result []struct {
					Value []any `json:"value"`
				} `json:"result"`
			} `json:"data"`
		}
		if json.NewDecoder(resp.Body).Decode(&body) != nil || len(body.Data.Result) == 0 {
			return false
		}
		v, _ := body.Data.Result[0].Value[1].(string)
		return v != "" && v != "0"
	})
}

func eventually(t *testing.T, within time.Duration, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(within)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("not within %s: %s", within, what)
		}
		time.Sleep(3 * time.Second)
	}
}
