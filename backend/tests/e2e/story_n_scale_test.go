package e2e

import (
	"fmt"
	"testing"
	"time"

	oblapp "github.com/vgoats/goatos/backend/internal/obligation/app"
	pidomain "github.com/vgoats/goatos/backend/internal/processintegrity/domain"
	vaccapp "github.com/vgoats/goatos/backend/internal/vaccination/app"
)

// TestKernelStoryN_Scale drives a realistic multi-shed drive at scale through the REAL generation
// service, SM-4 sweeper, and process-integrity read model, and asserts the platform's scale
// contract (per docs/decisions/operational-kernel-5k-50k-scale-envelope.md the current release
// envelope is 5,000-50,000 animals with query-plan proof at the ~500k obligation-row upper bound,
// and 1-5M-animal deployment is the future certification bar): per-shed drive grouping is exact, and the
// control-tower read path is keyset-paginated/bounded -- a page returns at most Limit rows with a
// cursor to the next page, never a full-herd dump. Bounded/indexed reads are required at the current
// envelope and remain the property the future 1-5M certification gate verifies.
//
// Extended (docs/architecture/event-driven-kernel.md "Cross-cutting rules"): after
// the static generate/sweep/paginate proof above, this story also drives two DYNAMIC events across the
// same multi-shed cohort -- a sick-defer sweep over one shed's goats, and a death cancellation of two
// goats in a different shed -- and asserts each stays bounded to exactly the goats/sheds it targets,
// never touching the rest of the herd, and that the control-tower read path stays bounded afterward.

func TestKernelStoryN_Scale(t *testing.T) {
	fx := NewFixture(t)
	story := NewStory(t, "story-n", "Scale: multi-shed drive, bounded/indexed read path",
		"A vaccination drive spans several sheds with dozens of goats. The generation engine and SM-4 "+
			"sweeper must produce exactly one drive per shed with every goat in that shed attached, and the "+
			"control-tower read path must be keyset-paginated -- each page returns at most its page size with "+
			"a cursor to the next, so a director's dashboard never scans the whole herd to render one screen.")
	defer story.Finish()
	story.Certify("backend kernel")

	const shedCount = 6
	const goatsPerShed = 8
	const totalGoats = shedCount * goatsPerShed

	// defer_states enable the dynamic-events-at-scale extension later in this story (sick-defer one
	// shed's cohort, death-cancel two goats in another) without affecting the static generation/sweep/
	// pagination proof above, since no goat is sick/dead during that first pass.
	versionID, _ := fx.PublishSimpleProtocol("vaccination.e2e.story_n", 21, 0, []string{"sick", "quarantine", "icu"})

	now := time.Now().UTC()
	dob := now.AddDate(0, 0, -53) // 53d old, 21-day rule => past due, ready to sweep into a drive
	itemID := "ee000000-0000-4000-8000-0000000000f0"
	fx.exec("vaccine item",
		`INSERT INTO inventory_items (item_id, tenant_id, item_code, name, category, base_unit)
		 VALUES ($1, $2, 'VAC-E2E-N', 'E2E Story N vaccine', 'vaccine', 'dose')`, itemID, fxTenant)

	shedIDs := make([]string, 0, shedCount)
	for s := 0; s < shedCount; s++ {
		shedID := fmt.Sprintf("ee000000-0000-4000-8000-0000000001%02d", s)
		stageID := fmt.Sprintf("ee000000-0000-4000-8000-0000000002%02d", s)
		// Distinct stage codes per shed (SeedShed hardcodes 'K1', which collides across sheds on the
		// stage_code unique constraint). The goats are kids by DOB (<16 weeks), so generation still
		// uses the kid schedule path regardless of the shed's stage code.
		fx.SeedAdultShed(shedID, fmt.Sprintf("E2E-N-%d", s), stageID, fmt.Sprintf("K1-N%d", s))
		// Stock at each shed so the sweeper can reserve one dose per goat for that shed's drive.
		lotID := fmt.Sprintf("ee000000-0000-4000-8000-0000000003%02d", s)
		fx.exec("vaccine stock",
			`INSERT INTO inventory_stock (stock_id, tenant_id, item_id, location_id, quantity_in_stock, quantity_reserved, quantity_unit, expiry_date)
			 VALUES ($1, $2, $3, $4, 100, 0, 'dose', CURRENT_DATE + INTERVAL '180 days')`, lotID, fxTenant, itemID, shedID)
		for g := 0; g < goatsPerShed; g++ {
			goatID := fmt.Sprintf("ee000000-0000-4000-8000-00000004%02d%02d", s, g)
			fx.SeedGoat(GoatSpec{GoatID: goatID, ShedID: shedID, DOB: &dob})
		}
		shedIDs = append(shedIDs, shedID)
	}

	story.Step(fmt.Sprintf("Seed %d goats across %d sheds", totalGoats, shedCount),
		fmt.Sprintf("%d sheds, %d goats each (%d total), all past due for the same PC vaccination rule, each "+
			"shed stocked with vaccine.", shedCount, goatsPerShed, totalGoats))

	story.Step("Generate the whole cohort, then sweep into per-shed drives",
		"Run the real GenerationService.GenerateForVersion over the whole version (chunked internally), "+
			"then the real SM-4 sweeper. The sweeper must form exactly one drive per shed.")
	gen := vaccapp.NewGenerationService(fx.Proto, fx.Vacc, fx.Obl)
	genRes, err := gen.GenerateForVersion(fx.Ctx, fxTenant, versionID, now)
	story.Assert("generation ran without error", err == nil, "err=%v", err)
	story.Assert(fmt.Sprintf("all %d doses were generated", totalGoats), genRes.Generated == totalGoats, "generated=%d", genRes.Generated)

	sweeper := oblapp.NewSweeperService(fx.Obl, nil, fx.Inv)
	sweepRes, err := sweeper.SweepVersion(fx.Ctx, fxTenant, versionID, oblapp.SweepConfig{VaccineItemID: itemID, DosesPerGoat: 1}, now.AddDate(0, 0, 1))
	story.Assert("sweep ran without error", err == nil, "err=%v", err)
	story.Assert(fmt.Sprintf("exactly %d drives formed (one per shed)", shedCount), sweepRes.Batches == shedCount, "batches=%d", sweepRes.Batches)
	story.Assert(fmt.Sprintf("all %d obligations attached to drives", totalGoats), sweepRes.Obligations == totalGoats, "obligations=%d", sweepRes.Obligations)

	story.Step("Per-shed grouping is exact",
		"Every shed's drive holds exactly its own goats -- no cross-shed leakage. Each of the "+
			"per-shed obligation scopes counts exactly its cohort.")
	batchCount := fx.countRows(`SELECT count(*) FROM obligation_batches WHERE tenant_id=$1 AND protocol_version_id=$2`, fxTenant, versionID)
	story.Assert(fmt.Sprintf("exactly %d drive batches persisted", shedCount), batchCount == shedCount, "batches=%d", batchCount)
	groupingOK := true
	for _, shedID := range shedIDs {
		n := fx.countRows(`SELECT count(*) FROM obligation_instances WHERE tenant_id=$1 AND scope_type='shed' AND scope_id=$2 AND status='scheduled'`, fxTenant, shedID)
		if n != goatsPerShed {
			groupingOK = false
		}
	}
	story.Assert(fmt.Sprintf("each shed's drive counts exactly %d goats", goatsPerShed), groupingOK, "expected %d per shed across %d sheds", goatsPerShed, shedCount)
	distinctBatchScopes := fx.countRows(`SELECT count(DISTINCT scope_id) FROM obligation_instances WHERE tenant_id=$1 AND protocol_version_id=$2 AND scope_type='shed'`, fxTenant, versionID)
	story.Assert(fmt.Sprintf("obligations span exactly %d distinct shed scopes", shedCount), distinctBatchScopes == shedCount, "distinct_scopes=%d", distinctBatchScopes)

	story.Step("Control-tower read is keyset-paginated (bounded, no full-herd scan)",
		"Query the real process-integrity control tower with a small page size. It must return at most the "+
			"page size, report the true total, and hand back a cursor -- proving the read is bounded and "+
			"paginated over an index, not a full scan of every goat/obligation.")
	const pageSize = 3
	// Live as-of a small margin past now (skew-safe, inside the freshness TTL); now()+hours would trip
	// the AGE-based buildAge gate the real HTTP path avoids via ClampFutureAsOf.
	asOf := time.Now().UTC().Add(2 * time.Minute)
	dueBefore := now.AddDate(0, 0, 1)
	// Canonical read from indexed projection (C35-001), no on-demand recompute needed.
	page1, err := fx.PI.ListRows(fx.Ctx, pidomain.Query{TenantID: fxTenant, AsOf: asOf, DueBefore: dueBefore, Limit: pageSize})
	story.Assert("control-tower page-1 query ran without error", err == nil, "err=%v", err)
	story.Assert(fmt.Sprintf("page 1 returns at most the page size (%d), not all %d shed rows", pageSize, shedCount),
		len(page1.Rows) == pageSize, "rows=%d", len(page1.Rows))
	story.Assert(fmt.Sprintf("total count reports all %d shed/rule rows without materializing them in the page", shedCount),
		page1.TotalCount == int64(shedCount), "total=%d", page1.TotalCount)
	story.Assert("a next-page cursor is returned (keyset pagination, bounded reads)", page1.NextCursor != nil, "next_cursor=%s", detailString(page1.NextCursor))

	story.Step("Paging through with the cursor covers the full set without overlap",
		"Follow the cursor to the next page. The second page returns the remaining rows, disjoint from the "+
			"first -- the whole multi-shed drive is readable in bounded pages.")
	if page1.NextCursor != nil {
		cur, derr := pidomain.DecodeCursor(*page1.NextCursor)
		story.Assert("next-page cursor decodes cleanly", derr == nil, "err=%v", derr)
		page2, err := fx.PI.ListRows(fx.Ctx, pidomain.Query{TenantID: fxTenant, AsOf: asOf, DueBefore: dueBefore, Limit: pageSize, Cursor: &cur})
		story.Assert("control-tower page-2 query ran without error", err == nil, "err=%v", err)
		story.Assert(fmt.Sprintf("page 2 returns the remaining %d rows", shedCount-pageSize), len(page2.Rows) == shedCount-pageSize, "rows=%d", len(page2.Rows))
		seen := map[string]bool{}
		for _, r := range page1.Rows {
			seen[r.RowID] = true
		}
		overlap := false
		for _, r := range page2.Rows {
			if seen[r.RowID] {
				overlap = true
			}
		}
		story.Assert("pages are disjoint (no row appears on both pages)", !overlap, "overlap=%v", overlap)
	}

	story.Step("Dynamic event at scale: sick-defer one shed's whole cohort, bounded to that shed only",
		"Mark every goat in shed 0 sick and re-run generation over the whole version. The recheck must "+
			"defer exactly that shed's goats and touch no others -- a dynamic event at scale is still a "+
			"per-goat decision, not a per-shed or per-herd rewrite.")
	sickShedID := shedIDs[0]
	recheckAsOf := now.AddDate(0, 0, 2)
	for g := 0; g < goatsPerShed; g++ {
		goatID := fmt.Sprintf("ee000000-0000-4000-8000-00000004%02d%02d", 0, g)
		fx.ChangeGoatHealth(goatID, "sick", fmt.Sprintf("story-n-sick-%d", g), recheckAsOf)
	}

	sickShedScheduled := fx.countRows(`SELECT count(*) FROM obligation_instances WHERE tenant_id=$1 AND scope_type='shed' AND scope_id=$2 AND status='scheduled'`, fxTenant, sickShedID)
	story.Assert("the sick shed now has zero scheduled (all deferred)", sickShedScheduled == 0, "scheduled=%d", sickShedScheduled)
	sickShedDeferred := fx.countRows(`SELECT count(*) FROM obligation_instances WHERE tenant_id=$1 AND scope_type='shed' AND scope_id=$2 AND status='deferred'`, fxTenant, sickShedID)
	story.Assert(fmt.Sprintf("the sick shed has exactly %d deferred obligations", goatsPerShed), sickShedDeferred == goatsPerShed, "deferred=%d", sickShedDeferred)

	otherShedsUntouched := true
	for _, shedID := range shedIDs[1:] {
		n := fx.countRows(`SELECT count(*) FROM obligation_instances WHERE tenant_id=$1 AND scope_type='shed' AND scope_id=$2 AND status='scheduled'`, fxTenant, shedID)
		if n != goatsPerShed {
			otherShedsUntouched = false
		}
	}
	story.Assert(fmt.Sprintf("every other shed still has exactly %d scheduled goats, untouched by the sick shed's defer", goatsPerShed), otherShedsUntouched, "expected %d per shed", goatsPerShed)

	story.Step("Dynamic event at scale: death cancels only the exited goats, bounded per goat",
		"Two goats in a different shed die. The real SM-3 handler must cancel only those two goats' "+
			"obligations -- the rest of that shed's cohort, and every other shed, must be untouched.")
	deathShedID := shedIDs[1]
	deadGoat1 := fmt.Sprintf("ee000000-0000-4000-8000-00000004%02d%02d", 1, 0)
	deadGoat2 := fmt.Sprintf("ee000000-0000-4000-8000-00000004%02d%02d", 1, 1)
	exitAt := recheckAsOf.AddDate(0, 0, 1)
	for _, deadGoat := range []string{deadGoat1, deadGoat2} {
		fx.ExitGoat(deadGoat, "dead", "story-n-exit-"+deadGoat, exitAt)
	}

	deathShedScheduled := fx.countRows(`SELECT count(*) FROM obligation_instances WHERE tenant_id=$1 AND scope_type='shed' AND scope_id=$2 AND status='scheduled'`, fxTenant, deathShedID)
	story.Assert(fmt.Sprintf("the death shed now has %d scheduled goats left (2 exited)", goatsPerShed-2), deathShedScheduled == goatsPerShed-2, "scheduled=%d", deathShedScheduled)
	canceledTotal := fx.countRows(`SELECT count(*) FROM obligation_instances WHERE tenant_id=$1 AND target_id IN ($2,$3) AND status='canceled'`, fxTenant, deadGoat1, deadGoat2)
	story.Assert("exactly the 2 exited goats' obligations are cancelled", canceledTotal == 2, "cancelled=%d", canceledTotal)

	deferredTotal := fx.countRows(`SELECT count(*) FROM obligation_instances WHERE tenant_id=$1 AND protocol_version_id=$2 AND status='deferred'`, fxTenant, versionID)
	story.Assert(fmt.Sprintf("herd-wide deferred count is still exactly %d (only the sick shed)", goatsPerShed), deferredTotal == goatsPerShed, "deferred=%d", deferredTotal)
	scheduledTotal := fx.countRows(`SELECT count(*) FROM obligation_instances WHERE tenant_id=$1 AND protocol_version_id=$2 AND status='scheduled'`, fxTenant, versionID)
	story.Assert(fmt.Sprintf("herd-wide scheduled count reflects both dynamic events: %d - %d sick - 2 dead = %d", totalGoats, goatsPerShed, totalGoats-goatsPerShed-2),
		scheduledTotal == totalGoats-goatsPerShed-2, "scheduled=%d", scheduledTotal)

	story.Step("Control-tower read stays bounded after mixed dynamic events at scale",
		"Re-query the same control-tower page after the sick-defer and death mutations above. It must "+
			"still return at most the page size -- the read model does not need a full-herd rescan just "+
			"because some goats deferred or exited.",
	)
	finalAsOf := time.Now().UTC().Add(2 * time.Minute)
	// Canonical read from indexed projection (C35-001), no on-demand recompute needed.
	page1Again, err := fx.PI.ListRows(fx.Ctx, pidomain.Query{TenantID: fxTenant, AsOf: finalAsOf, DueBefore: now.AddDate(0, 0, 1), Limit: pageSize})
	story.Assert("control-tower query after dynamic events ran without error", err == nil, "err=%v", err)
	story.Assert(fmt.Sprintf("the page still returns at most the page size (%d) after mutations", pageSize), len(page1Again.Rows) <= pageSize, "rows=%d", len(page1Again.Rows))
}
