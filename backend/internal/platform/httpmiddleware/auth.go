package httpmiddleware

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strings"

	"github.com/vgoats/goatos/backend/internal/permissions"
	platformauth "github.com/vgoats/goatos/backend/internal/platform/auth"
	"github.com/vgoats/goatos/backend/internal/platform/authallow"
	"github.com/vgoats/goatos/backend/internal/platform/uuidutil"
)

const (
	AuthModeBearer     = "bearer"
	AuthModeDevHeaders = "dev_headers"

	AppCheckModeOff     = "off"
	AppCheckModeMonitor = "monitor"
	AppCheckModeEnforce = "enforce"

	FirebaseAppCheckHeader = "X-Firebase-AppCheck"
)

var (
	ErrInvalidAuthConfig = errors.New("invalid auth config")
)

type TokenVerifier interface {
	Verify(token string) (platformauth.Claims, error)
}

type AuthConfig struct {
	Mode             string
	AppCheckMode     string
	AppCheckVerifier TokenVerifier

	DevHeadersAllowed bool
	Environment       string
	AllowedEmails     []string
}

type AuthMiddleware struct {
	mode             string
	verifier         TokenVerifier
	appCheckMode     string
	appCheckVerifier TokenVerifier
	grants           permissions.GrantSource
	log              *slog.Logger
	allowedEmails    authallow.EmailSet
}

func NewAuthMiddleware(cfg AuthConfig, verifier TokenVerifier, grants permissions.GrantSource, log *slog.Logger) (*AuthMiddleware, error) {
	mode := strings.TrimSpace(cfg.Mode)
	if mode == "" {
		mode = AuthModeBearer
	}
	if log == nil {
		log = slog.Default()
	}
	appCheckMode, err := normalizeAppCheckMode(cfg.AppCheckMode)
	if err != nil {
		return nil, err
	}
	if appCheckMode != AppCheckModeOff && cfg.AppCheckVerifier == nil {
		return nil, fmt.Errorf("%w: app check verifier is required", ErrInvalidAuthConfig)
	}
	if grants == nil {
		return nil, ErrInvalidAuthConfig
	}
	allowedEmails, err := authallow.NewEmailSet(cfg.AllowedEmails)
	if err != nil {
		return nil, fmt.Errorf("%w: GOATOS_AUTH_ALLOWED_EMAILS must contain valid email addresses", ErrInvalidAuthConfig)
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
		if appCheckMode != AppCheckModeOff {
			return nil, fmt.Errorf("%w: app check requires bearer auth mode", ErrInvalidAuthConfig)
		}
		log.Warn("dev header auth enabled; never use outside local development")
	default:
		return nil, ErrInvalidAuthConfig
	}
	return &AuthMiddleware{
		mode:             mode,
		verifier:         verifier,
		appCheckMode:     appCheckMode,
		appCheckVerifier: cfg.AppCheckVerifier,
		grants:           grants,
		log:              log,
		allowedEmails:    allowedEmails,
	}, nil
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

		grants, err := a.grants.ActiveTenantGrants(ctx, userID, tenantID)
		if err != nil {
			a.log.ErrorContext(ctx, "auth_grant_lookup_failed",
				slog.String("request_id", RequestIDFromContext(ctx)),
				slog.String("trace_id", TraceIDFromContext(ctx)),
				slog.String("route", route.OperationID),
			)
			writeAuthError(w, r.WithContext(ctx), http.StatusInternalServerError, "auth_grant_lookup_failed", "authorization lookup failed")
			return
		}
		ctx = WithAuthGrants(ctx, grants)
		roles := routeRoles(route, grants, tenantID)
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
		if !a.verifyAppCheck(w, r) {
			return r.Context(), "", "", false
		}
		if !a.allowedEmails.Allows(claims.Email, claims.EmailVerified) {
			writeAuthError(w, r, http.StatusForbidden, "email_not_allowed", "this Google account is not allowed for Mesha Admin")
			return r.Context(), "", "", false
		}
		tenantID := claims.TenantID
		if tenantID == "" {
			tenantID = strings.TrimSpace(TenantIDFromContext(r.Context()))
		}
		if !uuidutil.IsUUIDString(tenantID) {
			writeAuthError(w, r, http.StatusUnauthorized, "missing_tenant_context", "tenant context is required")
			return r.Context(), "", "", false
		}
		ctx := WithActorID(WithTenantID(r.Context(), tenantID), claims.Subject)
		return ctx, claims.Subject, tenantID, true
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

func normalizeAppCheckMode(mode string) (string, error) {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "" {
		mode = AppCheckModeOff
	}
	switch mode {
	case AppCheckModeOff, AppCheckModeMonitor, AppCheckModeEnforce:
		return mode, nil
	default:
		return "", fmt.Errorf("%w: GOATOS_APPCHECK_ENFORCE must be off, monitor, or enforce", ErrInvalidAuthConfig)
	}
}

func (a *AuthMiddleware) verifyAppCheck(w http.ResponseWriter, r *http.Request) bool {
	switch a.appCheckMode {
	case AppCheckModeOff:
		return true
	case AppCheckModeMonitor, AppCheckModeEnforce:
	default:
		writeAuthError(w, r, http.StatusUnauthorized, "invalid_auth_mode", "auth mode is invalid")
		return false
	}

	token := strings.TrimSpace(r.Header.Get(FirebaseAppCheckHeader))
	if token == "" {
		if a.appCheckMode == AppCheckModeEnforce {
			writeAuthError(w, r, http.StatusUnauthorized, "missing_app_check", FirebaseAppCheckHeader+" header is required")
			return false
		}
		a.logAppCheckMonitor(r, "missing", nil)
		return true
	}
	if _, err := a.appCheckVerifier.Verify(token); err != nil {
		if a.appCheckMode == AppCheckModeEnforce {
			writeAuthError(w, r, http.StatusUnauthorized, "invalid_app_check", "Firebase App Check token is invalid")
			return false
		}
		a.logAppCheckMonitor(r, "invalid", err)
		return true
	}
	if a.appCheckMode == AppCheckModeMonitor {
		a.logAppCheckMonitor(r, "pass", nil)
	}
	return true
}

func (a *AuthMiddleware) logAppCheckMonitor(r *http.Request, result string, err error) {
	args := []any{
		slog.String("request_id", RequestIDFromContext(r.Context())),
		slog.String("trace_id", TraceIDFromContext(r.Context())),
		slog.String("method", r.Method),
		slog.String("path", r.URL.Path),
		slog.String("result", result),
	}
	if err != nil {
		args = append(args, slog.String("error", err.Error()))
	}
	if result == "pass" {
		a.log.InfoContext(r.Context(), "app_check_monitor", args...)
		return
	}
	a.log.WarnContext(r.Context(), "app_check_monitor", args...)
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
	return r.Method == http.MethodGet && (r.URL.Path == "/healthz" || r.URL.Path == "/livez" || r.URL.Path == "/readyz")
}

func routeRoles(route permissions.Route, grants []permissions.ActiveGrant, tenantID string) []string {
	roles := make([]string, 0, len(grants))
	seen := map[string]struct{}{}
	for _, grant := range grants {
		if !routeAllowsScopedGrants(route) && !(grant.ScopeType == "tenant" && grant.ScopeID == tenantID) {
			continue
		}
		if _, ok := seen[grant.Role]; ok {
			continue
		}
		seen[grant.Role] = struct{}{}
		roles = append(roles, grant.Role)
	}
	return roles
}

func routeAllowsScopedGrants(route permissions.Route) bool {
	return strings.HasPrefix(route.Pattern, "/calendar/") ||
		strings.HasPrefix(route.Pattern, "/app/vaccination/")
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

// ParkScopeDecision is the resolved park-scope authorization for a vaccination read handler:
// which park the actor may see, or why access is denied. Handlers clamp their repository queries
// to Decision.ParkID when Allowed, else return Status/Code/Message.
type ParkScopeDecision struct {
	ParkID  string
	Allowed bool
	Status  int
	Code    string
	Message string
}

// ResolveAuthorizedParkScope clamps a requested park to the actor's grant scope. A tenant-wide
// grant (or no grants, e.g. an internal service context) sees the requested park verbatim; a
// park-scoped actor may only see a park within AuthorizedParkIDs, and an empty request defaults
// to their first authorized park. Used by vaccination-execution read handlers before querying.
func ResolveAuthorizedParkScope(ctx context.Context, tenantID, requestedParkID string) ParkScopeDecision {
	grants := AuthGrantsFromContext(ctx)
	if len(grants) == 0 || HasTenantWideGrant(grants, tenantID) {
		return ParkScopeDecision{ParkID: requestedParkID, Allowed: true}
	}
	parkIDs := AuthorizedParkIDs(grants)
	if len(parkIDs) == 0 {
		return ParkScopeDecision{
			Allowed: false,
			Status:  http.StatusForbidden,
			Code:    "park_scope_required",
			Message: "this route requires an authorized park scope",
		}
	}
	if requestedParkID != "" {
		for _, parkID := range parkIDs {
			if parkID == requestedParkID {
				return ParkScopeDecision{ParkID: requestedParkID, Allowed: true}
			}
		}
		return ParkScopeDecision{
			Allowed: false,
			Status:  http.StatusForbidden,
			Code:    "park_scope_forbidden",
			Message: "requested park is outside the actor's authorized scope",
		}
	}
	return ParkScopeDecision{ParkID: parkIDs[0], Allowed: true}
}

// HasTenantWideGrant reports whether any grant is scoped to the whole tenant.
func HasTenantWideGrant(grants []permissions.ActiveGrant, tenantID string) bool {
	for _, grant := range grants {
		if grant.ScopeType == "tenant" && grant.ScopeID == tenantID {
			return true
		}
	}
	return false
}

// AuthorizedParkIDs returns the distinct, sorted park scope ids across the actor's grants.
func AuthorizedParkIDs(grants []permissions.ActiveGrant) []string {
	seen := map[string]struct{}{}
	out := []string{}
	for _, grant := range grants {
		if grant.ScopeType != "park" || strings.TrimSpace(grant.ScopeID) == "" {
			continue
		}
		if _, ok := seen[grant.ScopeID]; ok {
			continue
		}
		seen[grant.ScopeID] = struct{}{}
		out = append(out, grant.ScopeID)
	}
	sort.Strings(out)
	return out
}
