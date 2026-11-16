// Package tracing sets up OpenTelemetry tracing for a NeuroMesh service: a TracerProvider that
// exports spans by OTLP to the collector gateway, and the W3C trace-context propagator that carries
// a trace across the gateway to runtime hop. With no endpoint it exports nothing and costs nothing
// beyond the no-op API (#10).
package tracing

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
)

// Config is the otel section of a service's config file.
type Config struct {
	// Endpoint is the collector gateway's OTLP gRPC address (host:port). Empty turns tracing off.
	Endpoint string `yaml:"endpoint"`
	// Insecure sends OTLP without TLS, as the in-cluster collector gateway expects.
	Insecure bool `yaml:"insecure"`
	// SampleRatio is the share of new traces kept, 0 to 1. Zero means 1: keep everything, the
	// right default at local volume. A parent's decision is always followed.
	SampleRatio float64 `yaml:"sample_ratio"`
}

// Shutdown flushes and stops what Setup started.
type Shutdown func(context.Context) error

// Setup installs the global TracerProvider and propagator for service at version. The propagator is
// installed even when tracing is off, so a service in the middle of a call still passes a trace on.
func Setup(ctx context.Context, service, version string, cfg Config) (Shutdown, error) {
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))
	if cfg.Endpoint == "" {
		return func(context.Context) error { return nil }, nil
	}
	opts := []otlptracegrpc.Option{otlptracegrpc.WithEndpoint(cfg.Endpoint)}
	if cfg.Insecure {
		opts = append(opts, otlptracegrpc.WithInsecure())
	}
	exp, err := otlptracegrpc.New(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("tracing: otlp exporter: %w", err)
	}
	tp, err := NewProvider(service, version, cfg.SampleRatio, sdktrace.WithBatcher(exp, sdktrace.WithBatchTimeout(2*time.Second)))
	if err != nil {
		return nil, err
	}
	otel.SetTracerProvider(tp)
	return tp.Shutdown, nil
}

// NewProvider builds a TracerProvider for service with the given span processors. Tests pass a
// synchronous processor over an in-memory exporter.
func NewProvider(service, version string, sampleRatio float64, opts ...sdktrace.TracerProviderOption) (*sdktrace.TracerProvider, error) {
	if sampleRatio < 0 || sampleRatio > 1 {
		return nil, errors.New("tracing: sample_ratio must be between 0 and 1")
	}
	if sampleRatio == 0 {
		sampleRatio = 1
	}
	res, err := resource.Merge(resource.Default(), resource.NewSchemaless(
		semconv.ServiceName(service),
		semconv.ServiceVersion(version),
	))
	if err != nil {
		return nil, fmt.Errorf("tracing: resource: %w", err)
	}
	return sdktrace.NewTracerProvider(append([]sdktrace.TracerProviderOption{
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(sampleRatio))),
	}, opts...)...), nil
}
