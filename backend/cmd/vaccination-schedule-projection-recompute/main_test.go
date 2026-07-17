package main

import (
	"testing"
	"time"
)

func TestParseFlagsUsesCallerConfiguredTimeoutBudget(t *testing.T) {
	cfg, err := parseFlags([]string{
		"-tenant-id", "00000000-0000-4000-8000-000000000001",
		"-timeout", "7m",
	})
	if err != nil {
		t.Fatalf("parseFlags: %v", err)
	}
	if cfg.Timeout != 7*time.Minute {
		t.Fatalf("timeout = %s, want 7m", cfg.Timeout)
	}
}
