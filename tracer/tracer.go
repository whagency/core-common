package tracer

import (
	"context"
	"fmt"
	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.38.0"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc/credentials"
)

var (
	tp           *sdktrace.TracerProvider
	globalTracer trace.Tracer
	initOnce     sync.Once // ensures Init is only called  once
	initErr      error     // stores initialization error if any
)

// Init configures the global logger. Safe for concurrent calls; only first invocation takes effect.
func Init(cfg Config) error {
	initOnce.Do(func() {
		initErr = validateAndSetConfig(cfg)
		if initErr != nil {
			return
		}
		// Build from the normalized package config, not the caller's struct.
		// validateAndSetConfig takes cfg by value, so its defaulting — protocol,
		// HTTP path, sample ratio clamping, batch limits — only ever reached the
		// package-level config. Passing cfg here meant a hand-constructed Config
		// bypassed all of it.
		initErr = buildTracer(config)
	})
	return initErr
}

// buildTracer creates and configures the tracer provider.
func buildTracer(cfg Config) error {
	if !cfg.Enable {
		return nil
	}

	exp, err := newExporter(cfg, cfg.Protocol, cfg.Endpoint, cfg.HTTPPath)
	if err != nil {
		return fmt.Errorf("[tracer][Initialize]%w", err)
	}

	res, err := resource.New(context.Background(),
		resource.WithAttributes(
			semconv.ServiceNameKey.String(cfg.AppName),
			semconv.ServiceVersionKey.String(cfg.Version),
			semconv.DeploymentEnvironmentNameKey.String(cfg.Environment),
		),
	)
	if err != nil {
		_ = exp.Shutdown(context.Background())
		return fmt.Errorf("[tracer][Initialize][err: create resource: %w]", err)
	}

	tp = sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exp, batcherOptions(cfg)...),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(
			sdktrace.ParentBased(sdktrace.TraceIDRatioBased(cfg.SampleRatio)),
		),
	)

	otel.SetTracerProvider(tp)

	// W3C TraceContext + Baggage propagation
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	globalTracer = tp.Tracer(cfg.AppName)

	return nil
}

// batcherOptions builds BatchSpanProcessor options from Config, applying only
// values the caller explicitly set. Anything left unset falls through to the
// OTel SDK's own defaults (BatchTimeout: 5s, MaxExportBatchSize: 512).
func batcherOptions(cfg Config) []sdktrace.BatchSpanProcessorOption {
	var opts []sdktrace.BatchSpanProcessorOption
	if cfg.BatchTimeout > 0 {
		opts = append(opts, sdktrace.WithBatchTimeout(cfg.BatchTimeout))
	}
	if cfg.MaxExportBatchSize > 0 {
		opts = append(opts, sdktrace.WithMaxExportBatchSize(cfg.MaxExportBatchSize))
	}
	return opts
}

// newExporter creates a new exporter based on the protocol.
func newExporter(cfg Config, protocol, endpoint, httpPath string) (*otlptrace.Exporter, error) {
	if cfg.TLSEnabled {
		tlsCfg, err := buildTLSConfig(cfg)
		if err != nil {
			return nil, fmt.Errorf("[newExporter][err: build tls config: %w]", err)
		}

		switch protocol {
		case "http":
			return otlptracehttp.New(context.Background(),
				otlptracehttp.WithEndpoint(endpoint),
				otlptracehttp.WithURLPath(httpPath),
				otlptracehttp.WithTLSClientConfig(tlsCfg),
			)
		case "grpc":
			return otlptracegrpc.New(context.Background(),
				otlptracegrpc.WithEndpoint(endpoint),
				otlptracegrpc.WithTLSCredentials(credentials.NewTLS(tlsCfg)),
			)
		default:
			return nil, fmt.Errorf("[newExporter][err: unsupported protocol %q]", protocol)
		}
	}
	switch protocol {
	case "http":
		// endpoint must be host:port, and path must be /v1/traces for Jaeger OTLP HTTP
		return otlptracehttp.New(context.Background(),
			otlptracehttp.WithEndpoint(endpoint),
			otlptracehttp.WithURLPath(httpPath),
			otlptracehttp.WithInsecure(),
		)
	case "grpc":
		return otlptracegrpc.New(context.Background(),
			otlptracegrpc.WithEndpoint(endpoint),
			otlptracegrpc.WithInsecure(),
		)
	default:
		return nil, fmt.Errorf("[newExporter][err: unsupported protocol %q]", protocol)
	}
}

// Enabled returns true if the tracer is enabled.
func Enabled() bool { return globalTracer != nil }

// Tracer returns the tracer instance.
func Tracer() trace.Tracer { return globalTracer }

// Shutdown gracefully shuts down the tracer provider.
func Shutdown(ctx context.Context) error {
	if tp == nil {
		return nil
	}
	return tp.Shutdown(ctx)
}
