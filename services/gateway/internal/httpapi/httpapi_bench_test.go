package httpapi

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kokou-egbewatt/NeuroMesh/services/gateway/internal/router"
)

// BenchmarkRouteHandler measures the gateway's in-process cost for one
// /v1/route call: body read and bound, JSON decode and validation, request ID,
// the router lookup, the response encode and the access log line (formatted
// as JSON at info, as deployed, into io.Discard). The upstream is a fake, so
// the wire and the runtime are not in this number; benchmarks/inference has
// the end-to-end figure and the difference between the two is the hop.
func BenchmarkRouteHandler(b *testing.B) {
	r, err := router.NewStatic(&fakeClient{})
	if err != nil {
		b.Fatal(err)
	}
	h := New(r, Limits{}, slog.New(slog.NewJSONHandler(io.Discard, nil)))
	const body = `{"model":"llama3","prompt":"hello there world"}`
	b.ReportAllocs()
	for b.Loop() {
		req := httptest.NewRequest(http.MethodPost, "/v1/route", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			b.Fatalf("status %d: %s", rec.Code, rec.Body)
		}
	}
}
