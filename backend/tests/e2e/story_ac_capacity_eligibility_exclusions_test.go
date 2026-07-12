package e2e

import (
	"testing"
	"time"

	vaccapp "github.com/vgoats/goatos/backend/internal/vaccination/app"
	vaccexecdomain "github.com/vgoats/goatos/backend/internal/vaccinationexecution/domain"
)

// TestKernelStoryAC_CapacityEligibilityExclusions proves the shed capacity COUNT (open_cells, the session
// planner's input, and the capacity_status it drives) is built only from ELIGIBLE, PLANNED vaccination
// cells — never the raw herd headcount. It drives the real ShedSummary read model (shedSummarySQL) over a
// live Postgres, with genuine generated + recheck-deferred + recovery-reopened obligations, and asserts
// five distinct rules the CEO capacity view depends on:
//
//  1. a sick goat's cells are excluded from today's capacity count
//  2. a second clinically held goat's cells are excluded from today's capacity count
//  3. a medically held goat generates follow-up obligations after hold release (they re-enter the count)
//  4. one goat needing FMD + HS counts as 2 vaccination cells (capacity is vaccination-level, not animal-level)
//  5. capacity_status is computed from eligible planned cells, not total herd size
//  6. the merged shed headline can stay overdue while capacity_status is capacity_breach
//
// Three goats share ONE shed; every goat is on a 2-dose (FMD + HS) version, so each contributes 2 cells.
func TestKernelStoryAC_CapacityEligibilityExclusions(t *testing.T) {
	fx := NewFixture(t)
	story := NewStory(t, "story-ac", "Shed capacity count uses eligible planned cells, not herd size",
		"Three goats share a shed; each is due FMD + HS (2 vaccination cells per goat). The shed capacity "+
			"count must total eligible planned cells only: two sick goats drop out of "+
			"today's count when their doses defer, a recovered goat's doses come back after the hold is "+
			"released, and one goat needing two vaccines counts as two cells. capacity_status follows those "+
			"cells — never the raw headcount — while the merged shed headline still surfaces overdue work.")
	defer story.Finish()
	story.Certify("backend kernel")

	// Small tenant cap so capacity_status is sensitive to the exact cell count: 2 vaccinations/day, 1 buffer
	// day => a 2-day safe window (sessions <= 1 -> within_cap; <= 2 -> over_cap/split; > 2 -> needs review).
	fx.exec("tight capacity for a sensitive classification",
		`UPDATE vaccination_capacity_config
		   SET max_per_day = 2, max_buffer_days = 1, capacity_scope = 'tenant',
		       overflow_policy = 'split_within_safe_window_then_mark_needs_review',
		       row_version = row_version + 1, updated_at = now()
		 WHERE tenant_id = $1`, fxTenant)

	const (
		shedID  = "ac000000-0000-4000-8000-000000000001"
		stageID = "ac000000-0000-4000-8000-00000000000a"
		gFMDHS  = "ac000000-0000-4000-8000-000000000010" // healthy throughout — the always-counted goat
		gSick   = "ac000000-0000-4000-8000-000000000011" // sick -> deferred -> recovered -> reopened
		gHeld   = "ac000000-0000-4000-8000-000000000012" // second clinical hold -> deferred (stays out)
	)
	fx.SeedShed(shedID, "E2E-AC", stageID)

	// FMD and HS are INDEPENDENT vaccines (separate protocols), not two doses of one sequenced course —
	// so both are open at once and a goat needing both carries 2 cells. Each protocol's single dose is
	// birth-age offset 0 (due at birth), with defer_states covering the clinical holds.
	const deferDSL = `{"eligibility":{"defer_states":["sick","quarantine","icu","under_treatment"]}}`
	versionFMD, _ := fx.PublishScheduleProtocol("vaccination.e2e.story_ac.fmd", deferDSL,
		[]RuleSpec{{DoseCode: "FMD", Sequence: 1, TriggerType: "birth_age", OffsetDays: 0, DueWindowDays: 7}})
	versionHS, _ := fx.PublishScheduleProtocol("vaccination.e2e.story_ac.hs", deferDSL,
		[]RuleSpec{{DoseCode: "HS", Sequence: 1, TriggerType: "birth_age", OffsetDays: 0, DueWindowDays: 7}})

	// DOB == generation day so the offset-0 dose is due TODAY (a current open dose, not a 50-days-late
	// catch-up that would defer for pc_approval). A due dose is what the capacity planner counts.
	asOf := time.Date(2026, 6, 23, 0, 0, 0, 0, time.UTC)
	dob := asOf
	fx.SeedGoat(GoatSpec{GoatID: gFMDHS, ShedID: shedID, DOB: &dob})
	fx.SeedGoat(GoatSpec{GoatID: gSick, ShedID: shedID, DOB: &dob})
	fx.SeedGoat(GoatSpec{GoatID: gHeld, ShedID: shedID, DOB: &dob})

	gen := vaccapp.NewGenerationService(fx.Proto, fx.Vacc, fx.Obl)

	story.Step("Generate: three goats × two vaccines = six planned cells",
		"Publishing FMD + HS and generating both as of 2026-06-23 gives each of the three goats two overdue "+
			"obligations — six vaccination cells across three animals.")
	fmdRes, errF := gen.GenerateForVersion(fx.Ctx, fxTenant, versionFMD, asOf)
	hsRes, errH := gen.GenerateForVersion(fx.Ctx, fxTenant, versionHS, asOf)
	story.Assert("both generations ran without error", errF == nil && errH == nil, "errF=%v errH=%v", errF, errH)
	story.Assert("six cells generated (3 goats × FMD+HS)", fmdRes.Generated+hsRes.Generated == 6, "fmd=%d hs=%d", fmdRes.Generated, hsRes.Generated)
	fmdhsCells := fx.countRows(`SELECT COUNT(*) FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2`, fxTenant, gFMDHS)
	story.Assert("case 4: one goat needing FMD + HS carries 2 obligation cells", fmdhsCells == 2, "gFMDHS cells=%d", fmdhsCells)

	// ---- Case 4 + 5 (headcount divergence): 6 cells from 3 animals. ----
	story.Step("Capacity count = eligible cells, not headcount",
		"The shed has 3 alive animals but 6 open vaccination cells. open_cells must read 6 (the planner "+
			"input), and capacity_status must classify off those 6 cells: ceil(6 / cap 2) = 3 sessions > the "+
			"2-day window => needs review. A headcount-based count (3 animals) would only be over_cap. "+
			"Because these cells are already late in the as-of read model, the merged shed status stays overdue.")
	base := shedRow(fx, story, shedID, asOf)
	story.Assert("shed has 3 alive animals", base.Animals == 3, "animals=%d", base.Animals)
	story.Assert("case 4/5: open_cells counts vaccinations (6), not animals (3)", base.OpenCells == 6, "open_cells=%d animals=%d", base.OpenCells, base.Animals)
	story.Assert("case 5: capacity_status is driven by the 6 cells (needs review), not the 3 animals (would be over_cap)",
		base.Capacity == vaccexecdomain.CapacityBreach, "capacity=%q sessions=%d", base.Capacity, base.Sessions)
	story.Assert("case 6: merged shed status remains overdue even when capacity needs review",
		base.Status == vaccexecdomain.ShedStatusOverdue, "status=%q capacity=%q", base.Status, base.Capacity)

	// ---- Case 1 + 2: two clinically held goats leave today's count. ----
	story.Step("Case 1 & 2: two clinically held goats defer and drop out of the capacity count",
		"Apply two independent sick transitions through the identity command; each emitted recheck defers that goat's two open doses. "+
			"Both goats' cells must leave open_cells, leaving only gFMDHS's 2 — and capacity_status must fall "+
			"to within_cap (2 cells, one session). With three animals unchanged, this proves the count follows "+
			"eligible cells, not the herd.")
	holdAsOf := asOf // same business day: the dose is still exactly due, so the defer is clinical, not a catch-up

	fx.ChangeGoatHealth(gSick, "sick", "story-ac-sick-1", holdAsOf)
	fx.ChangeGoatHealth(gHeld, "sick", "story-ac-sick-2", holdAsOf)

	sickDeferred := fx.countRows(`SELECT COUNT(*) FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2 AND status='deferred'`, fxTenant, gSick)
	heldDeferred := fx.countRows(`SELECT COUNT(*) FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2 AND status='deferred'`, fxTenant, gHeld)
	story.Assert("gSick both obligations are deferred (SQL)", sickDeferred == 2, "deferred rows=%d", sickDeferred)
	story.Assert("gHeld both obligations are deferred (SQL)", heldDeferred == 2, "deferred rows=%d", heldDeferred)

	held := shedRow(fx, story, shedID, holdAsOf)
	story.Assert("case 1+2: open_cells drops 6 -> 2 (clinically held cells excluded)", held.OpenCells == 2, "open_cells=%d", held.OpenCells)
	story.Assert("headcount is still 3 while the count fell to 2 — exclusion is per-cell, not per-animal", held.Animals == 3, "animals=%d", held.Animals)
	story.Assert("case 5: capacity_status is now within_cap (2 cells, 1 session) — cells, not 3 animals", held.Capacity == vaccexecdomain.CapacityWithinCap, "capacity=%q sessions=%d", held.Capacity, held.Sessions)

	// ---- Case 3: hold release regenerates the follow-up doses and they re-enter the count. ----
	story.Step("Case 3: gSick recovers; its doses reopen and re-enter the capacity count",
		"Flip gSick back to 'healthy' and run the real recovery recheck. Both held doses must reopen "+
			"(follow-up obligations after the hold), realigned onto the recovery calendar. Read the shed "+
			"as of just past the realigned due date: open_cells must climb back to 4 (gFMDHS 2 + gSick 2). "+
			"gHeld stays clinically held, so its cells remain excluded — recovery is per-goat.")
	recoverAsOf := holdAsOf.AddDate(0, 0, 3)
	fx.ChangeGoatHealth(gSick, "healthy", "story-ac-recover", recoverAsOf)
	sickReopened := fx.countRows(`SELECT COUNT(*) FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2 AND status='scheduled'`, fxTenant, gSick)
	story.Assert("gSick both obligations are scheduled again (SQL)", sickReopened == 2, "scheduled rows=%d", sickReopened)

	// Read the shed as of just past gSick's realigned due date so the reopened follow-up doses are countable.
	realignedDue := fx.scanTime(`SELECT MAX(due_at) FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2`, fxTenant, gSick)
	postRecovery := shedRow(fx, story, shedID, realignedDue.AddDate(0, 0, 1))
	story.Assert("case 3: recovered follow-up doses re-enter open_cells -> 4 (gFMDHS 2 + gSick 2)", postRecovery.OpenCells == 4, "open_cells=%d", postRecovery.OpenCells)
	heldStillDeferred := fx.countRows(`SELECT COUNT(*) FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2 AND status='deferred'`, fxTenant, gHeld)
	story.Assert("gHeld stays clinically held/excluded — recovery is per-goat", heldStillDeferred == 2, "gHeld deferred=%d", heldStillDeferred)
}

// shedRow reads exactly one shed's ShedSummary projection (open_cells + capacity classification) from the
// real read model as of asOf, failing the story if the shed is not found. It targets the repository
// (not the app Service) so the story exercises the capacity SQL without the cross-module ownership read.
func shedRow(fx *Fixture, story *Story, shedID string, asOf time.Time) vaccexecdomain.ShedSummaryProjection {
	fx.T.Helper()
	id := shedID
	rows, err := fx.VaccExec.ShedSummary(fx.Ctx, vaccexecdomain.ShedSummaryQuery{
		TenantID: fxTenant, ShedID: &id, AsOf: asOf,
	})
	story.Assert("shed summary read without error", err == nil, "err=%v", err)
	if len(rows) != 1 {
		fx.T.Fatalf("expected exactly one shed row for %s, got %d", shedID, len(rows))
	}
	return rows[0]
}
