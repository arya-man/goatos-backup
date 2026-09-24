package postgres

import (
	"os"
	"strings"
	"testing"
)

func TestIngestPacketsEmitsPostgresLiveNotification(t *testing.T) {
	src, err := os.ReadFile("repository.go")
	if err != nil {
		t.Fatalf("read repository.go: %v", err)
	}
	text := string(src)
	if !strings.Contains(text, `const liveNotifyChannel = "herd_signals_live"`) {
		t.Fatalf("repository must define the fixed herd signals live notify channel")
	}
	if !strings.Contains(text, "SELECT pg_notify($1, json_build_object('tenant_id', $2::text)::text)") {
		t.Fatalf("IngestPackets must emit a tenant-scoped Postgres NOTIFY after latest state advances")
	}
	if !strings.Contains(text, "latestUpdated > 0") {
		t.Fatalf("live notifications must only fire for ingest batches that advance tag_latest")
	}
}

func TestTenantIDFromLiveNotification(t *testing.T) {
	got := tenantIDFromLiveNotification(`{"tenant_id":"tenant-1"}`)
	if got != "tenant-1" {
		t.Fatalf("tenantIDFromLiveNotification = %q, want tenant-1", got)
	}
	if got := tenantIDFromLiveNotification(`not-json`); got != "" {
		t.Fatalf("invalid payload tenant = %q, want empty", got)
	}
}
