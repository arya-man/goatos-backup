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

	"github.com/vgoats/goatos/backend/internal/platform/localization"
)

const (
	AcceptLanguageHeader = "Accept-Language"
	LocaleContextHeader  = "X-GoatOS-Locale"
	headerRequestID      = "X-Request-ID"
	headerTrace          = "traceparent"
	// TenantContextHeader carries request tenant context for tokens whose
	// verified claims do not include a Goat OS tenant.
	TenantContextHeader = "X-GoatOS-Tenant-ID"
	// X-GoatOS-Tenant-ID carries request tenant context. Bearer auth prefers a
	// verified token tenant claim, but falls back to this header for external
	// IdPs such as Firebase whose ID tokens do not carry Goat OS tenant claims.
	// X-GoatOS-Actor-ID is a local-development placeholder; bearer auth always
	// overwrites actor context from the verified token subject.
	headerTenantID = TenantContextHeader
	headerActorID  = "X-GoatOS-Actor-ID"
	// DeviceContextHeader lets a client identify which physical device made a
	// request, so the same operator signed in on multiple phones can be told
	// apart in logs/audit rows. Optional: older clients that omit it simply
	// get an empty device_id, never an error.
	DeviceContextHeader = "X-Device-Id"
)

// LocaleTagFromRequest resolves the normalized locale from request context or headers.
func LocaleTagFromRequest(r *http.Request) string {
	if tag, ok := r.Context().Value(localeTagKey).(string); ok && tag != "" {
		return localization.Normalize(tag)
	}
	return localization.FromHeaders(r.Header.Get(LocaleContextHeader), r.Header.Get(AcceptLanguageHeader))
}

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
			localeTag := localization.FromHeaders(r.Header.Get(LocaleContextHeader), r.Header.Get(AcceptLanguageHeader))

			ctx := r.Context()
			ctx = context.WithValue(ctx, requestIDKey, requestID)
			ctx = context.WithValue(ctx, traceIDKey, traceID)
			ctx = WithLocaleTag(ctx, localeTag)
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
