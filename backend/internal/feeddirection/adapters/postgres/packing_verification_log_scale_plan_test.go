package postgres

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

// TestPackingVerificationLogQueryPlanUsesIndexesAtScale is the at-scale plan proof (PP-22) for
// packingVerificationLogSQL -- the FEED VERIFICATION drawer on /verify (maintainer decision
// 2026-09-28). It reads ONE feed day, so however many sheets the tenant has issued it must stay a
// bounded read of that day's rows.
//
// Same ~525k-row fixture as the feed analytics proof (2 parks x 365 days x a 110-pen normal and a
// 10-pen experiment sheet, 2 sessions x 3 items per pen), ANALYZEd, EXPLAIN (ANALYZE)'d with the
// exact production arguments WITHOUT enable_seqscan=off, both TENANT-WIDE (nil park set: how the
// verifier and the CXO call it) and park-scoped. It may not Seq Scan feed_direction_issue_rows or
// touch more than a bounded slice of it, and it must return that day's bags, not an empty set that
// would prove nothing.
func TestPackingVerificationLogQueryPlanUsesIndexesAtScale(t *testing.T) {
	ctx := context.Background()
	_, pool := setupIssueDB(t, ctx)
	const park2 = "fd100000-0000-4000-8000-000000003002"
	seedFeedSheetsAtScale(t, ctx, pool, park2)

	for _, c := range []struct {
		name  string
		parks []uuid.UUID
	}{
		{"tenant-wide", nil},
		{"one park", []uuid.UUID{uuid.MustParse(fdiPark)}},
	} {
		args := []any{fdiTenant, c.parks, "2026-07-15"}
		plan, ms := explainAnalyzeFeed(t, ctx, pool, packingVerificationLogSQL, args...)
		if !assertFeedIndexBound(t, "packingVerificationLogSQL "+c.name, plan, ms) {
			logFeedPlanText(t, ctx, pool, packingVerificationLogSQL, args...)
		}
		if plan.ActualRows == 0 {
			t.Errorf("packingVerificationLogSQL %s returned no rows for a seeded day: the plan proves nothing", c.name)
		}
	}
}
