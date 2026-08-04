package httpresponse

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"reflect"
	"strings"

	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
)

// WriteJSON writes a JSON response with the standard content type.
func WriteJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

// WriteError writes an error envelope and logs the outcome once at the HTTP
// boundary.
//
//	5xx -> ERROR "http_5xx" (server fault; carries the cause)
//	4xx -> WARN  "http_4xx" (a real user was REFUSED; carries the envelope's
//	             machine code so the refusal is traceable to WHY)
//
// The 4xx line exists because a user-facing refusal that leaves only an
// access-log `409 POST /weighing/campaigns` behind is not diagnosable: the
// status alone cannot distinguish "shed already scheduled" from "operator
// outside park" from "verification pending". Every envelope in this codebase
// already carries a `code`; this is the single point that puts it in the log,
// so no handler needs per-call-site logging boilerplate.
//
// 401 is deliberately excluded: the auth middleware already logs every
// authentication failure with full context (auth_failed), and unauthenticated
// probe traffic would otherwise emit a WARN pair per scanner hit.
func WriteError(w http.ResponseWriter, r *http.Request, log *slog.Logger, status int, envelope any, cause error) {
	if log != nil && status >= http.StatusBadRequest {
		attrs := []slog.Attr{
			slog.String("code", errorCode(envelope)),
			slog.String("trace_id", httpmiddleware.TraceIDFromContext(r.Context())),
			slog.String("request_id", httpmiddleware.RequestIDFromContext(r.Context())),
			slog.String("tenant_id", httpmiddleware.TenantIDFromContext(r.Context())),
			slog.String("actor_id", httpmiddleware.ActorIDFromContext(r.Context())),
			slog.String("route", r.Method+" "+r.URL.Path),
			slog.Int("status", status),
		}
		if cause != nil {
			attrs = append([]slog.Attr{slog.String("error", cause.Error())}, attrs...)
		}
		switch {
		case status >= http.StatusInternalServerError:
			log.LogAttrs(r.Context(), slog.LevelError, "http_5xx", attrs...)
		case status != http.StatusUnauthorized:
			log.LogAttrs(r.Context(), slog.LevelWarn, "http_4xx", attrs...)
		}
	}
	WriteJSON(w, status, envelope)
}

// errorCode pulls the machine-readable `code` out of an error envelope without
// forcing all ~180 call sites onto one struct type. Both envelope shapes in use
// are covered: a struct with a `json:"code"` string field, and a
// map[string]any carrying "code". Returns "unspecified" when neither matches —
// visible in the log as a prompt to give that envelope a code, rather than a
// silently empty field.
func errorCode(envelope any) string {
	if envelope == nil {
		return "unspecified"
	}
	if m, ok := envelope.(map[string]any); ok {
		if code, ok := m["code"].(string); ok && code != "" {
			return code
		}
		return "unspecified"
	}
	v := reflect.ValueOf(envelope)
	for v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return "unspecified"
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return "unspecified"
	}
	t := v.Type()
	for i := range t.NumField() {
		if strings.Split(t.Field(i).Tag.Get("json"), ",")[0] != "code" {
			continue
		}
		if f := v.Field(i); f.Kind() == reflect.String && f.String() != "" {
			return f.String()
		}
	}
	return "unspecified"
}
