package main

import (
	"context"
	"fmt"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
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
	for _, bound := range []string{"event_timestamp >= @start AND event_timestamp < @end", "event_timestamp >= @start AND event_timestamp < @observed_through", "f.event_timestamp >= s.session_started_at"} {
		if !strings.Contains(appCrashSQL, bound) {
			t.Fatalf("missing cohort bound %s", bound)
		}
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

// Execute the production aggregate in PostgreSQL with fixture source tables.
// BigQuery's nested application record is represented by a lateral record;
// bind-marker conversion changes dialect only, not membership or aggregation.
func TestCrashSessionCohortsIncludeFatalEventsAfterMidnightPostgres(t *testing.T) {
	pool := pgtest.StartPostgres(t, context.Background())
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	_, err = conn.Exec(ctx, `CREATE TEMP TABLE cohort_sessions(instance_id text, session_id text, app_version text, event_timestamp timestamptz, event_type text, crashlytics_data_collection_enabled boolean);
 CREATE TEMP TABLE cohort_crashes(firebase_session_id text,event_timestamp timestamptz,error_type text);
 INSERT INTO cohort_sessions VALUES
 ('install1','midnight','1','2020-09-15 23:59+05:30','SESSION_START',true),
 ('install1','midnight','1','2020-09-15 23:59+05:30','SESSION_START',true),
 ('install2','long-session','1','2020-09-15 22:00+05:30','SESSION_START',true),
 ('install3','healthy','1','2020-09-15 12:00+05:30','SESSION_START',true),
 ('install4','disabled','1','2020-09-15 12:00+05:30','SESSION_START',false);
 INSERT INTO cohort_crashes VALUES
 ('midnight','2020-09-16 00:01+05:30','FATAL'),
 ('midnight','2020-09-16 00:01+05:30','FATAL'),
 ('long-session','2020-09-17 02:00+05:30','FATAL'),
 ('healthy','2020-09-15 13:00+05:30','NON_FATAL'),
 ('disabled','2020-09-16 00:01+05:30','FATAL');`)
	if err != nil {
		t.Fatal(err)
	}
	day, _ := time.ParseInLocation("2006-01-02", "2020-09-15", biztime.DefaultLocation())
	query := fmt.Sprintf(appCrashSQL, "cohort_sessions CROSS JOIN LATERAL (SELECT app_version AS display_version) application", "cohort_crashes")
	query = strings.NewReplacer("@start", "$1", "@end", "$2", "@observed_through", "$3").Replace(query)
	for _, tc := range []struct {
		elapsed time.Duration
		crashed int64
	}{{24 * time.Hour, 0}, {36 * time.Hour, 1}, {72 * time.Hour, 2}} {
		args := []any{day, day.AddDate(0, 0, 1), day.Add(tc.elapsed)}
		var row appCrashRow
		if err = conn.QueryRow(ctx, query, args...).Scan(&row.AppVersion, &row.TotalUsers, &row.TotalSessions, &row.CrashedUsers, &row.CrashedSessions); err != nil {
			t.Fatal(err)
		}
		if row.TotalUsers != 3 || row.TotalSessions != 3 || row.CrashedUsers != tc.crashed || row.CrashedSessions != tc.crashed {
			t.Fatalf("observed through %s: %+v; want 3 sessions and %d crashed", tc.elapsed, row, tc.crashed)
		}
	}
}
