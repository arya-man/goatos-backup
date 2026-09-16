package main

import (
	"context"
	"encoding/json"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"testing"
	"time"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }
func TestAppEventsRollupMultipleDimensionsPostgres(t *testing.T) {
	pool := pgtest.StartPostgres(t, context.Background())
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	day, _ := time.ParseInLocation("2006-01-02", "2020-09-15", biztime.DefaultLocation())
	cfg := config{TenantID: testTenantID, SourceDate: day, Source: "app_events"}
	actor := "00000000-0000-4000-8000-000000000002"
	add := func(tenant, user, device, journey, event, id string, at time.Time, extra map[string]string) {
		t.Helper()
		p := map[string]string{"journey_id": journey}
		for k, v := range extra {
			p[k] = v
		}
		raw, _ := json.Marshal(p)
		_, err := pool.Exec(ctx, `INSERT INTO analytics.app_events(tenant_id,actor_id,device_id,event_name,properties,client_event_id,client_event_time,received_at) VALUES($1,NULLIF($2,'')::uuid,$3,$4,$5,$6,$7,$7)`, tenant, user, device, event, string(raw), id, at)
		if err != nil {
			t.Fatal(err)
		}
	}
	addProof := func(tenant, user, device, journey, proof string, offset time.Duration, names []string) {
		for i, name := range names {
			add(tenant, user, device, journey, name, tenant+device+proof+name, day.Add(offset+time.Duration(i)*time.Second), map[string]string{"proof_id": proof})
		}
	}
	// A long observed work session must not shorten a late-opened feature's
	// own24hour completion window across midnight.
	add(testTenantID, actor, "d1", "j", "route_entered", "early", day.Add(5*time.Minute), nil)
	late := map[string]string{"group_key": "late", "outbox_item_id": "late", "action": "submit"}
	add(testTenantID, actor, "d1", "j", "feed_transport_opened", "late-open", day.Add(23*time.Hour+55*time.Minute), late)
	add(testTenantID, actor, "d1", "j", "feed_transport_submitted", "late-enqueue", day.Add(23*time.Hour+56*time.Minute), late)
	add(testTenantID, actor, "d1", "j", "sync_write_succeeded", "late-ack", day.Add(24*time.Hour+10*time.Minute), late)
	events := appEventFlows()[0].Events
	addProof(testTenantID, actor, "d1", "j", "p1", time.Hour, events)
	addProof(testTenantID, actor, "d2", "j", "p1", 2*time.Hour, events[:1])
	addProof(testTenantID, actor, "d1", "j", "p2", 3*time.Hour, []string{events[3], events[0], events[2]})
	addProof(testTenantID, actor, "d3", "", "p3", 4*time.Hour, events)
	addProof("00000000-0000-4000-8000-000000000003", actor, "d1", "j", "p1", time.Hour, events)
	addProof(testTenantID, actor, "d1", "j", "midnight", 24*time.Hour-2*time.Second, events)
	props := map[string]string{"group_key": "g", "outbox_item_id": "correct", "action": "submit"}
	add(testTenantID, actor, "d1", "j", "feed_packing_complete_opened", "open", day.Add(5*time.Hour), props)
	add(testTenantID, actor, "d1", "j", "feed_packing_submitted", "submit", day.Add(5*time.Hour+time.Second), props)
	add(testTenantID, actor, "d1", "j", "sync_write_succeeded", "wrong", day.Add(5*time.Hour+2*time.Second), map[string]string{"outbox_item_id": "wrong"})
	add(testTenantID, actor, "d1", "old", "route_entered", "prior", day.AddDate(0, 0, -3), nil)
	add(testTenantID, "00000000-0000-4000-8000-000000000004", "d4", "old", "route_entered", "prior2", day.AddDate(0, 0, -3), nil)
	if _, err := runAppEventsRollup(ctx, pool, cfg); err != nil {
		t.Fatal(err)
	}
	scalar := func(q string) int64 {
		t.Helper()
		var n int64
		if err := pool.QueryRow(ctx, q).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	for q, want := range map[string]int64{
		`SELECT conversions FROM analytics.funnel_daily WHERE funnel_key='proof_delivery' AND step_index=3`: 2,
		`SELECT sessions FROM analytics.funnel_daily WHERE funnel_key='proof_delivery' AND step_index=0`:    2,
		`SELECT conversions FROM analytics.funnel_daily WHERE funnel_key='feed_packing' AND step_index=3`:   0,
		`SELECT conversions FROM analytics.funnel_daily WHERE funnel_key='feed_transport' AND step_index=3`: 1,
		`SELECT dau FROM analytics.engagement_daily`:                                                        1, `SELECT wau_approx FROM analytics.engagement_daily`: 2,
		`SELECT drop_offs FROM analytics.journey_daily WHERE journey_key='proof_delivery'`: 2,
	} {
		if n := scalar(q); n != want {
			t.Fatalf("%s got=%d want=%d", q, n, want)
		}
	}
	add(testTenantID, actor, "d1", "j", "sync_write_succeeded", "correct", day.Add(5*time.Hour+3*time.Second), props)
	for i := 0; i < 2; i++ {
		if _, err := runAppEventsRollup(ctx, pool, cfg); err != nil {
			t.Fatal(err)
		}
	}
	if n := scalar(`SELECT conversions FROM analytics.funnel_daily WHERE funnel_key='feed_packing' AND step_index=3`); n != 1 {
		t.Fatalf("late ack/idempotency=%d", n)
	}
	// Legacy duplicate deliveries without the newer client_event_id column
	// still dedupe on their property id, even if a retry payload drifted.
	_, err := pool.Exec(ctx, `INSERT INTO analytics.app_events(tenant_id,actor_id,device_id,event_name,properties,client_event_time,received_at)
 SELECT $1,$2,'d1','proof_capture_completed',jsonb_build_object('journey_id','j','proof_id','legacy-'||n,'client_event_id','legacy-id'),$3::timestamptz+n*interval '1 second',$3::timestamptz+n*interval '1 second'
 FROM generate_series(1,2) n`, testTenantID, actor, day.Add(6*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = runAppEventsRollup(ctx, pool, cfg); err != nil {
		t.Fatal(err)
	}
	if n := scalar(`SELECT conversions FROM analytics.funnel_daily WHERE funnel_key='proof_delivery' AND step_index=0`); n != 5 {
		t.Fatalf("legacy duplicate inflated cohorts=%d", n)
	}
	// A late failure must roll back ALL replacements, including deletion of
	// prior definitions. Inject a constraint only new writes need to satisfy.
	_, err = pool.Exec(ctx, `INSERT INTO analytics.funnel_daily(tenant_id,event_date,funnel_key,step_key,step_index) VALUES($1,$2,'previous_definition','old',0)`, testTenantID, day.Format("2006-01-02"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, `ALTER TABLE analytics.engagement_daily ADD CONSTRAINT injected_rollup_failure CHECK(dau=999) NOT VALID`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = runAppEventsRollup(ctx, pool, cfg); err == nil {
		t.Fatal("expected injected final-write failure")
	}
	if n := scalar(`SELECT count(*) FROM analytics.funnel_daily WHERE funnel_key='previous_definition'`); n != 1 {
		t.Fatalf("failed transaction deleted earlier summary")
	}
	parsed, err := parseConfig([]string{"--tenant-id", testTenantID})
	if err != nil || parsed.Source != "app_events" {
		t.Fatalf("default config=%+v err=%v", parsed, err)
	}
}

func TestAppEventsRollupWholePopulationBeyondPageBoundaryPostgres(t *testing.T) {
	pool := pgtest.StartPostgres(t, context.Background())
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	day, _ := time.ParseInLocation("2006-01-02", "2020-09-15", biztime.DefaultLocation())
	// 46k rows exceed all mobile/API page limits. The summary must include the
	// final cohort, not just a first page, and must collapse four stage rows per
	// cohort without multiplying distinct session counts.
	_, err := pool.Exec(ctx, `INSERT INTO analytics.app_events(tenant_id,actor_id,device_id,event_name,properties,client_event_time)
 SELECT $1,'00000000-0000-4000-8000-000000000002','device',
 (ARRAY['proof_capture_completed','proof_processing_completed','proof_upload_started','proof_upload_completed'])[step],
 jsonb_build_object('journey_id','session','proof_id',n::text),$2::timestamptz+n*interval '1 second'+step*interval '1 millisecond'
 FROM generate_series(1,11500) n CROSS JOIN generate_series(1,4) step`, testTenantID, day)
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	rows, err := runAppEventsRollup(ctx, pool, config{TenantID: testTenantID, SourceDate: day, Source: "app_events"})
	if err != nil {
		t.Fatal(err)
	}
	var n int64
	if err = pool.QueryRow(ctx, `SELECT conversions FROM analytics.funnel_daily WHERE funnel_key='proof_delivery' AND step_index=3`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	var sessions int64
	if err = pool.QueryRow(ctx, `SELECT sessions FROM analytics.funnel_daily WHERE funnel_key='proof_delivery' AND step_index=3`).Scan(&sessions); err != nil {
		t.Fatal(err)
	}
	if sessions != 1 {
		t.Fatalf("one session across all pages became %d", sessions)
	}
	if n != 11500 {
		t.Fatalf("conversions=%d", n)
	}
	t.Logf("source_rows=46000 proof_cohorts=11500 summary_rows=%d rollup_ms=%d", rows, time.Since(started).Milliseconds())
}

func TestAppEventsLookbackRefreshesAllDaysPostgres(t *testing.T) {
	pool := pgtest.StartPostgres(t, context.Background())
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	day, _ := time.ParseInLocation("2006-01-02", "2020-09-15", biztime.DefaultLocation())
	run, err := startRollupRun(ctx, pool, day)
	if err != nil {
		t.Fatal(err)
	}
	if err = runAppEventsAndFinish(ctx, pool, config{TenantID: testTenantID, SourceDate: day, Source: "app_events", LookbackDays: 3}, run, time.Now()); err != nil {
		t.Fatal(err)
	}
	var days int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM analytics.engagement_daily WHERE tenant_id=$1 AND event_date BETWEEN '2020-09-13' AND '2020-09-15'`, testTenantID).Scan(&days); err != nil {
		t.Fatal(err)
	}
	if days != 3 {
		t.Fatalf("lookback refreshed%d days", days)
	}
	var status string
	if err = pool.QueryRow(ctx, `SELECT status FROM analytics.rollup_run WHERE run_id=$1`, run).Scan(&status); err != nil || status != "succeeded" {
		t.Fatalf("audit=%s err=%v", status, err)
	}
}

func TestRollupFailureAuditSurvivesWorkCancellationPostgres(t *testing.T) {
	pool := pgtest.StartPostgres(t, context.Background())
	run, err := startRollupRun(context.Background(), pool, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err = finishRollupRun(ctx, pool, run, "failed", 0, 0, context.DeadlineExceeded); err != nil {
		t.Fatal(err)
	}
	var status string
	if err = pool.QueryRow(context.Background(), `SELECT status FROM analytics.rollup_run WHERE run_id=$1`, run).Scan(&status); err != nil || status != "failed" {
		t.Fatalf("auditstatus=%s err=%v", status, err)
	}
}

// Android's photo/video counters and recreated launchers can all emit token 1
// under the same persisted journey. Only the per-launcher capture identity is safe.
func TestCameraCohortsDoNotMergeReusedLocalTokensPostgres(t *testing.T) {
	pool := pgtest.StartPostgres(t, context.Background())
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	day, _ := time.ParseInLocation("2006-01-02", "2020-09-15", biztime.DefaultLocation())
	add := func(id, event string, offset time.Duration) {
		t.Helper()
		props, _ := json.Marshal(map[string]string{"journey_id": "persisted", "request_token": "1", "capture_request_id": id})
		_, err := pool.Exec(ctx, `INSERT INTO analytics.app_events(tenant_id,actor_id,device_id,event_name,properties,client_event_time)
   VALUES($1,'00000000-0000-4000-8000-000000000002','device',$2,$3,$4)`, testTenantID, event, string(props), day.Add(offset))
		if err != nil {
			t.Fatal(err)
		}
	}
	// A canceled request, a photo, and video after launcher/process recreation.
	add("video-before:1", "proof_camera_requested", time.Hour)
	add("video-before:1", "proof_camera_cancelled", time.Hour+time.Second)
	add("photo:1", "proof_camera_requested", 2*time.Hour)
	add("photo:1", "proof_camera_finalized", 2*time.Hour+2*time.Second)
	add("video-after:1", "proof_camera_requested", 3*time.Hour)
	add("video-after:1", "proof_camera_finalized", 3*time.Hour+4*time.Second)
	// Old APKs have no globally unique capture identity. Never guess a completion
	// from ambiguous legacy counters or mix it into a new capture.
	add("", "proof_camera_requested", 4*time.Hour)
	add("", "proof_camera_finalized", 4*time.Hour+time.Second)
	for i := 0; i < 2; i++ {
		if _, err := runAppEventsRollup(ctx, pool, config{TenantID: testTenantID, SourceDate: day}); err != nil {
			t.Fatal(err)
		}
		var starts, completed, dropped, p50 int64
		if err := pool.QueryRow(ctx, `SELECT conversions FROM analytics.funnel_daily WHERE funnel_key='camera_capture' AND step_index=0`).Scan(&starts); err != nil {
			t.Fatal(err)
		}
		if err := pool.QueryRow(ctx, `SELECT completions,drop_offs,p50_ms FROM analytics.journey_daily WHERE journey_key='camera_capture'`).Scan(&completed, &dropped, &p50); err != nil {
			t.Fatal(err)
		}
		if starts != 3 || completed != 2 || dropped != 1 || p50 != 3000 {
			t.Fatalf("starts=%d completed=%d dropped=%d p50=%d; want 3,2,1,3000", starts, completed, dropped, p50)
		}
	}
}

// A provider failure must still leave the full first-party lookback fresh and
// the audit failed; it must never certify missing Firebase data as success.
func TestFirebaseFailureDoesNotStrandFirstPartyDatesPostgres(t *testing.T) {
	pool := pgtest.StartPostgres(t, context.Background())
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	day, _ := time.ParseInLocation("2006-01-02", "2020-09-15", biztime.DefaultLocation())
	cfg := config{TenantID: testTenantID, SourceDate: day, Source: "app_events", LookbackDays: 3, CrashlyticsTable: "invalid"}
	runID, err := startRollupRun(ctx, pool, day)
	if err != nil {
		t.Fatal(err)
	}
	if err := runAppEventsAndFinish(ctx, pool, cfg, runID, time.Now()); err == nil {
		t.Fatal("provider configuration failure must remain visible")
	}
	var dates int
	if err := pool.QueryRow(ctx, `SELECT count(DISTINCT event_date) FROM analytics.engagement_daily WHERE tenant_id=$1 AND event_date BETWEEN $2::date-2 AND $2::date`, testTenantID, day).Scan(&dates); err != nil {
		t.Fatal(err)
	}
	if dates != 3 {
		t.Fatalf("refreshed %d dates; want all 3 despite provider failure", dates)
	}
	var status string
	if err := pool.QueryRow(ctx, `SELECT status FROM analytics.rollup_run WHERE run_id=$1`, runID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "failed" {
		t.Fatalf("status=%s; want failed", status)
	}
}
