package main

import (
	"context"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"strings"
	"testing"
	"time"
)

func TestCrashSessionsConfig(t *testing.T) {
	for _, v := range [][2]string{{"", ""}, {"p.firebase_crashlytics.app", "p.firebase_sessions.app"}} {
		if err := crashSessionsAvailability(v[0], v[1]); err != nil {
			t.Fatal(err)
		}
	}
	for _, v := range [][2]string{{"p.d.t", ""}, {"", "p.d.t"}, {"p.d.t` injected", "p.d.t"}} {
		if crashSessionsAvailability(v[0], v[1]) == nil {
			t.Fatalf("accepted %#v", v)
		}
	}
}
func TestObservedCrashFree(t *testing.T) {
	if p, e := observedCrashFree(0, 0); e != nil || p != nil {
		t.Fatal("missing denominator must be unavailable")
	}
	if p, e := observedCrashFree(1, 4); e != nil || p == nil || *p != 75 {
		t.Fatal(p, e)
	}
	if _, e := observedCrashFree(5, 4); e == nil {
		t.Fatal("invalid numerator accepted")
	}
}
func TestCrashSessionsDocumentedSchemaAndBounds(t *testing.T) {
	for _, s := range []string{"crashlytics_data_collection_enabled = TRUE", "event_type = 'SESSION_START'", "firebase_session_id", "error_type = 'FATAL'", "COUNT(DISTINCT s.instance_id)", "LEFT JOIN fatal"} {
		if !strings.Contains(appCrashSQL, s) {
			t.Fatal(s)
		}
	}
	if strings.Count(appCrashSQL, "event_timestamp >= @start AND event_timestamp < @end") != 2 {
		t.Fatal("both exports must be bounded")
	}
	for _, s := range []string{"is_fatal", "issue.id", "tenant_id", "GA4"} {
		if strings.Contains(appCrashSQL, s) {
			t.Fatal(s)
		}
	}
}

func TestAppCrashReplacementPostgres(t *testing.T) {
	pool := pgtest.StartPostgres(t, context.Background())
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cfg := config{SourceAppID: "sg.mesha.goatos", SourceDate: time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC)}
	rows := []appCrashRow{{AppVersion: "1", TotalUsers: 4, TotalSessions: 5, CrashedUsers: 1, CrashedSessions: 2}}
	for i := 0; i < 2; i++ {
		if _, err := replaceAppCrashRows(ctx, pool, cfg, rows); err != nil {
			t.Fatal(err)
		}
	}
	bad := []appCrashRow{{AppVersion: "1", TotalUsers: 1, CrashedUsers: 2}}
	if _, err := replaceAppCrashRows(ctx, pool, cfg, bad); err == nil {
		t.Fatal("bad replacement accepted")
	}
	var count int
	var pct float64
	if err := pool.QueryRow(ctx, `SELECT count(*),max(crash_free_users_pct) FROM analytics.app_crash_daily WHERE source_app_id=$1`, cfg.SourceAppID).Scan(&count, &pct); err != nil {
		t.Fatal(err)
	}
	if count != 1 || pct != 75 {
		t.Fatalf("replay/rollback lost existing row: %d %f", count, pct)
	}
}
