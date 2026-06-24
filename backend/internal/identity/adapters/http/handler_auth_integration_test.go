package identityhttp

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
	"strings"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/identity/app"
	"github.com/vgoats/goatos/backend/internal/permissions"
	platformauth "github.com/vgoats/goatos/backend/internal/platform/auth"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
)

const (
	authChainSecret   = "0123456789abcdef0123456789abcdef"
	authChainIssuer   = "goatos-test"
	authChainAudience = "goatos-api"
	authChainTenant   = "00000000-0000-4000-8000-000000000001"
	authChainUser     = "90000000-0000-4000-8000-000000000001"
)

func TestBearerAuthWiredThroughRealMuxAndHandler(t *testing.T) {
	handler := authWrappedIdentityMux(t, grantSourceForRoles(permissions.RoleOperator))
	req := httptest.NewRequest(http.MethodGet, "/goats/10000000-0000-4000-8000-000000000001", nil)
	req.Header.Set("Authorization", "Bearer "+authChainToken(t, authChainUser, authChainTenant))
	req.Header.Set("X-GoatOS-Tenant-ID", "00000000-0000-4000-8000-000000000099")
	req.Header.Set("X-GoatOS-Actor-ID", "90000000-0000-4000-8000-000000000099")
	req.Header.Set("X-Request-ID", "req-auth-chain")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if body["trace_id"] != "req-auth-chain" {
		t.Fatalf("handler did not run through request context: %#v", body)
	}
}

func TestBearerAuthRunsBeforeIdentifierWriteHandler(t *testing.T) {
	handler := authWrappedIdentityMux(t, grantSourceForRoles(permissions.RoleVerifier))
	path := "/admin/goats/10000000-0000-4000-8000-000000000001/identifiers"

	unauthReq := httptest.NewRequest(http.MethodPost, path, nil)
	unauthRec := httptest.NewRecorder()
	handler.ServeHTTP(unauthRec, unauthReq)
	if unauthRec.Code != http.StatusUnauthorized {
		t.Fatalf("unauth status=%d body=%s", unauthRec.Code, unauthRec.Body.String())
	}

	unauthorizedHandler := authWrappedIdentityMux(t, grantSourceForRoles(permissions.RoleOperator))
	forbiddenReq := httptest.NewRequest(http.MethodPost, path, nil)
	forbiddenReq.Header.Set("Authorization", "Bearer "+authChainToken(t, authChainUser, authChainTenant))
	forbiddenRec := httptest.NewRecorder()
	unauthorizedHandler.ServeHTTP(forbiddenRec, forbiddenReq)
	if forbiddenRec.Code != http.StatusForbidden {
		t.Fatalf("forbidden status=%d body=%s", forbiddenRec.Code, forbiddenRec.Body.String())
	}

	authorizedReq := httptest.NewRequest(http.MethodPost, path, strings.NewReader(validAddIdentifierBody()))
	authorizedReq.Header.Set("Authorization", "Bearer "+authChainToken(t, authChainUser, authChainTenant))
	authorizedReq.Header.Set("Content-Type", "application/json")
	authorizedReq.Header.Set("Idempotency-Key", "idem-auth-chain-add-identifier")
	authorizedRec := httptest.NewRecorder()
	handler.ServeHTTP(authorizedRec, authorizedReq)
	if authorizedRec.Code != http.StatusOK {
		t.Fatalf("authorized status=%d body=%s", authorizedRec.Code, authorizedRec.Body.String())
	}
}

func TestPhase1BWriteRoutesUseExpectedPermissions(t *testing.T) {
	productWriteAllowed := []string{permissions.RoleAdmin, permissions.RoleCEOInternal, permissions.RoleVerifier}
	productWriteDenied := []string{permissions.RoleOperator, permissions.RoleParkHead}

	routes := []struct {
		name         string
		path         string
		body         string
		wantStatus   int
		allowedRoles []string
		deniedRoles  []string
	}{
		{
			name:         "add goat identifier",
			path:         "/admin/goats/10000000-0000-4000-8000-000000000001/identifiers",
			body:         validAddIdentifierBody(),
			wantStatus:   http.StatusOK,
			allowedRoles: productWriteAllowed,
			deniedRoles:  productWriteDenied,
		},
		{
			name:         "retire goat identifier",
			path:         "/admin/goats/10000000-0000-4000-8000-000000000001/identifiers/30000000-0000-4000-8000-000000000001/retire",
			body:         validRetireIdentifierBody(),
			wantStatus:   http.StatusOK,
			allowedRoles: productWriteAllowed,
			deniedRoles:  productWriteDenied,
		},
	}

	for _, route := range routes {
		for _, role := range route.allowedRoles {
			t.Run(route.name+" allows "+role, func(t *testing.T) {
				rec := authWriteRequest(t, role, route.path, route.body)
				if rec.Code != route.wantStatus {
					t.Fatalf("role %s path %s status=%d body=%s", role, route.path, rec.Code, rec.Body.String())
				}
			})
		}
		for _, role := range route.deniedRoles {
			t.Run(route.name+" denies "+role, func(t *testing.T) {
				rec := authWriteRequest(t, role, route.path, route.body)
				if rec.Code != http.StatusForbidden {
					t.Fatalf("role %s path %s status=%d body=%s", role, route.path, rec.Code, rec.Body.String())
				}
			})
		}
	}
}

func authWriteRequest(t *testing.T, role, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	handler := authWrappedIdentityMux(t, grantSourceForRoles(role))
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+authChainToken(t, authChainUser, authChainTenant))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", "idem-auth-chain-write")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func authWrappedIdentityMux(t *testing.T, grants permissions.GrantSource) http.Handler {
	t.Helper()
	verifier, err := platformauth.NewHS256Verifier(platformauth.Config{
		Issuer:   authChainIssuer,
		Audience: authChainAudience,
		Secret:   []byte(authChainSecret),
		MaxTTL:   platformauth.DefaultMaxTokenTTL,
		Now:      func() time.Time { return time.Unix(1_700_000_000, 0).UTC() },
	})
	if err != nil {
		t.Fatalf("verifier: %v", err)
	}
	authz, err := httpmiddleware.NewAuthMiddleware(httpmiddleware.AuthConfig{Mode: httpmiddleware.AuthModeBearer}, verifier, grants, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("auth middleware: %v", err)
	}
	mux := http.NewServeMux()
	Register(mux, NewHandler(app.NewService(&handlerRepo{})))
	return httpmiddleware.RequestContext(slog.New(slog.NewTextHandler(io.Discard, nil)))(authz.Wrap(mux))
}

func grantSourceForRoles(roles ...string) permissions.GrantSource {
	return authChainGrants{roles: roles}
}

type authChainGrants struct {
	roles []string
}

func (g authChainGrants) ActiveTenantRoles(_ context.Context, userID, tenantID string) ([]string, error) {
	if userID == authChainUser && tenantID == authChainTenant {
		return g.roles, nil
	}
	return nil, nil
}

func authChainToken(t *testing.T, sub, tenant string) string {
	t.Helper()
	header := map[string]any{"alg": "HS256", "typ": "JWT"}
	payload := map[string]any{
		"iss":       authChainIssuer,
		"aud":       authChainAudience,
		"sub":       sub,
		"tenant_id": tenant,
		"exp":       time.Unix(1_700_000_000, 0).Add(time.Hour).Unix(),
		"nbf":       time.Unix(1_700_000_000, 0).Add(-time.Minute).Unix(),
	}
	headerSegment := authChainJSONSegment(t, header)
	payloadSegment := authChainJSONSegment(t, payload)
	data := headerSegment + "." + payloadSegment
	mac := hmac.New(sha256.New, []byte(authChainSecret))
	_, _ = mac.Write([]byte(data))
	return data + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func authChainJSONSegment(t *testing.T, value map[string]any) string {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal token segment: %v", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}
