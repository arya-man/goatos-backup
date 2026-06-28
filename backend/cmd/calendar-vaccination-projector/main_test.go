package main

import (
	"strings"
	"testing"
)

func TestParseFlagsRequiresTenantID(t *testing.T) {
	t.Setenv("GOATOS_TENANT_ID", "")
	_, err := parseFlags([]string{"-timeout", "1s"})
	if err == nil || !strings.Contains(err.Error(), "tenant-id is required") {
		t.Fatalf("parseFlags err = %v, want tenant-id required", err)
	}
}

func TestParseFlagsEnablesClosedProjectionPruneByDefault(t *testing.T) {
	t.Setenv("GOATOS_TENANT_ID", "00000000-0000-4000-8000-000000000001")
	cfg, err := parseFlags([]string{"-timeout", "1s"})
	if err != nil {
		t.Fatalf("parseFlags: %v", err)
	}
	if !cfg.PruneClosed {
		t.Fatalf("PruneClosed = false, want true")
	}
	if cfg.PruneLimit != 1000 {
		t.Fatalf("PruneLimit = %d, want 1000", cfg.PruneLimit)
	}
	if cfg.ClosedRetention.String() != "2160h0m0s" {
		t.Fatalf("ClosedRetention = %s, want 2160h", cfg.ClosedRetention)
	}
}

func TestParseFlagsCanDisableClosedProjectionPrune(t *testing.T) {
	t.Setenv("GOATOS_TENANT_ID", "00000000-0000-4000-8000-000000000001")
	cfg, err := parseFlags([]string{"-timeout", "1s", "-prune-closed=false", "-prune-limit=25", "-closed-retention=720h"})
	if err != nil {
		t.Fatalf("parseFlags: %v", err)
	}
	if cfg.PruneClosed {
		t.Fatalf("PruneClosed = true, want false")
	}
	if cfg.PruneLimit != 25 {
		t.Fatalf("PruneLimit = %d, want 25", cfg.PruneLimit)
	}
	if cfg.ClosedRetention.String() != "720h0m0s" {
		t.Fatalf("ClosedRetention = %s, want 720h", cfg.ClosedRetention)
	}
}
