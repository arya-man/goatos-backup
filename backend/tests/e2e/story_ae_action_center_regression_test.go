package e2e

import (
	"testing"
	"time"

	pidomain "github.com/vgoats/goatos/backend/internal/processintegrity/domain"
)

// TestKernelStoryAE_ActionCenterRegression proves the July 2026 vaccination Action Center fixes at the
// read-model boundary: same-day unbatched rows group into one board row, and old completed history stays
// out of the hot projection while the next live cycle remains visible.
func TestKernelStoryAE_ActionCenterRegression(t *testing.T) {
	fx := NewFixture(t)
	story := NewStory(t, "story-ae", "Action Center groups same-day work and bounds completed history",
		"Two unbatched goat obligations in the same shed are due at different clock times but on the same "+
			"Asia/Kolkata business day. The Action Center must show one shed/day board row, not one row per "+
			"timestamp. The same story also proves the hot projection excludes old completed history while "+
			"keeping the future next-cycle row visible.")
	defer story.Finish()

	const (
		shedID        = "ae000000-0000-4000-8000-000000000001"
		stageID       = "ae000000-0000-4000-8000-000000000002"
		operatorID    = "ae000000-0000-4000-8000-000000000003"
		parkHeadID    = "ae000000-0000-4000-8000-000000000004"
		verifierID    = "ae000000-0000-4000-8000-000000000005"
		goatEarlyID   = "ae000000-0000-4000-8000-000000000010"
		goatLaterID   = "ae000000-0000-4000-8000-000000000011"
		oblEarlyID    = "ae000000-0000-4000-8000-000000000020"
		oblLaterID    = "ae000000-0000-4000-8000-000000000021"
		historyOblID  = "ae000000-0000-4000-8000-000000000030"
		historyCompID = "ae000000-0000-4000-8000-000000000031"
		nextOblID     = "ae000000-0000-4000-8000-000000000032"
	)

	fx.SeedShed(shedID, "E2E-AE", stageID)
	fx.SeedWorkforce(operatorID, parkHeadID, verifierID, shedID)
	dob := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	fx.SeedGoat(GoatSpec{GoatID: goatEarlyID, ShedID: shedID, DOB: &dob})
	fx.SeedGoat(GoatSpec{GoatID: goatLaterID, ShedID: shedID, DOB: &dob})
	versionID, ruleID := fx.PublishSimpleProtocol("vaccination.e2e.story_ae", 21, 14, nil)

	story.Step("Seed two unbatched obligations on one IST business day",
		"The obligations are intentionally not batch-attached and use different raw due_at instants. "+
			"Both instants fall on 2026-07-11 in Asia/Kolkata.")
	fx.exec("AE early same-day obligation",
		`INSERT INTO obligation_instances (obligation_id, tenant_id, protocol_version_id, rule_id,
		   target_type, target_id, scope_type, scope_id, due_at, status, idempotency_key, sequence)
		 VALUES ($1, $2, $3, $4, 'goat', $5, 'shed', $6, TIMESTAMPTZ '2026-07-10 19:15:00+00',
		   'scheduled', 'story-ae-same-day-early', 1)`,
		oblEarlyID, fxTenant, versionID, ruleID, goatEarlyID, shedID)
	fx.exec("AE later same-day obligation",
		`INSERT INTO obligation_instances (obligation_id, tenant_id, protocol_version_id, rule_id,
		   target_type, target_id, scope_type, scope_id, due_at, status, idempotency_key, sequence)
		 VALUES ($1, $2, $3, $4, 'goat', $5, 'shed', $6, TIMESTAMPTZ '2026-07-11 12:00:00+00',
		   'scheduled', 'story-ae-same-day-later', 1)`,
		oblLaterID, fxTenant, versionID, ruleID, goatLaterID, shedID)

	category := pidomain.CategoryVaccination
	dueAfter := time.Date(2026, 7, 10, 18, 0, 0, 0, time.UTC)
	board, err := fx.PI.ListRows(fx.Ctx, pidomain.Query{
		TenantID:  fxTenant,
		Category:  &category,
		DueAfter:  &dueAfter,
		AsOf:      time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC),
		DueBefore: time.Date(2026, 7, 12, 0, 0, 0, 0, time.UTC),
		Limit:     10,
	})
	story.Assert("Action Center query ran without error", err == nil, "err=%v", err)
	if err == nil {
		story.Assert("same business-day rows group into one board row", len(board.Rows) == 1, "rows=%d", len(board.Rows))
		if len(board.Rows) == 1 {
			story.Assert("grouped row carries both goats", board.Rows[0].ExpectedCount == 2, "expected_count=%d", board.Rows[0].ExpectedCount)
			story.Assert("grouped row keeps the earliest due instant as its sort due",
				board.Rows[0].DueAt.Equal(time.Date(2026, 7, 10, 19, 15, 0, 0, time.UTC)),
				"due_at=%s", board.Rows[0].DueAt.Format(time.RFC3339))
		}
	}

	story.Step("Seed old completed history plus a future next-cycle obligation",
		"The projector should not carry old completed history into the hot Action Center projection, but "+
			"the future next-cycle obligation must still be visible.")
	fx.exec("AE old completed history obligation",
		`INSERT INTO obligation_instances (obligation_id, tenant_id, protocol_version_id, rule_id,
		   target_type, target_id, scope_type, scope_id, due_at, status, completed_at, idempotency_key, sequence)
		 VALUES ($1, $2, $3, $4, 'goat', $5, 'shed', $6, TIMESTAMPTZ '2026-02-21 03:30:00+00',
		   'completed', TIMESTAMPTZ '2026-02-21 03:30:00+00', 'story-ae-history-obl', 50)`,
		historyOblID, fxTenant, versionID, ruleID, goatEarlyID, shedID)
	fx.exec("AE old accepted completion",
		`INSERT INTO vaccination_completions (completion_id, tenant_id, obligation_id, goat_id, administered_at, status, idempotency_key, recorded_by)
		 VALUES ($1, $2, $3, $4, TIMESTAMPTZ '2026-02-21 03:30:00+00', 'accepted', 'story-ae-history-comp', $5)`,
		historyCompID, fxTenant, historyOblID, goatEarlyID, operatorID)
	fx.exec("AE next-cycle obligation",
		`INSERT INTO obligation_instances (obligation_id, tenant_id, protocol_version_id, rule_id,
		   target_type, target_id, scope_type, scope_id, due_at, status, idempotency_key, sequence)
		 VALUES ($1, $2, $3, $4, 'goat', $5, 'shed', $6, TIMESTAMPTZ '2026-12-20 03:30:00+00',
		   'scheduled', 'story-ae-next-obl', 51)`,
		nextOblID, fxTenant, versionID, ruleID, goatEarlyID, shedID)

	asOf := time.Date(2026, 7, 11, 11, 30, 0, 0, time.UTC)
	recomputed, err := fx.PI.RecomputeProjection(fx.Ctx, pidomain.ProjectionRecomputeRequest{TenantID: fxTenant, AsOf: asOf})
	story.Assert("projection recompute ran without error", err == nil, "err=%v", err)
	story.Assert("projection remains small and bounded for this fixture", err == nil && recomputed.Rows > 0 && recomputed.Rows < 10,
		"rows=%d", recomputed.Rows)
	projectedHistory := fx.countRows(`
SELECT count(*)
FROM process_integrity_projection_rows
WHERE tenant_id=$1 AND obligation_id=$2`, fxTenant, historyOblID)
	story.Assert("old completed history is not stored in the hot projection", projectedHistory == 0, "projected_history_rows=%d", projectedHistory)

	nextBoard, err := fx.PI.ListRows(fx.Ctx, pidomain.Query{
		TenantID:  fxTenant,
		Category:  &category,
		AsOf:      asOf,
		DueBefore: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC),
		Limit:     20,
	})
	story.Assert("projected Action Center read ran without error", err == nil, "err=%v", err)
	if err == nil {
		story.Assert("old completed history is absent from the default board", rowWithObligation(nextBoard.Rows, historyOblID) == nil, "rows=%d", len(nextBoard.Rows))
		story.Assert("future next-cycle row remains visible", rowWithObligation(nextBoard.Rows, nextOblID) != nil, "rows=%d", len(nextBoard.Rows))
	}
}

func rowWithObligation(rows []pidomain.Row, obligationID string) *pidomain.Row {
	for i := range rows {
		if rows[i].ObligationID == obligationID {
			return &rows[i]
		}
	}
	return nil
}
