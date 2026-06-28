package main

import "testing"

func TestParseFlagsRequiresTenantID(t *testing.T) {
	t.Setenv("GOATOS_TENANT_ID", "")
	if _, err := parseFlags([]string{}); err == nil {
		t.Fatalf("parseFlags err = nil, want tenant-id required")
	}
}

func TestParseFlagsDefaultsAndCapsLimit(t *testing.T) {
	t.Setenv("GOATOS_TENANT_ID", "00000000-0000-4000-8000-000000000001")
	cfg, err := parseFlags([]string{"-limit=9000", "-timeout=1s"})
	if err != nil {
		t.Fatalf("parseFlags: %v", err)
	}
	if cfg.Limit != 5000 {
		t.Fatalf("Limit = %d, want 5000", cfg.Limit)
	}
}
