// Package observability provides the single, canonical constructor for the
// process logger used by all Goat OS backend binaries and middleware.
//
// # Rule
//
// All *slog.Logger values MUST be obtained from New. Do not call slog.New,
// slog.NewJSONHandler, or slog.NewTextHandler in cmd/, bootstrap/, or
// internal/ outside of this package. Tests that need a discarding logger may
// use slog.New(slog.NewTextHandler(io.Discard, nil)) locally — the guard
// script exempts test files.
//
// # Sink selection (GOATOS_OBS_SINK)
//
//	stdout_json (default) — structured JSON to stdout, and the only log
//	                         destination in every mode (see below).
//	otlp                  — logs still ship as structured JSON to stdout
//	                         (Cloud Logging ingests via the stdout scrape);
//	                         GOATOS_OTLP_ENDPOINT additionally activates a
//	                         real OTLP-HTTP exporter (not gRPC per
//	                         go-backend-stack ADR) for METRICS and TRACES via
//	                         SetupTelemetry (telemetry.go). Logs are never
//	                         routed through OTLP by design.
//	gcm                   — alias for otlp; intended for GCP OTLP ingestion.
package observability

import (
	"io"
	"log/slog"
	"net/url"
	"os"
	"strings"
)

// Config holds the fields used to construct the process logger.
type Config struct {
	// Service is the service/binary name, e.g. "api", "outbox-relay".
	Service string
	// Version is the build version, e.g. a git SHA or semver tag.
	// Defaults to "dev" when empty.
	Version string
	// Env is the deployment environment, e.g. "local", "dev", "prod".
	// Defaults to GOATOS_ENV when empty.
	Env string
	// Level overrides the log level (debug/info/warn/error).
	// Defaults to GOATOS_LOG_LEVEL when empty, then "info".
	Level string
	// Sink overrides the output sink.
	// Defaults to GOATOS_OBS_SINK when empty, then "stdout_json".
	Sink string
	// W is the sink writer. Defaults to os.Stdout. Useful for tests.
	W io.Writer

	// skipSinkNoticeLog suppresses the boot-time otlp/gcm sink notice below.
	// Unexported: only SetupTelemetry (same package, telemetry.go) sets it,
	// for the throwaway logger it builds internally to emit its own
	// warn/info lines. Without this, every process that calls
	// observability.New(cfg) for its real logger AND observability.
	// SetupTelemetry(ctx, cfg) for telemetry (the common pattern across
	// cmd/*/main.go) would log the identical sink notice twice at boot.
	skipSinkNoticeLog bool
}

// New builds and returns the canonical *slog.Logger for a Goat OS process.
// Standard fields (service, version, env) are stamped on every log record.
func New(cfg Config) *slog.Logger {
	level := resolveLevel(coalesce(cfg.Level, os.Getenv("GOATOS_LOG_LEVEL")))
	requestedSink := strings.ToLower(strings.TrimSpace(coalesce(cfg.Sink, os.Getenv("GOATOS_OBS_SINK"), "stdout_json")))
	env := coalesce(cfg.Env, os.Getenv("GOATOS_ENV"), "local")
	version := coalesce(cfg.Version, "dev")
	otlpEndpointTarget, otlpEndpointConfigured := safeEndpointTarget(os.Getenv("GOATOS_OTLP_ENDPOINT"))

	w := cfg.W
	if w == nil {
		w = os.Stdout
	}

	sink := requestedSink
	unknownSink := false
	switch requestedSink {
	case "stdout_json", "otlp", "gcm":
	default:
		unknownSink = true
		sink = "stdout_json"
	}

	var handler slog.Handler
	switch sink {
	case "otlp", "gcm":
		// Phase 1: OTLP endpoint noted but output still goes to stdout_json.
		// A real OTLP-HTTP exporter can be wired here in a future ADR iteration.
		handler = slog.NewJSONHandler(w, &slog.HandlerOptions{Level: level})
	default: // "stdout_json"
		handler = slog.NewJSONHandler(w, &slog.HandlerOptions{Level: level})
	}

	log := slog.New(newTraceContextHandler(handler)).With(
		slog.String("service", coalesce(cfg.Service, "goatos")),
		slog.String("version", version),
		slog.String("env", env),
	)
	if unknownSink {
		log.Warn("observability_sink_unknown",
			slog.String("requested_sink", requestedSink),
			slog.String("fallback_sink", "stdout_json"),
			slog.String("next_step", "use GOATOS_OBS_SINK=stdout_json, otlp, or gcm"),
		)
	}
	if (sink == "otlp" || sink == "gcm") && !cfg.skipSinkNoticeLog {
		// Informational, not a warning: logs staying on stdout while
		// metrics/traces ship via OTLP is the intended stg/prod shape, not a
		// missing feature. See the package doc comment above.
		log.Info("observability_logs_stdout_metrics_traces_otlp",
			slog.String("requested_sink", sink),
			slog.String("log_destination", "stdout_json"),
			slog.Bool("otlp_endpoint_configured", otlpEndpointConfigured),
			slog.String("otlp_endpoint_target", otlpEndpointTarget),
			slog.String("note", "logs intentionally stay on stdout for Cloud Logging ingestion; metrics and traces ship via SetupTelemetry's OTLP exporter when GOATOS_OTLP_ENDPOINT is configured"),
		)
	}
	return log
}

func resolveLevel(raw string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// coalesce returns the first non-empty string value.
func coalesce(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func safeEndpointTarget(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "configured", true
	}
	return parsed.Scheme + "://" + parsed.Host, true
}
