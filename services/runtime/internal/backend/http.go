package backend

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// HTTPConfig points the runtime at a model server over HTTP.
type HTTPConfig struct {
	// BaseURL is the server, e.g. http://ollama:11434.
	BaseURL string
	// DialTimeout bounds connecting; an unreachable server is Unavailable within it.
	DialTimeout time.Duration
	// ResponseHeaderTimeout bounds the wait for the first byte of an answer. A unary generation
	// answers only once it is complete, so this also caps how long one may run, model load included.
	ResponseHeaderTimeout time.Duration
}

// httpServer is what the HTTP adapters share: the base URL, the client, and how failures map onto
// the sentinel errors.
type httpServer struct {
	kind   string
	base   string
	client *http.Client
}

// newHTTPServer checks the URL and builds the client. It does not contact the server: the runtime
// boots whether or not the backend is up, and each request reports Unavailable until it is.
func newHTTPServer(kind string, cfg HTTPConfig) (httpServer, error) {
	u, err := url.Parse(cfg.BaseURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return httpServer{}, fmt.Errorf("backend.%s.base_url %q: want http(s)://host:port", kind, cfg.BaseURL)
	}
	if cfg.DialTimeout <= 0 {
		cfg.DialTimeout = 3 * time.Second
	}
	if cfg.ResponseHeaderTimeout <= 0 {
		cfg.ResponseHeaderTimeout = 60 * time.Second
	}
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.DialContext = (&net.Dialer{Timeout: cfg.DialTimeout, KeepAlive: 30 * time.Second}).DialContext
	t.ResponseHeaderTimeout = cfg.ResponseHeaderTimeout
	t.MaxIdleConnsPerHost = 64
	return httpServer{kind: kind, base: strings.TrimRight(u.String(), "/"), client: &http.Client{Transport: t}}, nil
}

// post sends a JSON body. A transport failure is Unavailable, or the caller's own error when the
// context ended.
func (s httpServer) post(ctx context.Context, path string, body io.Reader) (*http.Response, error) {
	hr, err := http.NewRequestWithContext(ctx, http.MethodPost, s.base+path, body)
	if err != nil {
		return nil, err
	}
	hr.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(hr)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("%s at %s: %w: %w", s.kind, s.base, ErrUnavailable, err)
	}
	return resp, nil
}

// statusErr maps a non-200 answer: 404 is a model the server doesn't have, 400 a request it
// rejected, 502 and 503 a server that can't serve now.
func (s httpServer) statusErr(code int, msg string) error {
	switch code {
	case http.StatusNotFound:
		return fmt.Errorf("%s: %w: %s", s.kind, ErrModelNotFound, msg)
	case http.StatusBadRequest:
		return fmt.Errorf("%s: %w: %s", s.kind, ErrInvalid, msg)
	case http.StatusServiceUnavailable, http.StatusBadGateway:
		return fmt.Errorf("%s at %s: %w: %s", s.kind, s.base, ErrUnavailable, msg)
	}
	return fmt.Errorf("%s: %d: %s", s.kind, code, msg)
}

// readErr is a failure partway through an answer: the caller's cancellation if that is what it was,
// otherwise the server going away.
func (s httpServer) readErr(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) {
		return s.cutOff()
	}
	return fmt.Errorf("%s at %s: %w: %w", s.kind, s.base, ErrUnavailable, err)
}

func (s httpServer) cutOff() error {
	return fmt.Errorf("%s at %s: %w: answer cut off", s.kind, s.base, ErrUnavailable)
}
