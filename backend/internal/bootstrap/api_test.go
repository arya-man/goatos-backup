package bootstrap

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
)

func TestBuildAuthVerifierDefaultsToBearerAndRejectsWeakConfig(t *testing.T) {
	if _, err := buildAuthVerifier(AuthConfig{}, nil); err == nil {
		t.Fatal("empty auth config should default to bearer and reject missing secret")
	}
	if _, err := buildAuthVerifier(AuthConfig{
		Mode:        httpmiddleware.AuthModeBearer,
		Issuer:      "goatos-test",
		Audience:    "goatos-api",
		HS256Secret: "short",
	}, nil); err == nil {
		t.Fatal("weak bearer secret accepted")
	}
	if _, err := buildAuthVerifier(AuthConfig{
		Issuer:      "goatos-test",
		Audience:    "goatos-api",
		HS256Secret: "0123456789abcdef0123456789abcdef",
		Environment: "local",
		MaxTokenTTL: 24 * time.Hour,
	}, nil); err != nil {
		t.Fatalf("valid default bearer config rejected: %v", err)
	}
}

func TestBuildAuthVerifierRejectsHS256OutsideDevEnvironments(t *testing.T) {
	for _, env := range []string{"", "stg", "prod", "production"} {
		t.Run(env, func(t *testing.T) {
			if _, err := buildAuthVerifier(AuthConfig{
				Mode:        httpmiddleware.AuthModeBearer,
				Issuer:      "goatos-test",
				Audience:    "goatos-api",
				HS256Secret: "0123456789abcdef0123456789abcdef",
				Environment: env,
			}, nil); err == nil {
				t.Fatal("HS256 accepted outside local/dev/test")
			}
		})
	}
}

func TestBuildAuthVerifierRejectsUnknownModeAndProdDevHeaders(t *testing.T) {
	if _, err := buildAuthVerifier(AuthConfig{Mode: "surprise"}, nil); err == nil {
		t.Fatal("unknown auth mode accepted")
	}
	if _, err := buildAuthVerifier(AuthConfig{
		Mode:              httpmiddleware.AuthModeDevHeaders,
		Environment:       "staging",
		DevHeadersAllowed: true,
	}, nil); err == nil {
		t.Fatal("dev headers accepted staging environment")
	}
	if _, err := buildAuthVerifier(AuthConfig{
		Mode:              httpmiddleware.AuthModeDevHeaders,
		Environment:       "local-prod",
		DevHeadersAllowed: true,
	}, nil); err == nil {
		t.Fatal("dev headers accepted non-allowlisted environment")
	}
	if _, err := buildAuthVerifier(AuthConfig{
		Mode:              httpmiddleware.AuthModeDevHeaders,
		Environment:       "local",
		DevHeadersAllowed: true,
	}, nil); err != nil {
		t.Fatalf("local dev headers rejected: %v", err)
	}
}

func TestBuildAuthVerifierJWKSMode(t *testing.T) {
	// Missing JWKS URL should fail closed.
	if _, err := buildAuthVerifier(AuthConfig{
		Mode:     AuthModeJWKS,
		Issuer:   "goatos-test",
		Audience: "goatos-api",
	}, nil); err == nil {
		t.Fatal("jwks mode without URL should be rejected")
	}

	// A valid jwks config pointing at a real (test) server should succeed.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"keys":[]}`))
	}))
	t.Cleanup(srv.Close)
	if _, err := buildAuthVerifier(AuthConfig{
		Mode:     AuthModeJWKS,
		Issuer:   "goatos-test",
		Audience: "goatos-api",
		JWKSUrl:  srv.URL + "/.well-known/jwks.json",
	}, nil); err != nil {
		t.Fatalf("valid jwks config rejected: %v", err)
	}
}

func TestBuildAppCheckVerifier(t *testing.T) {
	if verifier, err := buildAppCheckVerifier(AuthConfig{}); err != nil || verifier != nil {
		t.Fatalf("default app check = (%T, %v), want nil nil", verifier, err)
	}
	if _, err := buildAppCheckVerifier(AuthConfig{AppCheckMode: "surprise"}); err == nil {
		t.Fatal("unknown app check mode accepted")
	}
	for _, mode := range []string{httpmiddleware.AppCheckModeMonitor, httpmiddleware.AppCheckModeEnforce} {
		t.Run(mode+"/missing-project", func(t *testing.T) {
			if _, err := buildAppCheckVerifier(AuthConfig{AppCheckMode: mode}); err == nil {
				t.Fatal("enabled app check accepted missing issuer/audience")
			}
		})
		t.Run(mode+"/valid", func(t *testing.T) {
			verifier, err := buildAppCheckVerifier(AuthConfig{
				AppCheckMode:     mode,
				AppCheckIssuer:   "https://firebaseappcheck.googleapis.com/514832198871",
				AppCheckAudience: "projects/514832198871",
			})
			if err != nil {
				t.Fatalf("valid app check config rejected: %v", err)
			}
			if verifier == nil {
				t.Fatal("valid app check config returned nil verifier")
			}
		})
	}
}

func TestAuthMaxTokenTTLFromEnv(t *testing.T) {
	t.Setenv("GOATOS_AUTH_MAX_TOKEN_TTL", "")
	if got := authMaxTokenTTLFromEnv(); got != 24*time.Hour {
		t.Fatalf("default ttl=%s", got)
	}
	t.Setenv("GOATOS_AUTH_MAX_TOKEN_TTL", "2h")
	if got := authMaxTokenTTLFromEnv(); got != 2*time.Hour {
		t.Fatalf("configured ttl=%s", got)
	}
	for _, value := range []string{"0", "-1h", "not-a-duration"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("GOATOS_AUTH_MAX_TOKEN_TTL", value)
			if got := authMaxTokenTTLFromEnv(); got >= 0 {
				t.Fatalf("invalid ttl parsed as %s", got)
			}
		})
	}
}

func TestBuildProofStorageFailsClosedOutsideLocal(t *testing.T) {
	for _, env := range []string{"dev", "stg", "staging", "prod", "production"} {
		t.Run(env+"/empty", func(t *testing.T) {
			t.Setenv("GOATOS_ENV", env)
			t.Setenv("GOATOS_MEDIA_STORAGE", "")
			if _, err := buildProofStorage(); err == nil {
				t.Fatal("empty GOATOS_MEDIA_STORAGE accepted outside local/test")
			}
		})
		t.Run(env+"/local", func(t *testing.T) {
			t.Setenv("GOATOS_ENV", env)
			t.Setenv("GOATOS_MEDIA_STORAGE", "local")
			if _, err := buildProofStorage(); err == nil {
				t.Fatal("local proof storage accepted outside local/test")
			}
		})
	}
}

func TestBuildProofStorageAllowsLocalOnlyForLocalTestDevelopment(t *testing.T) {
	for _, env := range []string{"", "local", "test", "development"} {
		t.Run(env, func(t *testing.T) {
			t.Setenv("GOATOS_ENV", env)
			t.Setenv("GOATOS_MEDIA_STORAGE", "")
			if _, err := buildProofStorage(); err != nil {
				t.Fatalf("local proof storage rejected for %q: %v", env, err)
			}
		})
	}
}

func TestValidateBulkImportPreviewSigningKeyFailsClosedOutsideLocal(t *testing.T) {
	for _, env := range []string{"", "dev", "development", "stg", "staging", "prod", "production"} {
		t.Run(env, func(t *testing.T) {
			_, err := bulkImportPreviewSigningKey(Config{Auth: AuthConfig{Environment: env}})
			if err == nil {
				t.Fatal("empty preview signing key accepted outside local/test")
			}
		})
	}
}

func TestValidateBulkImportPreviewSigningKeyAllowsLocalAndExplicitKey(t *testing.T) {
	for _, env := range []string{"local", "test"} {
		t.Run(env, func(t *testing.T) {
			key, err := bulkImportPreviewSigningKey(Config{Auth: AuthConfig{Environment: env}})
			if err != nil {
				t.Fatalf("local preview signing key fallback rejected: %v", err)
			}
			if key == "" {
				t.Fatal("local preview signing key fallback returned empty key")
			}
		})
	}
	key, err := bulkImportPreviewSigningKey(Config{
		BulkImportPreviewSigningKey: "configured-secret",
		Auth:                        AuthConfig{Environment: "prod"},
	})
	if err != nil {
		t.Fatalf("configured production preview signing key rejected: %v", err)
	}
	if key != "configured-secret" {
		t.Fatalf("configured key = %q, want configured-secret", key)
	}
}

func TestAuthAuditOptionsFromConfig(t *testing.T) {
	options, err := buildAuthAuditOptions(AuthConfig{
		AuthSessionAllowedTenantIDs:   []string{"00000000-0000-4000-8000-000000000001"},
		AuthSessionRateLimitPerMinute: 60,
		AllowedEmails:                 []string{"ravi@mesha.sg"},
	})
	if err != nil {
		t.Fatalf("valid auth audit options rejected: %v", err)
	}
	if len(options) != 3 {
		t.Fatalf("options=%d want 3", len(options))
	}

	options, err = buildAuthAuditOptions(AuthConfig{AuthSessionRateLimitPerMinute: 0})
	if err != nil {
		t.Fatalf("disabled rate limit rejected: %v", err)
	}
	if len(options) != 2 {
		t.Fatalf("options=%d want tenant/email allowlist options only", len(options))
	}

	if _, err := buildAuthAuditOptions(AuthConfig{AuthSessionRateLimitInvalidValue: true}); err == nil {
		t.Fatal("invalid rate limit accepted")
	}
	if _, err := buildAuthAuditOptions(AuthConfig{AuthSessionRateLimitPerMinute: -1}); err == nil {
		t.Fatal("negative rate limit accepted")
	}
	if _, err := buildAuthAuditOptions(AuthConfig{AuthSessionAllowedTenantIDs: []string{"not-a-uuid"}}); err == nil {
		t.Fatal("invalid auth session tenant allowlist accepted")
	}
	if _, err := buildAuthAuditOptions(AuthConfig{AllowedEmails: []string{"not-an-email"}}); err == nil {
		t.Fatal("invalid auth email allowlist accepted")
	}
}

func TestJWKSRequiresNonEmptyAuthAllowedEmails(t *testing.T) {
	if _, err := buildAuthAuditOptions(AuthConfig{
		Mode: AuthModeJWKS,
	}); err == nil {
		t.Fatal("jwks accepted empty auth email allowlist")
	}
	if _, err := buildAuthAuditOptions(AuthConfig{
		Mode:          AuthModeJWKS,
		AllowedEmails: []string{"ravi@mesha.sg"},
	}); err != nil {
		t.Fatalf("jwks rejected configured auth email allowlist: %v", err)
	}
	if _, err := buildAuthAuditOptions(AuthConfig{
		Mode:          httpmiddleware.AuthModeBearer,
		AllowedEmails: nil,
	}); err != nil {
		t.Fatalf("local bearer without auth email allowlist should remain allowed: %v", err)
	}
}

func TestAuthSessionRateLimitFromEnv(t *testing.T) {
	t.Setenv("GOATOS_AUTH_SESSION_RATE_LIMIT_PER_MINUTE", "")
	if got := authSessionRateLimitFromEnv(); got != defaultAuthSessionRateLimitPerMinute {
		t.Fatalf("default rate limit=%d", got)
	}
	t.Setenv("GOATOS_AUTH_SESSION_RATE_LIMIT_PER_MINUTE", "0")
	if got := authSessionRateLimitFromEnv(); got != 0 {
		t.Fatalf("disabled rate limit=%d", got)
	}
	t.Setenv("GOATOS_AUTH_SESSION_RATE_LIMIT_PER_MINUTE", "15")
	if got := authSessionRateLimitFromEnv(); got != 15 {
		t.Fatalf("configured rate limit=%d", got)
	}
	for _, value := range []string{"-1", "abc"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("GOATOS_AUTH_SESSION_RATE_LIMIT_PER_MINUTE", value)
			if got := authSessionRateLimitFromEnv(); got >= 0 {
				t.Fatalf("invalid rate limit parsed as %d", got)
			}
			if !authSessionRateLimitInvalidFromEnv() {
				t.Fatal("invalid rate limit not flagged")
			}
		})
	}
}

func TestAuthStringListFromEnv(t *testing.T) {
	t.Setenv("GOATOS_AUTH_SESSION_ALLOWED_TENANT_IDS", " a, ,b , c ")
	got := authStringListFromEnv("GOATOS_AUTH_SESSION_ALLOWED_TENANT_IDS")
	want := []string{"a", "b", "c"}
	if len(got) != len(want) {
		t.Fatalf("list=%v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("list=%v want %v", got, want)
		}
	}
}

func TestConfigFromEnvParsesAuthAllowedEmails(t *testing.T) {
	t.Setenv("GOATOS_AUTH_ALLOWED_EMAILS", " ravi@mesha.sg, abhishek@mesha.sg ,, manju@mesha.sg ")
	cfg := ConfigFromEnv()
	want := []string{"ravi@mesha.sg", "abhishek@mesha.sg", "manju@mesha.sg"}
	if len(cfg.Auth.AllowedEmails) != len(want) {
		t.Fatalf("emails=%v want %v", cfg.Auth.AllowedEmails, want)
	}
	for i := range want {
		if cfg.Auth.AllowedEmails[i] != want[i] {
			t.Fatalf("emails=%v want %v", cfg.Auth.AllowedEmails, want)
		}
	}
}

func TestConfigFromEnvParsesAppCheckConfig(t *testing.T) {
	t.Setenv("GOATOS_APPCHECK_ENFORCE", httpmiddleware.AppCheckModeMonitor)
	t.Setenv("GOATOS_APPCHECK_ISSUER", "https://firebaseappcheck.googleapis.com/514832198871")
	t.Setenv("GOATOS_APPCHECK_AUDIENCE", "projects/514832198871")
	t.Setenv("GOATOS_APPCHECK_JWKS_URL", "https://example.test/jwks")
	t.Setenv("GOATOS_APPCHECK_CLOCK_SKEW", "30s")
	t.Setenv("GOATOS_APPCHECK_JWKS_CACHE_TTL", "2m")

	cfg := ConfigFromEnv()
	if cfg.Auth.AppCheckMode != httpmiddleware.AppCheckModeMonitor {
		t.Fatalf("app check mode=%q", cfg.Auth.AppCheckMode)
	}
	if cfg.Auth.AppCheckIssuer != "https://firebaseappcheck.googleapis.com/514832198871" {
		t.Fatalf("app check issuer=%q", cfg.Auth.AppCheckIssuer)
	}
	if cfg.Auth.AppCheckAudience != "projects/514832198871" {
		t.Fatalf("app check audience=%q", cfg.Auth.AppCheckAudience)
	}
	if cfg.Auth.AppCheckJWKSUrl != "https://example.test/jwks" {
		t.Fatalf("app check jwks=%q", cfg.Auth.AppCheckJWKSUrl)
	}
	if cfg.Auth.AppCheckClockSkew != 30*time.Second {
		t.Fatalf("app check skew=%s", cfg.Auth.AppCheckClockSkew)
	}
	if cfg.Auth.AppCheckJWKSCacheTTL != 2*time.Minute {
		t.Fatalf("app check cache ttl=%s", cfg.Auth.AppCheckJWKSCacheTTL)
	}
}
