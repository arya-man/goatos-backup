package main

import (
	"testing"
	"time"
)

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
