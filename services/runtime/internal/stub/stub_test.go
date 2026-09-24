package stub

import (
	"context"
	"testing"

	neuromeshv1 "github.com/kokou-egbewatt/NeuroMesh/sdk/go/gen/neuromesh/v1"
)

func TestRouteEchoesModelAndCountsWords(t *testing.T) {
	resp, err := Server{}.Route(context.Background(), &neuromeshv1.RouteRequest{RequestId: "r1", Model: "llama3", Prompt: "hello there world"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.GetRequestId() != "r1" || resp.GetOutput() != "stub response for model llama3" {
		t.Fatalf("unexpected response: %v", resp)
	}
	if resp.GetUsage().GetPromptTokens() != 3 || resp.GetUsage().GetCompletionTokens() != 5 {
		t.Fatalf("unexpected usage: %v", resp.GetUsage())
	}
}
