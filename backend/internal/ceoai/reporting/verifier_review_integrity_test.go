// Verifier video-review integrity aggregate (ceo_ai.verifier_review_integrity, migration
// 000114) -- closes leadership-assistant coverage gap G10. These are the projection-review
// adversarial proofs the aggregate-projection guard requires: whole-filter (not page-local),
// correct below-threshold integrity count, correct median/p90 against a known distribution, and
// no fan-out across (verifier, park, category, business_day).
package reporting

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// seedDecidedItem inserts one decided (approved/rejected) verification_items row plus a matching
// review-event trail (item_opened, video_play, video_ended, verdict_recorded) so the view under
// test has both the authoritative verdict (verification_items) and the client telemetry
// (verification_review_events) it joins.
func seedDecidedItem(
	t *testing.T, ctx context.Context, pool *pgxpool.Pool,
	tenant, parkID, actorID, category string,
	status string, verdictReason *string,
	openedAt, verdictAt time.Time,
	proofDurationMs, watchedMs int64,
) {
	t.Helper()
	var itemID string
	if err := pool.QueryRow(ctx, `
INSERT INTO verification_items (
    item_id, tenant_id, vertical, module, category, source_module, source_ref_type, source_ref_id,
    media_refs, status, verdict_reason, shed_id, park_id, captured_at, verified_by, verified_at,
    idempotency_key, row_version
) VALUES (
    gen_random_uuid(), $1::uuid, 'preventive_care', 'vaccination', $2, 'vaccination', 'sop_submission',
    gen_random_uuid(), '["proof-seed"]'::jsonb, $3, $4, NULL, $5::uuid, $6, $7, $8,
    'test-' || gen_random_uuid()::text, 1
) RETURNING item_id::text`,
		tenant, category, status, verdictReason, parkID, openedAt, actorID, verdictAt,
	).Scan(&itemID); err != nil {
		t.Fatalf("insert verification_items: %v", err)
	}

	insertEvent := func(eventType string, at time.Time, payload string) {
		if _, err := pool.Exec(ctx, `
INSERT INTO verification_review_events (
    tenant_id, item_id, actor_id, session_id, event_type, occurred_at, payload, client_event_id
) VALUES ($1::uuid, $2::uuid, $3::uuid, 'sess-test', $4, $5, $6::jsonb, gen_random_uuid())`,
			tenant, itemID, actorID, eventType, at, payload); err != nil {
			t.Fatalf("insert review event %s: %v", eventType, err)
		}
	}
	insertEvent("item_opened", openedAt, `{}`)
	insertEvent("video_play", openedAt, fmt.Sprintf(`{"video_position_ms":0,"video_duration_ms":%d}`, proofDurationMs))
	insertEvent("video_ended", openedAt.Add(time.Duration(watchedMs)*time.Millisecond), fmt.Sprintf(`{"video_position_ms":%d}`, watchedMs))
	insertEvent("verdict_recorded", verdictAt, `{}`)
}

type integrityRow struct {
	videosReviewed        int64
	medianTimeToVerdict   *float64
	p90TimeToVerdict      *float64
	medianWatchFraction   *float64
	belowThresholdCount   int64
	missingTelemetryCount int64
	rejectedCount         int64
	rejectRate            *float64
	rejectReasonBreakdown string
}

func queryIntegrity(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenant, actorID string) integrityRow {
	t.Helper()
	var row integrityRow
	err := pool.QueryRow(ctx, `
SELECT videos_reviewed, median_time_to_verdict_seconds, p90_time_to_verdict_seconds,
       median_watch_fraction, below_watch_threshold_count, missing_review_telemetry_count,
       rejected_count, reject_rate, reject_reason_breakdown::text
FROM ceo_ai.verifier_review_integrity
WHERE tenant_id = $1::uuid AND verifier_id = $2::uuid`, tenant, actorID).Scan(
		&row.videosReviewed, &row.medianTimeToVerdict, &row.p90TimeToVerdict,
		&row.medianWatchFraction, &row.belowThresholdCount, &row.missingTelemetryCount,
		&row.rejectedCount, &row.rejectRate, &row.rejectReasonBreakdown,
	)
	if err != nil {
		t.Fatalf("query ceo_ai.verifier_review_integrity: %v", err)
	}
	return row
}

// TestVerifierReviewIntegrityIsWholeFilterNotPageLocal seeds 25 decided items (more than the
// verifier queue's 20-row default page size) for ONE (verifier, park, category, business_day) key
// and asserts videos_reviewed reports the full 25, proving the aggregate is whole-filter rather
// than scoped to a page.
func TestVerifierReviewIntegrityIsWholeFilterNotPageLocal_RealPostgres(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool, tenant := newDB(t, ctx)
	defer pool.Close()
	parkID := park(t, ctx, pool, tenant, "Whole Filter Park")
	actorID := custodian(t, ctx, pool, tenant) // any party row works as a stand-in verifier id

	const pageSize = 20
	const seedCount = 25
	now := time.Now()
	for i := 0; i < seedCount; i++ {
		opened := now.Add(-time.Duration(seedCount-i) * time.Minute)
		verdict := opened.Add(10 * time.Second)
		seedDecidedItem(t, ctx, pool, tenant, parkID, actorID, "vaccination_proof",
			"approved", nil, opened, verdict, 3000, 3000)
	}

	row := queryIntegrity(t, ctx, pool, tenant, actorID)
	if row.videosReviewed <= pageSize {
		t.Fatalf("videos_reviewed = %d, want > %d (whole-filter, not page-local)", row.videosReviewed, pageSize)
	}
	if row.videosReviewed != seedCount {
		t.Fatalf("videos_reviewed = %d, want exactly %d", row.videosReviewed, seedCount)
	}
}

// TestVerifierReviewIntegrityBelowThresholdCount seeds a known mix of fully-watched and skimmed
// verdicts and asserts the below-threshold integrity count matches exactly.
func TestVerifierReviewIntegrityBelowThresholdCount_RealPostgres(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool, tenant := newDB(t, ctx)
	defer pool.Close()
	parkID := park(t, ctx, pool, tenant, "Threshold Park")
	actorID := custodian(t, ctx, pool, tenant)
	now := time.Now()

	// 3 watched-full (3000/3000 = 1.0), 2 skimmed (900/3000 = 0.3, below the 0.9 threshold).
	for i := 0; i < 3; i++ {
		opened := now.Add(-time.Duration(10+i) * time.Minute)
		seedDecidedItem(t, ctx, pool, tenant, parkID, actorID, "vaccination_proof",
			"approved", nil, opened, opened.Add(5*time.Second), 3000, 3000)
	}
	for i := 0; i < 2; i++ {
		opened := now.Add(-time.Duration(20+i) * time.Minute)
		seedDecidedItem(t, ctx, pool, tenant, parkID, actorID, "vaccination_proof",
			"approved", nil, opened, opened.Add(5*time.Second), 3000, 900)
	}

	row := queryIntegrity(t, ctx, pool, tenant, actorID)
	if row.videosReviewed != 5 {
		t.Fatalf("videos_reviewed = %d, want 5", row.videosReviewed)
	}
	if row.belowThresholdCount != 2 {
		t.Fatalf("below_watch_threshold_count = %d, want 2 (the two 30%%-watched verdicts)", row.belowThresholdCount)
	}
	if row.missingTelemetryCount != 0 {
		t.Fatalf("missing_review_telemetry_count = %d, want 0 (every seeded item sent telemetry)", row.missingTelemetryCount)
	}
}

// TestVerifierReviewIntegrityMedianAndP90 seeds a known time-to-verdict distribution
// (6..30 seconds in 1-second steps, 25 values) and asserts the median (18s) and p90 (27.6s)
// match the hand-computed percentile_cont values for that exact distribution.
func TestVerifierReviewIntegrityMedianAndP90_RealPostgres(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool, tenant := newDB(t, ctx)
	defer pool.Close()
	parkID := park(t, ctx, pool, tenant, "Percentile Park")
	actorID := custodian(t, ctx, pool, tenant)
	now := time.Now()

	for i := 1; i <= 25; i++ {
		opened := now.Add(-time.Duration(60-i) * time.Minute)
		verdict := opened.Add(time.Duration(5+i) * time.Second) // time-to-verdict = 6..30 seconds
		seedDecidedItem(t, ctx, pool, tenant, parkID, actorID, "vaccination_proof",
			"approved", nil, opened, verdict, 3000, 3000)
	}

	row := queryIntegrity(t, ctx, pool, tenant, actorID)
	if row.medianTimeToVerdict == nil || *row.medianTimeToVerdict < 17.9 || *row.medianTimeToVerdict > 18.1 {
		t.Fatalf("median_time_to_verdict_seconds = %v, want ~18 (median of 6..30)", row.medianTimeToVerdict)
	}
	if row.p90TimeToVerdict == nil || *row.p90TimeToVerdict < 27.5 || *row.p90TimeToVerdict > 27.7 {
		t.Fatalf("p90_time_to_verdict_seconds = %v, want ~27.6 (percentile_cont(0.9) of 6..30)", row.p90TimeToVerdict)
	}
}

// TestVerifierReviewIntegrityGroupingDoesNotFanOut seeds items across TWO distinct
// (park, category, business_day) keys for the same verifier and asserts each verdict is counted
// exactly once in its own group -- a join fan-out bug would double a group's videos_reviewed or
// leak counts across park/category boundaries.
func TestVerifierReviewIntegrityGroupingDoesNotFanOut_RealPostgres(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool, tenant := newDB(t, ctx)
	defer pool.Close()
	parkA := park(t, ctx, pool, tenant, "Fan-out Park A")
	parkB := park(t, ctx, pool, tenant, "Fan-out Park B")
	actorID := custodian(t, ctx, pool, tenant)
	now := time.Now()

	for i := 0; i < 4; i++ {
		opened := now.Add(-time.Duration(10+i) * time.Minute)
		seedDecidedItem(t, ctx, pool, tenant, parkA, actorID, "vaccination_proof",
			"approved", nil, opened, opened.Add(5*time.Second), 3000, 3000)
	}
	for i := 0; i < 7; i++ {
		opened := now.Add(-time.Duration(30+i) * time.Minute)
		seedDecidedItem(t, ctx, pool, tenant, parkB, actorID, "vaccination_proof",
			"approved", nil, opened, opened.Add(5*time.Second), 3000, 3000)
	}

	rows, err := pool.Query(ctx, `
SELECT park_id::text, videos_reviewed
FROM ceo_ai.verifier_review_integrity
WHERE tenant_id = $1::uuid AND verifier_id = $2::uuid
ORDER BY park_id`, tenant, actorID)
	if err != nil {
		t.Fatalf("query grouped rows: %v", err)
	}
	defer rows.Close()
	counts := map[string]int64{}
	for rows.Next() {
		var parkID string
		var count int64
		if err := rows.Scan(&parkID, &count); err != nil {
			t.Fatalf("scan: %v", err)
		}
		counts[parkID] = count
	}
	if len(counts) != 2 {
		t.Fatalf("expected 2 distinct park groups, got %d: %v", len(counts), counts)
	}
	if counts[parkA] != 4 {
		t.Fatalf("park A videos_reviewed = %d, want 4 (no fan-out into park B)", counts[parkA])
	}
	if counts[parkB] != 7 {
		t.Fatalf("park B videos_reviewed = %d, want 7 (no fan-out into park A)", counts[parkB])
	}
}
