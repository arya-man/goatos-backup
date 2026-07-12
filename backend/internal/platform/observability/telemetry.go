package observability

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

// defaultTraceSampleRatio is used when GOATOS_TRACE_SAMPLE_RATIO is unset or
// invalid. It matches the stg default documented in
// docs/observability/OBSERVABILITY_DESIGN.md section 3.
const defaultTraceSampleRatio = 0.1

// DefaultShutdownTimeout bounds the telemetry-flush context every cmd/*
// main() passes when deferring its Shutdown call. Without a deadline, a
// hung/unreachable OTel Collector could block process exit indefinitely -
// fatal for a Cloud Run Job, which must exit promptly after its one-shot work
// completes. Matches the timeout cmd/api/main.go already used explicitly;
// FlushWithTimeout/this constant let every worker main share the same
// defense-in-depth bound via one call instead of hand-rolling
// context.WithTimeout at each of the ~18 call sites.
const DefaultShutdownTimeout = 10 * time.Second

// Shutdown flushes and stops any OTel providers configured by SetupTelemetry.
// It is always non-nil and always safe to call, even when telemetry was
// never activated (no-op in that case).
type Shutdown func(ctx context.Context) error

// noopShutdown is returned whenever SetupTelemetry does not activate real
// OTel export (sink != otlp/gcm, or no endpoint configured). Global
// otel.Tracer/otel.Meter calls remain safe no-ops in that case.
func noopShutdown(context.Context) error { return nil }

// FlushWithTimeout calls shutdown with a context bounded by timeout,
// so a stuck/unreachable OTel Collector cannot block process exit
// indefinitely. Intended for the common
// `defer func() { _ = observability.FlushWithTimeout(shutdown,
// observability.DefaultShutdownTimeout) }()` pattern used by every cmd/*
// main after observability.SetupTelemetry.
func FlushWithTimeout(shutdown Shutdown, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return shutdown(ctx)
}

// SetupTelemetry wires the process-wide OpenTelemetry TracerProvider and
// MeterProvider so every otelhttp/otelpgx/kmetrics instrument created via the
// OTel global APIs (otel.Tracer, otel.Meter) ships real spans/metrics to the
// OTel Collector named by GOATOS_OTLP_ENDPOINT, per
// docs/observability/OBSERVABILITY_DESIGN.md section 2.1-2.3.
//
// The Collector runs as a single-ingress-port Cloud Run service, so it speaks
// OTLP over HTTP (protobuf) only - not gRPC. GOATOS_OTLP_ENDPOINT is the
// collector's base HTTPS URL (e.g. "https://otel-collector-xyz.a.run.app");
// this function appends the standard "/v1/traces" and "/v1/metrics" paths
// itself (matching OTEL_EXPORTER_OTLP_ENDPOINT semantics), so callers must
// NOT include a path.
//
// SetupTelemetry activates only when the resolved sink is "otlp" or "gcm" AND
// an OTLP endpoint is configured. In every other case (local/dev without a
// collector, unset sink, missing endpoint) it leaves the OTel global
// providers as their default no-op implementations and returns a no-op
// shutdown - callers can unconditionally call SetupTelemetry and defer the
// returned shutdown.
//
// SetupTelemetry never returns an error for a reachable-but-misbehaving
// collector: the HTTP exporters connect lazily on first export, so a
// collector that is down at boot does not fail the process. Construction
// errors (e.g. a malformed endpoint) are logged and fall back to the no-op
// providers rather than failing the caller - the collector is a best-effort
// sidecar, not a dependency the API/workers should crash-loop on.
func SetupTelemetry(ctx context.Context, cfg Config) (Shutdown, error) {
	// internalLogCfg suppresses the otlp/gcm sink notice for this throwaway
	// logger: callers overwhelmingly also build their own "real" logger via
	// observability.New(cfg) with the same Config, which already logs that
	// notice once - see the skipSinkNoticeLog doc comment in observability.go.
	internalLogCfg := cfg
	internalLogCfg.skipSinkNoticeLog = true
	log := New(internalLogCfg)

	requestedSink := strings.ToLower(strings.TrimSpace(coalesce(cfg.Sink, os.Getenv("GOATOS_OBS_SINK"), "stdout_json")))
	endpoint := strings.TrimSpace(os.Getenv("GOATOS_OTLP_ENDPOINT"))

	if requestedSink != "otlp" && requestedSink != "gcm" {
		return noopShutdown, nil
	}
	if endpoint == "" {
		log.Warn("otel_telemetry_disabled",
			slog.String("reason", "GOATOS_OTLP_ENDPOINT not set"),
			slog.String("sink", requestedSink),
		)
		return noopShutdown, nil
	}

	res, err := buildResource(ctx, cfg)
	if err != nil {
		log.Warn("otel_resource_build_failed", slog.String("error", err.Error()))
		return noopShutdown, nil
	}

	target, insecure := normalizeOTLPEndpoint(endpoint)
	ratio := traceSampleRatioFromEnv()

	traceExporter, err := otlptracehttp.New(ctx, otlpTraceHTTPOptions(target, insecure)...)
	if err != nil {
		log.Warn("otel_trace_exporter_init_failed", slog.String("error", err.Error()))
		return noopShutdown, nil
	}

	metricExporter, err := otlpmetrichttp.New(ctx, otlpMetricHTTPOptions(target, insecure)...)
	if err != nil {
		log.Warn("otel_metric_exporter_init_failed", slog.String("error", err.Error()))
		return noopShutdown, nil
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(traceExporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(ratio))),
	)
	reader := metric.NewPeriodicReader(metricExporter)
	mp := metric.NewMeterProvider(
		metric.WithReader(reader),
		metric.WithResource(res),
	)

	otel.SetTracerProvider(tp)
	otel.SetMeterProvider(mp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))
	otel.SetErrorHandler(otel.ErrorHandlerFunc(func(err error) {
		log.Warn("otel_internal_error", slog.String("error", err.Error()))
	}))

	log.Info("otel_telemetry_enabled",
		slog.String("endpoint", target),
		slog.Bool("insecure", insecure),
		slog.Float64("trace_sample_ratio", ratio),
		slog.String("protocol", "http/protobuf"),
	)

	shutdown := func(shutdownCtx context.Context) error {
		var errs []error
		// ForceFlush before Shutdown so batched spans/metrics still in the
		// exporter queue are sent - critical for short-lived Cloud Run Jobs
		// that exit immediately after their one-shot work completes.
		if err := tp.ForceFlush(shutdownCtx); err != nil {
			errs = append(errs, fmt.Errorf("tracer provider force flush: %w", err))
		}
		if err := mp.ForceFlush(shutdownCtx); err != nil {
			errs = append(errs, fmt.Errorf("meter provider force flush: %w", err))
		}
		if err := tp.Shutdown(shutdownCtx); err != nil {
			errs = append(errs, fmt.Errorf("tracer provider shutdown: %w", err))
		}
		if err := mp.Shutdown(shutdownCtx); err != nil {
			errs = append(errs, fmt.Errorf("meter provider shutdown: %w", err))
		}
		if len(errs) == 0 {
			return nil
		}
		return errors.Join(errs...)
	}
	return shutdown, nil
}

// buildResource constructs the OTel Resource carrying service.name,
// service.version, deployment.environment, and cloud.region, per
// docs/observability/OBSERVABILITY_DESIGN.md section 3.
func buildResource(ctx context.Context, cfg Config) (*resource.Resource, error) {
	env := coalesce(cfg.Env, os.Getenv("GOATOS_ENV"), "local")
	version := coalesce(cfg.Version, "dev")
	service := coalesce(cfg.Service, "goatos")
	region := coalesce(os.Getenv("GOATOS_REGION"), os.Getenv("GOOGLE_CLOUD_REGION"), os.Getenv("CLOUD_RUN_REGION"))

	attrs := []attribute.KeyValue{
		semconv.ServiceNameKey.String("goatos-" + service),
		semconv.ServiceVersionKey.String(version),
		semconv.DeploymentEnvironmentKey.String(env),
	}
	if region != "" {
		attrs = append(attrs, semconv.CloudRegionKey.String(region))
	}

	return resource.New(ctx,
		resource.WithSchemaURL(semconv.SchemaURL),
		resource.WithAttributes(attrs...),
	)
}

// traceSampleRatioFromEnv resolves GOATOS_TRACE_SAMPLE_RATIO, defaulting to
// defaultTraceSampleRatio (stg) when unset or invalid. dev/prod override via
// the env var per section 3 of the design doc.
func traceSampleRatioFromEnv() float64 {
	raw := strings.TrimSpace(os.Getenv("GOATOS_TRACE_SAMPLE_RATIO"))
	if raw == "" {
		return defaultTraceSampleRatio
	}
	ratio, err := strconv.ParseFloat(raw, 64)
	if err != nil || ratio < 0 || ratio > 1 {
		return defaultTraceSampleRatio
	}
	return ratio
}

// normalizeOTLPEndpoint splits a configured collector base URL into a bare
// host[:port][/path] target (suitable for WithEndpoint, which appends the
// standard "/v1/traces" / "/v1/metrics" suffix itself) plus whether the
// connection should be insecure (plain HTTP, no TLS). Accepts
// "https://host[:port]" (secure - the expected Cloud Run collector shape),
// "http://host[:port]" (insecure, local/dev collector), or a bare
// "host[:port]" (treated as insecure).
func normalizeOTLPEndpoint(raw string) (target string, insecure bool) {
	switch {
	case strings.HasPrefix(raw, "https://"):
		return strings.TrimPrefix(raw, "https://"), false
	case strings.HasPrefix(raw, "http://"):
		return strings.TrimPrefix(raw, "http://"), true
	default:
		return raw, true
	}
}

func otlpTraceHTTPOptions(target string, insecure bool) []otlptracehttp.Option {
	opts := []otlptracehttp.Option{otlptracehttp.WithEndpoint(target)}
	if insecure {
		opts = append(opts, otlptracehttp.WithInsecure())
	}
	return opts
}

func otlpMetricHTTPOptions(target string, insecure bool) []otlpmetrichttp.Option {
	opts := []otlpmetrichttp.Option{otlpmetrichttp.WithEndpoint(target)}
	if insecure {
		opts = append(opts, otlpmetrichttp.WithInsecure())
	}
	return opts
}
