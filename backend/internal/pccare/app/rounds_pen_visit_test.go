package app

import (
	"context"
	"testing"

	"github.com/vgoats/goatos/backend/internal/pccare/domain"
	"github.com/vgoats/goatos/backend/internal/pccare/ports"
	penvisitdomain "github.com/vgoats/goatos/backend/internal/penvisits/domain"
)

type fakePenVisitReader struct {
	visits map[string]penvisitdomain.Task
	asked  []string
}

func (f *fakePenVisitReader) ForSources(_ context.Context, _, _ string, refIDs []string) (map[string]penvisitdomain.Task, error) {
	f.asked = append(f.asked, refIDs...)
	return f.visits, nil
}

// TestRoundCardReadsVisitPensTodayNotDoneWhileItsPensOweTheVisit pins the defect the Realme
// proof found on 2026-09-12: a hoof round whose two videos the verifier had approved read
// "Done" on the director's Pending list while both pens still owed their next-day visit. The
// card's chip is rolled up over the owed pens' visits, backend-side, in ONE batched read.
func TestRoundCardReadsVisitPensTodayNotDoneWhileItsPensOweTheVisit(t *testing.T) {
	svc, rounds := roundSvc(pinnedIST(12, 9, 0))
	reader := &fakePenVisitReader{visits: map[string]penvisitdomain.Task{
		"pen-a": {TaskID: "visit-a", Status: penvisitdomain.StatusOpen, WorkState: penvisitdomain.WorkStateScheduled, DueDate: "2026-09-12", PlannedDate: "2026-09-12"},
		"pen-b": {TaskID: "visit-b", Status: penvisitdomain.StatusPendingVerification, WorkState: penvisitdomain.WorkStateScheduled, DueDate: "2026-09-12", PlannedDate: "2026-09-12"},
	}}
	svc = svc.WithPenVisits(reader)
	rounds.cards = []ports.RoundCard{
		{CardKey: "round-1", RoundID: "round-1", Status: domain.StatusCompleted, VisitOwedTaskIDs: []string{"pen-a", "pen-b"}},
		{CardKey: "round-2", RoundID: "round-2", Status: domain.StatusPendingVerification},
	}

	page, err := svc.ListRoundCards(plannerCtx(), plannerActor(), "", domain.CategoryHoofTrimming, "", ports.RoundCardsFilterActive, "", 25, false, ports.RoundCardsWindow{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(reader.asked) != 2 {
		t.Fatalf("the owed pens must be read in one batch, asked=%v", reader.asked)
	}
	if got := page.Cards[0]; got.PenVisitChip != "Visit pen today" || got.PenVisitTone != "info" {
		t.Fatalf("a verified round with a pen still owing its visit must read the visit, got %q/%q", got.PenVisitChip, got.PenVisitTone)
	}
	if got := page.Cards[1]; got.PenVisitChip != "" {
		t.Fatalf("a round still in review keeps its status chip, got %q", got.PenVisitChip)
	}
}
