package server

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"

	"github.com/kokou-egbewatt/NeuroMesh/packages/config"
)

func TestUnknownBackendKindFailsAtStartup(t *testing.T) {
	_, err := New(Config{TLS: config.TLS{Insecure: true}, Backend: BackendConfig{Kind: "triton"}}, quiet)
	if err == nil || !strings.Contains(err.Error(), "vllm") {
		t.Fatalf("got %v, want an error naming the kind", err)
	}
}

// With backend.kind ollama and no Ollama running, the runtime still boots and reports SERVING (it is
// up; its backend isn't), and Route is Unavailable, never the stub's answer (#1).
func TestOllamaDownBootsAndRouteIsUnavailable(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	gone := l.Addr().String()
	_ = l.Close()
	addr, _ := start(t, Config{TLS: config.TLS{Insecure: true}, Backend: BackendConfig{
		Kind: "ollama", Ollama: HTTPBackend{BaseURL: "http://" + gone, DialTimeout: time.Second},
	}})
	conn := dial(t, addr, insecure.NewCredentials())
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	resp, err := healthpb.NewHealthClient(conn).Check(ctx, &healthpb.HealthCheckRequest{})
	if err != nil || resp.GetStatus() != healthpb.HealthCheckResponse_SERVING {
		t.Fatalf("health: %v %v", resp, err)
	}
	err = route(conn, "hi")
	if status.Code(err) != codes.Unavailable || !strings.Contains(status.Convert(err).Message(), "ollama") {
		t.Fatalf("route: %v, want Unavailable naming ollama", err)
	}
}
