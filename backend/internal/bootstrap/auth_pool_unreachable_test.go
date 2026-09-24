package bootstrap

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	platformauth "github.com/vgoats/goatos/backend/internal/platform/auth"
	"github.com/vgoats/goatos/backend/internal/platform/authaudit"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	platformpg "github.com/vgoats/goatos/backend/internal/platform/postgres"
)

// A cold instance whose auth pool cannot reach Postgres (refused, max_connections,
// shutting down) must answer a sign-in with a retryable 503 -- never a 403
// "email not allowed" for a person who IS on the DB allowlist. Uses the REAL
// production wiring (newAuthSessionHandler: AllowedEmailSource, grant claimer,
// audit recorder) against a pool that cannot connect.
func TestSessionEventOnUnreachableAuthDBIs503NotForbidden(t *testing.T) {
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	cfg := platformpg.Config{
		DatabaseURL:    "postgres://u:p@127.0.0.1:1/none?sslmode=disable&connect_timeout=1",
		ConnectTimeout: time.Second,
	}
	pool, err := connectAuthPool(context.Background(), cfg, log)
	if err != nil {
		t.Fatalf("auth pool: %v", err)
	}
	defer pool.Close()

	verified := true
	verifier := authPoolStaticVerifier{claims: platformauth.Claims{
		Subject: "10000000-0000-4000-8000-0000000000aa", Email: "allowlisted@mesha.sg",
		EmailVerified: &verified, Expires: time.Now().Add(time.Hour),
	}}
	mux := http.NewServeMux()
	authaudit.Register(mux, newAuthSessionHandler(pool, cfg, verifier, []authaudit.Option{
		// Not in the static list: the DB allowlist is what would admit this person.
		authaudit.WithAllowedEmails([]string{"someone-else@mesha.sg"}),
	}, log))
	req := httptest.NewRequest(http.MethodPost, "/auth/session-events", strings.NewReader(`{"event_type":"auth.sign_in","source":"admin-web"}`))
	req.Header.Set("Authorization", "Bearer test")
	req.Header.Set(httpmiddleware.TenantContextHeader, "20000000-0000-4000-8000-000000000001")
	rec := httptest.NewRecorder()
	httpmiddleware.RequestContext(log)(mux).ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("status=%d retry-after=%q body=%s; want 503 + Retry-After", rec.Code, rec.Header().Get("Retry-After"), rec.Body.String())
	}
}
