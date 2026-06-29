package main

import (
	"strings"
	"testing"

	notificationdomain "github.com/vgoats/goatos/backend/internal/notification/domain"
)

func TestRunRequiresTenantID(t *testing.T) {
	t.Setenv("GOATOS_TENANT_ID", "")
	t.Setenv("DATABASE_URL", "")
	err := run([]string{"-timeout", "1s"})
	if err == nil || !strings.Contains(err.Error(), "tenant-id is required") {
		t.Fatalf("run err = %v, want tenant-id required", err)
	}
}

func TestRunRejectsBadLimit(t *testing.T) {
	err := run([]string{"-tenant-id", "00000000-0000-4000-8000-000000000001", "-limit", "0"})
	if err == nil || !strings.Contains(err.Error(), "limit must be between") {
		t.Fatalf("run err = %v, want limit error", err)
	}
}

func TestFormatDispatchResultIncludesExhaustedCount(t *testing.T) {
	out := formatDispatchResult(notificationdomain.DispatchResult{
		ReclaimedStaleCount: 1,
		ClaimedCount:        2,
		SentCount:           3,
		FailedCount:         4,
		ExhaustedCount:      5,
	}, "00000000-0000-4000-8000-000000000001")

	for _, want := range []string{"reclaimed=1", "claimed=2", "sent=3", "failed=4", "exhausted=5"} {
		if !strings.Contains(out, want) {
			t.Fatalf("formatDispatchResult() = %q, want %q", out, want)
		}
	}
}
