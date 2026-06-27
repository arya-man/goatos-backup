package main

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	platformpg "github.com/vgoats/goatos/backend/internal/platform/postgres"
)

func TestBuildPublisherFailsClosed(t *testing.T) {
	ctx := context.Background()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := platformpg.Config{}

	// Empty publisher must error: an unset GOATOS_OUTBOX_PUBLISHER in prod previously fell back to a
	// logging publisher that marked outbox rows published with no fanout (silent event loss).
	if _, _, err := buildPublisher(ctx, "", nil, cfg, logger); err == nil {
		t.Fatal("empty GOATOS_OUTBOX_PUBLISHER should fail closed")
	}

	// Non-durable publishers fail closed without an explicit opt-in (the error paths return before the
	// pool is touched, so a nil pool is safe here).
	t.Setenv("GOATOS_OUTBOX_ALLOW_NONDURABLE", "")
	for _, kind := range []string{"logging", "log", "eventbus", "local", "inprocess"} {
		if _, _, err := buildPublisher(ctx, kind, nil, cfg, logger); err == nil {
			t.Fatalf("non-durable publisher %q without opt-in should fail closed", kind)
		}
	}

	// With the explicit local/dev opt-in, the logging publisher is permitted.
	t.Setenv("GOATOS_OUTBOX_ALLOW_NONDURABLE", "1")
	if _, _, err := buildPublisher(ctx, "logging", nil, cfg, logger); err != nil {
		t.Fatalf("logging publisher with GOATOS_OUTBOX_ALLOW_NONDURABLE=1 should be allowed: %v", err)
	}
}

func TestParseFlagsDefaultsAndBounds(t *testing.T) {
	cfg, err := parseFlags(nil)
	if err != nil {
		t.Fatalf("parse defaults: %v", err)
	}
	if cfg.Limit != 50 || cfg.Timeout != 30*time.Second || cfg.LeaseTimeout != 5*time.Minute || cfg.MaxAttempts != 5 {
		t.Fatalf("unexpected defaults: %#v", cfg)
	}

	if _, err := parseFlags([]string{"--limit", "0"}); err == nil {
		t.Fatal("limit=0 should fail")
	}
	if _, err := parseFlags([]string{"--limit", "501"}); err == nil {
		t.Fatal("limit=501 should fail")
	}
	if _, err := parseFlags([]string{"--max-attempts", "0"}); err == nil {
		t.Fatal("max-attempts=0 should fail")
	}
}

func TestFindDomainEventEnvelopeSchema(t *testing.T) {
	path, err := findDomainEventEnvelopeSchema()
	if err != nil {
		t.Fatalf("find schema: %v", err)
	}
	if path == "" {
		t.Fatal("schema path is empty")
	}
}
