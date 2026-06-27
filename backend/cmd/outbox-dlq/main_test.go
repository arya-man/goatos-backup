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

func TestRunReplayRequiresOutboxIDAndReason(t *testing.T) {
	args := []string{"-mode", "replay", "-tenant-id", "00000000-0000-4000-8000-000000000001"}
	err := run(args)
	if err == nil || !strings.Contains(err.Error(), "outbox-id is required") {
		t.Fatalf("run err = %v, want outbox-id required", err)
	}
	err = run(append(args, "-outbox-id", "86000000-0000-4000-8000-000000000001"))
	if err == nil || !strings.Contains(err.Error(), "reason is required") {
		t.Fatalf("run err = %v, want reason required", err)
	}
}

func TestRunRejectsUnsupportedStatus(t *testing.T) {
	err := run([]string{
		"-tenant-id", "00000000-0000-4000-8000-000000000001",
		"-status", "pending",
	})
	if err == nil || !strings.Contains(err.Error(), "status must be dead_letter or failed") {
		t.Fatalf("run err = %v, want status error", err)
	}
}

func TestSplitCSV(t *testing.T) {
	got := splitCSV(" a, ,b ")
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("splitCSV=%#v", got)
	}
}
