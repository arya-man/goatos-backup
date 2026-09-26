package postgres

import (
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/tasks/domain"
	"github.com/vgoats/goatos/backend/internal/tasks/ports"
)

// TestPlannedSaleStepsAreDueOnTheSaleDateNotAtRecording (2026-09-26, found on the Sales E2E run):
// every sales.deal step is due "immediately", and immediately used to mean the RECORDING instant,
// so an Advance Paid sale planned five days out read "Tag the animals sold · Overdue" the moment it
// was saved. The sale's clock now anchors on the sale's own business day: a future-dated sale's
// steps are due on that day, a sale dated today (or earlier) is due at recording exactly as before,
// and when the sale closes early its unfinished steps follow the close date -- once, however often
// the close event is redelivered.
func TestPlannedSaleStepsAreDueOnTheSaleDateNotAtRecording(t *testing.T) {
	repo, _, ctx := newWorkflowRepo(t)
	recordedAt := wfEventAt
	planned := biztime.BusinessDayStart(recordedAt).AddDate(0, 0, 5)

	open := func(dealID, saleDate string) string {
		t.Helper()
		ref := dealID
		anchor := domain.SaleClockAnchor(recordedAt, saleDate)
		if _, err := repo.OpenWorkflow(ctx, ports.OpenWorkflowCommand{TenantID: wfTenant, TemplateKey: domain.TemplateKeySalesDeal,
			EventAt: recordedAt, ClockAnchor: anchor, SubjectRefID: &ref}); err != nil {
			t.Fatalf("open %s: %v", dealID, err)
		}
		id, err := repo.WorkflowIDBySubjectRef(ctx, wfTenant, domain.TemplateKeySalesDeal, dealID)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	tagDue := func(workflowID string, now time.Time) (time.Time, bool) {
		t.Helper()
		d, err := repo.GetWorkflow(ctx, wfTenant, workflowID, now)
		if err != nil {
			t.Fatal(err)
		}
		for _, a := range d.Actions {
			if a.ActionKey == "tag_animals" {
				if a.DueAt == nil {
					t.Fatalf("tag step has no due")
				}
				return *a.DueAt, d.Card.NextAction != nil && d.Card.NextAction.Overdue
			}
		}
		t.Fatalf("no tag step")
		return time.Time{}, false
	}

	// Advance Paid, planned +5 days: not overdue today, due on the planned day.
	plannedWF := open("5a1e5a1e-0000-4000-8000-00000000c001", biztime.BusinessDate(planned))
	due, overdue := tagDue(plannedWF, recordedAt.Add(time.Minute))
	if overdue || biztime.BusinessDate(due) != biztime.BusinessDate(planned) {
		t.Fatalf("planned sale: tag due %s overdue=%v, want due on %s and not overdue", due, overdue, biztime.BusinessDate(planned))
	}

	// Dated today: unchanged -- due at the recording instant.
	todayWF := open("5a1e5a1e-0000-4000-8000-00000000c002", biztime.BusinessDate(recordedAt))
	if due, _ := tagDue(todayWF, recordedAt); !due.Equal(recordedAt) {
		t.Fatalf("a sale dated today must be due at recording (%s), got %s", recordedAt, due)
	}

	// Closed early (+2 days): the planned sale's unfinished steps follow the close date, once.
	closeDate := biztime.BusinessDayStart(recordedAt).AddDate(0, 0, 2)
	for i := 0; i < 2; i++ { // the second pass is a redelivered close event
		if err := repo.ReanchorSaleWorkflow(ctx, wfTenant, "5a1e5a1e-0000-4000-8000-00000000c001", biztime.BusinessDate(closeDate)); err != nil {
			t.Fatalf("reanchor pass %d: %v", i, err)
		}
	}
	if due, _ := tagDue(plannedWF, recordedAt); !due.Equal(closeDate) {
		t.Fatalf("after closing on %s the tag step must be due then, got %s", biztime.BusinessDate(closeDate), due)
	}
	// Closed on the recording day: due at the recording instant, as a sale dated today would be.
	if err := repo.ReanchorSaleWorkflow(ctx, wfTenant, "5a1e5a1e-0000-4000-8000-00000000c001", biztime.BusinessDate(recordedAt)); err != nil {
		t.Fatal(err)
	}
	if due, _ := tagDue(plannedWF, recordedAt); !due.Equal(recordedAt) {
		t.Fatalf("closing on the recording day must bring the step back to recording, got %s", due)
	}
	// A sale anchored on its recording (NULL anchor = event_at) follows a close on a LATER day to
	// that day, once: a close restamps the sale date, and the sale's work is due on its sale day.
	// A close on the recording day itself moves nothing.
	if err := repo.ReanchorSaleWorkflow(ctx, wfTenant, "5a1e5a1e-0000-4000-8000-00000000c002", biztime.BusinessDate(recordedAt)); err != nil {
		t.Fatal(err)
	}
	if due, _ := tagDue(todayWF, recordedAt); !due.Equal(recordedAt) {
		t.Fatalf("a sale closed on its recording day must not move, got %s", due)
	}
	for i := 0; i < 2; i++ { // the second pass is a redelivered close event
		if err := repo.ReanchorSaleWorkflow(ctx, wfTenant, "5a1e5a1e-0000-4000-8000-00000000c002", biztime.BusinessDate(closeDate)); err != nil {
			t.Fatal(err)
		}
	}
	if due, _ := tagDue(todayWF, recordedAt); !due.Equal(closeDate) {
		t.Fatalf("a sale dated at recording and closed on %s must be due then, got %s", biztime.BusinessDate(closeDate), due)
	}
}
