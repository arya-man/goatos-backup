package httpmiddleware

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/vgoats/goatos/backend/internal/permissions"
	platformauth "github.com/vgoats/goatos/backend/internal/platform/auth"
)

const (
	AuthModeBearer     = "bearer"
	AuthModeDevHeaders = "dev_headers"
)

var (
	ErrInvalidAuthConfig = errors.New("invalid auth config")
)

type TokenVerifier interface {
	Verify(token string) (platformauth.Claims, error)
}

type AuthConfig struct {
	Mode              string
	DevHeadersAllowed bool
	Environment       string
}

type AuthMiddleware struct {
	mode     string
	verifier TokenVerifier
	grants   permissions.GrantSource
	log      *slog.Logger
}

func NewAuthMiddleware(cfg AuthConfig, verifier TokenVerifier, grants permissions.GrantSource, log *slog.Logger) (*AuthMiddleware, error) {
	mode := strings.TrimSpace(cfg.Mode)
	if mode == "" {
		mode = AuthModeBearer
	}
	if log == nil {
		log = slog.Default()
	}
	if grants == nil {
		return nil, ErrInvalidAuthConfig
	}
	switch mode {
	case AuthModeBearer:
		if verifier == nil {
			return nil, ErrInvalidAuthConfig
		}
	case AuthModeDevHeaders:
		if !cfg.DevHeadersAllowed || !DevHeadersEnvironmentAllowed(cfg.Environment) {
			return nil, ErrInvalidAuthConfig
		}
		log.Warn("dev header auth enabled; never use outside local development")
	default:
		return nil, ErrInvalidAuthConfig
	}
	return &AuthMiddleware{mode: mode, verifier: verifier, grants: grants, log: log}, nil
}

func (a *AuthMiddleware) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isPublicHealthRoute(r) {
			next.ServeHTTP(w, r)
			return
		}

		ctx, userID, tenantID, ok := a.authenticate(w, r)
		if !ok {
			return
		}

		route, ok := permissions.Match(r.Method, r.URL.Path)
		if !ok {
			writeAuthError(w, r, http.StatusForbidden, "route_not_registered", "route is not registered for Phase 1 authorization")
			return
		}

		roles, err := a.grants.ActiveTenantRoles(ctx, userID, tenantID)
		if err != nil {
			a.log.ErrorContext(ctx, "auth_grant_lookup_failed",
				slog.String("request_id", RequestIDFromContext(ctx)),
				slog.String("trace_id", TraceIDFromContext(ctx)),
				slog.String("route", route.OperationID),
			)
			writeAuthError(w, r.WithContext(ctx), http.StatusInternalServerError, "auth_grant_lookup_failed", "authorization lookup failed")
			return
		}
		if !permissions.RolesAuthorize(roles, route.Permissions, route.AdminOnly) {
			writeAuthError(w, r.WithContext(ctx), http.StatusForbidden, "permission_denied", "permission denied")
			return
		}
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (a *AuthMiddleware) authenticate(w http.ResponseWriter, r *http.Request) (context.Context, string, string, bool) {
	switch a.mode {
	case AuthModeBearer:
		token, ok := bearerToken(r.Header.Get("Authorization"))
		if !ok {
			writeAuthError(w, r, http.StatusUnauthorized, "missing_bearer_token", "Authorization: Bearer token is required")
			return r.Context(), "", "", false
		}
		claims, err := a.verifier.Verify(token)
		if err != nil {
			writeAuthError(w, r, http.StatusUnauthorized, "invalid_bearer_token", "bearer token is invalid")
			return r.Context(), "", "", false
		}
		ctx := WithActorID(WithTenantID(r.Context(), claims.TenantID), claims.Subject)
		return ctx, claims.Subject, claims.TenantID, true
	case AuthModeDevHeaders:
		tenantID := strings.TrimSpace(r.Header.Get(headerTenantID))
		actorID := strings.TrimSpace(r.Header.Get(headerActorID))
		if tenantID == "" || actorID == "" {
			writeAuthError(w, r, http.StatusUnauthorized, "missing_dev_auth_headers", "local development auth headers are required")
			return r.Context(), "", "", false
		}
		ctx := WithActorID(WithTenantID(r.Context(), tenantID), actorID)
		return ctx, actorID, tenantID, true
	default:
		writeAuthError(w, r, http.StatusUnauthorized, "invalid_auth_mode", "auth mode is invalid")
		return r.Context(), "", "", false
	}
}

func bearerToken(value string) (string, bool) {
	value = strings.TrimSpace(value)
	const prefix = "Bearer "
	if !strings.HasPrefix(value, prefix) {
		return "", false
	}
	token := strings.TrimSpace(strings.TrimPrefix(value, prefix))
	return token, token != ""
}

func isPublicHealthRoute(r *http.Request) bool {
	return r.Method == http.MethodGet && (r.URL.Path == "/healthz" || r.URL.Path == "/readyz")
}

// DevHeadersEnvironmentAllowed is the exact allowlist for the local/dev header
// auth escape hatch. It is exported so bootstrap config validation and the
// middleware cannot drift.
func DevHeadersEnvironmentAllowed(env string) bool {
	env = strings.ToLower(strings.TrimSpace(env))
	return env == "local" || env == "dev" || env == "test"
}

type authErrorEnvelope struct {
	Code        string           `json:"code"`
	Message     string           `json:"message"`
	FieldErrors []authFieldError `json:"field_errors"`
	TraceID     string           `json:"trace_id"`
	Retryable   bool             `json:"retryable"`
}

type authFieldError struct {
	Field   string `json:"field"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

func writeAuthError(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	traceID := TraceIDFromContext(r.Context())
	if traceID == "" {
		traceID = "missing-trace"
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(authErrorEnvelope{
		Code:        code,
		Message:     message,
		FieldErrors: []authFieldError{},
		TraceID:     traceID,
		Retryable:   false,
	})
}
