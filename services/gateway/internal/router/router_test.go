package router

import (
	"testing"

	neuromeshv1 "github.com/kokou-egbewatt/NeuroMesh/sdk/go/gen/neuromesh/v1"
)

type fakeClient struct {
	neuromeshv1.InferenceServiceClient
	id int
}

// conformance runs against any Router so later implementations reuse the table.
func conformance(t *testing.T, r Router, want neuromeshv1.InferenceServiceClient) {
	t.Helper()
	for i := range 100 {
		got, err := r.Client([]string{"a", "b", ""}[i%3])
		if err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
		if got != want {
			t.Fatalf("call %d returned a different client", i)
		}
	}
}

func TestStaticReturnsTheSameClient(t *testing.T) {
	c := &fakeClient{id: 1}
	r, err := NewStatic(c)
	if err != nil {
		t.Fatal(err)
	}
	conformance(t, r, c)
}

func TestStaticRejectsNilClient(t *testing.T) {
	if _, err := NewStatic(nil); err == nil {
		t.Fatal("expected an error for a nil client")
	}
}
