package httpmiddleware

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"
)

const (
	headerRequestID = "X-Request-ID"
	headerTrace     = "traceparent"
	// X-GoatOS-Tenant-ID and X-GoatOS-Actor-ID are local-development
	// placeholders. The API bootstrap defaults to bearer auth, which overwrites
	// both values from a verified token before handlers run.
	headerTenantID = "X-GoatOS-Tenant-ID"
	headerActorID  = "X-GoatOS-Actor-ID"
)

// RequestContext preserves inbound request/trace IDs, generates missing IDs,
// records local-development tenant/actor placeholders, and logs HTTP outcomes.
func RequestContext(log *slog.Logger) func(http.Handler) http.Handler {
	if log == nil {
		log = slog.Default()
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			requestID := strings.TrimSpace(r.Header.Get(headerRequestID))
			if requestID == "" {
				requestID = generateID()
			}

			traceID := strings.TrimSpace(r.Header.Get(headerTrace))
			if traceID == "" {
				traceID = requestID
			}

			tenantID := strings.TrimSpace(r.Header.Get(headerTenantID))
			actorID := strings.TrimSpace(r.Header.Get(headerActorID))

			ctx := r.Context()
			ctx = context.WithValue(ctx, requestIDKey, requestID)
			ctx = context.WithValue(ctx, traceIDKey, traceID)
			if tenantID != "" {
				ctx = WithTenantID(ctx, tenantID)
			}
			if actorID != "" {
				ctx = WithActorID(ctx, actorID)
			}

			w.Header().Set(headerRequestID, requestID)
			if r.Header.Get(headerTrace) != "" {
				w.Header().Set(headerTrace, r.Header.Get(headerTrace))
			}

			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rec, r.WithContext(ctx))
			logHTTPRequest(log, ctx, requestID, traceID, r.Method, r.URL.Path, rec.status, time.Since(start))
		})
	}
}

type statusRecorder struct {
	http.ResponseWriter
	status  int
	started bool
}

func (r *statusRecorder) WriteHeader(status int) {
	if r.started {
		return
	}
	r.status = status
	r.started = true
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Write(data []byte) (int, error) {
	if !r.started {
		r.status = http.StatusOK
		r.started = true
	}
	return r.ResponseWriter.Write(data)
}

func (r *statusRecorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}

func (r *statusRecorder) FlushError() error {
	err := http.NewResponseController(r.ResponseWriter).Flush()
	if err == nil && !r.started {
		r.status = http.StatusOK
		r.started = true
	}
	return err
}

func (r *statusRecorder) Flush() {
	_ = r.FlushError()
}

func (r *statusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	conn, rw, err := http.NewResponseController(r.ResponseWriter).Hijack()
	if err == nil {
		r.started = true
	}
	return conn, rw, err
}

func logHTTPRequest(log *slog.Logger, ctx context.Context, requestID, traceID, method, path string, status int, duration time.Duration) {
	log.InfoContext(ctx, "http_request",
		slog.String("request_id", requestID),
		slog.String("trace_id", traceID),
		slog.String("method", method),
		slog.String("path", path),
		slog.Int("status", status),
		slog.Duration("duration", duration),
	)
}

func generateID() string {
	var b [16]byte
	if _, err := io.ReadFull(rand.Reader, b[:]); err != nil {
		panic(fmt.Errorf("generate request id: %w", err))
	}
	return hex.EncodeToString(b[:])
}
