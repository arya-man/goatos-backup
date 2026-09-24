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
		t.Fatalf("repository must emit tenant-scoped Postgres NOTIFY payloads")
	}
	if !strings.Contains(text, "notifyLiveUpdateTx(ctx, tx, tenantID)") {
		t.Fatalf("IngestPackets must emit a tenant-scoped Postgres NOTIFY after latest state advances")
	}
	if !strings.Contains(text, "latestUpdated > 0") {
		t.Fatalf("live notifications must only fire for ingest batches that advance tag_latest")
	}
}

func TestMappingWritesEmitPostgresLiveNotification(t *testing.T) {
	src, err := os.ReadFile("mapping.go")
	if err != nil {
		t.Fatalf("read mapping.go: %v", err)
	}
	text := string(src)
	if got := strings.Count(text, "notifyLiveUpdateTx(ctx, tx, tenantID)"); got < 3 {
		t.Fatalf("bind/unmap/replace must each notify live streams after tag_latest mapping sync, got %d notify calls", got)
	}
	if got := strings.Count(text, "syncTagLatestMonitoring(ctx, tx"); got < 4 {
		t.Fatalf("mapping writes must continue syncing tag_latest before notifying, got %d sync calls", got)
	}
}

func TestListenerBroadcastsCatchupAfterListen(t *testing.T) {
	src, err := os.ReadFile("live_notifications.go")
	if err != nil {
		t.Fatalf("read live_notifications.go: %v", err)
	}
	text := string(src)
	if !strings.Contains(text, "publishAll func()") {
		t.Fatalf("live notification source must accept a broadcast catch-up hook")
	}
	if !strings.Contains(text, "if publishAll != nil {\n\t\tpublishAll()\n\t}") {
		t.Fatalf("listener must broadcast one catch-up snapshot after LISTEN reconnects")
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
