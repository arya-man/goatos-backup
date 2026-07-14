package e2e

import (
	"testing"
	"time"

	pidomain "github.com/vgoats/goatos/backend/internal/processintegrity/domain"
	vaccapp "github.com/vgoats/goatos/backend/internal/vaccination/app"
)

// TestKernelStoryAE_ActionCenterRegression proves the July 2026 Action Center behavior through the
// real generation, completion, vaccination.completed, and process-integrity projection paths.
func TestKernelStoryAE_ActionCenterRegression(t *testing.T) {
	fx := NewFixture(t)
	story := NewStory(t, "story-ae", "Action Center groups generated work and keeps the next live cycle",
		"Two goats generate the same shed/day vaccination work through goat.created. One is accepted "+
			"through SM-5; the durable vaccination.completed consumer creates its next 182-day cycle. The "+
			"Action Center removes completed history from the hot board while retaining the future cycle.")
	story.Certify("backend kernel + SOP proof/submission/review + durable outbox envelope + domain consumer")
	defer story.Finish()

	const (
		shedID      = "ae000000-0000-4000-8000-000000000001"
		stageID     = "ae000000-0000-4000-8000-000000000002"
		laterShedID = "ae000000-0000-4000-8000-000000000003"
		goatEarlyID = "ae000000-0000-4000-8000-000000000010"
		goatLaterID = "ae000000-0000-4000-8000-000000000011"
	)

	fx.SeedShed(shedID, "E2E-AE", stageID)
	fx.SeedShed(laterShedID, "E2E-AE-LATER", stageID)
	dob := time.Date(2026, 6, 20, 0, 0, 0, 0, time.UTC)
	fx.SeedGoat(GoatSpec{GoatID: goatEarlyID, ShedID: shedID, DOB: &dob})
	fx.SeedGoat(GoatSpec{GoatID: goatLaterID, ShedID: shedID, DOB: &dob})
	versionID, ruleIDs := fx.PublishScheduleProtocol("vaccination.e2e.story_ae", "{}", []RuleSpec{{
		DoseCode: "adult_revac", Sequence: 1, TriggerType: "birth_age", OffsetDays: 21,
		DueWindowDays: 14, MinGapDays: 182, Repeat: "every_n_days", CatchUp: "next_cycle",
	}})

	due := time.Date(2026, 7, 11, 0, 0, 0, 0, time.UTC)
	story.Step("Generate two same-shed, same-day obligations through goat.created",
		"Both identity events reach SM-1 and materialize one rule-backed obligation per goat for the July 11 business day.")
	fx.PublishGoatEvent(vaccapp.EventGoatCreated, goatEarlyID, due)
	fx.PublishGoatEvent(vaccapp.EventGoatCreated, goatLaterID, due)
	// Canonical read from indexed projection (C35-001), no on-demand recompute needed.

	category := pidomain.CategoryVaccination
	board, err := fx.PI.ListRows(fx.Ctx, pidomain.Query{
		TenantID: fxTenant, Category: &category,
		AsOf: due, DueBefore: due.AddDate(0, 0, 1), Limit: 10,
	})
	story.Assert("Action Center query ran without error", err == nil, "err=%v", err)
	story.Assert("same shed/day work groups into one board row", err == nil && len(board.Rows) == 1, "rows=%d", len(board.Rows))
	if len(board.Rows) == 1 {
		story.Assert("grouped row carries both generated goats", board.Rows[0].ExpectedCount == 2, "expected_count=%d", board.Rows[0].ExpectedCount)
	}
	fx.MoveGoat(goatLaterID, laterShedID, "story-ae-isolate-later-goat", due.Add(time.Hour))

	story.Step("Accept one goat and dispatch its durable completion event",
		"SM-5 writes accepted history and completes the current obligation. SM-7 consumes vaccination.completed and creates exactly one next repeat anchored to administered_at + 182 days.")
	currentObligationID := fx.scanText(`SELECT obligation_id::text FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2 AND rule_id=$3 AND status='scheduled'`,
		fxTenant, goatEarlyID, ruleIDs["adult_revac"])
	administeredAt := due.AddDate(0, 0, 3)
	completeVaccinationObligationThroughSOP(t, fx, versionID, currentObligationID, shedID, []string{goatEarlyID}, administeredAt, "story-ae-current")
	fx.DispatchVaccinationCompleted(currentObligationID)
	nextObligationID := fx.scanText(`SELECT obligation_id::text FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2 AND rule_id=$3 AND status='scheduled'`,
		fxTenant, goatEarlyID, ruleIDs["adult_revac"])
	nextDue := fx.scanTime(`SELECT due_at FROM obligation_instances WHERE tenant_id=$1 AND obligation_id=$2`, fxTenant, nextObligationID)
	wantNextDue := administeredAt.AddDate(0, 0, 182)
	story.Assert("one future repeat was created by SM-7", nextObligationID != currentObligationID, "current=%s next=%s", currentObligationID, nextObligationID)
	story.Assert("late administration re-anchors the next cycle", sameDay(nextDue, wantNextDue), "got=%s want=%s", nextDue.Format("2006-01-02"), wantNextDue.Format("2006-01-02"))

	story.Step("Recompute the production Action Center projection",
		"The real projector excludes the completed obligation from the hot board but keeps the future recurring obligation visible.")
	// Fast-forward beyond the hot completed-row retention horizon while staying before the 182-day
	// repeat. This proves old history is bounded without sleeping or hand-seeding an old row.
	asOf := administeredAt.AddDate(0, 0, 120)
	// Canonical read from indexed projection (C35-001), no on-demand recompute needed.
	// Pinned as-of read: asOf is a fixed simulated instant 120 days ahead of the build's wall clock
	// (proving completed-row retention expiry), so read in explicit as-of mode. The live-freshness
	// buildAge gate (build-vs-now age) does not apply to a pinned instant; correctness is based on
	// the indexed projection serving the exact projection-as_of == query-as_of match.
	nextBoard, err := fx.PI.ListRows(fx.Ctx, pidomain.Query{
		TenantID: fxTenant, Category: &category, AsOf: asOf, HistoricalAsOf: true,
		DueBefore: wantNextDue.AddDate(0, 0, 1), Limit: 20,
	})
	story.Assert("projected Action Center read ran without error", err == nil, "err=%v", err)
	if err == nil {
		story.Assert("completed obligation is absent from the default board", rowWithObligation(nextBoard.Rows, currentObligationID) == nil, "rows=%d", len(nextBoard.Rows))
		story.Assert("future recurring obligation remains visible", rowWithObligation(nextBoard.Rows, nextObligationID) != nil, "rows=%d", len(nextBoard.Rows))
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
