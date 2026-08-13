package app

import (
	"context"
	"testing"

	"github.com/vgoats/goatos/backend/internal/feeddirection/domain"
)

// experimentClock is the second dispatch clock a park runs: experiment feed is directed at 14:00,
// seven hours after normal. Both parks carry exactly this pair in STG.
func experimentClock() domain.WorkflowClock {
	return domain.WorkflowClock{Workflow: domain.WorkflowExperiment, DirectionTime: "14:00:00", CorrectionTime: "14:00:00"}
}

// newTwoWorkflowService is the realistic park: normal fires at 07:00, experiment at 14:00.
func newTwoWorkflowService(nowHour int) (*Service, *fakeIssueStore) {
	svc, _, _, store := newLifecycleService(istInstant(2026, 7, 29, nowHour))
	svc.schedule = &fakeScheduleReader{
		clocks: []domain.WorkflowClock{normalClock(), experimentClock()},
		parks:  []string{testPark},
	}
	return svc, store
}

// THE MAINTAINER RULE (2026-08-08): feed PACKING freezes for normal at 07:00 and for experiment at
// 14:00. Packing has no clock of its own -- it reads the direction sheet's frozen rows -- so this
// asserts the rule on the packing worklist, which is the surface the crew actually works from.
//
// The interesting hour is 08:00, when the two workflows DISAGREE: normal has fired and must be
// frozen and visible, while experiment has not and must show nothing at all. A gate that keyed off
// the park rather than the workflow would either leak experiment early or hide normal late; both
// failures are invisible in a single-workflow fixture, which is why this fixture carries two.
func TestPackingFreezesNormalAtSevenAndExperimentAtTwo(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("before 07:00 neither workflow is visible", func(t *testing.T) {
		t.Parallel()
		svc, store := newTwoWorkflowService(6)
		page, err := svc.PackingWorklist(ctx, domain.PackingQuery{TenantID: testTenant, ParkID: testPark, TargetDate: feedDayTarget()})
		if err != nil {
			t.Fatalf("PackingWorklist: %v", err)
		}
		if page.Lifecycle.State != domain.LifecycleStatePending {
			t.Fatalf("06:00 state = %q, want pending", page.Lifecycle.State)
		}
		if len(page.Items) != 0 {
			t.Fatalf("06:00 must show no bags, got %d", len(page.Items))
		}
		if len(store.headers) != 0 {
			t.Fatalf("06:00 must freeze nothing, got %d issue(s)", len(store.headers))
		}
	})

	t.Run("at 08:00 normal is frozen and experiment is still hidden", func(t *testing.T) {
		t.Parallel()
		svc, store := newTwoWorkflowService(8)
		page, err := svc.PackingWorklist(ctx, domain.PackingQuery{TenantID: testTenant, ParkID: testPark, TargetDate: feedDayTarget()})
		if err != nil {
			t.Fatalf("PackingWorklist: %v", err)
		}
		if len(page.Items) == 0 {
			t.Fatal("08:00 must show the frozen NORMAL bags")
		}
		// Exactly one sheet exists, and it is the normal one: experiment must not have been dragged
		// across the gate by its sibling.
		if len(store.headers) != 1 {
			t.Fatalf("08:00 must freeze exactly the normal sheet, got %d issue(s)", len(store.headers))
		}
		for _, h := range store.headers {
			if h.Workflow != domain.WorkflowNormal {
				t.Fatalf("08:00 froze %q; only normal is due at 07:00", h.Workflow)
			}
		}
		// The still-gated experiment must be reported with its 14:00 arrival time, so the screen can
		// say when the second sheet lands rather than silently omitting it.
		var experiment *domain.WorkflowLifecycle
		for i := range page.Lifecycle.Workflows {
			if page.Lifecycle.Workflows[i].Workflow == domain.WorkflowExperiment {
				experiment = &page.Lifecycle.Workflows[i]
			}
		}
		if experiment == nil {
			t.Fatalf("08:00 lifecycle must still name experiment, got %+v", page.Lifecycle.Workflows)
		}
		if experiment.State != domain.LifecycleStatePending {
			t.Fatalf("experiment at 08:00 = %q, want pending", experiment.State)
		}
		if experiment.ExpectedIssueAt == nil ||
			*experiment.ExpectedIssueAt != domain.FormatBusinessInstant(istInstant(2026, 7, 29, 14)) {
			t.Fatalf("experiment must name its 14:00 arrival, got %v", experiment.ExpectedIssueAt)
		}
	})

	t.Run("at 15:00 both workflows are frozen", func(t *testing.T) {
		t.Parallel()
		svc, store := newTwoWorkflowService(15)
		page, err := svc.PackingWorklist(ctx, domain.PackingQuery{TenantID: testTenant, ParkID: testPark, TargetDate: feedDayTarget()})
		if err != nil {
			t.Fatalf("PackingWorklist: %v", err)
		}
		if len(page.Items) == 0 {
			t.Fatal("15:00 must show bags")
		}
		if len(store.headers) != 2 {
			t.Fatalf("15:00 must have frozen BOTH sheets, got %d", len(store.headers))
		}
		seen := map[string]bool{}
		for _, h := range store.headers {
			seen[h.Workflow] = true
		}
		if !seen[domain.WorkflowNormal] || !seen[domain.WorkflowExperiment] {
			t.Fatalf("15:00 must carry both workflows, got %v", seen)
		}
	})
}
