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
//	stdout_json (default) — structured JSON to stdout.
//	otlp                  — OTLP over HTTP (not gRPC per go-backend-stack ADR).
//	                         Phase 1 stub: falls back to stdout_json. A real
//	                         OTLP-HTTP exporter can be wired here without changing
//	                         callers.
//	gcm                   — alias for otlp; intended for GCP OTLP ingestion.
package observability

import (
	"io"
	"log/slog"
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
}

// New builds and returns the canonical *slog.Logger for a Goat OS process.
// Standard fields (service, version, env) are stamped on every log record.
func New(cfg Config) *slog.Logger {
	level := resolveLevel(coalesce(cfg.Level, os.Getenv("GOATOS_LOG_LEVEL")))
	sink := strings.ToLower(strings.TrimSpace(coalesce(cfg.Sink, os.Getenv("GOATOS_OBS_SINK"), "stdout_json")))
	env := coalesce(cfg.Env, os.Getenv("GOATOS_ENV"), "local")
	version := coalesce(cfg.Version, "dev")
	otlpEndpoint := strings.TrimSpace(os.Getenv("GOATOS_OTLP_ENDPOINT"))

	w := cfg.W
	if w == nil {
		w = os.Stdout
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

	log := slog.New(handler).With(
		slog.String("service", coalesce(cfg.Service, "goatos")),
		slog.String("version", version),
		slog.String("env", env),
	)
	if sink == "otlp" || sink == "gcm" {
		log.Warn("observability_sink_not_implemented",
			slog.String("requested_sink", sink),
			slog.String("fallback_sink", "stdout_json"),
			slog.String("otlp_endpoint", otlpEndpoint),
			slog.String("next_step", "wire a real OTLP-HTTP exporter before expecting logs to ship outside stdout"),
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
