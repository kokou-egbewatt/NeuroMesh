// Package httpapi is the gateway's public HTTP surface: request IDs, size
// limits, validation, the one JSON error shape, and the gRPC to HTTP status
// mapping. It holds no transport or listener concerns; pkg/server does.
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/kokou-egbewatt/NeuroMesh/services/gateway/internal/router"
	neuromeshv1 "github.com/kokou-egbewatt/NeuroMesh/sdk/go/gen/neuromesh/v1"
)

// Limits bounds what the edge accepts. Zero values mean the defaults, never
// unlimited.
type Limits struct {
	MaxBodyBytes   int64 `yaml:"max_body_bytes"`
	MaxPromptBytes int   `yaml:"max_prompt_bytes"`
	// UpstreamTimeout bounds one call to the runtime.
	UpstreamTimeout time.Duration `yaml:"upstream_timeout"`
}

// Defaults fills unset fields.
func (l *Limits) Defaults() {
	if l.MaxBodyBytes <= 0 {
		l.MaxBodyBytes = 1 << 20
	}
	if l.MaxPromptBytes <= 0 {
		l.MaxPromptBytes = 512 << 10
	}
	if l.UpstreamTimeout <= 0 {
		l.UpstreamTimeout = 30 * time.Second
	}
}

const (
	requestIDHeader = "X-Request-Id"
	maxModelLen     = 128
	maxStopEntries  = 8
	maxStopLen      = 64
)

var (
	requestIDPattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)
	modelPattern     = regexp.MustCompile(`^[A-Za-z0-9._:/-]+$`)
)

// Handler serves the gateway's HTTP API.
type Handler struct {
	router router.Router
	limits Limits
	log    *slog.Logger
	mux    *http.ServeMux
}

// New builds the handler.
func New(r router.Router, limits Limits, log *slog.Logger) *Handler {
	limits.Defaults()
	h := &Handler{router: r, limits: limits, log: log, mux: http.NewServeMux()}
	h.mux.HandleFunc("/healthz", h.healthz)
	h.mux.HandleFunc("/v1/route", h.route)
	h.mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, r, http.StatusNotFound, apiError{Code: "not_found", Message: "no such endpoint"})
	})
	return h
}

// ServeHTTP assigns the request ID, serves, and writes one access log line.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	id, fromClient := r.Header.Get(requestIDHeader), true
	if !requestIDPattern.MatchString(id) {
		id, fromClient = uuid.NewString(), false
	}
	ctx := context.WithValue(r.Context(), ridKey{}, &requestID{id: id, fromClient: fromClient})
	rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
	rec.Header().Set(requestIDHeader, id)
	h.mux.ServeHTTP(rec, r.WithContext(ctx))
	h.log.LogAttrs(ctx, slog.LevelInfo, "http",
		slog.String("method", r.Method),
		slog.String("http_route", routeTemplate(r.URL.Path)),
		slog.Int("status", rec.status),
		slog.String("request_id", ridFrom(ctx).id),
		slog.Int64("duration_ms", time.Since(start).Milliseconds()))
}

func (h *Handler) healthz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		methodNotAllowed(w, r, "GET, HEAD")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

type routeRequest struct {
	RequestID   string   `json:"request_id"`
	Model       string   `json:"model"`
	Prompt      string   `json:"prompt"`
	Temperature float32  `json:"temperature"`
	MaxTokens   int32    `json:"max_tokens"`
	Stop        []string `json:"stop"`
}

type routeResponse struct {
	RequestID        string `json:"request_id"`
	Output           string `json:"output"`
	PromptTokens     int32  `json:"prompt_tokens"`
	CompletionTokens int32  `json:"completion_tokens"`
}

func (h *Handler) route(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, r, http.MethodPost)
		return
	}
	var req routeRequest
	if e := h.decode(w, r, &req); e != nil {
		writeError(w, r, e.status, e.apiError)
		return
	}
	rid := ridFrom(r.Context())
	if !rid.fromClient && requestIDPattern.MatchString(req.RequestID) {
		rid.id = req.RequestID
		w.Header().Set(requestIDHeader, rid.id)
	}
	if e := h.validate(&req); e != nil {
		writeError(w, r, http.StatusBadRequest, *e)
		return
	}

	client, err := h.router.Client(req.Model)
	if err != nil {
		h.log.ErrorContext(r.Context(), "no backend for model", "model", req.Model, "err", err, "request_id", rid.id)
		writeError(w, r, http.StatusServiceUnavailable, apiError{Code: "no_backend", Message: "no backend is available for this model"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), h.limits.UpstreamTimeout)
	defer cancel()
	resp, err := client.Route(ctx, &neuromeshv1.RouteRequest{
		RequestId: rid.id,
		Model:     req.Model,
		Prompt:    req.Prompt,
		Params: &neuromeshv1.GenerationParams{
			Temperature: req.Temperature,
			MaxTokens:   req.MaxTokens,
			Stop:        req.Stop,
		},
	})
	if err != nil {
		st, _ := status.FromError(err)
		h.log.WarnContext(r.Context(), "upstream call failed", "model", req.Model, "code", st.Code().String(), "detail", st.Message(), "request_id", rid.id)
		code, e := mapUpstream(st.Code())
		writeError(w, r, code, e)
		return
	}
	writeJSON(w, http.StatusOK, routeResponse{
		RequestID:        resp.GetRequestId(),
		Output:           resp.GetOutput(),
		PromptTokens:     resp.GetUsage().GetPromptTokens(),
		CompletionTokens: resp.GetUsage().GetCompletionTokens(),
	})
}

type decodeError struct {
	status int
	apiError
}

// decode reads at most MaxBodyBytes and requires exactly one JSON object.
func (h *Handler) decode(w http.ResponseWriter, r *http.Request, dst any) *decodeError {
	body := http.MaxBytesReader(w, r.Body, h.limits.MaxBodyBytes)
	dec := json.NewDecoder(body)
	err := dec.Decode(dst)
	if err == nil {
		// Trailing data after the object is a malformed request, not a second one.
		if _, extra := dec.Token(); extra != io.EOF {
			err = errors.New("trailing data")
		}
	}
	if err == nil {
		return nil
	}
	var tooBig *http.MaxBytesError
	switch {
	case errors.As(err, &tooBig):
		return &decodeError{http.StatusRequestEntityTooLarge, apiError{Code: "body_too_large", Message: "request body exceeds the limit", Limit: h.limits.MaxBodyBytes}}
	case errors.Is(err, io.EOF):
		return &decodeError{http.StatusBadRequest, apiError{Code: "empty_body", Message: "request body is empty"}}
	default:
		return &decodeError{http.StatusBadRequest, apiError{Code: "invalid_json", Message: "request body is not a single valid JSON object"}}
	}
}

func (h *Handler) validate(req *routeRequest) *apiError {
	switch {
	case req.Model == "":
		return &apiError{Code: "missing_field", Field: "model", Message: "model is required"}
	case len(req.Model) > maxModelLen:
		return &apiError{Code: "field_too_large", Field: "model", Message: "model name is too long", Limit: maxModelLen}
	case !modelPattern.MatchString(req.Model):
		return &apiError{Code: "invalid_field", Field: "model", Message: "model may contain only letters, digits and . _ : / -"}
	case req.Prompt == "":
		return &apiError{Code: "missing_field", Field: "prompt", Message: "prompt is required"}
	case len(req.Prompt) > h.limits.MaxPromptBytes:
		return &apiError{Code: "field_too_large", Field: "prompt", Message: "prompt exceeds the limit", Limit: int64(h.limits.MaxPromptBytes)}
	case !utf8.ValidString(req.Prompt):
		return &apiError{Code: "invalid_field", Field: "prompt", Message: "prompt must be valid UTF-8"}
	case req.Temperature < 0 || req.Temperature > 2:
		return &apiError{Code: "invalid_field", Field: "temperature", Message: "temperature must be between 0 and 2"}
	case req.MaxTokens < 0:
		return &apiError{Code: "invalid_field", Field: "max_tokens", Message: "max_tokens must not be negative"}
	case len(req.Stop) > maxStopEntries:
		return &apiError{Code: "field_too_large", Field: "stop", Message: "too many stop sequences", Limit: maxStopEntries}
	}
	for _, s := range req.Stop {
		if len(s) > maxStopLen {
			return &apiError{Code: "field_too_large", Field: "stop", Message: "stop sequence is too long", Limit: maxStopLen}
		}
	}
	return nil
}

// mapUpstream turns a runtime status into the client-facing status and error.
// The upstream message is logged by the caller and never returned: it names
// internal addresses and backend details.
func mapUpstream(c codes.Code) (int, apiError) {
	switch c {
	case codes.InvalidArgument, codes.FailedPrecondition, codes.OutOfRange:
		return http.StatusBadRequest, apiError{Code: "invalid_request", Message: "the backend rejected the request"}
	case codes.NotFound:
		return http.StatusNotFound, apiError{Code: "model_not_found", Message: "the model is not available"}
	case codes.ResourceExhausted:
		return http.StatusTooManyRequests, apiError{Code: "backend_saturated", Message: "the backend is saturated or the request is too large"}
	case codes.Unauthenticated:
		return http.StatusBadGateway, apiError{Code: "upstream_auth", Message: "the gateway could not authenticate to the backend"}
	case codes.PermissionDenied:
		return http.StatusForbidden, apiError{Code: "forbidden", Message: "the request is not permitted"}
	case codes.Unimplemented:
		return http.StatusNotImplemented, apiError{Code: "not_implemented", Message: "the backend does not support this operation"}
	case codes.Unavailable:
		return http.StatusServiceUnavailable, apiError{Code: "backend_unavailable", Message: "the inference backend is unavailable"}
	case codes.DeadlineExceeded:
		return http.StatusGatewayTimeout, apiError{Code: "backend_timeout", Message: "the inference backend did not answer in time"}
	case codes.Canceled:
		return 499, apiError{Code: "client_closed_request", Message: "the request was canceled"}
	default:
		return http.StatusBadGateway, apiError{Code: "upstream_error", Message: "the inference backend failed"}
	}
}

func routeTemplate(path string) string {
	switch path {
	case "/healthz", "/v1/route":
		return path
	}
	return "unmatched"
}

type apiError struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id"`
	Field     string `json:"field,omitempty"`
	Limit     int64  `json:"limit,omitempty"`
}

func methodNotAllowed(w http.ResponseWriter, r *http.Request, allow string) {
	w.Header().Set("Allow", allow)
	writeError(w, r, http.StatusMethodNotAllowed, apiError{Code: "method_not_allowed", Message: "use " + strings.ReplaceAll(allow, ", ", " or ")})
}

func writeError(w http.ResponseWriter, r *http.Request, code int, e apiError) {
	e.RequestID = ridFrom(r.Context()).id
	writeJSON(w, code, struct {
		Error apiError `json:"error"`
	}{e})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

type ridKey struct{}

type requestID struct {
	id         string
	fromClient bool
}

func ridFrom(ctx context.Context) *requestID {
	if v, ok := ctx.Value(ridKey{}).(*requestID); ok {
		return v
	}
	return &requestID{}
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}
