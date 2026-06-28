package main

import "testing"

func TestParseFlagsVersionIDRequiresUnsafeGate(t *testing.T) {
	t.Setenv("GOATOS_TENANT_ID", "00000000-0000-4000-8000-000000000001")
	cfg, err := parseFlags([]string{"-version-id=86000000-0000-4000-8000-000000000001", "-as-of=2026-06-01T00:00:00Z"})
	if err != nil {
		t.Fatalf("parseFlags: %v", err)
	}
	if cfg.UnsafeVersionRun {
		t.Fatalf("UnsafeVersionRun = true, want false")
	}
}

func TestParseFlagsVersionIDUnsafeGate(t *testing.T) {
	t.Setenv("GOATOS_TENANT_ID", "00000000-0000-4000-8000-000000000001")
	cfg, err := parseFlags([]string{
		"-version-id=86000000-0000-4000-8000-000000000001",
		"-unsafe-version-id-bypass-effective-resolution",
		"-as-of=2026-06-01T00:00:00Z",
	})
	if err != nil {
		t.Fatalf("parseFlags: %v", err)
	}
	if !cfg.UnsafeVersionRun {
		t.Fatalf("UnsafeVersionRun = false, want true")
	}
}
