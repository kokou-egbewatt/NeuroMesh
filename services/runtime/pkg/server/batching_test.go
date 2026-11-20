package server

import (
	"context"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/proto"

	"github.com/kokou-egbewatt/NeuroMesh/packages/config"
	"github.com/kokou-egbewatt/NeuroMesh/packages/telemetry"
	neuromeshv1 "github.com/kokou-egbewatt/NeuroMesh/sdk/go/gen/neuromesh/v1"
)

func TestBatchingWithNoCapFailsAtStartup(t *testing.T) {
	_, err := New(Config{TLS: config.TLS{Insecure: true}, Batching: BatchingConfig{Enabled: true}}, quiet)
	if err == nil || !strings.Contains(err.Error(), "batching.max_in_flight") {
		t.Fatalf("got %v, want an error naming batching.max_in_flight", err)
	}
}

// Off, a bad cap is never read.
func TestBatchingOffIgnoresTheCap(t *testing.T) {
	if _, err := New(Config{TLS: config.TLS{Insecure: true}, Batching: BatchingConfig{MaxQueue: -1}}, quiet); err != nil {
		t.Fatal(err)
	}
}

func TestBatchingRegistersItsMetrics(t *testing.T) {
	s, err := New(Config{
		TLS: config.TLS{Insecure: true}, Metrics: telemetry.Config{Enabled: true, Addr: "127.0.0.1:0"},
		Batching: BatchingConfig{Enabled: true, MaxInFlight: 3, MaxQueue: 2},
	}, quiet)
	if err != nil {
		t.Fatal(err)
	}
	mfs, err := s.Metrics().Gatherer().Gather()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, mf := range mfs {
		if mf.GetName() == "neuromesh_backend_max_in_flight" {
			found = mf.GetMetric()[0].GetGauge().GetValue() == 3
		}
	}
	if !found {
		t.Error("neuromesh_backend_max_in_flight 3 not exported")
	}
}

// The same prompt gives the same bytes with batching on and off.
func TestBatchingDoesNotChangeTheResponse(t *testing.T) {
	answer := func(b BatchingConfig) []byte {
		addr, _ := start(t, Config{TLS: config.TLS{Insecure: true}, Batching: b})
		conn := dial(t, addr, insecure.NewCredentials())
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		resp, err := neuromeshv1.NewInferenceServiceClient(conn).Route(ctx, &neuromeshv1.RouteRequest{
			RequestId: "fixture", Model: "m", Prompt: "the fixed prompt",
			Params: &neuromeshv1.GenerationParams{MaxTokens: 16, Temperature: 0.5},
		})
		if err != nil {
			t.Fatal(err)
		}
		out, err := proto.MarshalOptions{Deterministic: true}.Marshal(resp)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	off := answer(BatchingConfig{})
	on := answer(BatchingConfig{Enabled: true, MaxInFlight: 1})
	if string(off) != string(on) {
		t.Errorf("batching changed the response:\noff %x\non  %x", off, on)
	}
}
