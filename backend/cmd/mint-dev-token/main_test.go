package main

import (
	"testing"
	"time"

	platformauth "github.com/vgoats/goatos/backend/internal/platform/auth"
)

func TestMaxTokenTTLFromEnvUsesDefault(t *testing.T) {
	t.Setenv("GOATOS_AUTH_MAX_TOKEN_TTL", "")
	ttl, err := maxTokenTTLFromEnv()
	if err != nil {
		t.Fatalf("maxTokenTTLFromEnv: %v", err)
	}
	if ttl != platformauth.DefaultMaxTokenTTL {
		t.Fatalf("ttl=%s want %s", ttl, platformauth.DefaultMaxTokenTTL)
	}
}

func TestMaxTokenTTLFromEnvRejectsInvalid(t *testing.T) {
	t.Setenv("GOATOS_AUTH_MAX_TOKEN_TTL", "0s")
	if _, err := maxTokenTTLFromEnv(); err == nil {
		t.Fatal("expected invalid ttl error")
	}
	t.Setenv("GOATOS_AUTH_MAX_TOKEN_TTL", "bad")
	if _, err := maxTokenTTLFromEnv(); err == nil {
		t.Fatal("expected parse error")
	}
}

func TestMaxTokenTTLFromEnvParsesDuration(t *testing.T) {
	t.Setenv("GOATOS_AUTH_MAX_TOKEN_TTL", "2h")
	ttl, err := maxTokenTTLFromEnv()
	if err != nil {
		t.Fatalf("maxTokenTTLFromEnv: %v", err)
	}
	if ttl != 2*time.Hour {
		t.Fatalf("ttl=%s", ttl)
	}
}
