//go:build cluster

// The golden path through the cluster-deployed services (#14): the same request and golden file as
// TestGoldenRouteOverHTTPSAndMutualTLS, sent to the gateway behind the local edge instead of the
// in-process harness. `task test:cluster` runs it after `task svc:up`.
//
//	NEUROMESH_GATEWAY_URL  default https://gateway.neuromesh.localhost
//	NEUROMESH_CA           default ../../certs/dev/ca.crt (the dev CA the edge's certificate is from)
package integration

import (
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"os"
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
