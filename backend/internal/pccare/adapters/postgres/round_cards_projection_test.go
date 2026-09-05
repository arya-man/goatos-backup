package postgres

import (
	"context"
	"slices"
	"testing"

	"github.com/vgoats/goatos/backend/internal/pccare/domain"
	"github.com/vgoats/goatos/backend/internal/pccare/ports"
)

// The planner's ROUND-grained list against the REAL schema (maintainer decision 2026-09-05).
// These are the adversarial cases the aggregate/projection rule demands of any read that
// combines JOIN with COUNT and GROUP BY: one-to-many fan-out, page boundaries, park scope, and
// the whole status matrix. Each one is a way this query could silently report a wrong number.

// seedRoundCardsPark adds a second shed so a round can cover more than one pen.
func seedRoundCardsPark(t *testing.T, ctx context.Context, repo *Repository, shedID, code, name string) {
	t.Helper()
	if _, err := repo.pool.Exec(ctx, `
INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status, parent_location_id, display_order)
VALUES ($3::uuid, $1::uuid, 'shed', $4, $5, 'active', $2::uuid, 2)
ON CONFLICT (location_id) DO NOTHING`, pcTenant, pcPark, shedID, code, name); err != nil {
		t.Fatalf("seed shed %s: %v", name, err)
	}
}

func listActiveCards(t *testing.T, ctx context.Context, repo *Repository, limit int, cursor string) ports.RoundCardPage {
	t.Helper()
	page, err := repo.ListRoundCards(ctx, ports.ListRoundCardsQuery{
		TenantID:   pcTenant,
		TenantWide: true,
		Category:   domain.CategoryDeworming,
		Filter:     ports.RoundCardsFilterActive,
		Limit:      limit,
		Cursor:     cursor,
	})
	if err != nil {
		t.Fatalf("ListRoundCards: %v", err)
	}
	return page
}

// ONE-TO-MANY: a round's pens each carry MULTIPLE assignees, and a pen carries MULTIPLE
// scanned animals. Joining either side raw would multiply the grouped rows and report a
// three-pen round as six or nine pens. The count must range over PENS alone.
func TestRoundCardsOneToManyAssigneesDoNotInflateThePenCount(t *testing.T) {
	ctx := context.Background()
	repo, _ := setupPCCareDB(t, ctx)
	shedB := "9c000000-0000-4000-8000-000000004077"
	seedRoundCardsPark(t, ctx, repo, shedB, "S-B", "Gandhi")

	round, err := repo.CreateRound(ctx, ports.CreateRoundParams{
		TenantID:            pcTenant,
		Category:            domain.CategoryDeworming,
		ParkID:              pcPark,
		Pens:                []domain.RoundPen{{ShedID: pcShedA}, {ShedID: shedB}},
		PlannedBusinessDate: pcBusinessDay(2026, 9, 14),
		// TWO operators on every pen: the many side that would fan the group out.
		AssigneeUserIDs: []string{pcOperator1, pcOperator2},
		IdempotencyKey:  "round-cards-one-to-many",
		CreatedBy:       pcVerifier,
		ActorID:         pcVerifier,
	})
	if err != nil {
		t.Fatalf("CreateRound: %v", err)
	}

	page := listActiveCards(t, ctx, repo, 25, "")
	var card ports.RoundCard
	for _, c := range page.Cards {
		if c.RoundID == round.RoundID {
			card = c
		}
	}
	if card.CardKey == "" {
		t.Fatalf("round %s missing from the card list", round.RoundID)
	}
	if card.PenCount != 2 {
		t.Fatalf("pen_count = %d, want 2 — the assignee join fanned the group out", card.PenCount)
	}
	if len(card.PenLabels) != 2 {
		t.Fatalf("pen labels = %v, want exactly the two pens", card.PenLabels)
	}
	if len(card.AssigneeNames) != 2 {
		t.Fatalf("assignee names = %v, want both operators once each", card.AssigneeNames)
	}
}

func TestRoundCardsMultipleDimensionsPaginationParkScopeStatusMatrixUsesOperationalLocationDisplay(t *testing.T) {
	ctx := context.Background()
	repo, _ := setupPCCareDB(t, ctx)

	round, err := repo.CreateRound(ctx, ports.CreateRoundParams{
		TenantID: pcTenant, Category: domain.CategoryDeworming, ParkID: pcPark,
		Pens: []domain.RoundPen{
			{ShedID: pcShedA, PartitionLabel: "Part 1"},
			{ShedID: pcShedA, PartitionLabel: "Part 2"},
		},
		PlannedBusinessDate: pcBusinessDay(2026, 9, 17),
		AssigneeUserIDs:     []string{pcOperator1, pcOperator2},
		IdempotencyKey:      "round-cards-operational-location-display",
		CreatedBy:           pcVerifier, ActorID: pcVerifier,
	})
	if err != nil {
		t.Fatalf("CreateRound: %v", err)
	}

	page := listActiveCards(t, ctx, repo, 1, "")
	if len(page.Cards) != 1 || page.Cards[0].RoundID != round.RoundID {
		t.Fatalf("first page cards = %+v, want only round %s", page.Cards, round.RoundID)
	}
	card := page.Cards[0]
	if card.PenCount != 2 {
		t.Fatalf("pen_count = %d, want 2", card.PenCount)
	}
	if !slices.Contains(card.PenLabels, "Castro - Part 1") || !slices.Contains(card.PenLabels, "Castro - Part 2") {
		t.Fatalf("pen labels = %v, want operational location display labels", card.PenLabels)
	}
	if card.Status != domain.StatusOpen {
		t.Fatalf("status = %q, want open", card.Status)
	}

	clamped, err := repo.ListRoundCards(ctx, ports.ListRoundCardsQuery{
		TenantID: pcTenant, AuthorizedParkIDs: []string{pcOtherPark},
		Category: domain.CategoryDeworming, Filter: ports.RoundCardsFilterActive, Limit: 25,
	})
	if err != nil {
		t.Fatalf("ListRoundCards (other park): %v", err)
	}
	for _, c := range clamped.Cards {
		if c.RoundID == round.RoundID {
			t.Fatal("round leaked across the park clamp")
		}
	}
}

// PAGE BOUNDARY: the keyset must hand back every card exactly once across pages. A cursor that
// repeats or skips a card is the classic keyset defect, and it is invisible on one page.
func TestRoundCardsPaginationWalksEveryCardExactlyOnce(t *testing.T) {
	ctx := context.Background()
	repo, _ := setupPCCareDB(t, ctx)
	shedB := "9c000000-0000-4000-8000-000000004078"
	seedRoundCardsPark(t, ctx, repo, shedB, "S-C", "Godel")

	// Three rounds on three different days, so the (due_date, card_key) cursor has to advance.
	for i, day := range []int{21, 22, 23} {
		if _, err := repo.CreateRound(ctx, ports.CreateRoundParams{
			TenantID: pcTenant, Category: domain.CategoryDeworming, ParkID: pcPark,
			Pens:                []domain.RoundPen{{ShedID: pcShedA}, {ShedID: shedB}},
			PlannedBusinessDate: pcBusinessDay(2026, 9, day),
			AssigneeUserIDs:     []string{pcOperator1},
			IdempotencyKey:      "round-cards-page-" + string(rune('a'+i)),
			CreatedBy:           pcVerifier, ActorID: pcVerifier,
		}); err != nil {
			t.Fatalf("CreateRound day %d: %v", day, err)
		}
	}

	seen := map[string]int{}
	cursor := ""
	for pages := 0; pages < 10; pages++ {
		page := listActiveCards(t, ctx, repo, 1, cursor)
		for _, c := range page.Cards {
			seen[c.CardKey]++
		}
		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
	}
	if len(seen) < 3 {
		t.Fatalf("walked %d cards, want at least the three rounds", len(seen))
	}
	for key, times := range seen {
		if times != 1 {
			t.Fatalf("card %s returned %d times across pages, want exactly once", key, times)
		}
	}
}

// PARK SCOPE: a caller clamped to one park must not see another park's rounds. The clamp is
// the only thing between two parks' planners.
func TestRoundCardsParkScopeClampHidesAnotherParksRound(t *testing.T) {
	ctx := context.Background()
	repo, _ := setupPCCareDB(t, ctx)

	round, err := repo.CreateRound(ctx, ports.CreateRoundParams{
		TenantID: pcTenant, Category: domain.CategoryDeworming, ParkID: pcPark,
		Pens:                []domain.RoundPen{{ShedID: pcShedA}},
		PlannedBusinessDate: pcBusinessDay(2026, 9, 15),
		AssigneeUserIDs:     []string{pcOperator1},
		IdempotencyKey:      "round-cards-scope",
		CreatedBy:           pcVerifier, ActorID: pcVerifier,
	})
	if err != nil {
		t.Fatalf("CreateRound: %v", err)
	}

	// Clamped to the OTHER park: the round must not appear.
	page, err := repo.ListRoundCards(ctx, ports.ListRoundCardsQuery{
		TenantID:          pcTenant,
		AuthorizedParkIDs: []string{pcOtherPark},
		Category:          domain.CategoryDeworming,
		Filter:            ports.RoundCardsFilterActive,
		Limit:             25,
	})
	if err != nil {
		t.Fatalf("ListRoundCards (other park): %v", err)
	}
	for _, c := range page.Cards {
		if c.RoundID == round.RoundID {
			t.Fatal("a round leaked across the park clamp")
		}
	}

	// Clamped to its OWN park: it appears.
	own, err := repo.ListRoundCards(ctx, ports.ListRoundCardsQuery{
		TenantID:          pcTenant,
		AuthorizedParkIDs: []string{pcPark},
		Category:          domain.CategoryDeworming,
		Filter:            ports.RoundCardsFilterActive,
		Limit:             25,
	})
	if err != nil {
		t.Fatalf("ListRoundCards (own park): %v", err)
	}
	found := false
	for _, c := range own.Cards {
		if c.RoundID == round.RoundID {
			found = true
		}
	}
	if !found {
		t.Fatal("the round is missing from its own park's list")
	}
}

// STATUS MATRIX: the card's ONE status is a roll-up over its pens, and the tabs must split the
// same rows disjointly. A pen still OPEN outranks submitted ones — a card reading "in review"
// while a pen is still full would tell the crew their evening is finished.
func TestRoundCardsStatusMatrixRollsUpAndSplitsTheTabsDisjointly(t *testing.T) {
	ctx := context.Background()
	repo, _ := setupPCCareDB(t, ctx)
	shedB := "9c000000-0000-4000-8000-000000004079"
	seedRoundCardsPark(t, ctx, repo, shedB, "S-D", "Mandela")

	round, err := repo.CreateRound(ctx, ports.CreateRoundParams{
		TenantID: pcTenant, Category: domain.CategoryDeworming, ParkID: pcPark,
		Pens:                []domain.RoundPen{{ShedID: pcShedA}, {ShedID: shedB}},
		PlannedBusinessDate: pcBusinessDay(2026, 9, 16),
		AssigneeUserIDs:     []string{pcOperator1},
		IdempotencyKey:      "round-cards-status",
		CreatedBy:           pcVerifier, ActorID: pcVerifier,
	})
	if err != nil {
		t.Fatalf("CreateRound: %v", err)
	}
	if len(round.Pens) != 2 {
		t.Fatalf("round pens = %d, want 2", len(round.Pens))
	}

	cardFor := func(t *testing.T, filter string) (ports.RoundCard, bool) {
		t.Helper()
		page, err := repo.ListRoundCards(ctx, ports.ListRoundCardsQuery{
			TenantID: pcTenant, TenantWide: true, Category: domain.CategoryDeworming,
			Filter: filter, Limit: 50,
		})
		if err != nil {
			t.Fatalf("ListRoundCards(%s): %v", filter, err)
		}
		for _, c := range page.Cards {
			if c.RoundID == round.RoundID {
				return c, true
			}
		}
		return ports.RoundCard{}, false
	}

	// Both pens open -> the card is open, and it is ACTIVE, not completed.
	card, ok := cardFor(t, ports.RoundCardsFilterActive)
	if !ok || card.Status != domain.StatusOpen {
		t.Fatalf("all-open card = %+v ok=%v, want status open on the active tab", card, ok)
	}
	if _, alsoCompleted := cardFor(t, ports.RoundCardsFilterCompleted); alsoCompleted {
		t.Fatal("an open round appeared on BOTH tabs; the tabs must be disjoint")
	}

	// One pen submitted, one still open -> the card STILL reads open: work is still owed.
	if _, err := repo.pool.Exec(ctx, `
UPDATE pc_care_tasks SET status = 'pending_verification'
WHERE tenant_id = $1::uuid AND task_id = $2::uuid`, pcTenant, round.Pens[0].TaskID); err != nil {
		t.Fatalf("mark pen submitted: %v", err)
	}
	card, _ = cardFor(t, ports.RoundCardsFilterActive)
	if card.Status != domain.StatusOpen {
		t.Fatalf("status with one pen still open = %q, want open — a card must never read done while work is owed", card.Status)
	}

	// Both submitted -> the card reads in-review, still active work.
	if _, err := repo.pool.Exec(ctx, `
UPDATE pc_care_tasks SET status = 'pending_verification'
WHERE tenant_id = $1::uuid AND task_id = $2::uuid`, pcTenant, round.Pens[1].TaskID); err != nil {
		t.Fatalf("mark second pen submitted: %v", err)
	}
	card, _ = cardFor(t, ports.RoundCardsFilterActive)
	if card.Status != domain.StatusPendingVerification {
		t.Fatalf("status with both pens submitted = %q, want pending_verification", card.Status)
	}

	// A REWORK pen outranks the submitted one: somebody must redo it tonight.
	if _, err := repo.pool.Exec(ctx, `
UPDATE pc_care_tasks SET status = 'rework'
WHERE tenant_id = $1::uuid AND task_id = $2::uuid`, pcTenant, round.Pens[0].TaskID); err != nil {
		t.Fatalf("mark pen rework: %v", err)
	}
	card, _ = cardFor(t, ports.RoundCardsFilterActive)
	if card.Status != domain.StatusRework {
		t.Fatalf("status with a bounced pen = %q, want rework", card.Status)
	}

	// Finished work leaves the active tab for the completed one — never both, never neither.
	if _, err := repo.pool.Exec(ctx, `
UPDATE pc_care_tasks SET work_state = 'completed', status = 'completed'
WHERE tenant_id = $1::uuid AND round_id = $2::uuid`, pcTenant, round.RoundID); err != nil {
		t.Fatalf("complete the round: %v", err)
	}
	if _, stillActive := cardFor(t, ports.RoundCardsFilterActive); stillActive {
		t.Fatal("a completed round stayed on the active tab")
	}
	done, ok := cardFor(t, ports.RoundCardsFilterCompleted)
	if !ok || done.Status != domain.StatusCompleted {
		t.Fatalf("completed card = %+v ok=%v, want status completed on the completed tab", done, ok)
	}
}
