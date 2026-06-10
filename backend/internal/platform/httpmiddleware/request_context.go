package httpmiddleware

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
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

			log.InfoContext(ctx, "http_request",
				slog.String("request_id", requestID),
				slog.String("trace_id", traceID),
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", rec.status),
				slog.Duration("duration", time.Since(start)),
			)
		})
	}
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func generateID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return hex.EncodeToString([]byte(time.Now().Format(time.RFC3339Nano)))
	}
	return hex.EncodeToString(b[:])
}
