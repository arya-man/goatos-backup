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
	req := httptest.NewRequest(http.MethodPost, "/admin/import-runs", nil)
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

	unauth := httptest.NewRequest(http.MethodPost, "/admin/identity/candidates/80000000-0000-4000-8000-000000000001/approve", nil)
	unauthRec := httptest.NewRecorder()
	handler.ServeHTTP(unauthRec, unauth)
	if unauthRec.Code != http.StatusUnauthorized {
		t.Fatalf("unauth status=%d body=%s", unauthRec.Code, unauthRec.Body.String())
	}

	forbidden := httptest.NewRequest(http.MethodPost, "/admin/identity/candidates/80000000-0000-4000-8000-000000000001/approve", nil)
	forbidden.Header.Set("Authorization", "Bearer "+testToken(t, authTestUser, authTestTenant, nil))
	mwNoGrant := testBearerMiddleware(t, fakeGrantSource{})
	forbiddenRec := httptest.NewRecorder()
	RequestContext(slog.New(slog.NewTextHandler(io.Discard, nil)))(mwNoGrant.Wrap(next)).ServeHTTP(forbiddenRec, forbidden)
	if forbiddenRec.Code != http.StatusForbidden {
		t.Fatalf("forbidden status=%d body=%s", forbiddenRec.Code, forbiddenRec.Body.String())
	}

	authorized := httptest.NewRequest(http.MethodPost, "/admin/identity/candidates/80000000-0000-4000-8000-000000000001/approve", nil)
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

func TestDevHeadersRequireExplicitLocalOptIn(t *testing.T) {
	if _, err := NewAuthMiddleware(AuthConfig{Mode: AuthModeDevHeaders, Environment: "prod", DevHeadersAllowed: true}, nil, grantAdapter{fakeGrantSource{}}, slog.Default()); err == nil {
		t.Fatal("dev_headers accepted prod-looking environment")
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
}

func TestHealthRoutesBypassAuth(t *testing.T) {
	mw := testBearerMiddleware(t, fakeGrantSource{})
	handler := RequestContext(slog.New(slog.NewTextHandler(io.Discard, nil)))(mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})))
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

type fakeGrantSource struct {
	roles map[string][]string
	err   error
}

func (f fakeGrantSource) activeTenantRoles(userID, tenantID string) ([]string, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.roles[userID+"|"+tenantID], nil
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
