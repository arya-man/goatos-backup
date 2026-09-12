package postgres

import (
	"context"
	"testing"

	"github.com/vgoats/goatos/backend/internal/pccare/domain"
	"github.com/vgoats/goatos/backend/internal/pccare/ports"
)

// TestRoundCardsVisitOwedPensOneToManyParkScopePaginationStatusMatrix pins the read behind the
// round card's visit chip (maintainer decision 2026-09-12): VisitOwedTaskIDs names EXACTLY the
// pens whose videos are verified (status completed) while the clock is still open -- never a
// pen in review or rework, never a closed one -- across every pen status of a two-pen round,
// under the park clamp, and on a one-card page so the keyset cursor cannot drop it.
func TestRoundCardsVisitOwedPensOneToManyParkScopePaginationStatusMatrix(t *testing.T) {
	ctx := context.Background()
	repo, _ := setupPCCareDB(t, ctx)
	shedB := "9c000000-0000-4000-8000-000000004091"
	seedRoundCardsPark(t, ctx, repo, shedB, "S-V", "Gandhi")

	round, err := repo.CreateRound(ctx, ports.CreateRoundParams{
		TenantID: pcTenant, Category: domain.CategoryDeworming, ParkID: pcPark,
		Pens:                []domain.RoundPen{{ShedID: pcShedA}, {ShedID: shedB}},
		PlannedBusinessDate: pcBusinessDay(2026, 9, 16),
		AssigneeUserIDs:     []string{pcOperator1},
		IdempotencyKey:      "round-cards-visit-owed",
		CreatedBy:           pcVerifier, ActorID: pcVerifier,
	})
	if err != nil {
		t.Fatalf("CreateRound: %v", err)
	}
	if len(round.Pens) != 2 {
		t.Fatalf("round pens = %d, want 2", len(round.Pens))
	}
	penA, penB := round.Pens[0].TaskID, round.Pens[1].TaskID

	setPen := func(t *testing.T, taskID, status, workState string) {
		t.Helper()
		if _, err := repo.pool.Exec(ctx, `
UPDATE pc_care_tasks SET status = $3, work_state = $4
WHERE tenant_id = $1::uuid AND task_id = $2::uuid`, pcTenant, taskID, status, workState); err != nil {
			t.Fatalf("set pen %s to %s/%s: %v", taskID, status, workState, err)
		}
	}
	cardFor := func(t *testing.T, q ports.ListRoundCardsQuery) (ports.RoundCard, bool) {
		t.Helper()
		q.TenantID, q.Category = pcTenant, domain.CategoryDeworming
		page, err := repo.ListRoundCards(ctx, q)
		if err != nil {
			t.Fatalf("ListRoundCards: %v", err)
		}
		for _, c := range page.Cards {
			if c.RoundID == round.RoundID {
				return c, true
			}
		}
		return ports.RoundCard{}, false
	}
	active := ports.ListRoundCardsQuery{TenantWide: true, Filter: ports.RoundCardsFilterActive, Limit: 50}

	// Status matrix over pen A while pen B stays open: only a VERIFIED pen with an open clock owes.
	for _, tc := range []struct {
		status, workState string
		owes              bool
	}{
		{domain.StatusOpen, domain.WorkStateScheduled, false},
		{domain.StatusPendingVerification, domain.WorkStateScheduled, false},
		{domain.StatusRework, domain.WorkStateScheduled, false},
		{domain.StatusCompleted, domain.WorkStateScheduled, true},
		{domain.StatusCompleted, domain.WorkStateDelayed, true},
		{domain.StatusCompleted, domain.WorkStateCompleted, false},
	} {
		setPen(t, penA, tc.status, tc.workState)
		card, ok := cardFor(t, active)
		if !ok {
			t.Fatalf("%s/%s: the round left the active tab while pen B is still open", tc.status, tc.workState)
		}
		got := map[string]bool{}
		for _, id := range card.VisitOwedTaskIDs {
			got[id] = true
		}
		if got[penA] != tc.owes || got[penB] || len(card.VisitOwedTaskIDs) > 1 {
			t.Fatalf("%s/%s: visit-owed pens = %v, want penA owes=%v and penB never", tc.status, tc.workState, card.VisitOwedTaskIDs, tc.owes)
		}
	}

	// One-to-many: both pens verified with open clocks -> BOTH owe, each exactly once, and the
	// card's pen count is untouched by the aggregate.
	setPen(t, penA, domain.StatusCompleted, domain.WorkStateScheduled)
	setPen(t, penB, domain.StatusCompleted, domain.WorkStateScheduled)
	card, _ := cardFor(t, active)
	if len(card.VisitOwedTaskIDs) != 2 || card.PenCount != 2 {
		t.Fatalf("both verified: owed=%v pen_count=%d, want two distinct pens and pen_count 2", card.VisitOwedTaskIDs, card.PenCount)
	}

	// Park scope: the clamp to another park hides the card entirely; its own park keeps the pens.
	if _, visible := cardFor(t, ports.ListRoundCardsQuery{AuthorizedParkIDs: []string{"9c000000-0000-4000-8000-0000000040ff"}, Filter: ports.RoundCardsFilterActive, Limit: 50}); visible {
		t.Fatal("another park's clamp must not list this round")
	}
	own, ok := cardFor(t, ports.ListRoundCardsQuery{AuthorizedParkIDs: []string{pcPark}, Filter: ports.RoundCardsFilterActive, Limit: 50})
	if !ok || len(own.VisitOwedTaskIDs) != 2 {
		t.Fatalf("own park clamp: ok=%v owed=%v, want the two pens", ok, own.VisitOwedTaskIDs)
	}

	// Pagination: a page of ONE still carries the whole card's owed pens (the aggregate is per
	// card, not per page row), and the cursor walk reaches it exactly once.
	seen := 0
	cursor := ""
	for i := 0; i < 20; i++ {
		page := listActiveCards(t, ctx, repo, 1, cursor)
		for _, c := range page.Cards {
			if c.RoundID == round.RoundID {
				seen++
				if len(c.VisitOwedTaskIDs) != 2 {
					t.Fatalf("page of one: owed=%v, want both pens", c.VisitOwedTaskIDs)
				}
			}
		}
		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
	}
	if seen != 1 {
		t.Fatalf("the round appeared %d times across the cursor walk, want exactly once", seen)
	}
}
