package postgres

import (
	"context"
	"testing"
)

// TestDistributionFrozenSheetTotalQueryPlanUsesIndexesAtScale is the at-scale plan proof (PP-22) for
// distributionFrozenSheetTotalSQL -- the plan the distribution verifier's total-feed reading is
// warned against (maintainer decision 2026-09-28). It runs on every approve of a distribution item
// whose pen-session has no usable packing snapshot, so it must stay a bounded read of ONE
// pen-session's sheet rows however many sheets the tenant has issued.
//
// Same ~525k-row fixture as the feed analytics proof (2 parks x 365 days x a 110-pen normal and a
// 10-pen experiment sheet, 2 sessions x 3 items per pen), ANALYZEd, EXPLAIN (ANALYZE)'d with the
// exact production arguments WITHOUT enable_seqscan=off. It may not Seq Scan
// feed_direction_issue_rows or touch more than a bounded slice of it, and it must answer the real
// pen-session total (3 items x 1.5 kg = 4.5 kg), not an empty sum that would prove nothing.
func TestDistributionFrozenSheetTotalQueryPlanUsesIndexesAtScale(t *testing.T) {
	ctx := context.Background()
	_, pool := setupIssueDB(t, ctx)
	const park2 = "fd100000-0000-4000-8000-000000003002"
	seedFeedSheetsAtScale(t, ctx, pool, park2)

	// One real pen-session from the fixture, addressed exactly as a distribution completion is:
	// (park, feed day, workflow, shed, partition key, session).
	var shedID, partitionKey string
	if err := pool.QueryRow(ctx, `
SELECT r.shed_id::text, r.partition_key
FROM feed_direction_issues i
JOIN feed_direction_issue_rows r
  ON r.tenant_id = i.tenant_id AND r.feed_direction_issue_id = i.feed_direction_issue_id
WHERE i.tenant_id = $1::uuid AND i.park_id = $2::uuid AND i.feed_day = DATE '2026-07-15'
  AND i.workflow = 'normal' AND r.session_no = 1 AND r.shed_label = 'Pen 42'
LIMIT 1`, fdiTenant, fdiPark).Scan(&shedID, &partitionKey); err != nil {
		t.Fatalf("pick a pen-session: %v", err)
	}
	args := []any{fdiTenant, fdiPark, "2026-07-15", "normal", shedID, partitionKey, int32(1)}

	plan, ms := explainAnalyzeFeed(t, ctx, pool, distributionFrozenSheetTotalSQL, args...)
	if !assertFeedIndexBound(t, "distributionFrozenSheetTotalSQL", plan, ms) {
		logFeedPlanText(t, ctx, pool, distributionFrozenSheetTotalSQL, args...)
	}

	var total float64
	if err := pool.QueryRow(ctx, distributionFrozenSheetTotalSQL, args...).Scan(&total); err != nil {
		t.Fatalf("run the plan query: %v", err)
	}
	if total < 4.5-1e-9 || total > 4.5+1e-9 {
		t.Fatalf("pen-session total = %v, want 4.5 (3 items x 1.5 kg): the plan proves nothing unless it reads the real rows", total)
	}
}
