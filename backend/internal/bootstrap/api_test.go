package bootstrap

import (
	"testing"

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
		Environment:       "local",
		DevHeadersAllowed: true,
	}); err != nil {
		t.Fatalf("local dev headers rejected: %v", err)
	}
}
