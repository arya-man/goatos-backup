package httpmiddleware

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/permissions"
	permissionspg "github.com/vgoats/goatos/backend/internal/permissions/adapters/postgres"
	platformauth "github.com/vgoats/goatos/backend/internal/platform/auth"
)

// Incident 2026-09-24 shape on protected routes: a cold instance whose DB-backed
// allowlist cannot be read (pool refused / saturated) must answer 503 + Retry-After,
// never 403 email_not_allowed -- the Android app reads that 403 as "access not
// provisioned". Real AuthMiddleware + real AllowedEmailSource on an unreachable pool.
func TestProtectedRouteAllowlistUnavailableIs503NotForbidden(t *testing.T) {
	pool, err := pgxpool.New(context.Background(), "postgres://u:p@127.0.0.1:1/none?sslmode=disable&connect_timeout=1")
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	defer pool.Close()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	verified := true
	externalSubject := "firebase-uid-dynamic"
	actorID := platformauth.StableSubjectID(authTestIssuer, externalSubject)
	mw, err := NewAuthMiddleware(
		AuthConfig{
			Mode:                 AuthModeBearer,
			AllowedEmails:        []string{"ravi@mesha.sg"},
			DynamicAllowedEmails: permissionspg.NewAllowedEmailSource(pool, time.Second, log),
		},
		staticVerifier{claims: platformauth.Claims{
			Subject: actorID, ExternalSubject: externalSubject,
			Issuer: authTestIssuer, Audience: authTestAudience,
			Email: "added-on-people@mesha.sg", EmailVerified: &verified,
		}},
		grantAdapter{fakeGrantSource{roles: map[string][]string{actorID + "|" + authTestTenant: {permissions.RoleOperator}}}},
		log,
	)
	if err != nil {
		t.Fatalf("NewAuthMiddleware: %v", err)
	}
	handler := RequestContext(log)(mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Fatal("handler must not run when the allowlist could not be read")
	})))
	req := httptest.NewRequest(http.MethodGet, "/goats/search?limit=10", nil)
	req.Header.Set("Authorization", "Bearer verified-firebase-token")
	req.Header.Set("X-GoatOS-Tenant-ID", authTestTenant)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("status=%d retry-after=%q body=%s; want 503 + Retry-After", rec.Code, rec.Header().Get("Retry-After"), rec.Body.String())
	}
	assertAuthErrorCode(t, rec, "auth_database_busy")
	var body struct {
		Retryable bool `json:"retryable"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid error json: %v", err)
	}
	if !body.Retryable {
		t.Fatalf("auth_database_busy must be retryable:true (matches authaudit); body=%s", rec.Body.String())
	}
}

// During the allowlist reload backoff (cached set past its hard max age, DB
// still down) protected routes must keep answering 503, never 403.
func TestProtectedRouteAllowlistBackoffIs503NotForbidden(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	now := time.Unix(1_800_000_000, 0)
	fail := false
	src := permissionspg.NewAllowedEmailSourceWithLoader(func(context.Context, string) (map[string]struct{}, error) {
		if fail {
			return nil, context.DeadlineExceeded
		}
		return map[string]struct{}{"added-on-people@mesha.sg": {}}, nil
	}, func() time.Time { return now }, log)

	verified := true
	externalSubject := "firebase-uid-dynamic"
	actorID := platformauth.StableSubjectID(authTestIssuer, externalSubject)
	mw, err := NewAuthMiddleware(
		AuthConfig{Mode: AuthModeBearer, AllowedEmails: []string{"ravi@mesha.sg"}, DynamicAllowedEmails: src},
		staticVerifier{claims: platformauth.Claims{
			Subject: actorID, ExternalSubject: externalSubject,
			Issuer: authTestIssuer, Audience: authTestAudience,
			Email: "added-on-people@mesha.sg", EmailVerified: &verified,
		}},
		grantAdapter{fakeGrantSource{roles: map[string][]string{actorID + "|" + authTestTenant: {permissions.RoleOperator}}}},
		log,
	)
	if err != nil {
		t.Fatalf("NewAuthMiddleware: %v", err)
	}
	handler := RequestContext(log)(mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})))
	serve := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/goats/search?limit=10", nil)
		req.Header.Set("Authorization", "Bearer verified-firebase-token")
		req.Header.Set("X-GoatOS-Tenant-ID", authTestTenant)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}
	if rec := serve(); rec.Code != http.StatusNoContent {
		t.Fatalf("warm: status=%d body=%s", rec.Code, rec.Body.String())
	}
	fail = true
	now = now.Add(11 * time.Minute)
	for i, label := range []string{"failed reload", "backoff"} {
		rec := serve()
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("%s (%d): status=%d body=%s; want 503", label, i, rec.Code, rec.Body.String())
		}
		assertAuthErrorCode(t, rec, "auth_database_busy")
		now = now.Add(time.Second)
	}
}
