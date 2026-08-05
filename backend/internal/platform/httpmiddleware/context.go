package httpmiddleware

import (
	"strings"
	"context"

	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/localization"
)

type contextKey string

const (
	requestIDKey  contextKey = "request_id"
	traceIDKey    contextKey = "trace_id"
	tenantIDKey   contextKey = "tenant_id"
	actorIDKey    contextKey = "actor_id"
	deviceIDKey   contextKey = "device_id"
	localeTagKey  contextKey = "locale_tag"
	authGrantsKey contextKey = "auth_grants"
)

// RequestIDFromContext returns the request ID attached by RequestContext.
func RequestIDFromContext(ctx context.Context) string {
	v, _ := ctx.Value(requestIDKey).(string)
	return v
}

// TraceIDFromContext returns the trace ID attached by RequestContext.
func TraceIDFromContext(ctx context.Context) string {
	v, _ := ctx.Value(traceIDKey).(string)
	return v
}

// TenantIDFromContext returns the tenant scope attached by RequestContext.
func TenantIDFromContext(ctx context.Context) string {
	v, _ := ctx.Value(tenantIDKey).(string)
	return v
}

// ActorIDFromContext returns the authenticated actor attached to the request.
func ActorIDFromContext(ctx context.Context) string {
	v, _ := ctx.Value(actorIDKey).(string)
	return v
}

// DeviceIDFromContext returns the client-supplied device identifier attached to the
// request, or "" when the caller (e.g. an older client) did not send one.
func DeviceIDFromContext(ctx context.Context) string {
	v, _ := ctx.Value(deviceIDKey).(string)
	return v
}

// LocaleTagFromContext returns the normalized app locale attached to the request.
func LocaleTagFromContext(ctx context.Context) string {
	v, _ := ctx.Value(localeTagKey).(string)
	if v == "" {
		return localization.DefaultTag
	}
	return localization.Normalize(v)
}

// WithTenantID attaches a tenant scope to a context.
func WithTenantID(ctx context.Context, tenantID string) context.Context {
	return context.WithValue(ctx, tenantIDKey, tenantID)
}

// WithActorID attaches an actor/user scope to a context.
func WithActorID(ctx context.Context, actorID string) context.Context {
	return context.WithValue(ctx, actorIDKey, actorID)
}

// WithDeviceID attaches a client-supplied device identifier to a context. An empty
// deviceID is tolerated (older clients do not send one) and simply round-trips as "".
func WithDeviceID(ctx context.Context, deviceID string) context.Context {
	return context.WithValue(ctx, deviceIDKey, sanitizeDeviceID(deviceID))
}

// MaxDeviceIDLength bounds the client-supplied device identifier.
//
// The value is attacker-controlled -- any caller can send any X-Device-Id -- and it is persisted
// verbatim into audit_log metadata (jsonb, unbounded) and echoed into every log line for the
// request. Unbounded, a single caller can push a header the size of Go's whole header ceiling
// into an audit row on EVERY scan, bloating the table and drowning the log. It is descriptive
// metadata only -- never used for authn or authz -- so truncating it costs nothing.
const MaxDeviceIDLength = 128

// sanitizeDeviceID bounds the length and drops non-printable bytes. Control characters in a value
// that lands in logs are a forged-log-line vector in any consumer that is less careful than
// slog's own quoting, and they make an audit row unreadable for no legitimate purpose.
func sanitizeDeviceID(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	cleaned := strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, trimmed)
	if len(cleaned) > MaxDeviceIDLength {
		return cleaned[:MaxDeviceIDLength]
	}
	return cleaned
}

// WithLocaleTag attaches a normalized locale tag to a context.
func WithLocaleTag(ctx context.Context, tag string) context.Context {
	return context.WithValue(ctx, localeTagKey, localization.Normalize(tag))
}

// AuthGrantsFromContext returns the active authorization grants attached by AuthMiddleware.
func AuthGrantsFromContext(ctx context.Context) []permissions.ActiveGrant {
	grants, _ := ctx.Value(authGrantsKey).([]permissions.ActiveGrant)
	out := make([]permissions.ActiveGrant, len(grants))
	copy(out, grants)
	return out
}

// WithAuthGrants attaches active authorization grants to a context.
func WithAuthGrants(ctx context.Context, grants []permissions.ActiveGrant) context.Context {
	out := make([]permissions.ActiveGrant, len(grants))
	copy(out, grants)
	return context.WithValue(ctx, authGrantsKey, out)
}
