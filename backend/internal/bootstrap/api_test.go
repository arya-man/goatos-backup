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
