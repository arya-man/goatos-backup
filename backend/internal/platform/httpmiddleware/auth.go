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
		log.Error("auth_middleware_config_invalid",
			slog.String("field", "app_check_mode"),
			slog.String("value", cfg.AppCheckMode),
			slog.Any("error", err))
		return nil, fmt.Errorf("auth middleware config: %w", err)
	}
	if appCheckMode != AppCheckModeOff && cfg.AppCheckVerifier == nil {
		return nil, fmt.Errorf("%w: app check verifier is required", ErrInvalidAuthConfig)
	}
	if grants == nil {
		return nil, ErrInvalidAuthConfig
	}
	allowedEmails, err := authallow.NewEmailSet(cfg.AllowedEmails)
	if err != nil {
		log.Error("auth_middleware_config_invalid",
			slog.String("field", "allowed_emails"),
			slog.Any("error", err))
		return nil, fmt.Errorf("%w: GOATOS_AUTH_ALLOWED_EMAILS must contain valid email addresses: %v", ErrInvalidAuthConfig, err)
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
			a.logAuthFailure(r, http.StatusForbidden, "route_not_registered",
				slog.String("route", r.Method+" "+r.URL.Path),
				slog.String("actor_id", userID),
				slog.String("tenant_id", tenantID),
			)
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
		if !permissions.AuthorizeRoute(route, roles) {
			// required_* travel with the denial: `roles:""` alone says the caller
			// held nothing, but not what the route WANTED — and that missing half
			// is what turns a 403 into an actionable grant/seeding fix instead of
			// a code-reading expedition.
			a.logAuthFailure(r, http.StatusForbidden, "permission_denied",
				slog.String("route", route.OperationID),
				slog.String("actor_id", userID),
				slog.String("tenant_id", tenantID),
				slog.String("roles", strings.Join(roles, ",")),
				slog.String("required_permissions", strings.Join(route.Permissions, ",")),
				slog.String("required_any_permissions", strings.Join(route.AnyPermissions, ",")),
				slog.Bool("required_admin_only", route.AdminOnly),
			)
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
			a.logAuthFailure(r, http.StatusUnauthorized, "missing_bearer_token",
				slog.Bool("authorization_header_present", strings.TrimSpace(r.Header.Get("Authorization")) != ""),
			)
			writeAuthError(w, r, http.StatusUnauthorized, "missing_bearer_token", "Authorization: Bearer token is required")
			return r.Context(), "", "", false
		}
		claims, err := a.verifier.Verify(token)
		if err != nil {
			a.logAuthFailure(r, http.StatusUnauthorized, "invalid_bearer_token",
				slog.String("error", err.Error()),
				slog.Int("token_length", len(token)),
			)
			writeAuthError(w, r, http.StatusUnauthorized, "invalid_bearer_token", "bearer token is invalid")
			return r.Context(), "", "", false
		}
		if !a.verifyAppCheck(w, r) {
			return r.Context(), "", "", false
		}
		if !a.allowedEmails.Allows(claims.Email, claims.EmailVerified) {
			a.logAuthFailure(r, http.StatusForbidden, "email_not_allowed",
				slog.String("email", normalizedEmailForLog(claims.Email)),
				slog.String("firebase_uid", claims.ExternalSubject),
				slog.String("actor_id", claims.Subject),
				slog.Bool("email_verified", claims.EmailVerified != nil && *claims.EmailVerified),
			)
			writeAuthError(w, r, http.StatusForbidden, "email_not_allowed", "this Google account is not allowed for Mesha Admin")
			return r.Context(), "", "", false
		}
		tenantID := claims.TenantID
		if tenantID == "" {
			tenantID = strings.TrimSpace(TenantIDFromContext(r.Context()))
		}
		if !uuidutil.IsUUIDString(tenantID) {
			a.logAuthFailure(r, http.StatusUnauthorized, "missing_tenant_context",
				slog.String("email", normalizedEmailForLog(claims.Email)),
				slog.String("firebase_uid", claims.ExternalSubject),
				slog.String("actor_id", claims.Subject),
				slog.String("tenant_id", tenantID),
			)
			writeAuthError(w, r, http.StatusUnauthorized, "missing_tenant_context", "tenant context is required")
			return r.Context(), "", "", false
		}
		ctx := WithDeviceID(WithActorID(WithTenantID(r.Context(), tenantID), claims.Subject), strings.TrimSpace(r.Header.Get(DeviceContextHeader)))
		if shouldLogAuthSuccess(r.Method, r.URL.Path) {
			a.log.InfoContext(ctx, "auth_succeeded",
				slog.String("request_id", RequestIDFromContext(ctx)),
				slog.String("trace_id", TraceIDFromContext(ctx)),
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.String("email", normalizedEmailForLog(claims.Email)),
				slog.String("firebase_uid", claims.ExternalSubject),
				slog.String("actor_id", claims.Subject),
				slog.String("tenant_id", tenantID),
			)
		}
		return ctx, claims.Subject, tenantID, true
	case AuthModeDevHeaders:
		tenantID := strings.TrimSpace(r.Header.Get(headerTenantID))
		actorID := strings.TrimSpace(r.Header.Get(headerActorID))
		if tenantID == "" || actorID == "" {
			a.logAuthFailure(r, http.StatusUnauthorized, "missing_dev_auth_headers")
			writeAuthError(w, r, http.StatusUnauthorized, "missing_dev_auth_headers", "local development auth headers are required")
			return r.Context(), "", "", false
		}
		ctx := WithDeviceID(WithActorID(WithTenantID(r.Context(), tenantID), actorID), strings.TrimSpace(r.Header.Get(DeviceContextHeader)))
		return ctx, actorID, tenantID, true
	default:
		a.logAuthFailure(r, http.StatusUnauthorized, "invalid_auth_mode")
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

func shouldLogAuthSuccess(method string, path string) bool {
	switch {
	case method == http.MethodPost && path == "/auth/session-events":
		return true
	case method == http.MethodGet && path == "/app/bootstrap":
		return true
	case method == http.MethodPost && path == "/app/devices/register":
		return true
	default:
		return false
	}
}

func (a *AuthMiddleware) verifyAppCheck(w http.ResponseWriter, r *http.Request) bool {
	switch a.appCheckMode {
	case AppCheckModeOff:
		return true
	case AppCheckModeMonitor, AppCheckModeEnforce:
	default:
		a.logAuthFailure(r, http.StatusUnauthorized, "invalid_auth_mode")
		writeAuthError(w, r, http.StatusUnauthorized, "invalid_auth_mode", "auth mode is invalid")
		return false
	}

	token := strings.TrimSpace(r.Header.Get(FirebaseAppCheckHeader))
	if token == "" {
		if a.appCheckMode == AppCheckModeEnforce {
			a.logAuthFailure(r, http.StatusUnauthorized, "missing_app_check")
			writeAuthError(w, r, http.StatusUnauthorized, "missing_app_check", FirebaseAppCheckHeader+" header is required")
			return false
		}
		a.logAppCheckMonitor(r, "missing", nil)
		return true
	}
	if _, err := a.appCheckVerifier.Verify(token); err != nil {
		if a.appCheckMode == AppCheckModeEnforce {
			a.logAuthFailure(r, http.StatusUnauthorized, "invalid_app_check",
				slog.String("error", err.Error()),
			)
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

func (a *AuthMiddleware) logAuthFailure(r *http.Request, status int, code string, fields ...slog.Attr) {
	args := []any{
		slog.String("request_id", RequestIDFromContext(r.Context())),
		slog.String("trace_id", TraceIDFromContext(r.Context())),
		slog.String("method", r.Method),
		slog.String("path", r.URL.Path),
		slog.Int("status", status),
		slog.String("code", code),
		slog.String("remote_addr", r.RemoteAddr),
		slog.String("user_agent", r.UserAgent()),
	}
	for _, field := range fields {
		args = append(args, field)
	}
	a.log.WarnContext(r.Context(), "auth_failed", args...)
}

func normalizedEmailForLog(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
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
	if r.Method != http.MethodGet {
		return false
	}
	switch r.URL.Path {
	case "/healthz", "/livez", "/readyz", "/version":
		// /version is a diagnostic surface (build SHA + migration-drift
		// status, see bootstrap.NewAPI), not a data route, so it is public
		// for the same reason the other three are: an operator or an
		// external prober needs it reachable without a token.
		return true
	default:
		return false
	}
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
		// Android reads the generated feed sheet from this legacy non-/app route. Admit a
		// park-scoped feed reader here; GetPreview capability-clamps park_id before reading.
		route.Pattern == "/feed-direction/preview" ||
		// Bug found on STG 2026-08-08: a park-scoped OPERATOR could see Feed Direction but got 403 on
		// packing, transport and the distribution completion -- 13 denials, zero successes -- because
		// this function dropped their park grant before AuthorizeRoute ever ran (roles resolved to "").
		// RoleOperator genuinely holds FeedPackingRead / FeedTransportRead / FeedDirectionComplete, so
		// no amount of granting a permission could have fixed it; only a tenant-scoped principal
		// (Chandrakant, CEO) worked, and that was incidental rather than intended. Same shape as the
		// /control-tower/ case documented below.
		//
		// Each of these three CLAMPS park_id to the caller's own grant via
		// ResolveAuthorizedParkScopeForCapabilities before it reads or writes anything, which is the
		// precondition for being listed here -- a CPT operator naming a CBE park is refused by the
		// handler, not merely hidden. Do NOT add a fourth feed route here without the same clamp.
		route.Pattern == "/feed-packing/worklist" ||
		route.Pattern == "/feed-transport/tasks" ||
		route.Pattern == "/feed-direction/distribution/complete" ||
		route.Pattern == "/feed-direction/packing/complete" ||
		// The transport SUBMIT was missed when the three routes above were admitted (2026-08-08), so
		// a park-scoped operator could open his transport task, record the mandatory video, and be
		// refused 403 on submit -- with roles resolved to "" here, before AuthorizeRoute ever ran.
		// He holds feed_direction.complete, the same permission the distribution completion above
		// accepts, which is why no permission change could have fixed it.
		//
		// It clamps like the others, but against the TASK's park rather than a park in the request:
		// this route names none, so PostTransportSubmit resolves the caller's own scope and
		// SubmitTransport refuses a task outside it (ports.ErrTransportParkForbidden).
		route.Pattern == "/feed-transport/tasks/{task_id}/submit" ||
		// Android shifting resolves a scanned tag through this legacy non-/app route. Admit
		// only the search route; SearchGoats capability-clamps park_id before reading.
		route.Pattern == "/goats/search" ||
		// The Android surface is scope-aware by construction: bootstrap carries the
		// actor's grants, lists are actor/park filtered, and writes validate task
		// assignment or handler scope. Requiring a tenant-wide grant here made a
		// genuinely park-scoped operator or park head unable to start the app.
		strings.HasPrefix(route.Pattern, "/app/") ||
		strings.HasPrefix(route.Pattern, "/verification/") ||
		// Bug found on-device 2026-08-04: GET /control-tower/vaccination is the ONLY
		// route registered under this prefix (permissions/routes.go), it backs the
		// mobile Alerts tab (AlertsViewModel -> ControlTowerRepository), and its
		// handler (processintegrity/adapters/http/handler.go, buildQuery) already
		// self-narrows every request to the caller's OWN park via
		// ResolveAuthorizedParkScope -- an explicit other-park park_id 403s
		// (park_scope_test.go). A park-scoped operator's grant was being dropped by
		// THIS function before permissions.AuthorizeRoute ever ran (roles resolved to
		// ""), so no amount of granting operator a permission in permissions.go could
		// ever authorize it -- the same "mobile surface needs its scoped grant seen"
		// rationale documented for /app/ above. If a SECOND route is ever registered
		// under /control-tower/ that is NOT proven park-scoped in its own handler the
		// way this one is, it must not rely on this prefix match without an equivalent
		// scope-narrowing guard -- re-audit this comment at that time.
		strings.HasPrefix(route.Pattern, "/control-tower/")
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
	ParkID string
	// Every park the actor may see. Populated whenever the actor is park-scoped, so a
	// caller that can genuinely serve a multi-park view has the set rather than having to
	// re-derive it from the grants.
	ParkIDs []string
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
	return decideParkScope(AuthorizedParkIDs(grants), requestedParkID)
}

// ResolveAuthorizedParkScopeForCapabilities is the CAPABILITY-AWARE form of
// ResolveAuthorizedParkScope and is what every authorization decision must call.
//
// It differs on both halves of the check:
//
//   - tenant-wide is HasTenantWideCapability, not HasTenantWideGrant: a tenant grant for an
//     unrelated role (a growth director's weighing grant) no longer waves through another
//     module's park scope.
//   - the park set is the union of AuthorizedParkIDsForCapability over `capabilities`, so a
//     grant's park counts only when THAT grant's own role carries one of them. The blind
//     AuthorizedParkIDs form let an actor combine an unrelated park-A grant with a
//     capability-carrying park-B grant and act in park A.
//
// `capabilities` is a set of alternatives (any one suffices), which is how a surface that
// serves both an executor and a read-only overseer expresses itself. Passing none is a
// programming error and fails closed.
func ResolveAuthorizedParkScopeForCapabilities(ctx context.Context, tenantID, requestedParkID string, capabilities ...string) ParkScopeDecision {
	grants := AuthGrantsFromContext(ctx)
	// No grants at all = internal/service context (e.g. context.Background() in an
	// integration test or a CLI), same escape hatch the blind form has always had.
	if len(grants) == 0 {
		return ParkScopeDecision{ParkID: requestedParkID, Allowed: true}
	}
	for _, capability := range capabilities {
		if HasTenantWideCapability(grants, tenantID, capability) {
			return ParkScopeDecision{ParkID: requestedParkID, Allowed: true}
		}
	}
	seen := map[string]struct{}{}
	parkIDs := []string{}
	for _, capability := range capabilities {
		for _, parkID := range AuthorizedParkIDsForCapability(grants, capability) {
			if _, ok := seen[parkID]; ok {
				continue
			}
			seen[parkID] = struct{}{}
			parkIDs = append(parkIDs, parkID)
		}
	}
	sort.Strings(parkIDs)
	return decideParkScope(parkIDs, requestedParkID)
}

// decideParkScope is the shared tail of both resolvers: match the request against an
// already-computed authorized park set.
func decideParkScope(parkIDs []string, requestedParkID string) ParkScopeDecision {
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
				return ParkScopeDecision{ParkID: requestedParkID, ParkIDs: parkIDs, Allowed: true}
			}
		}
		return ParkScopeDecision{
			ParkIDs: parkIDs,
			Allowed: false,
			Status:  http.StatusForbidden,
			Code:    "park_scope_forbidden",
			Message: "requested park is outside the actor's authorized scope",
		}
	}
	if len(parkIDs) > 1 {
		// Someone who covers more than one park without holding a tenant grant. Silently
		// answering for parkIDs[0] would show a director half their herd and no error --
		// the worst possible outcome, because a wrong number that looks right is acted on.
		return ParkScopeDecision{
			ParkIDs: parkIDs,
			Allowed: false,
			Status:  http.StatusBadRequest,
			Code:    "park_selection_required",
			Message: "choose a park to view",
		}
	}
	return ParkScopeDecision{ParkID: parkIDs[0], ParkIDs: parkIDs, Allowed: true}
}

// HasTenantWideCapability reports whether any grant is scoped to the whole tenant AND that
// SAME grant's role carries `capability`. This is the role-aware counterpart to
// HasTenantWideGrant, which checks scope only and therefore treats an unrelated tenant-wide
// role as authority over every module.
func HasTenantWideCapability(grants []permissions.ActiveGrant, tenantID, capability string) bool {
	for _, grant := range grants {
		if grant.ScopeType == "tenant" && grant.ScopeID == tenantID &&
			permissions.RoleHasPermission(grant.Role, capability) {
			return true
		}
	}
	return false
}

// HasTenantWideGrant reports whether any grant is scoped to the whole tenant.
//
// SCOPE-ONLY -- prefer HasTenantWideCapability for authorization decisions.
func HasTenantWideGrant(grants []permissions.ActiveGrant, tenantID string) bool {
	for _, grant := range grants {
		if grant.ScopeType == "tenant" && grant.ScopeID == tenantID {
			return true
		}
	}
	return false
}

// AuthorizedParkIDs returns the distinct, sorted park scope ids across the actor's grants,
// WITHOUT regard to what capability the grant's role actually carries.
//
// CAPABILITY-BLIND -- DO NOT USE FOR AUTHORIZATION DECISIONS. It only tells you which parks
// the actor has ANY grant in, not which parks they may exercise a given capability in. A
// caller that separately checks "does any role in my flat role list carry capability X" and
// then intersects with this list is vulnerable to privilege escalation: an actor with an
// unrelated park-A grant plus a capability-X-carrying grant scoped to park B gets capability X
// in park A too, because the two checks were decoupled from each other's grant.
//
// Use AuthorizedParkIDsForCapability instead for any authorization/scoping decision. This
// function is kept only for existing non-authorization callers (e.g. building a set of "parks
// I have some presence in" for display purposes); it must never gate a capability.
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

// AuthorizedParkIDsForCapability returns the distinct, sorted park scope ids across the
// actor's grants, ONLY counting a grant's park if that grant's own role carries the given
// capability (per the permissions role -> capability table, permissions.RoleHasPermission).
//
// This is the capability-aware replacement for AuthorizedParkIDs: it keeps each grant's role
// and its scope bound together, so an actor cannot combine an unrelated park grant with a
// capability-carrying grant scoped to a DIFFERENT park to gain that capability in the first
// park. Every caller that authorizes a park-scoped action or read MUST use this function (or
// permissions.ScopeIDsForPermission directly), never AuthorizedParkIDs.
func AuthorizedParkIDsForCapability(grants []permissions.ActiveGrant, capability string) []string {
	ids := permissions.ScopeIDsForPermission(grants, capability, "park")
	sort.Strings(ids)
	return ids
}
