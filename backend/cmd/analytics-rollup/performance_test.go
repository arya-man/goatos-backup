package main

import (
	"context"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"strings"
	"testing"
	"time"
)

func TestPerformanceDocumentedSchemaAndConfig(t *testing.T) {
	for _, fragment := range []string{"event_type = 'NETWORK_REQUEST'", "event_name route_pattern", "app_display_version", "network_info.request_http_method", "network_info.response_completed_time_us / 1000.0", "event_timestamp >= @start AND event_timestamp < @end", "OFFSET(50)", "OFFSET(95)", "OFFSET(99)"} {
		if !strings.Contains(appNetworkSQL, fragment) {
			t.Fatal(fragment)
		}
	}
	good := config{PerformanceTable: "p.firebase_performance.android", GA4BQProject: "p", SourceAppID: "sg.mesha.goatos"}
	if err := validatePerformanceConfig(good); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []config{{PerformanceTable: "p.d.t`"}, {PerformanceTable: "other.d.t", GA4BQProject: "p", SourceAppID: "app"}, {PerformanceTable: "p.d.t", GA4BQProject: "p"}} {
		if validatePerformanceConfig(bad) == nil {
			t.Fatalf("accepted %+v", bad)
		}
	}
	if validatePerformanceConfig(config{}) != nil {
		t.Fatal("absent export must remain optional")
	}
	t.Setenv("GOATOS_PERFORMANCE_BQ_TABLE", "p.d.t")
	cfg, err := parseConfig([]string{"--tenant-id", testTenantID})
	if err != nil || cfg.PerformanceTable != "p.d.t" {
		t.Fatal(cfg, err)
	}
	cfg, err = parseConfig([]string{"--tenant-id", testTenantID, "--performance-bq-table", "p.d.override"})
	if err != nil || cfg.PerformanceTable != "p.d.override" {
		t.Fatal(cfg, err)
	}
}

func TestAppNetworkReplacementPostgres(t *testing.T) {
	pool := pgtest.StartPostgres(t, context.Background())
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cfg := config{SourceAppID: "sg.mesha.goatos", SourceDate: time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC)}
	rows := []appNetworkRow{{AppVersion: "77", RoutePattern: "api.mesha.sg/app/*", HTTPMethod: "GET", ResponseClass: "2xx", TotalRequests: 10, P50MS: 120, P95MS: 450, P99MS: 900}}
	for i := 0; i < 2; i++ {
		if _, err := replaceAppNetworkRows(ctx, pool, cfg, rows); err != nil {
			t.Fatal(err)
		}
	}
	other := cfg
	other.SourceAppID = "other-app"
	if _, err := replaceAppNetworkRows(ctx, pool, other, rows); err != nil {
		t.Fatal(err)
	}
	bad := append([]appNetworkRow{}, rows...)
	bad = append(bad, appNetworkRow{RoutePattern: "bad", TotalRequests: 1, P50MS: 20, P95MS: 10})
	if _, err := replaceAppNetworkRows(ctx, pool, cfg, bad); err == nil {
		t.Fatal("accepted unordered percentiles")
	}
	var count int
	var p95 float64
	if err := pool.QueryRow(ctx, `SELECT count(*),max(p95_ms) FROM analytics.app_network_daily WHERE source_app_id=$1`, cfg.SourceAppID).Scan(&count, &p95); err != nil {
		t.Fatal(err)
	}
	if count != 1 || p95 != 450 {
		t.Fatalf("replay/rollback changed rows: %d %g", count, p95)
	}
	if _, err := replaceAppNetworkRows(ctx, pool, cfg, nil); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM analytics.app_network_daily`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("empty refresh affected other app: %d %v", count, err)
	}
}
