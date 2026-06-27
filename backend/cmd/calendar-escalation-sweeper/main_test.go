package main

import (
	"strings"
	"testing"
)

func TestRunRequiresTenantID(t *testing.T) {
	t.Setenv("GOATOS_TENANT_ID", "")
	t.Setenv("DATABASE_URL", "")
	err := run([]string{"-timeout", "1s"})
	if err == nil || !strings.Contains(err.Error(), "tenant-id is required") {
		t.Fatalf("run err = %v, want tenant-id required", err)
	}
}

func TestRunRejectsBadThresholds(t *testing.T) {
	err := run([]string{
		"-tenant-id", "00000000-0000-4000-8000-000000000001",
		"-level2-after", "1h",
		"-level3-after", "30m",
	})
	if err == nil || !strings.Contains(err.Error(), "SLA thresholds") {
		t.Fatalf("run err = %v, want threshold error", err)
	}
}
