package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/calendar/domain"
	"github.com/vgoats/goatos/backend/internal/calendar/ports"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// TestRecomputeVaccinationHistoryProjectionMatchesLiveHistoryList is the parity proof for the
// Calendar completed-history + date-marker projection (migration 000178 + history_projection.go).
// RecomputeVaccinationHistoryProjection must reproduce, event-for-event, what the OLD live
// completed_history CTE (calendarListSQL) and the OLD live vaccination_completions marker aggregate
// (calendarDateMarkersSQL) used to compute per request straight off
// vaccination_completions/obligation_instances/protocol_*. ListEvents(status=completed) and
// GetEventDetail now read exclusively from this projection.
func TestRecomputeVaccinationHistoryProjectionMatchesLiveHistoryList(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	repo := NewRepository(pool, 5*time.Second)

	const (
		protocolID  = "87000000-0000-4000-8000-000000000931"
		versionID   = "87000000-0000-4000-8000-000000000932"
		ruleID      = "87000000-0000-4000-8000-000000000933"
		obligationA = "87000000-0000-4000-8000-000000000934"
		obligationB = "87000000-0000-4000-8000-000000000935"
		completionA = "87000000-0000-4000-8000-000000000936"
		completionB = "87000000-0000-4000-8000-000000000937"
	)
	administeredAt := time.Date(2025, time.December, 9, 3, 30, 0, 0, time.UTC)
	completedStatus := domain.StatusCompleted

	// Seed exactly like TestCalendarListsOldAcceptedVaccinationHistoryWithoutHotProjection: accepted
	// vaccination_completions + completed obligation_instances + protocol_* + locations.
	seedCalendarLocations(t, ctx, pool, testParkA, testShedA)
	seedVaccinationObligation(t, ctx, pool, protocolID, versionID, ruleID, obligationA, administeredAt)
	seedAdditionalVaccinationObligation(t, ctx, pool, versionID, ruleID, obligationB, administeredAt.Add(20*time.Minute))
	seedCalendarGoat(t, ctx, pool, obligationA)
	if _, err := pool.Exec(ctx, `
UPDATE goats
SET park_id=$2::uuid, shed_id=$3::uuid, current_location_id=$3::uuid, updated_at=now()
	WHERE tenant_id=$1::uuid AND goat_id IN ($4::uuid, $5::uuid)`,
		testTenantID, testParkA, testShedA, obligationA, obligationB); err != nil {
		t.Fatalf("locate history goats: %v", err)
	}
	if _, err := pool.Exec(ctx, `
UPDATE obligation_instances
SET target_type='goat', target_id=obligation_id, scope_type='shed', scope_id=$2::uuid,
    status='completed', completed_at=due_at, updated_at=now()
	WHERE tenant_id=$1::uuid AND obligation_id IN ($3::uuid, $4::uuid)`,
		testTenantID, testShedA, obligationA, obligationB); err != nil {
		t.Fatalf("complete history obligations: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO vaccination_completions (
  completion_id, tenant_id, obligation_id, goat_id, administered_at, verified_at,
  verified_by, status, idempotency_key
) VALUES
	  ($2::uuid, $1::uuid, $3::uuid, $3::uuid, $6::timestamptz, $6::timestamptz, $5::uuid, 'accepted', 'history-projection-a'),
	  ($7::uuid, $1::uuid, $4::uuid, $4::uuid, $8::timestamptz, $8::timestamptz, $5::uuid, 'accepted', 'history-projection-b')`,
		testTenantID, completionA, obligationA, obligationB, testActorID,
		administeredAt, completionB, administeredAt.Add(20*time.Minute)); err != nil {
		t.Fatalf("seed accepted vaccination history: %v", err)
	}

	wantEventID := "history:2025-12-09:" + testParkA + ":" + testShedA + ":" + ruleID

	// Parity oracle: an independent reconstruction straight off the same canonical tables the
	// projector reads, mirroring the shape of the OLD live completed_history CTE this projection
	// replaces (target_count/window = count/min/max over accepted completions for this rule).
	var wantTargetCount int
	var wantWindowStart, wantWindowEnd time.Time
	if err := pool.QueryRow(ctx, `
SELECT count(*), min(vc.administered_at), max(vc.administered_at)
FROM vaccination_completions vc
JOIN obligation_instances oi ON oi.tenant_id = vc.tenant_id AND oi.obligation_id = vc.obligation_id
WHERE vc.tenant_id = $1::uuid AND vc.status = 'accepted' AND oi.status = 'completed'
  AND oi.target_type = 'goat' AND oi.rule_id = $2::uuid`, testTenantID, ruleID).
		Scan(&wantTargetCount, &wantWindowStart, &wantWindowEnd); err != nil {
		t.Fatalf("canonical oracle query: %v", err)
	}
	if wantTargetCount != 2 {
		t.Fatalf("fixture setup failed: canonical target_count=%d, want 2", wantTargetCount)
	}

	// Before any recompute, the completed-history read must FAIL CLOSED (C5-002), never return a
	// misleading empty success: an empty result here would be indistinguishable from "genuinely no
	// history" when it is really "the projection has never been built". This is independent of the hot
	// vaccination (UPCOMING) projection's own freshness gate, which stays a separate concern.
	if _, err := repo.ListEvents(ctx, domain.Query{
		TenantID: testTenantID, OwnerKey: domain.OwnerAll, Status: &completedStatus,
		DateFrom: administeredAt.Add(-time.Hour), DateTo: administeredAt.Add(24 * time.Hour),
		Limit: 20, IncludeDateMarkers: true, Scope: domain.ScopeFilter{TenantWide: true},
	}); !errors.Is(err, ports.ErrProjectionUnavailable) {
		t.Fatalf("ListEvents before first history recompute = %v, want ErrProjectionUnavailable (never-synced fails closed)", err)
	}

	rows, err := repo.RecomputeVaccinationHistoryProjection(ctx, ports.RefreshVaccinationHistoryProjection{
		TenantID: testTenantID,
		DateFrom: administeredAt.Add(-24 * time.Hour),
		// +48h (not +24h): the coverage contract projects one day beyond the max query range (same
		// inclusive-query/exclusive-projection-bound contract as the UPCOMING gate), and the ListEvents
		// call below queries DateTo=administeredAt+24h, whose exclusive bound is DateTo+24h. Matching it
		// exactly here lets the later fresh-in-window assertion prove full (non-partial) coverage.
		DateTo: administeredAt.Add(48 * time.Hour),
	})
	if err != nil {
		t.Fatalf("RecomputeVaccinationHistoryProjection: %v", err)
	}
	if rows != 1 {
		t.Fatalf("history projection rows = %d, want 1 grouped event", rows)
	}

	servingVersion, ok := repo.historyProjectionServingVersion(ctx, testTenantID)
	if !ok || servingVersion <= 0 {
		t.Fatalf("expected a serving history projection version after recompute, ok=%v version=%d", ok, servingVersion)
	}

	// Direct projection-row parity: the projector's grouped output must match the canonical oracle.
	var gotEventID string
	var gotTargetCount int
	var gotWindowStart, gotWindowEnd time.Time
	if err := pool.QueryRow(ctx, `
SELECT event_id, target_count, window_start, window_end
FROM calendar_history_projection_rows
WHERE tenant_id = $1::uuid AND projection_version = $2::bigint`,
		testTenantID, servingVersion).Scan(&gotEventID, &gotTargetCount, &gotWindowStart, &gotWindowEnd); err != nil {
		t.Fatalf("read projected history row: %v", err)
	}
	if gotEventID != wantEventID {
		t.Fatalf("projected event_id = %q, want %q", gotEventID, wantEventID)
	}
	if gotTargetCount != wantTargetCount {
		t.Fatalf("projected target_count = %d, want %d (parity with canonical oracle)", gotTargetCount, wantTargetCount)
	}
	if !gotWindowStart.Equal(wantWindowStart) || !gotWindowEnd.Equal(wantWindowEnd) {
		t.Fatalf("projected window = [%s, %s], want [%s, %s] (parity with canonical oracle)",
			gotWindowStart, gotWindowEnd, wantWindowStart, wantWindowEnd)
	}

	// Marker parity: completion_count must equal the canonical per-day/per-scope accepted-completion
	// count (the OLD live marker branch's count(*) over vaccination_completions for that day/scope).
	var gotMarkerCount int64
	if err := pool.QueryRow(ctx, `
SELECT completion_count FROM calendar_history_date_markers
WHERE tenant_id = $1::uuid AND projection_version = $2::bigint AND business_date = $3::date
  AND park_key = $4::text AND shed_key = $5::text`,
		testTenantID, servingVersion, "2025-12-09", testParkA, testShedA).Scan(&gotMarkerCount); err != nil {
		t.Fatalf("read projected history date marker: %v", err)
	}
	if gotMarkerCount != int64(wantTargetCount) {
		t.Fatalf("projected marker completion_count = %d, want %d", gotMarkerCount, wantTargetCount)
	}

	// ListEvents now reads the projection exclusively.
	list, err := repo.ListEvents(ctx, domain.Query{
		TenantID: testTenantID, OwnerKey: domain.OwnerAll, Status: &completedStatus,
		DateFrom: administeredAt.Add(-time.Hour), DateTo: administeredAt.Add(24 * time.Hour),
		Limit: 20, IncludeDateMarkers: true, Scope: domain.ScopeFilter{TenantWide: true},
	})
	if err != nil {
		t.Fatalf("ListEvents after history recompute: %v", err)
	}
	if len(list.Items) != 1 {
		t.Fatalf("history items=%d, want one shed/rule/day group: %#v", len(list.Items), list.Items)
	}
	history := list.Items[0]
	if history.EventID != wantEventID || history.TargetCount != wantTargetCount || history.Status != domain.StatusCompleted {
		t.Fatalf("history event=%#v, want event_id=%q target_count=%d", history, wantEventID, wantTargetCount)
	}
	if len(list.DateMarkers) != 1 || list.DateMarkers[0].Date != "2025-12-09" || list.DateMarkers[0].CompletedCount != wantTargetCount {
		t.Fatalf("history date markers=%#v, want one completed marker with %d administrations", list.DateMarkers, wantTargetCount)
	}
	// Fresh, in-window history projection metadata must be surfaced clean (C5-002): not stale, not
	// partial coverage -- the query window sits entirely inside the projector's own build window.
	if list.HistoryProjection == nil || list.HistoryProjection.Stale || list.HistoryProjection.PartialCoverage {
		t.Fatalf("history projection metadata=%#v, want non-nil, fresh, full coverage", list.HistoryProjection)
	}
	assertCount(t, ctx, pool, "history not copied to hot calendar_event_projections", `
SELECT count(*) FROM calendar_event_projections
WHERE tenant_id = $1::uuid AND event_id = $2`, 0, testTenantID, history.EventID)

	// GetEventDetail now reads calendar_history_projection_rows.detail directly by event_id.
	detail, err := repo.GetEventDetail(ctx, domain.EventQuery{
		TenantID: testTenantID, EventID: history.EventID, Scope: domain.ScopeFilter{TenantWide: true},
	})
	if err != nil {
		t.Fatalf("GetEventDetail history: %v", err)
	}
	wantCompletedCount := fmt.Sprintf(`"completed_count": %d`, wantTargetCount)
	if detail.Event.TargetCount != wantTargetCount || detail.Event.Status != domain.StatusCompleted ||
		!strings.Contains(string(detail.Execution), wantCompletedCount) {
		t.Fatalf("history detail=%#v execution=%s, want completed_count=%d", detail.Event, detail.Execution, wantTargetCount)
	}
	if detail.HistoryProjection == nil || detail.HistoryProjection.Stale {
		t.Fatalf("history detail projection metadata=%#v, want non-nil and fresh", detail.HistoryProjection)
	}
}

// TestCalendarHistoryProjectionServesStaleLastKnownGoodWithFlag proves a STALE (but previously
// synced) history projection is NOT fail-closed (C5-002): history is append-mostly, so serving the
// last-known-good rows is safer than a 503, but the response must surface Stale=true rather than
// silently presenting stale data as fully fresh.
func TestCalendarHistoryProjectionServesStaleLastKnownGoodWithFlag(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	repo := NewRepository(pool, 5*time.Second)

	const (
		protocolID  = "87000000-0000-4000-8000-000000000951"
		versionID   = "87000000-0000-4000-8000-000000000952"
		ruleID      = "87000000-0000-4000-8000-000000000953"
		obligationA = "87000000-0000-4000-8000-000000000954"
		completionA = "87000000-0000-4000-8000-000000000956"
	)
	administeredAt := time.Date(2025, time.October, 1, 3, 30, 0, 0, time.UTC)
	completedStatus := domain.StatusCompleted

	seedCalendarLocations(t, ctx, pool, testParkA, testShedA)
	seedVaccinationObligation(t, ctx, pool, protocolID, versionID, ruleID, obligationA, administeredAt)
	seedCalendarGoat(t, ctx, pool, obligationA)
	if _, err := pool.Exec(ctx, `
UPDATE goats
SET park_id=$2::uuid, shed_id=$3::uuid, current_location_id=$3::uuid, updated_at=now()
	WHERE tenant_id=$1::uuid AND goat_id = $4::uuid`,
		testTenantID, testParkA, testShedA, obligationA); err != nil {
		t.Fatalf("locate stale-fixture goat: %v", err)
	}
	if _, err := pool.Exec(ctx, `
UPDATE obligation_instances
SET target_type='goat', target_id=obligation_id, scope_type='shed', scope_id=$2::uuid,
    status='completed', completed_at=due_at, updated_at=now()
	WHERE tenant_id=$1::uuid AND obligation_id = $3::uuid`,
		testTenantID, testShedA, obligationA); err != nil {
		t.Fatalf("complete stale-fixture obligation: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO vaccination_completions (
  completion_id, tenant_id, obligation_id, goat_id, administered_at, verified_at,
  verified_by, status, idempotency_key
) VALUES ($2::uuid, $1::uuid, $3::uuid, $3::uuid, $5::timestamptz, $5::timestamptz, $4::uuid, 'accepted', 'history-projection-stale-a')`,
		testTenantID, completionA, obligationA, testActorID, administeredAt); err != nil {
		t.Fatalf("seed stale-fixture accepted completion: %v", err)
	}

	if _, err := repo.RecomputeVaccinationHistoryProjection(ctx, ports.RefreshVaccinationHistoryProjection{
		TenantID: testTenantID,
		DateFrom: administeredAt.Add(-24 * time.Hour),
		DateTo:   administeredAt.Add(24 * time.Hour),
	}); err != nil {
		t.Fatalf("RecomputeVaccinationHistoryProjection: %v", err)
	}

	// Backdate the serving state well past defaultHistoryProjectionFresh (90m) without touching the
	// projected rows themselves -- simulates a scheduled projector run that stopped landing.
	if _, err := pool.Exec(ctx, `
UPDATE calendar_history_projection_state
SET projected_at = now() - interval '3 hours'
WHERE tenant_id = $1::uuid`, testTenantID); err != nil {
		t.Fatalf("backdate history projection state: %v", err)
	}

	list, err := repo.ListEvents(ctx, domain.Query{
		TenantID: testTenantID, OwnerKey: domain.OwnerAll, Status: &completedStatus,
		DateFrom: administeredAt.Add(-time.Hour), DateTo: administeredAt.Add(24 * time.Hour),
		Limit: 20, IncludeDateMarkers: true, Scope: domain.ScopeFilter{TenantWide: true},
	})
	if err != nil {
		t.Fatalf("ListEvents stale history: %v, want last-known-good success (not fail-closed)", err)
	}
	if len(list.Items) != 1 {
		t.Fatalf("stale history items=%d, want the last-known-good grouped event still served", len(list.Items))
	}
	if list.HistoryProjection == nil || !list.HistoryProjection.Stale {
		t.Fatalf("history projection metadata=%#v, want Stale=true surfaced for a projection well past its TTL", list.HistoryProjection)
	}
}

// TestCalendarHistoryProjectionOutOfWindowSurfacesPartialCoverage proves a request whose date range
// extends beyond the history projection's own built [date_from, date_to) window still serves (never
// fails closed on coverage alone) but surfaces PartialCoverage=true so the caller knows the response
// may be missing rows outside what was actually projected.
func TestCalendarHistoryProjectionOutOfWindowSurfacesPartialCoverage(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	repo := NewRepository(pool, 5*time.Second)

	completedStatus := domain.StatusCompleted
	narrowFrom := time.Date(2025, time.September, 1, 0, 0, 0, 0, time.UTC)
	narrowTo := time.Date(2025, time.September, 10, 0, 0, 0, 0, time.UTC)
	seedCalendarHistoryProjectionState(t, ctx, pool, testTenantID, narrowFrom, narrowTo)

	// Request a window that starts well before the projected date_from.
	list, err := repo.ListEvents(ctx, domain.Query{
		TenantID: testTenantID, OwnerKey: domain.OwnerAll, Status: &completedStatus,
		DateFrom: narrowFrom.Add(-30 * 24 * time.Hour), DateTo: narrowFrom,
		Limit: 20, IncludeDateMarkers: true, Scope: domain.ScopeFilter{TenantWide: true},
	})
	if err != nil {
		t.Fatalf("ListEvents out-of-window history: %v, want success with partial coverage surfaced", err)
	}
	if list.HistoryProjection == nil || !list.HistoryProjection.PartialCoverage {
		t.Fatalf("history projection metadata=%#v, want PartialCoverage=true for a request outside the projected window", list.HistoryProjection)
	}
	if list.HistoryProjection.Stale {
		t.Fatalf("history projection metadata=%#v, want Stale=false -- coverage and staleness are independent signals", list.HistoryProjection)
	}
}

// TestRecomputeVaccinationHistoryProjectionPruneKeepsOnlyServingVersion proves the bounded prune
// removes stale projection_version rows/markers from a prior run while leaving the newly-published
// serving version's rows/markers untouched (same last-known-good discipline as
// vaccinationexecution's shed-projection prune).
func TestRecomputeVaccinationHistoryProjectionPruneKeepsOnlyServingVersion(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	repo := NewRepository(pool, 5*time.Second)

	const (
		protocolID  = "87000000-0000-4000-8000-000000000941"
		versionID   = "87000000-0000-4000-8000-000000000942"
		ruleID      = "87000000-0000-4000-8000-000000000943"
		obligationA = "87000000-0000-4000-8000-000000000944"
		completionA = "87000000-0000-4000-8000-000000000946"
	)
	administeredAt := time.Date(2025, time.November, 3, 3, 30, 0, 0, time.UTC)

	seedCalendarLocations(t, ctx, pool, testParkA, testShedA)
	seedVaccinationObligation(t, ctx, pool, protocolID, versionID, ruleID, obligationA, administeredAt)
	seedCalendarGoat(t, ctx, pool, obligationA)
	if _, err := pool.Exec(ctx, `
UPDATE goats
SET park_id=$2::uuid, shed_id=$3::uuid, current_location_id=$3::uuid, updated_at=now()
	WHERE tenant_id=$1::uuid AND goat_id = $4::uuid`,
		testTenantID, testParkA, testShedA, obligationA); err != nil {
		t.Fatalf("locate prune-fixture goat: %v", err)
	}
	if _, err := pool.Exec(ctx, `
UPDATE obligation_instances
SET target_type='goat', target_id=obligation_id, scope_type='shed', scope_id=$2::uuid,
    status='completed', completed_at=due_at, updated_at=now()
	WHERE tenant_id=$1::uuid AND obligation_id = $3::uuid`,
		testTenantID, testShedA, obligationA); err != nil {
		t.Fatalf("complete prune-fixture obligation: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO vaccination_completions (
  completion_id, tenant_id, obligation_id, goat_id, administered_at, verified_at,
  verified_by, status, idempotency_key
) VALUES ($2::uuid, $1::uuid, $3::uuid, $3::uuid, $5::timestamptz, $5::timestamptz, $4::uuid, 'accepted', 'history-projection-prune-a')`,
		testTenantID, completionA, obligationA, testActorID, administeredAt); err != nil {
		t.Fatalf("seed prune-fixture accepted completion: %v", err)
	}

	in := ports.RefreshVaccinationHistoryProjection{
		TenantID: testTenantID,
		DateFrom: administeredAt.Add(-24 * time.Hour),
		DateTo:   administeredAt.Add(24 * time.Hour),
	}
	if _, err := repo.RecomputeVaccinationHistoryProjection(ctx, in); err != nil {
		t.Fatalf("first RecomputeVaccinationHistoryProjection: %v", err)
	}
	firstVersion, ok := repo.historyProjectionServingVersion(ctx, testTenantID)
	if !ok {
		t.Fatalf("expected serving version after first recompute")
	}

	if _, err := repo.RecomputeVaccinationHistoryProjection(ctx, in); err != nil {
		t.Fatalf("second RecomputeVaccinationHistoryProjection: %v", err)
	}
	secondVersion, ok := repo.historyProjectionServingVersion(ctx, testTenantID)
	if !ok || secondVersion == firstVersion {
		t.Fatalf("expected a new serving version after second recompute, first=%d second=%d ok=%v", firstVersion, secondVersion, ok)
	}

	assertCount(t, ctx, pool, "stale history projection rows pruned", `
SELECT count(*) FROM calendar_history_projection_rows
WHERE tenant_id = $1::uuid AND projection_version = $2::bigint`, 0, testTenantID, firstVersion)
	assertCount(t, ctx, pool, "stale history date markers pruned", `
SELECT count(*) FROM calendar_history_date_markers
WHERE tenant_id = $1::uuid AND projection_version = $2::bigint`, 0, testTenantID, firstVersion)
	assertCount(t, ctx, pool, "serving history projection rows retained", `
SELECT count(*) FROM calendar_history_projection_rows
WHERE tenant_id = $1::uuid AND projection_version = $2::bigint`, 1, testTenantID, secondVersion)
	assertCount(t, ctx, pool, "serving history date markers retained", `
SELECT count(*) FROM calendar_history_date_markers
WHERE tenant_id = $1::uuid AND projection_version = $2::bigint`, 1, testTenantID, secondVersion)
}

// TestCalendarHistoryProjectionReadsUseIndexes proves the switched completed-history read
// (calendar_history_projection_rows) and the switched marker read (calendar_history_date_markers)
// stay indexed, never a sequential scan, at their production request shape.
func TestCalendarHistoryProjectionReadsUseIndexes(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, "SET LOCAL enable_seqscan = off"); err != nil {
		t.Fatal(err)
	}

	explain := func(label, sql string, args ...any) string {
		t.Helper()
		rows, err := tx.Query(ctx, "EXPLAIN (COSTS OFF) "+sql, args...)
		if err != nil {
			t.Fatalf("%s: explain query: %v", label, err)
		}
		defer rows.Close()
		var lines []string
		for rows.Next() {
			var line string
			if err := rows.Scan(&line); err != nil {
				t.Fatalf("%s: scan explain line: %v", label, err)
			}
			lines = append(lines, line)
		}
		return strings.Join(lines, "\n")
	}

	rowsPlan := explain("CalendarHistoryProjectionRowsRead", `
SELECT event_id, title, subtitle, target_count, window_start, window_end
FROM calendar_history_projection_rows
WHERE tenant_id = $1::uuid
  AND projection_version = $2::bigint
  AND business_date >= $3::date
  AND business_date < $4::date
ORDER BY business_date, event_id`,
		testTenantID, int64(1), "2025-01-01", "2025-02-01")
	if strings.Contains(rowsPlan, "Seq Scan on calendar_history_projection_rows") {
		t.Fatalf("calendar_history_projection_rows hot-list read used sequential scan:\n%s", rowsPlan)
	}
	if !strings.Contains(rowsPlan, "Index") {
		t.Fatalf("calendar_history_projection_rows hot-list read did not use an index:\n%s", rowsPlan)
	}

	markersPlan := explain("CalendarHistoryDateMarkersRead", `
SELECT business_date, SUM(completion_count)
FROM calendar_history_date_markers
WHERE tenant_id = $1::uuid
  AND projection_version = $2::bigint
  AND business_date >= $3::date
  AND business_date < $4::date
GROUP BY business_date`,
		testTenantID, int64(1), "2025-01-01", "2025-02-01")
	if strings.Contains(markersPlan, "Seq Scan on calendar_history_date_markers") {
		t.Fatalf("calendar_history_date_markers read used sequential scan:\n%s", markersPlan)
	}
	if !strings.Contains(markersPlan, "Index") {
		t.Fatalf("calendar_history_date_markers read did not use an index:\n%s", markersPlan)
	}
}
