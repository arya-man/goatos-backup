package httpmiddleware

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/permissions"
	platformauth "github.com/vgoats/goatos/backend/internal/platform/auth"
)

const (
	authTestSecret   = "0123456789abcdef0123456789abcdef"
	authTestIssuer   = "goatos-test"
	authTestAudience = "goatos-api"
	authTestTenant   = "00000000-0000-4000-8000-000000000001"
	authSpoofTenant  = "00000000-0000-4000-8000-000000000099"
	authTestUser     = "90000000-0000-4000-8000-000000000001"
)

func TestBearerAuthUsesTokenContextAndIgnoresSpoofHeaders(t *testing.T) {
	mw := testBearerMiddleware(t, fakeGrantSource{roles: map[string][]string{authTestUser + "|" + authTestTenant: {permissions.RoleOperator}}})
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := TenantIDFromContext(r.Context()); got != authTestTenant {
			t.Fatalf("tenant=%s want token tenant", got)
		}
		if got := ActorIDFromContext(r.Context()); got != authTestUser {
			t.Fatalf("actor=%s want token subject", got)
		}
		w.WriteHeader(http.StatusNoContent)
	})
	handler := RequestContext(slog.New(slog.NewTextHandler(io.Discard, nil)))(mw.Wrap(next))
	req := httptest.NewRequest(http.MethodGet, "/goats/search?limit=10", nil)
	req.Header.Set("Authorization", "Bearer "+testToken(t, authTestUser, authTestTenant, map[string]any{"role": "admin"}))
	req.Header.Set("X-GoatOS-Tenant-ID", authSpoofTenant)
	req.Header.Set("X-GoatOS-Actor-ID", "90000000-0000-4000-8000-000000000099")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestBearerAuthUsesHeaderTenantWhenTokenHasNoTenantClaim(t *testing.T) {
	externalSubject := "firebase-uid-abc123"
	actorID := platformauth.StableSubjectID(authTestIssuer, externalSubject)
	mw, err := NewAuthMiddleware(
		AuthConfig{Mode: AuthModeBearer},
		staticVerifier{claims: platformauth.Claims{Subject: actorID, ExternalSubject: externalSubject, Issuer: authTestIssuer, Audience: authTestAudience}},
		grantAdapter{fakeGrantSource{roles: map[string][]string{actorID + "|" + authTestTenant: {permissions.RoleOperator}}}},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	if err != nil {
		t.Fatalf("NewAuthMiddleware: %v", err)
	}
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := TenantIDFromContext(r.Context()); got != authTestTenant {
			t.Fatalf("tenant=%s want header tenant", got)
		}
		if got := ActorIDFromContext(r.Context()); got != actorID {
			t.Fatalf("actor=%s want mapped actor", got)
		}
		w.WriteHeader(http.StatusNoContent)
	})
	handler := RequestContext(slog.New(slog.NewTextHandler(io.Discard, nil)))(mw.Wrap(next))
	req := httptest.NewRequest(http.MethodGet, "/goats/search?limit=10", nil)
	req.Header.Set("Authorization", "Bearer verified-firebase-token")
	req.Header.Set("X-GoatOS-Tenant-ID", authTestTenant)
	req.Header.Set("X-GoatOS-Actor-ID", authTestUser)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestBearerAuthDeniesHeaderTenantWithoutMatchingGrant(t *testing.T) {
	externalSubject := "firebase-uid-abc123"
	actorID := platformauth.StableSubjectID(authTestIssuer, externalSubject)
	mw, err := NewAuthMiddleware(
		AuthConfig{Mode: AuthModeBearer},
		staticVerifier{claims: platformauth.Claims{Subject: actorID, ExternalSubject: externalSubject, Issuer: authTestIssuer, Audience: authTestAudience}},
		grantAdapter{fakeGrantSource{roles: map[string][]string{actorID + "|" + authTestTenant: {permissions.RoleOperator}}}},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	if err != nil {
		t.Fatalf("NewAuthMiddleware: %v", err)
	}
	handler := RequestContext(slog.New(slog.NewTextHandler(io.Discard, nil)))(mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Fatal("handler should not run without a grant for the requested tenant")
	})))
	req := httptest.NewRequest(http.MethodGet, "/goats/search?limit=10", nil)
	req.Header.Set("Authorization", "Bearer verified-firebase-token")
	req.Header.Set("X-GoatOS-Tenant-ID", authSpoofTenant)
	req.Header.Set("X-GoatOS-Actor-ID", authTestUser)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	assertAuthErrorCode(t, rec, "permission_denied")
}

func TestBearerAuthAllowsOnlyConfiguredVerifiedEmails(t *testing.T) {
	verified := true
	externalSubject := "firebase-uid-ravi"
	actorID := platformauth.StableSubjectID(authTestIssuer, externalSubject)
	mw, err := NewAuthMiddleware(
		AuthConfig{Mode: AuthModeBearer, AllowedEmails: []string{"ravi@mesha.sg", "abhishek@mesha.sg"}},
		staticVerifier{claims: platformauth.Claims{
			Subject:         actorID,
			ExternalSubject: externalSubject,
			Issuer:          authTestIssuer,
			Audience:        authTestAudience,
			Email:           "Ravi@Mesha.SG",
			EmailVerified:   &verified,
		}},
		grantAdapter{fakeGrantSource{roles: map[string][]string{actorID + "|" + authTestTenant: {permissions.RoleOperator}}}},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	if err != nil {
		t.Fatalf("NewAuthMiddleware: %v", err)
	}
	handler := RequestContext(slog.New(slog.NewTextHandler(io.Discard, nil)))(mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})))
	req := httptest.NewRequest(http.MethodGet, "/goats/search?limit=10", nil)
	req.Header.Set("Authorization", "Bearer verified-firebase-token")
	req.Header.Set("X-GoatOS-Tenant-ID", authTestTenant)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestBearerAuthRejectsEmailOutsideAllowlist(t *testing.T) {
	verified := true
	externalSubject := "firebase-uid-hr"
	actorID := platformauth.StableSubjectID(authTestIssuer, externalSubject)
	mw, err := NewAuthMiddleware(
		AuthConfig{Mode: AuthModeBearer, AllowedEmails: []string{"ravi@mesha.sg", "abhishek@mesha.sg"}},
		staticVerifier{claims: platformauth.Claims{
			Subject:         actorID,
			ExternalSubject: externalSubject,
			Issuer:          authTestIssuer,
			Audience:        authTestAudience,
			Email:           "hr@mesha.sg",
			EmailVerified:   &verified,
		}},
		grantAdapter{fakeGrantSource{roles: map[string][]string{actorID + "|" + authTestTenant: {permissions.RoleOperator}}}},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	if err != nil {
		t.Fatalf("NewAuthMiddleware: %v", err)
	}
	handler := RequestContext(slog.New(slog.NewTextHandler(io.Discard, nil)))(mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Fatal("handler should not run for an unlisted email")
	})))
	req := httptest.NewRequest(http.MethodGet, "/goats/search?limit=10", nil)
	req.Header.Set("Authorization", "Bearer verified-firebase-token")
	req.Header.Set("X-GoatOS-Tenant-ID", authTestTenant)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	assertAuthErrorCode(t, rec, "email_not_allowed")
}

func TestBearerAuthRejectsUnverifiedEmailWhenAllowlistIsConfigured(t *testing.T) {
	unverified := false
	mw, err := NewAuthMiddleware(
		AuthConfig{Mode: AuthModeBearer, AllowedEmails: []string{"ravi@mesha.sg"}},
		staticVerifier{claims: platformauth.Claims{
			Subject:       authTestUser,
			Issuer:        authTestIssuer,
			Audience:      authTestAudience,
			Email:         "ravi@mesha.sg",
			EmailVerified: &unverified,
		}},
		grantAdapter{fakeGrantSource{roles: map[string][]string{authTestUser + "|" + authTestTenant: {permissions.RoleOperator}}}},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	if err != nil {
		t.Fatalf("NewAuthMiddleware: %v", err)
	}
	handler := RequestContext(slog.New(slog.NewTextHandler(io.Discard, nil)))(mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Fatal("handler should not run for an unverified email")
	})))
	req := httptest.NewRequest(http.MethodGet, "/goats/search?limit=10", nil)
	req.Header.Set("Authorization", "Bearer verified-firebase-token")
	req.Header.Set("X-GoatOS-Tenant-ID", authTestTenant)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	assertAuthErrorCode(t, rec, "email_not_allowed")
}

func TestBearerAuthRequiresTenantContextWhenTokenHasNoTenantClaim(t *testing.T) {
	mw, err := NewAuthMiddleware(
		AuthConfig{Mode: AuthModeBearer},
		staticVerifier{claims: platformauth.Claims{Subject: authTestUser, Issuer: authTestIssuer, Audience: authTestAudience}},
		grantAdapter{fakeGrantSource{roles: map[string][]string{authTestUser + "|" + authTestTenant: {permissions.RoleOperator}}}},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	if err != nil {
		t.Fatalf("NewAuthMiddleware: %v", err)
	}
	handler := RequestContext(slog.New(slog.NewTextHandler(io.Discard, nil)))(mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})))
	req := httptest.NewRequest(http.MethodGet, "/goats/search?limit=10", nil)
	req.Header.Set("Authorization", "Bearer verified-firebase-token")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	assertAuthErrorCode(t, rec, "missing_tenant_context")
}

func TestBearerAuthDeniesMissingMalformedAndMissingGrant(t *testing.T) {
	tests := []struct {
		name     string
		header   string
		roles    []string
		wantCode int
		wantErr  string
	}{
		{name: "missing", wantCode: http.StatusUnauthorized, wantErr: "missing_bearer_token"},
		{name: "malformed", header: "Bearer not-a-token", wantCode: http.StatusUnauthorized, wantErr: "invalid_bearer_token"},
		{name: "no grant", header: "Bearer " + testTokenStatic(authTestUser, authTestTenant, nil), wantCode: http.StatusForbidden, wantErr: "permission_denied"},
		{name: "revoked modeled as no active grant", header: "Bearer " + testTokenStatic(authTestUser, authTestTenant, nil), roles: nil, wantCode: http.StatusForbidden, wantErr: "permission_denied"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mw := testBearerMiddleware(t, fakeGrantSource{roles: map[string][]string{authTestUser + "|" + authTestTenant: tt.roles}})
			handler := RequestContext(slog.New(slog.NewTextHandler(io.Discard, nil)))(mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNoContent)
			})))
			req := httptest.NewRequest(http.MethodGet, "/goats/search?limit=10", nil)
			if tt.header != "" {
				req.Header.Set("Authorization", tt.header)
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != tt.wantCode {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
			assertAuthErrorCode(t, rec, tt.wantErr)
		})
	}
}

func TestBearerAuthIgnoresForgedRoleClaim(t *testing.T) {
	mw := testBearerMiddleware(t, fakeGrantSource{roles: map[string][]string{authTestUser + "|" + authTestTenant: {permissions.RoleOperator}}})
	handler := RequestContext(slog.New(slog.NewTextHandler(io.Discard, nil)))(mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})))
	req := httptest.NewRequest(http.MethodPost, "/admin/goats/10000000-0000-4000-8000-000000000001/identifiers", nil)
	req.Header.Set("Authorization", "Bearer "+testToken(t, authTestUser, authTestTenant, map[string]any{"role": "admin"}))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	assertAuthErrorCode(t, rec, "permission_denied")
}

func TestBearerAuthRejectsTokenBeyondMaxTTL(t *testing.T) {
	mw := testBearerMiddlewareWithTTL(t, fakeGrantSource{roles: map[string][]string{authTestUser + "|" + authTestTenant: {permissions.RoleOperator}}}, 30*time.Minute)
	handler := RequestContext(slog.New(slog.NewTextHandler(io.Discard, nil)))(mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})))
	req := httptest.NewRequest(http.MethodGet, "/goats/search?limit=10", nil)
	req.Header.Set("Authorization", "Bearer "+testToken(t, authTestUser, authTestTenant, nil))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	assertAuthErrorCode(t, rec, "invalid_bearer_token")
}

func TestAuthRunsBeforeNotImplementedStubs(t *testing.T) {
	mw := testBearerMiddleware(t, fakeGrantSource{roles: map[string][]string{authTestUser + "|" + authTestTenant: {permissions.RoleVerifier}}})
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotImplemented)
	})
	handler := RequestContext(slog.New(slog.NewTextHandler(io.Discard, nil)))(mw.Wrap(next))

	path := "/admin/goats/10000000-0000-4000-8000-000000000001/identifiers"

	unauth := httptest.NewRequest(http.MethodPost, path, nil)
	unauthRec := httptest.NewRecorder()
	handler.ServeHTTP(unauthRec, unauth)
	if unauthRec.Code != http.StatusUnauthorized {
		t.Fatalf("unauth status=%d body=%s", unauthRec.Code, unauthRec.Body.String())
	}

	forbidden := httptest.NewRequest(http.MethodPost, path, nil)
	forbidden.Header.Set("Authorization", "Bearer "+testToken(t, authTestUser, authTestTenant, nil))
	mwNoGrant := testBearerMiddleware(t, fakeGrantSource{})
	forbiddenRec := httptest.NewRecorder()
	RequestContext(slog.New(slog.NewTextHandler(io.Discard, nil)))(mwNoGrant.Wrap(next)).ServeHTTP(forbiddenRec, forbidden)
	if forbiddenRec.Code != http.StatusForbidden {
		t.Fatalf("forbidden status=%d body=%s", forbiddenRec.Code, forbiddenRec.Body.String())
	}

	authorized := httptest.NewRequest(http.MethodPost, path, nil)
	authorized.Header.Set("Authorization", "Bearer "+testToken(t, authTestUser, authTestTenant, nil))
	authorizedRec := httptest.NewRecorder()
	handler.ServeHTTP(authorizedRec, authorized)
	if authorizedRec.Code != http.StatusNotImplemented {
		t.Fatalf("authorized status=%d body=%s", authorizedRec.Code, authorizedRec.Body.String())
	}
}

func TestAuthFailsClosedForUnregisteredProtectedRoute(t *testing.T) {
	mw := testBearerMiddleware(t, fakeGrantSource{roles: map[string][]string{authTestUser + "|" + authTestTenant: {permissions.RoleAdmin}}})
	handler := RequestContext(slog.New(slog.NewTextHandler(io.Discard, nil)))(mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})))
	req := httptest.NewRequest(http.MethodGet, "/admin/not-registered", nil)
	req.Header.Set("Authorization", "Bearer "+testToken(t, authTestUser, authTestTenant, nil))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	assertAuthErrorCode(t, rec, "route_not_registered")
}

func TestCalendarRoutesMayUseScopedGrantsWithoutBroadeningOtherRoutes(t *testing.T) {
	scopedGrant := permissions.ActiveGrant{Role: permissions.RoleParkHead, ScopeType: "park", ScopeID: "86000000-0000-4000-8000-000000000701"}
	mw := testBearerMiddleware(t, fakeGrantSource{grants: map[string][]permissions.ActiveGrant{authTestUser + "|" + authTestTenant: {scopedGrant}}})
	calendarHandler := RequestContext(slog.New(slog.NewTextHandler(io.Discard, nil)))(mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		grants := AuthGrantsFromContext(r.Context())
		if len(grants) != 1 || grants[0] != scopedGrant {
			t.Fatalf("grants=%#v want scoped calendar grant", grants)
		}
		w.WriteHeader(http.StatusNoContent)
	})))
	calendarReq := httptest.NewRequest(http.MethodGet, "/calendar/vaccination/events", nil)
	calendarReq.Header.Set("Authorization", "Bearer "+testToken(t, authTestUser, authTestTenant, nil))
	calendarRec := httptest.NewRecorder()
	calendarHandler.ServeHTTP(calendarRec, calendarReq)
	if calendarRec.Code != http.StatusNoContent {
		t.Fatalf("calendar status=%d body=%s", calendarRec.Code, calendarRec.Body.String())
	}

	otherHandler := RequestContext(slog.New(slog.NewTextHandler(io.Discard, nil)))(mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Fatal("scoped grant should not authorize non-calendar route")
	})))
	otherReq := httptest.NewRequest(http.MethodGet, "/vaccination/operations", nil)
	otherReq.Header.Set("Authorization", "Bearer "+testToken(t, authTestUser, authTestTenant, nil))
	otherRec := httptest.NewRecorder()
	otherHandler.ServeHTTP(otherRec, otherReq)
	if otherRec.Code != http.StatusForbidden {
		t.Fatalf("other status=%d body=%s", otherRec.Code, otherRec.Body.String())
	}
	assertAuthErrorCode(t, otherRec, "permission_denied")
}

func TestDevHeadersRequireExplicitLocalOptIn(t *testing.T) {
	if _, err := NewAuthMiddleware(AuthConfig{Mode: AuthModeDevHeaders, Environment: "prod", DevHeadersAllowed: true}, nil, grantAdapter{fakeGrantSource{}}, slog.Default()); err == nil {
		t.Fatal("dev_headers accepted prod environment")
	}
	if _, err := NewAuthMiddleware(AuthConfig{Mode: AuthModeDevHeaders, Environment: "not-production", DevHeadersAllowed: true}, nil, grantAdapter{fakeGrantSource{}}, slog.Default()); err == nil {
		t.Fatal("dev_headers accepted non-allowlisted environment")
	}
	if _, err := NewAuthMiddleware(AuthConfig{Mode: AuthModeDevHeaders, Environment: "local", DevHeadersAllowed: false}, nil, grantAdapter{fakeGrantSource{}}, slog.Default()); err == nil {
		t.Fatal("dev_headers accepted missing allow flag")
	}
	if _, err := NewAuthMiddleware(AuthConfig{Mode: "surprise"}, nil, grantAdapter{fakeGrantSource{}}, slog.Default()); err == nil {
		t.Fatal("unknown auth mode accepted")
	}
	if _, err := NewAuthMiddleware(AuthConfig{Mode: AuthModeDevHeaders, Environment: "local", DevHeadersAllowed: true}, nil, grantAdapter{fakeGrantSource{}}, slog.Default()); err != nil {
		t.Fatalf("dev_headers local opt-in rejected: %v", err)
	}
	if _, err := NewAuthMiddleware(AuthConfig{Mode: AuthModeDevHeaders, Environment: "dev", DevHeadersAllowed: true}, nil, grantAdapter{fakeGrantSource{}}, slog.Default()); err != nil {
		t.Fatalf("dev_headers dev opt-in rejected: %v", err)
	}
	if _, err := NewAuthMiddleware(AuthConfig{Mode: AuthModeDevHeaders, Environment: "test", DevHeadersAllowed: true}, nil, grantAdapter{fakeGrantSource{}}, slog.Default()); err != nil {
		t.Fatalf("dev_headers test opt-in rejected: %v", err)
	}
}

func TestHealthRoutesBypassAuth(t *testing.T) {
	mw := testBearerMiddleware(t, fakeGrantSource{})
	handler := RequestContext(slog.New(slog.NewTextHandler(io.Discard, nil)))(mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})))
	for _, path := range []string{"/healthz", "/livez", "/readyz"} {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusNoContent {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
		})
	}
}

type fakeGrantSource struct {
	roles  map[string][]string
	grants map[string][]permissions.ActiveGrant
	err    error
}

func (f fakeGrantSource) activeTenantRoles(userID, tenantID string) ([]string, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.roles[userID+"|"+tenantID], nil
}

func (f fakeGrantSource) activeTenantGrants(userID, tenantID string) ([]permissions.ActiveGrant, error) {
	if f.err != nil {
		return nil, f.err
	}
	key := userID + "|" + tenantID
	if grants, ok := f.grants[key]; ok {
		return grants, nil
	}
	roles := f.roles[key]
	grants := make([]permissions.ActiveGrant, 0, len(roles))
	for _, role := range roles {
		grants = append(grants, permissions.ActiveGrant{Role: role, ScopeType: "tenant", ScopeID: tenantID})
	}
	return grants, nil
}

func testBearerMiddleware(t *testing.T, grants fakeGrantSource) *AuthMiddleware {
	t.Helper()
	return testBearerMiddlewareWithTTL(t, grants, 24*time.Hour)
}

func testBearerMiddlewareWithTTL(t *testing.T, grants fakeGrantSource, maxTTL time.Duration) *AuthMiddleware {
	t.Helper()
	verifier, err := platformauth.NewHS256Verifier(platformauth.Config{
		Issuer:   authTestIssuer,
		Audience: authTestAudience,
		Secret:   []byte(authTestSecret),
		MaxTTL:   maxTTL,
		Now:      func() time.Time { return time.Unix(1_700_000_000, 0).UTC() },
	})
	if err != nil {
		t.Fatalf("verifier: %v", err)
	}
	mw, err := NewAuthMiddleware(AuthConfig{Mode: AuthModeBearer}, verifier, grantAdapter{grants}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewAuthMiddleware: %v", err)
	}
	return mw
}

type grantAdapter struct {
	fakeGrantSource
}

func (g grantAdapter) ActiveTenantRoles(_ context.Context, userID, tenantID string) ([]string, error) {
	return g.activeTenantRoles(userID, tenantID)
}

func (g grantAdapter) ActiveTenantGrants(_ context.Context, userID, tenantID string) ([]permissions.ActiveGrant, error) {
	return g.activeTenantGrants(userID, tenantID)
}

type staticVerifier struct {
	claims platformauth.Claims
	err    error
}

func (s staticVerifier) Verify(string) (platformauth.Claims, error) {
	if s.err != nil {
		return platformauth.Claims{}, s.err
	}
	return s.claims, nil
}

func testToken(t *testing.T, sub, tenant string, extra map[string]any) string {
	t.Helper()
	return signMiddlewareJWT(t, sub, tenant, extra)
}

func testTokenStatic(sub, tenant string, extra map[string]any) string {
	header := map[string]any{"alg": "HS256", "typ": "JWT"}
	payload := map[string]any{
		"iss":       authTestIssuer,
		"aud":       authTestAudience,
		"sub":       sub,
		"tenant_id": tenant,
		"exp":       time.Unix(1_700_000_000, 0).Add(time.Hour).Unix(),
		"nbf":       time.Unix(1_700_000_000, 0).Add(-time.Minute).Unix(),
	}
	for k, v := range extra {
		payload[k] = v
	}
	headerSegment := mustEncodeJSON(header)
	payloadSegment := mustEncodeJSON(payload)
	data := headerSegment + "." + payloadSegment
	mac := hmac.New(sha256.New, []byte(authTestSecret))
	_, _ = mac.Write([]byte(data))
	return data + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func signMiddlewareJWT(t *testing.T, sub, tenant string, extra map[string]any) string {
	t.Helper()
	return testTokenStatic(sub, tenant, extra)
}

func mustEncodeJSON(v map[string]any) string {
	raw, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

func assertAuthErrorCode(t *testing.T, rec *httptest.ResponseRecorder, want string) {
	t.Helper()
	var body struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid error json: %v", err)
	}
	if body.Code != want {
		t.Fatalf("code=%s want %s body=%s", body.Code, want, rec.Body.String())
	}
}
