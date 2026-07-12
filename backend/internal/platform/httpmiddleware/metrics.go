package httpmiddleware

import (
	"context"
	"net/http"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

const metricsMeterName = "github.com/vgoats/goatos/backend/http"

var (
	httpMeter           = otel.Meter(metricsMeterName)
	httpRequestDuration = mustFloat64Histogram(httpMeter, "http.server.request.duration", "s", "Duration of an HTTP request as observed by the server.")
	httpRequestsTotal   = mustInt64Counter(httpMeter, "http.server.requests", "{request}", "Count of HTTP requests handled by the server.")
	httpActiveRequests  = mustInt64UpDownCounter(httpMeter, "http.server.active_requests", "{request}", "Number of HTTP requests currently being handled by the server.")
)

func mustFloat64Histogram(m metric.Meter, name, unit, desc string) metric.Float64Histogram {
	inst, err := m.Float64Histogram(name, metric.WithUnit(unit), metric.WithDescription(desc))
	if err != nil {
		otel.Handle(err)
		return nil
	}
	return inst
}

func mustInt64Counter(m metric.Meter, name, unit, desc string) metric.Int64Counter {
	inst, err := m.Int64Counter(name, metric.WithUnit(unit), metric.WithDescription(desc))
	if err != nil {
		otel.Handle(err)
		return nil
	}
	return inst
}

func mustInt64UpDownCounter(m metric.Meter, name, unit, desc string) metric.Int64UpDownCounter {
	inst, err := m.Int64UpDownCounter(name, metric.WithUnit(unit), metric.WithDescription(desc))
	if err != nil {
		otel.Handle(err)
		return nil
	}
	return inst
}

// RoutePattern resolves a low-cardinality route label for a request. The
// http.ServeMux-backed implementation (see Metrics) uses (*http.ServeMux).Handler,
// which returns the *pattern* that matched (e.g. "GET /goats/{id}"), not the
// raw path - keeping the route label bounded per
// docs/observability/OBSERVABILITY_DESIGN.md section 3 ("route templates,
// never raw paths/ids"). A nil/empty result is bucketed to "unknown".
type RoutePattern func(r *http.Request) string

// MuxRoutePattern builds a RoutePattern backed by mux.Handler, which resolves
// the registered pattern for a request without invoking the handler.
func MuxRoutePattern(mux *http.ServeMux) RoutePattern {
	return func(r *http.Request) string {
		if mux == nil {
			return ""
		}
		_, pattern := mux.Handler(r)
		return pattern
	}
}

// Metrics returns middleware recording RED (Rate/Errors/Duration) metrics
// for every request: http.server.request.duration, http.server.requests, and
// http.server.active_requests, labeled by route/method/status_class/tenant
// per docs/observability/OBSERVABILITY_DESIGN.md section 2.1. routeOf may be
// nil, in which case every request is labeled route="unknown".
//
// Metrics wraps its own minimal status-capturing ResponseWriter rather than
// reusing the RequestContext middleware's private statusRecorder: the two
// middlewares sit at different points in the bootstrap chain (RequestContext
// wraps the outer request-logging mux; Metrics wraps the protected mux inside
// otelhttp), so they observe different ResponseWriter instances regardless -
// there is nothing to share across that boundary. Both use the same
// WriteHeader/Write capture technique.
func Metrics(routeOf RoutePattern) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			addUpDownCounter(r.Context(), httpActiveRequests, 1)
			defer addUpDownCounter(r.Context(), httpActiveRequests, -1)

			rec := &metricsStatusWriter{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rec, r)

			route := "unknown"
			if routeOf != nil {
				if p := routeOf(r); p != "" {
					route = p
				}
			}
			attrs := []attribute.KeyValue{
				attribute.String("route", route),
				attribute.String("method", r.Method),
				attribute.String("status_class", statusClass(rec.status)),
			}
			if tenant := TenantIDFromContext(r.Context()); tenant != "" {
				attrs = append(attrs, attribute.String("tenant", tenant))
			}

			addCounter(r.Context(), httpRequestsTotal, 1, attrs...)
			recordHistogram(r.Context(), httpRequestDuration, time.Since(start).Seconds(), attrs...)
		})
	}
}

func addCounter(ctx context.Context, c metric.Int64Counter, incr int64, attrs ...attribute.KeyValue) {
	if c == nil {
		return
	}
	c.Add(ctx, incr, metric.WithAttributes(attrs...))
}

func addUpDownCounter(ctx context.Context, c metric.Int64UpDownCounter, incr int64, attrs ...attribute.KeyValue) {
	if c == nil {
		return
	}
	c.Add(ctx, incr, metric.WithAttributes(attrs...))
}

func recordHistogram(ctx context.Context, h metric.Float64Histogram, value float64, attrs ...attribute.KeyValue) {
	if h == nil {
		return
	}
	h.Record(ctx, value, metric.WithAttributes(attrs...))
}

func statusClass(status int) string {
	switch {
	case status >= 500:
		return "5xx"
	case status >= 400:
		return "4xx"
	case status >= 300:
		return "3xx"
	case status >= 200:
		return "2xx"
	default:
		return "other"
	}
}

// metricsStatusWriter captures the final status code for RED metrics. Kept
// deliberately minimal (no Hijack/Flush passthrough) since it wraps the
// otelhttp-instrumented writer purely to read rec.status after ServeHTTP
// returns; streaming handlers are still supported because Write/WriteHeader
// delegate straight through.
type metricsStatusWriter struct {
	http.ResponseWriter
	status  int
	started bool
}

func (w *metricsStatusWriter) WriteHeader(status int) {
	if w.started {
		return
	}
	w.status = status
	w.started = true
	w.ResponseWriter.WriteHeader(status)
}

func (w *metricsStatusWriter) Write(data []byte) (int, error) {
	if !w.started {
		w.status = http.StatusOK
		w.started = true
	}
	return w.ResponseWriter.Write(data)
}

func (w *metricsStatusWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}
