// Package telemetry holds the request metrics every NeuroMesh service exposes in Prometheus format on
// an admin /metrics endpoint, which vmagent scrapes through the chart's VMServiceScrape (#10).
//
// Labels are service, rpc and code only: rpc is a route template or a full gRPC method, never a raw
// path, and nothing from a request's content becomes a label, so cardinality stays fixed.
package telemetry

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Config is the metrics section of a service's config file.
type Config struct {
	Enabled bool `yaml:"enabled"`
	// Addr is where /metrics listens, apart from the service's own port so it needs no TLS and no
	// client certificate. Default :9090.
	Addr string `yaml:"addr"`
}

// Metrics records requests for one service.
type Metrics struct {
	service  string
	reg      *prometheus.Registry
	requests *prometheus.CounterVec
	errors   *prometheus.CounterVec
	duration *prometheus.HistogramVec
}

// New registers the request metrics for service, plus the Go and process collectors.
func New(service string) *Metrics {
	reg := prometheus.NewRegistry()
	m := &Metrics{
		service: service,
		reg:     reg,
		requests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "neuromesh_requests_total",
			Help: "Requests handled, by service, rpc (route template or gRPC method) and status code.",
		}, []string{"service", "rpc", "code"}),
		errors: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "neuromesh_request_errors_total",
			Help: "Requests that failed, by service, rpc and status code.",
		}, []string{"service", "rpc", "code"}),
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "neuromesh_request_duration_seconds",
			Help:    "Request handling time, by service and rpc.",
			Buckets: []float64{.0005, .001, .0025, .005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10, 30},
		}, []string{"service", "rpc"}),
	}
	reg.MustRegister(m.requests, m.errors, m.duration,
		collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	return m
}

// Observe records one request. failed decides whether it also counts as an error, so each service
// keeps its own rule for which codes are failures.
func (m *Metrics) Observe(rpc, code string, d time.Duration, failed bool) {
	if m == nil {
		return
	}
	m.requests.WithLabelValues(m.service, rpc, code).Inc()
	m.duration.WithLabelValues(m.service, rpc).Observe(d.Seconds())
	if failed {
		m.errors.WithLabelValues(m.service, rpc, code).Inc()
	}
}

// Register adds a service's own collectors, such as the runtime's backend cap (#2). Does nothing when
// metrics are off.
func (m *Metrics) Register(cs ...prometheus.Collector) {
	if m == nil {
		return
	}
	m.reg.MustRegister(cs...)
}

// Handler serves the registry in Prometheus text format.
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.reg, promhttp.HandlerOpts{Registry: m.reg})
}

// Gatherer exposes the registry to tests.
func (m *Metrics) Gatherer() prometheus.Gatherer { return m.reg }

// Serve serves /metrics on cfg.Addr until ctx is done. It returns nil, and does nothing, when
// metrics are off.
func Serve(ctx context.Context, cfg Config, m *Metrics, log *slog.Logger) error {
	if !cfg.Enabled || m == nil {
		return nil
	}
	addr := cfg.Addr
	if addr == "" {
		addr = ":9090"
	}
	var lc net.ListenConfig
	lis, err := lc.Listen(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("metrics: listen on %s: %w", addr, err)
	}
	mux := http.NewServeMux()
	mux.Handle("/metrics", m.Handler())
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(sctx)
	}()
	log.Info("metrics listening", "addr", lis.Addr().String())
	if err := srv.Serve(lis); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
