package observability

import (
	"context"
	"log/slog"
	"os"
	"strings"

	"go.opentelemetry.io/otel/trace"
)

// traceContextHandler wraps a slog.Handler and stamps every record made
// through a *Context logging call (InfoContext, ErrorContext, ...) with the
// active OTel span's trace/span ids, per
// docs/observability/OBSERVABILITY_DESIGN.md section 2.3 ("Add
// logging.googleapis.com/trace + spanId fields to every record so Cloud
// Logging & Grafana pivot log<->trace").
//
// It is a no-op when the context carries no valid span (e.g. plain
// log.Info calls with no *Context suffix, or requests before otelhttp wraps
// them) - existing callers and tests are unaffected.
type traceContextHandler struct {
	slog.Handler
}

// newTraceContextHandler wraps inner. Kept as a constructor (rather than a
// bare struct literal at call sites) so future enrichment logic has one
// place to grow.
func newTraceContextHandler(inner slog.Handler) slog.Handler {
	return &traceContextHandler{Handler: inner}
}

func (h *traceContextHandler) Handle(ctx context.Context, r slog.Record) error {
	if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
		r.AddAttrs(
			slog.String("logging.googleapis.com/trace", traceResourceName(sc.TraceID().String())),
			slog.String("logging.googleapis.com/spanId", sc.SpanID().String()),
			slog.Bool("logging.googleapis.com/trace_sampled", sc.IsSampled()),
			slog.String("trace_id", sc.TraceID().String()),
			slog.String("span_id", sc.SpanID().String()),
		)
	}
	return h.Handler.Handle(ctx, r)
}

func (h *traceContextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &traceContextHandler{Handler: h.Handler.WithAttrs(attrs)}
}

func (h *traceContextHandler) WithGroup(name string) slog.Handler {
	return &traceContextHandler{Handler: h.Handler.WithGroup(name)}
}

// traceResourceName formats a bare trace id as the full Cloud Logging
// resource name ("projects/[PROJECT_ID]/traces/[TRACE_ID]") when a GCP
// project id can be resolved from the environment, matching the convention
// used elsewhere in this codebase (internal/platform/taskqueue falls back to
// GOOGLE_CLOUD_PROJECT). Falls back to the bare trace id when no project id
// is known - still useful for a non-GCP Grafana Cloud Logging datasource,
// just without the Cloud Logging trace-panel deep link.
func traceResourceName(traceID string) string {
	project := strings.TrimSpace(coalesce(os.Getenv("GOATOS_GCP_PROJECT_ID"), os.Getenv("GOOGLE_CLOUD_PROJECT")))
	if project == "" {
		return traceID
	}
	return "projects/" + project + "/traces/" + traceID
}
