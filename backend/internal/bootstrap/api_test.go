package bootstrap

import (
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
)

func TestBuildAuthVerifierDefaultsToBearerAndRejectsWeakConfig(t *testing.T) {
	if _, err := buildAuthVerifier(AuthConfig{}); err == nil {
		t.Fatal("empty auth config should default to bearer and reject missing secret")
	}
	if _, err := buildAuthVerifier(AuthConfig{
		Mode:        httpmiddleware.AuthModeBearer,
		Issuer:      "goatos-test",
		Audience:    "goatos-api",
		HS256Secret: "short",
	}); err == nil {
		t.Fatal("weak bearer secret accepted")
	}
	if _, err := buildAuthVerifier(AuthConfig{
		Issuer:      "goatos-test",
		Audience:    "goatos-api",
		HS256Secret: "0123456789abcdef0123456789abcdef",
		MaxTokenTTL: 24 * time.Hour,
	}); err != nil {
		t.Fatalf("valid default bearer config rejected: %v", err)
	}
}

func TestBuildAuthVerifierRejectsUnknownModeAndProdDevHeaders(t *testing.T) {
	if _, err := buildAuthVerifier(AuthConfig{Mode: "surprise"}); err == nil {
		t.Fatal("unknown auth mode accepted")
	}
	if _, err := buildAuthVerifier(AuthConfig{
		Mode:              httpmiddleware.AuthModeDevHeaders,
		Environment:       "staging",
		DevHeadersAllowed: true,
	}); err == nil {
		t.Fatal("dev headers accepted staging environment")
	}
	if _, err := buildAuthVerifier(AuthConfig{
		Mode:              httpmiddleware.AuthModeDevHeaders,
		Environment:       "local-prod",
		DevHeadersAllowed: true,
	}); err == nil {
		t.Fatal("dev headers accepted non-allowlisted environment")
	}
	if _, err := buildAuthVerifier(AuthConfig{
		Mode:              httpmiddleware.AuthModeDevHeaders,
		Environment:       "local",
		DevHeadersAllowed: true,
	}); err != nil {
		t.Fatalf("local dev headers rejected: %v", err)
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
