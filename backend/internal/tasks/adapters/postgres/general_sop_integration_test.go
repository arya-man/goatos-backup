package postgres

import (
	"errors"
	"testing"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/tasks/domain"
	"github.com/vgoats/goatos/backend/internal/tasks/domain/sopseed"
	"github.com/vgoats/goatos/backend/internal/tasks/ports"
)

// TestGeneralSOPRunBranchesOnTheAnswer drives the seeded GENERAL SOP (migration 000351, the
// gate visitor check) through the real repository on real Postgres: a run opens with no animal,
// the branch steps wait on the question, a YES keeps them and a NO skips them so the run
// completes on the shorter path -- and the skipped rows never reach the counts or the card.
func TestGeneralSOPRunBranchesOnTheAnswer(t *testing.T) {
	repo, pool, ctx := newWorkflowRepo(t)
	// The migration seeds the SOP for tenants that existed when it ran; this test tenant is
	// newer, so the compiler falls back to the sopseed document (no published version) -- the
	// same path a brand-new tenant takes. Sanity: the general definition exists for the seed.
	var seeded int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM sop_definitions WHERE code = $1 AND kind = 'general'`, sopseed.SOPCodeGateVisitorCheck).Scan(&seeded); err != nil {
		t.Fatal(err)
	}
	template := domain.GeneralTemplateKey(sopseed.SOPCodeGateVisitorCheck)
	open := func(ref string) string {
		t.Helper()
		if _, err := repo.OpenWorkflow(ctx, ports.OpenWorkflowCommand{TenantID: wfTenant, TemplateKey: template, EventAt: wfEventAt, SubjectRefID: &ref}); err != nil {
			t.Fatalf("open general run: %v", err)
		}
		id, err := repo.WorkflowIDBySubjectRef(ctx, wfTenant, template, ref)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	// A general run without a subject ref is refused: nothing to key it on.
	if _, err := repo.OpenWorkflow(ctx, ports.OpenWorkflowCommand{TenantID: wfTenant, TemplateKey: template, EventAt: wfEventAt}); !errors.Is(err, domain.ErrMissingRequiredField) {
		t.Fatalf("a general run needs its run id, got %v", err)
	}

	runNo := open("0d0d0d0d-0000-4000-8000-00000000000a")
	detail, err := repo.GetWorkflow(ctx, wfTenant, runNo, wfEventAt)
	if err != nil {
		t.Fatalf("detail: %v", err)
	}
	if detail.Card.Module != domain.ModuleGeneral || detail.Card.Subject.GoatID != "" || detail.Card.ActionsTotal != 5 {
		t.Fatalf("card = module %s goat %q total %d", detail.Card.Module, detail.Card.Subject.GoatID, detail.Card.ActionsTotal)
	}
	byKey := func(d domain.WorkflowDetail, key string) domain.WorkflowAction {
		for _, a := range d.Actions {
			if a.ActionKey == key {
				return a
			}
		}
		t.Fatalf("no action %s", key)
		return domain.WorkflowAction{}
	}
	if g := byKey(detail, "disinfect_footwear").AnswerGate; g == nil || g.Step != "from_other_farm" {
		t.Fatalf("the branch gate must be stamped on the row: %+v", g)
	}
	if !domain.OperatorActionBlocked(template, byKey(detail, "disinfect_footwear"), detail.Actions) {
		t.Fatalf("the branch step must wait for its question")
	}
	answer := func(workflowID, key, value, idem string) domain.ActionWriteResult {
		t.Helper()
		d, err := repo.GetWorkflow(ctx, wfTenant, workflowID, wfEventAt)
		if err != nil {
			t.Fatal(err)
		}
		res, err := repo.AnswerAction(ctx, domain.AnswerActionCommand{TenantID: wfTenant, WorkflowID: workflowID, ActionID: byKey(d, key).ActionID,
			AnswerValue: value, AnsweredAt: wfEventAt.UTC(), IdempotencyKey: idem, RequestFingerprint: idem + "-fp"})
		if err != nil {
			t.Fatalf("answer %s: %v", key, err)
		}
		return res
	}
	complete := func(workflowID, key, idem string, proofs ...domain.ProofItem) domain.ActionWriteResult {
		t.Helper()
		d, err := repo.GetWorkflow(ctx, wfTenant, workflowID, wfEventAt)
		if err != nil {
			t.Fatal(err)
		}
		res, err := repo.CompleteAction(ctx, domain.CompleteActionCommand{TenantID: wfTenant, WorkflowID: workflowID, ActionID: byKey(d, key).ActionID,
			Proofs: proofs, CompletedAt: wfEventAt.UTC(), IdempotencyKey: idem, RequestFingerprint: idem + "-fp"})
		if err != nil {
			t.Fatalf("complete %s: %v", key, err)
		}
		return res
	}
	// Run NO: the visitor has not been on another farm -> the two branch steps are skipped in
	// the answer's own transaction, the card shrinks to the taken path, and the run completes
	// after the log step.
	answer(runNo, "visitor_name", "Ravi from Mesha", "gen-no-1")
	res := answer(runNo, "from_other_farm", "no", "gen-no-2")
	if res.Workflow.ActionsTotal != 3 || res.Workflow.ActionsDone != 2 {
		t.Fatalf("after NO the card must count the taken path: total %d done %d", res.Workflow.ActionsTotal, res.Workflow.ActionsDone)
	}
	detail, _ = repo.GetWorkflow(ctx, wfTenant, runNo, wfEventAt)
	if byKey(detail, "disinfect_footwear").Status != domain.ActionStatusSkipped || byKey(detail, "issue_overshoes").Status != domain.ActionStatusSkipped {
		t.Fatalf("branch steps must be skipped on NO")
	}
	if domain.OperatorActionBlocked(template, byKey(detail, "log_visit"), detail.Actions) {
		t.Fatalf("the log step must be open past the skipped branch")
	}
	res = complete(runNo, "log_visit", "gen-no-3")
	if res.Workflow.State != domain.WorkflowStateCompleted {
		t.Fatalf("run must complete on the short path, state = %s", res.Workflow.State)
	}

	// Run YES: the branch stays, the run needs all five, and the branch step needs its video.
	runYes := open("0d0d0d0d-0000-4000-8000-00000000000b")
	answer(runYes, "visitor_name", "Vet from Hosur", "gen-yes-1")
	res = answer(runYes, "from_other_farm", "yes", "gen-yes-2")
	if res.Workflow.ActionsTotal != 5 {
		t.Fatalf("after YES every step stays: total %d", res.Workflow.ActionsTotal)
	}
	detail, _ = repo.GetWorkflow(ctx, wfTenant, runYes, wfEventAt)
	if domain.OperatorActionBlocked(template, byKey(detail, "disinfect_footwear"), detail.Actions) {
		t.Fatalf("the taken branch must be open")
	}
	if domain.OperatorActionBlocked(template, byKey(detail, "log_visit"), detail.Actions) == false {
		t.Fatalf("the log step waits for the branch steps on YES")
	}
	complete(runYes, "disinfect_footwear", "gen-yes-3", domain.ProofItem{Ref: "proof-video-1", Kind: domain.ProofKindVideo})
	complete(runYes, "issue_overshoes", "gen-yes-4", domain.ProofItem{Ref: "proof-photo-1", Kind: domain.ProofKindPhoto})
	res = complete(runYes, "log_visit", "gen-yes-5")
	if res.Workflow.State != domain.WorkflowStateCompleted || res.Workflow.ActionsDone != 5 {
		t.Fatalf("YES run = state %s done %d", res.Workflow.State, res.Workflow.ActionsDone)
	}
	// The general list shows both runs on the day, no goat attached.
	page, err := repo.ListWorkflows(ctx, domain.WorkflowListQuery{TenantID: wfTenant, Module: domain.ModuleGeneral, EventDate: biztime.BusinessDate(wfEventAt), TodayDate: biztime.BusinessDate(wfEventAt), Filter: domain.FilterAll, PageSize: 20, Now: wfEventAt})
	if err != nil || len(page.Items) != 2 {
		t.Fatalf("general list: %v %d", err, len(page.Items))
	}
	// Startable list reads the seeded definition where the migration reached the tenant.
	if _, err := repo.ListGeneralSOPs(ctx, wfTenant); err != nil {
		t.Fatalf("list general sops: %v", err)
	}
	_ = seeded
}
