package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	platformauth "github.com/vgoats/goatos/backend/internal/platform/auth"
)

func main() {
	var tenantID string
	var userID string
	var ttlRaw string
	flag.StringVar(&tenantID, "tenant-id", "", "tenant UUID for the token tenant_id claim")
	flag.StringVar(&userID, "user-id", "", "user UUID for the token sub claim")
	flag.StringVar(&ttlRaw, "ttl", "1h", "token lifetime, capped by GOATOS_AUTH_MAX_TOKEN_TTL")
	flag.Parse()

	ttl, err := time.ParseDuration(ttlRaw)
	if err != nil {
		fail("invalid ttl: %v", err)
	}
	maxTTL, err := maxTokenTTLFromEnv()
	if err != nil {
		fail("invalid GOATOS_AUTH_MAX_TOKEN_TTL: %v", err)
	}
	token, err := platformauth.MintHS256Token(platformauth.Config{
		Issuer:   os.Getenv("GOATOS_AUTH_ISSUER"),
		Audience: os.Getenv("GOATOS_AUTH_AUDIENCE"),
		Secret:   []byte(os.Getenv("GOATOS_AUTH_HS256_SECRET")),
		MaxTTL:   maxTTL,
	}, userID, tenantID, ttl)
	if err != nil {
		fail("mint dev token: %v", err)
	}
	fmt.Println(token)
}

func maxTokenTTLFromEnv() (time.Duration, error) {
	raw := strings.TrimSpace(os.Getenv("GOATOS_AUTH_MAX_TOKEN_TTL"))
	if raw == "" {
		return platformauth.DefaultMaxTokenTTL, nil
	}
	ttl, err := time.ParseDuration(raw)
	if err != nil {
		return 0, err
	}
	if ttl <= 0 {
		return 0, platformauth.ErrInvalidMaxTTL
	}
	return ttl, nil
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
