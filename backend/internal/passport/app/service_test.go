package app

import (
	"context"
	"testing"
	"time"

	obldomain "github.com/vgoats/goatos/backend/internal/obligation/domain"
	vaccdomain "github.com/vgoats/goatos/backend/internal/vaccination/domain"
)

type fakeVacc struct {
	history []vaccdomain.CompletionHistoryItem
	last    vaccdomain.LastAccepted
	found   bool
}

func (f fakeVacc) GoatHistory(context.Context, string, string, int32) ([]vaccdomain.CompletionHistoryItem, error) {
	return f.history, nil
}
func (f fakeVacc) LastAccepted(context.Context, string, string) (vaccdomain.LastAccepted, bool, error) {
	return f.last, f.found, nil
}

type fakeObl struct{ open []obldomain.OpenObligation }

func (f fakeObl) ListOpenByGoat(context.Context, string, string, int32) ([]obldomain.OpenObligation, error) {
	return f.open, nil
}

func TestGetPassportComposesAndPicksNextDue(t *testing.T) {
	ctx := context.Background()
	t0 := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	t1 := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)

	svc := NewService(
		fakeVacc{
			history: []vaccdomain.CompletionHistoryItem{
				{CompletionID: "c1", ObligationID: "o0", Status: "accepted", AdministeredAt: t0, Doses: 1},
			},
			last:  vaccdomain.LastAccepted{CompletionID: "c1", ObligationID: "o0", AdministeredAt: t0},
			found: true,
		},
		fakeObl{open: []obldomain.OpenObligation{
			{ObligationID: "o1", RuleID: "r1", ScopeType: "shed", ScopeID: "shed1", BatchID: "b1", DueAt: t0, Status: "due", Sequence: 1}, // earliest → next due
			{ObligationID: "o2", DueAt: t1, Status: "scheduled", Sequence: 2},
		}},
	)

	p, err := svc.GetPassport(ctx, "tenant", "goat")
	if err != nil {
		t.Fatalf("get passport: %v", err)
	}
	if p.GoatID != "goat" || len(p.OpenObligations) != 2 || len(p.VaccinationHistory) != 1 {
		t.Fatalf("passport shape: %+v", p)
	}
	if p.NextDue == nil || p.NextDue.ObligationID != "o1" {
		t.Fatalf("next due: want o1, got %+v", p.NextDue)
	}
	if p.NextDue.WorkflowRowID != "batch:b1:rule:r1:shed:shed1" {
		t.Fatalf("workflow row id: %q", p.NextDue.WorkflowRowID)
	}
	if p.OpenObligations[1].WorkflowRowID != "obligation:o2" {
		t.Fatalf("fallback workflow row id: %q", p.OpenObligations[1].WorkflowRowID)
	}
	if p.LastAccepted == nil || p.LastAccepted.CompletionID != "c1" {
		t.Fatalf("last accepted: %+v", p.LastAccepted)
	}
}

func TestGetPassportEmptyHasNoNextDueAndEmptyArrays(t *testing.T) {
	svc := NewService(fakeVacc{found: false}, fakeObl{})
	p, err := svc.GetPassport(context.Background(), "tenant", "goat")
	if err != nil {
		t.Fatalf("get passport: %v", err)
	}
	if p.NextDue != nil || p.LastAccepted != nil {
		t.Fatalf("expected no next-due / last-accepted, got %+v", p)
	}
	if p.OpenObligations == nil || p.VaccinationHistory == nil {
		t.Fatalf("empty sections must be non-nil arrays for a stable API shape")
	}
}
