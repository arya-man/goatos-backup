package httpresponse

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
)

// WriteJSON writes a JSON response with the standard content type.
func WriteJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

// WriteError writes an error envelope and logs 5xx causes once at the HTTP
// boundary. 4xx responses are client errors and intentionally do not emit
// server error logs.
func WriteError(w http.ResponseWriter, r *http.Request, log *slog.Logger, status int, envelope any, cause error) {
	if status >= http.StatusInternalServerError && log != nil {
		attrs := []slog.Attr{
			slog.String("trace_id", httpmiddleware.TraceIDFromContext(r.Context())),
			slog.String("request_id", httpmiddleware.RequestIDFromContext(r.Context())),
			slog.String("tenant_id", httpmiddleware.TenantIDFromContext(r.Context())),
			slog.String("route", r.Method+" "+r.URL.Path),
			slog.Int("status", status),
		}
		if cause != nil {
			attrs = append([]slog.Attr{slog.String("error", cause.Error())}, attrs...)
		}
		log.LogAttrs(r.Context(), slog.LevelError, "http_5xx", attrs...)
	}
	WriteJSON(w, status, envelope)
}
