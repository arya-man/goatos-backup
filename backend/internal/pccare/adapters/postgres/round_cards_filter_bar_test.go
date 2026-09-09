package postgres

import (
	"context"
	"testing"

	"github.com/vgoats/goatos/backend/internal/pccare/domain"
	"github.com/vgoats/goatos/backend/internal/pccare/ports"
)

// The phone's FILTER BAR over the planner's round list (maintainer request 2026-09-10): an
// inclusive due-date window and one pen on top of the Active / Completed tabs. Pins the
// carry rule (open work due before the window stays on the active tab), the whole-filter
// card counts behind the pills, and the status-blind pen vocabulary.
func TestRoundCardsFilterBarStatusMatrixPaginationAndOneToManyPen(t *testing.T) {
	ctx := context.Background()
	repo, _ := setupPCCareDB(t, ctx)
	shedB := "9c000000-0000-4000-8000-000000004081"
	seedRoundCardsPark(t, ctx, repo, shedB, "S-FB", "Gandhi")

	plan := func(t *testing.T, key string, day int, pens []domain.RoundPen) ports.RoundRow {
		t.Helper()
		round, err := repo.CreateRound(ctx, ports.CreateRoundParams{
			TenantID: pcTenant, Category: domain.CategoryDeworming, ParkID: pcPark,
			Pens:                pens,
			PlannedBusinessDate: pcBusinessDay(2026, 9, day),
			AssigneeUserIDs:     []string{pcOperator1},
			IdempotencyKey:      "round-filter-bar-" + key,
			CreatedBy:           pcVerifier, ActorID: pcVerifier,
		})
		if err != nil {
			t.Fatalf("CreateRound %s: %v", key, err)
		}
		return round
	}
	beforeOpen := plan(t, "before-open", 5, []domain.RoundPen{{ShedID: pcShedA}})
	inOpen := plan(t, "in-open", 12, []domain.RoundPen{{ShedID: pcShedA}, {ShedID: shedB}})
	inDone := plan(t, "in-done", 14, []domain.RoundPen{{ShedID: shedB}})
	beforeDone := plan(t, "before-done", 2, []domain.RoundPen{{ShedID: pcShedA}})
	afterOpen := plan(t, "after-open", 25, []domain.RoundPen{{ShedID: pcShedA}})
	for _, done := range []ports.RoundRow{inDone, beforeDone} {
		if _, err := repo.pool.Exec(ctx, `
UPDATE pc_care_tasks SET work_state = 'completed', status = 'completed'
WHERE tenant_id = $1::uuid AND round_id = $2::uuid`, pcTenant, done.RoundID); err != nil {
			t.Fatalf("complete round: %v", err)
		}
	}

	list := func(t *testing.T, filter, penShed, penKey string) ports.RoundCardPage {
		t.Helper()
		page, err := repo.ListRoundCards(ctx, ports.ListRoundCardsQuery{
			TenantID: pcTenant, TenantWide: true, Category: domain.CategoryDeworming,
			Filter: filter, DateFrom: "2026-09-10", DateTo: "2026-09-17", Today: "2026-09-10",
			PenShedID: penShed, PenPartitionKey: penKey, Limit: 50,
		})
		if err != nil {
			t.Fatalf("ListRoundCards(%s): %v", filter, err)
		}
		return page
	}
	ids := func(page ports.RoundCardPage) []string {
		out := []string{}
		for _, c := range page.Cards {
			out = append(out, c.RoundID)
		}
		return out
	}

	active := list(t, ports.RoundCardsFilterActive, "", "")
	if got := ids(active); len(got) != 2 || got[0] != beforeOpen.RoundID || got[1] != inOpen.RoundID {
		t.Fatalf("active window rows = %v, want [carried 5 Sep, 12 Sep] (not 25 Sep)", got)
	}
	if active.Counts.Active != 2 || active.Counts.Completed != 1 {
		t.Fatalf("active counts = %+v, want active 2 / completed 1", active.Counts)
	}
	for _, c := range active.Cards {
		if c.RoundID == inOpen.RoundID && c.PenCount != 2 {
			t.Fatalf("the two-pen round listed %d pens, want 2", c.PenCount)
		}
	}

	completed := list(t, ports.RoundCardsFilterCompleted, "", "")
	if got := ids(completed); len(got) != 1 || got[0] != inDone.RoundID {
		t.Fatalf("completed window rows = %v, want only the 14 Sep round (2 Sep is outside)", got)
	}
	if completed.Counts != active.Counts {
		t.Fatalf("counts differ by tab: %+v vs %+v; each pill is its own bucket", completed.Counts, active.Counts)
	}

	// Pen vocabulary: both pens, status-blind, counting CARDS.
	pens := map[string]ports.RoundPenOption{}
	for _, pen := range completed.Pens {
		pens[pen.ShedID] = pen
	}
	if pens[pcShedA].CardCount != 2 { // 5 Sep carried + 12 Sep
		t.Fatalf("pen A card_count = %d (%+v), want 2", pens[pcShedA].CardCount, completed.Pens)
	}
	if pens[shedB].CardCount != 2 { // 12 Sep open + 14 Sep done
		t.Fatalf("pen B card_count = %d (%+v), want 2", pens[shedB].CardCount, completed.Pens)
	}
	if pens[shedB].Label != "Gandhi" || pens[pcShedA].ParkName == "" {
		t.Fatalf("pen labels not composed: %+v", completed.Pens)
	}

	// One pen keeps the WHOLE card that holds it.
	onlyB := list(t, ports.RoundCardsFilterActive, shedB, domain.PartitionMatchKey(""))
	if got := ids(onlyB); len(got) != 1 || got[0] != inOpen.RoundID {
		t.Fatalf("pen-B active rows = %v, want only the 12 Sep round", got)
	}
	if onlyB.Cards[0].PenCount != 2 {
		t.Fatalf("pen filter shrank the card to %d pens, want 2 (the card stays whole)", onlyB.Cards[0].PenCount)
	}
	if onlyB.Counts.Active != 1 || onlyB.Counts.Completed != 1 {
		t.Fatalf("pen-B counts = %+v, want active 1 / completed 1", onlyB.Counts)
	}
	// A window starting AFTER today carries nothing: the 25 Sep round alone, no 5 Sep.
	future, err := repo.ListRoundCards(ctx, ports.ListRoundCardsQuery{
		TenantID: pcTenant, TenantWide: true, Category: domain.CategoryDeworming,
		Filter: ports.RoundCardsFilterActive, DateFrom: "2026-09-20", DateTo: "2026-09-27", Today: "2026-09-10", Limit: 50,
	})
	if err != nil {
		t.Fatalf("ListRoundCards future: %v", err)
	}
	if got := ids(future); len(got) != 1 || got[0] != afterOpen.RoundID {
		t.Fatalf("future window rows = %v, want only the 25 Sep round", got)
	}
	if future.Counts.Active != 1 {
		t.Fatalf("future window active count = %d, want 1", future.Counts.Active)
	}
}
