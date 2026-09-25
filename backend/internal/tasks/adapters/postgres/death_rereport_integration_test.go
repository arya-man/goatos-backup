package postgres

// A REJECTED DEATH REPORT MUST NOT LOCK THE ANIMAL OUT OF EVER BEING RECORDED DEAD (2026-09-25).
//
// Reject emits counts.death.rejected, which cancels the goat's single death workflow (the natural
// key workflow_instances_death_uq allows one per animal). A later report of the same animal opened
// the workflow through INSERT ... ON CONFLICT DO NOTHING, hit that canceled row and did nothing, so
// its steps never reopened and every approve answered 409 death_evidence_incomplete for ever.

import (
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/tasks/domain"
	"github.com/vgoats/goatos/backend/internal/tasks/ports"
)

const (
	deathReportA = "aaaaaaa1-0000-0000-0000-00000000da0a"
	deathReportB = "aaaaaaa1-0000-0000-0000-00000000da0b"
)

func TestDeathReReportAfterRejectionReopensTheStepsPg(t *testing.T) {
	repo, _, ctx := newWorkflowRepo(t)

	open := func(reportID string, at time.Time) bool {
		t.Helper()
		ref := reportID
		created, err := repo.OpenWorkflow(ctx, ports.OpenWorkflowCommand{
			TenantID: wfTenant, TemplateKey: domain.TemplateKeyDeath, SubjectGoatID: wfDead,
			EventAt: at, SubjectRefID: &ref,
		})
		if err != nil {
			t.Fatalf("open death workflow for report %s: %v", reportID, err)
		}
		return created
	}

	// Report A: the operator records both videos, then the approver rejects the report.
	if !open(deathReportA, wfEventAt) {
		t.Fatal("first report must open the death workflow")
	}
	workflowID := findWorkflowID(t, repo, ctx, domain.TemplateKeyDeath, wfDead)
	completeDeathVideosPg(t, repo, ctx, workflowID, "a")
	if err := repo.CancelDeathWorkflowForGoat(ctx, wfTenant, wfDead, wfEventAt.Add(time.Hour)); err != nil {
		t.Fatalf("cancel after rejection: %v", err)
	}

	// A redelivered counts.death.reported for the REJECTED report must not resurrect it.
	if open(deathReportA, wfEventAt) {
		t.Fatal("a redelivered report A must not reopen the workflow its own rejection canceled")
	}
	var state string
	if err := repo.pool.QueryRow(ctx, `SELECT state FROM workflow_instances WHERE workflow_id = $1::uuid`, workflowID).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != domain.WorkflowStateCanceled {
		t.Fatalf("state after redelivered report A = %q, want canceled", state)
	}

	// Report B, a day later: the death workflow must be OPEN again with fresh, pending steps.
	reReportAt := wfEventAt.Add(24 * time.Hour)
	if !open(deathReportB, reReportAt) {
		t.Fatal("a NEW report of the same animal must reopen its canceled death workflow")
	}
	if got := findWorkflowID(t, repo, ctx, domain.TemplateKeyDeath, wfDead); got != workflowID {
		t.Fatalf("workflow id changed %s -> %s; the natural key keeps one row per animal", workflowID, got)
	}
	detail, err := repo.GetWorkflow(ctx, wfTenant, workflowID, reReportAt)
	if err != nil {
		t.Fatalf("get reopened workflow: %v", err)
	}
	var reState string
	var reDone int
	var reAwaiting bool
	if err := repo.pool.QueryRow(ctx, `SELECT state, actions_done, awaiting_verification FROM workflow_instances WHERE workflow_id = $1::uuid`,
		workflowID).Scan(&reState, &reDone, &reAwaiting); err != nil {
		t.Fatal(err)
	}
	if reState != domain.WorkflowStateOpen || reDone != 0 || reAwaiting {
		t.Fatalf("reopened card state=%q actions_done=%d awaiting_verification=%t, want open/0/false", reState, reDone, reAwaiting)
	}
	for _, a := range detail.Actions {
		if a.Status != domain.ActionStatusPending {
			t.Fatalf("step %s status=%q after reopen, want pending", a.ActionKey, a.Status)
		}
	}

	// Report A's rejection delivered LATE (after report B reopened the workflow) must not cancel
	// report B's steps: that rejection happened before report B existed.
	if err := repo.CancelDeathWorkflowForGoat(ctx, wfTenant, wfDead, wfEventAt.Add(time.Hour)); err != nil {
		t.Fatalf("late cancel: %v", err)
	}
	if err := repo.pool.QueryRow(ctx, `SELECT state FROM workflow_instances WHERE workflow_id = $1::uuid`, workflowID).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != domain.WorkflowStateOpen {
		t.Fatalf("state after a late rejection of the OLD report = %q, want open", state)
	}

	// The operator records the steps again and the approval gate now passes.
	completeDeathVideosPg(t, repo, ctx, workflowID, "b")
	if !prepareDeathApproval(t, repo, ctx) {
		t.Fatal("after the re-report's steps are recorded the death approval gate must pass")
	}
}
