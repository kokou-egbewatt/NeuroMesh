package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/kokou-egbewatt/NeuroMesh/services/gateway/internal/router"
	neuromeshv1 "github.com/kokou-egbewatt/NeuroMesh/sdk/go/gen/neuromesh/v1"
)

// fakeClient records calls and returns a scripted result. No sockets.
type fakeClient struct {
	neuromeshv1.InferenceServiceClient
	calls int
	last  *neuromeshv1.RouteRequest
	err   error
}

func (f *fakeClient) Route(_ context.Context, in *neuromeshv1.RouteRequest, _ ...grpc.CallOption) (*neuromeshv1.RouteResponse, error) {
	f.calls++
	f.last = in
	if f.err != nil {
		return nil, f.err
	}
	return &neuromeshv1.RouteResponse{RequestId: in.GetRequestId(), Output: "out:" + in.GetModel(), Usage: &neuromeshv1.Usage{PromptTokens: 2, CompletionTokens: 3}}, nil
}

func newHandler(t *testing.T, fc *fakeClient) *Handler {
	t.Helper()
	r, err := router.NewStatic(fc)
	if err != nil {
		t.Fatal(err)
	}
	return New(r, Limits{MaxBodyBytes: 256, MaxPromptBytes: 64}, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

type errBody struct {
	Error struct {
		Code      string `json:"code"`
		Message   string `json:"message"`
		RequestID string `json:"request_id"`
		Field     string `json:"field"`
		Limit     int64  `json:"limit"`
	} `json:"error"`
}

func TestRouteTable(t *testing.T) {
	cases := []struct {
		name       string
		method     string
		path       string
		body       string
		upstream   error
		wantStatus int
		wantCode   string
		wantField  string
		wantCalls  int
	}{
		{name: "ok", method: "POST", path: "/v1/route", body: `{"model":"llama3","prompt":"hi"}`, wantStatus: 200, wantCalls: 1},
		{name: "get is 405", method: "GET", path: "/v1/route", wantStatus: 405, wantCode: "method_not_allowed"},
		{name: "unknown path", method: "GET", path: "/v2/nothing", wantStatus: 404, wantCode: "not_found"},
		{name: "malformed json", method: "POST", path: "/v1/route", body: `{bad`, wantStatus: 400, wantCode: "invalid_json"},
		{name: "trailing data", method: "POST", path: "/v1/route", body: `{"model":"m","prompt":"p"} {}`, wantStatus: 400, wantCode: "invalid_json"},
		{name: "empty body", method: "POST", path: "/v1/route", body: ``, wantStatus: 400, wantCode: "empty_body"},
		{name: "missing model", method: "POST", path: "/v1/route", body: `{"prompt":"p"}`, wantStatus: 400, wantCode: "missing_field", wantField: "model"},
		{name: "missing prompt", method: "POST", path: "/v1/route", body: `{"model":"m"}`, wantStatus: 400, wantCode: "missing_field", wantField: "prompt"},
		{name: "model with spaces", method: "POST", path: "/v1/route", body: `{"model":"a b","prompt":"p"}`, wantStatus: 400, wantCode: "invalid_field", wantField: "model"},
		{name: "prompt over limit", method: "POST", path: "/v1/route", body: `{"model":"m","prompt":"` + strings.Repeat("a", 65) + `"}`, wantStatus: 400, wantCode: "field_too_large", wantField: "prompt"},
		{name: "prompt at limit", method: "POST", path: "/v1/route", body: `{"model":"m","prompt":"` + strings.Repeat("a", 64) + `"}`, wantStatus: 200, wantCalls: 1},
		{name: "body over limit", method: "POST", path: "/v1/route", body: `{"model":"m","prompt":"` + strings.Repeat("a", 300) + `"}`, wantStatus: 413, wantCode: "body_too_large"},
		{name: "temperature out of range", method: "POST", path: "/v1/route", body: `{"model":"m","prompt":"p","temperature":3}`, wantStatus: 400, wantCode: "invalid_field", wantField: "temperature"},
		{name: "negative max tokens", method: "POST", path: "/v1/route", body: `{"model":"m","prompt":"p","max_tokens":-1}`, wantStatus: 400, wantCode: "invalid_field", wantField: "max_tokens"},
		{name: "upstream unavailable", method: "POST", path: "/v1/route", body: `{"model":"m","prompt":"p"}`, upstream: status.Error(codes.Unavailable, "dial tcp 10.0.0.7:50051: refused"), wantStatus: 503, wantCode: "backend_unavailable", wantCalls: 1},
		{name: "upstream resource exhausted", method: "POST", path: "/v1/route", body: `{"model":"m","prompt":"p"}`, upstream: status.Error(codes.ResourceExhausted, "grpc: received message larger than max"), wantStatus: 429, wantCode: "backend_saturated", wantCalls: 1},
		{name: "upstream invalid argument", method: "POST", path: "/v1/route", body: `{"model":"m","prompt":"p"}`, upstream: status.Error(codes.InvalidArgument, "bad"), wantStatus: 400, wantCode: "invalid_request", wantCalls: 1},
		{name: "upstream internal", method: "POST", path: "/v1/route", body: `{"model":"m","prompt":"p"}`, upstream: status.Error(codes.Internal, "panic in adapter at /src/x.go:12"), wantStatus: 502, wantCode: "upstream_error", wantCalls: 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fc := &fakeClient{err: tc.upstream}
			rec := httptest.NewRecorder()
			newHandler(t, fc).ServeHTTP(rec, httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body)))

			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body %s", rec.Code, tc.wantStatus, rec.Body)
			}
			if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
				t.Fatalf("Content-Type = %q", ct)
			}
			if rec.Header().Get("X-Request-Id") == "" {
				t.Fatal("X-Request-Id missing")
			}
			if fc.calls != tc.wantCalls {
				t.Fatalf("upstream calls = %d, want %d", fc.calls, tc.wantCalls)
			}
			if tc.wantCode == "" {
				return
			}
			var eb errBody
			if err := json.Unmarshal(rec.Body.Bytes(), &eb); err != nil {
				t.Fatalf("error body is not JSON: %v", err)
			}
			if eb.Error.Code != tc.wantCode || eb.Error.Field != tc.wantField {
				t.Fatalf("error = %+v, want code %q field %q", eb.Error, tc.wantCode, tc.wantField)
			}
			if eb.Error.RequestID != rec.Header().Get("X-Request-Id") {
				t.Fatal("error request_id does not match the header")
			}
			if tc.upstream != nil && bytes.Contains(rec.Body.Bytes(), []byte(status.Convert(tc.upstream).Message())) {
				t.Fatal("upstream detail leaked into the response body")
			}
		})
	}
}

func TestRequestIDPrecedence(t *testing.T) {
	fc := &fakeClient{}
	h := newHandler(t, fc)

	req := httptest.NewRequest("POST", "/v1/route", strings.NewReader(`{"model":"m","prompt":"p","request_id":"from-body"}`))
	req.Header.Set("X-Request-Id", "from-header")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if got := rec.Header().Get("X-Request-Id"); got != "from-header" || fc.last.GetRequestId() != "from-header" {
		t.Fatalf("header should win: header %q upstream %q", got, fc.last.GetRequestId())
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/v1/route", strings.NewReader(`{"model":"m","prompt":"p","request_id":"from-body"}`)))
	if got := rec.Header().Get("X-Request-Id"); got != "from-body" || fc.last.GetRequestId() != "from-body" {
		t.Fatalf("body id should be used without a header: %q", got)
	}

	req = httptest.NewRequest("POST", "/v1/route", strings.NewReader(`{"model":"m","prompt":"p"}`))
	req.Header.Set("X-Request-Id", "bad id\nwith newline")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if got := rec.Header().Get("X-Request-Id"); strings.ContainsAny(got, " \n") || got == "" {
		t.Fatalf("an invalid client id must be replaced, got %q", got)
	}
}

func TestHealthz(t *testing.T) {
	h := newHandler(t, &fakeClient{})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/healthz", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"ok"`) {
		t.Fatalf("healthz: %d %s", rec.Code, rec.Body)
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/healthz", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST /healthz: %d", rec.Code)
	}
}

func TestDefaultsNeverMeanUnlimited(t *testing.T) {
	var l Limits
	l.Defaults()
	if l.MaxBodyBytes <= 0 || l.MaxPromptBytes <= 0 || l.UpstreamTimeout <= 0 {
		t.Fatalf("zero limits must become defaults: %+v", l)
	}
}
