package app

import (
	"context"
	"testing"
	"time"

	penvisitdomain "github.com/vgoats/goatos/backend/internal/penvisits/domain"
	"github.com/vgoats/goatos/backend/internal/vaccinationexecution/domain"
)

type fakePenVisits struct {
	visits map[string]penvisitdomain.Task
	kind   string
}

func (f *fakePenVisits) ForPens(_ context.Context, _, sourceKind string, _ []penvisitdomain.PenRef) (map[string]penvisitdomain.Task, error) {
	f.kind = sourceKind
	return f.visits, nil
}

// TestShedDrilldownComposesTheVisitStepForTheViewerNotTheOperatorScope pins the 2026-09-12
// defect the vaccination E2E story found: the park head who walks the pen is a LEADERSHIP
// reader, so the drilldown carries no OperatorScopeActorID for him -- and keying the step's
// "may record" on that scope composed the visit for nobody. The step is composed for the
// caller (ViewerActorID) while the rows stay unscoped.
func TestShedDrilldownComposesTheVisitStepForTheViewerNotTheOperatorScope(t *testing.T) {
	t.Parallel()
	shedID := "30000000-0000-4000-8000-000000000101"
	dueAt := time.Date(2026, 9, 12, 0, 0, 0, 0, time.UTC)
	reader := &fakePenVisits{visits: map[string]penvisitdomain.Task{
		penvisitdomain.PenKey(shedID, ""): {
			TaskID: "visit-1", ShedID: shedID, Reasons: []string{penvisitdomain.ReasonVaccination},
			WorkState: penvisitdomain.WorkStateScheduled, Status: penvisitdomain.StatusOpen,
			VisitorIDs: []string{"park-head"}, PlannedDate: "2026-09-13", DueDate: "2026-09-13", SourceDate: "2026-09-12",
		},
	}}
	svc := NewService(fakeRepo{rows: []domain.ExecutionProjection{projection(shedID, dueAt, 1, nil)}}).WithPenVisits(reader)

	detail, found, err := svc.ShedDrilldown(context.Background(), domain.ExecutionQuery{
		TenantID: "tenant", ShedID: &shedID, AsOf: dueAt, DueBefore: dueAt.AddDate(0, 0, 1), Limit: 20,
		ViewerActorID: "park-head", // leadership: NO OperatorScopeActorID
	})
	if err != nil || !found {
		t.Fatalf("drilldown: err=%v found=%v", err, found)
	}
	if reader.kind != penvisitdomain.SourceKindVaccinationSubmission {
		t.Fatalf("the drilldown must read the vaccination-raised visit, got kind %q", reader.kind)
	}
	if detail.PenVisit == nil || detail.PenVisit.TaskID != "visit-1" {
		t.Fatalf("the drilldown must carry the pen's visit step, got %+v", detail.PenVisit)
	}
	if !detail.PenVisit.CanSubmit {
		t.Fatalf("the configured visitor reading his own park must be able to record the visit; step=%+v", detail.PenVisit)
	}

	stranger, _, err := svc.ShedDrilldown(context.Background(), domain.ExecutionQuery{
		TenantID: "tenant", ShedID: &shedID, AsOf: dueAt, DueBefore: dueAt.AddDate(0, 0, 1), Limit: 20,
		ViewerActorID: "someone-else",
	})
	if err != nil {
		t.Fatalf("stranger drilldown: %v", err)
	}
	if stranger.PenVisit == nil || stranger.PenVisit.CanSubmit {
		t.Fatalf("a reader who is not a configured visitor sees the step but cannot record it; step=%+v", stranger.PenVisit)
	}
}
