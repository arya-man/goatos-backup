package httpmiddleware

import (
	"bufio"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"runtime/debug"
	"strings"
	"time"
)

// PanicRecovery is the outermost HTTP middleware. It catches any panic that
// escapes inner handlers or middleware (including auth and RequestContext),
// logs the panic value and full stack trace with trace_id and request_id,
// then writes a 500 JSON envelope carrying trace_id so the caller can
// correlate to server logs.
//
// Wire it BEFORE RequestContext and auth so that panics in those layers are
// also caught.
//
// Trace ID extraction strategy: because PanicRecovery sits outside
// RequestContext in the chain, r.Context() does not have the trace/request IDs
// set by RequestContext when a panic fires inside an inner handler. We fall
// back in order:
//
//  1. TraceIDFromContext(r.Context()) — set when panic happens after
//     RequestContext has stored the value.
//  2. The X-Request-ID response header — RequestContext writes this before
//     calling the inner handler, so it is present even when the panic fires
//     deep in the handler.
//  3. The X-Request-ID request header — present for all inbound requests that
//     set it; empty string when not set.
func PanicRecovery(log *slog.Logger) func(http.Handler) http.Handler {
	if log == nil {
		log = slog.Default()
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			tracker := &panicResponseTracker{ResponseWriter: w, status: http.StatusOK}
			defer func() {
				if p := recover(); p != nil {
					stack := string(debug.Stack())
					// Resolve trace/request IDs using the fallback chain.
					traceID := resolveTraceID(r, tracker)
					requestID := resolveRequestID(r, tracker)
					status := tracker.status
					if !tracker.started {
						status = http.StatusInternalServerError
					}
					logHTTPRequest(log, r.Context(), requestID, traceID, r.Method, r.URL.Path, status, time.Since(start))
					log.ErrorContext(r.Context(), "http_panic",
						slog.Any("panic", p),
						slog.String("stack", stack),
						slog.String("trace_id", traceID),
						slog.String("request_id", requestID),
						slog.String("method", r.Method),
						slog.String("path", r.URL.Path),
					)
					if tracker.started {
						return
					}
					tracker.Header().Set("Content-Type", "application/json")
					tracker.WriteHeader(http.StatusInternalServerError)
					envelope := map[string]any{
						"code":         "internal_error",
						"message":      "internal server error",
						"field_errors": []any{},
						"trace_id":     traceID,
						"retryable":    false,
					}
					_ = json.NewEncoder(tracker).Encode(envelope)
				}
			}()
			next.ServeHTTP(tracker, r)
		})
	}
}

type panicResponseTracker struct {
	http.ResponseWriter
	started bool
	status  int
}

func (w *panicResponseTracker) WriteHeader(status int) {
	if !w.started {
		w.started = true
		w.status = status
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *panicResponseTracker) Write(data []byte) (int, error) {
	if !w.started {
		w.started = true
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(data)
}

func (w *panicResponseTracker) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

func (w *panicResponseTracker) FlushError() error {
	err := http.NewResponseController(w.ResponseWriter).Flush()
	if err == nil {
		w.started = true
		w.status = http.StatusOK
	}
	return err
}

func (w *panicResponseTracker) Flush() {
	_ = w.FlushError()
}

func (w *panicResponseTracker) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	conn, rw, err := http.NewResponseController(w.ResponseWriter).Hijack()
	if err == nil {
		w.started = true
		w.status = http.StatusOK
	}
	return conn, rw, err
}

// resolveTraceID extracts the trace ID using the fallback chain:
// context value → response/request traceparent → response/request X-Request-ID.
func resolveTraceID(r *http.Request, w http.ResponseWriter) string {
	if v := TraceIDFromContext(r.Context()); v != "" {
		return v
	}
	// RequestContext echoes an inbound traceparent to the response before
	// calling next. Prefer that W3C trace over the request ID fallback so panic
	// logs correlate with successful request logs.
	if v := strings.TrimSpace(w.Header().Get(headerTrace)); v != "" {
		return v
	}
	if v := strings.TrimSpace(r.Header.Get(headerTrace)); v != "" {
		return v
	}
	// RequestContext always writes X-Request-ID to the response before calling
	// next. This remains available even after a downstream panic.
	if v := strings.TrimSpace(w.Header().Get(headerRequestID)); v != "" {
		return v
	}
	// Last resort: the raw inbound header.
	if v := strings.TrimSpace(r.Header.Get(headerRequestID)); v != "" {
		return v
	}
	return ""
}

// resolveRequestID mirrors resolveTraceID but returns the request-scoped ID.
func resolveRequestID(r *http.Request, w http.ResponseWriter) string {
	if v := RequestIDFromContext(r.Context()); v != "" {
		return v
	}
	if v := strings.TrimSpace(w.Header().Get(headerRequestID)); v != "" {
		return v
	}
	if v := strings.TrimSpace(r.Header.Get(headerRequestID)); v != "" {
		return v
	}
	return ""
}
