package postgres

import (
	"errors"
	"testing"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/tasks/domain"
	"github.com/vgoats/goatos/backend/internal/tasks/ports"
)

// TestSaleWorkflowRunsTheSalesSOP drives the SALE workflow (migration 000366, docs/decisions/
// sales-sop.md) through the real repository on real Postgres, the production path the two
// consumers take: the sales.deal.recorded opener keys a goat-less workflow on the deal, every
// step is stamped with the designation the SOP names, a caller holding another designation is
// refused while the park head is not, the tag step is completed by the allocation confirm and
// never by hand, the money question skips the balance step on YES, and the whole thing reads
// back by subject with the sale's buyer and animal count on the card.
func TestSaleWorkflowRunsTheSalesSOP(t *testing.T) {
	repo, pool, ctx := newWorkflowRepo(t)
	const dealID = "5a1e5a1e-0000-4000-8000-000000000001"
	// The sales_deals row the card enriches from (a display join, 1:0..1 on the deal's id).
	if _, err := pool.Exec(ctx, `
INSERT INTO sales_deals (id, tenant_id, sale_date, farm, buyer_name, product_type, breed, animal_count, sales_value, status)
VALUES ($1::uuid, $2::uuid, DATE '2026-07-27', 'CBE', 'Kumar Traders', 'Goat', 'Malai', 12, 150000, 'Deal Closed')`, dealID, wfTenant); err != nil {
		t.Fatalf("seed deal: %v", err)
	}

	ref := dealID
	// Goat-less, keyed on the deal: the opener refuses a missing subject ref and never needs an animal.
	if _, err := repo.OpenWorkflow(ctx, ports.OpenWorkflowCommand{TenantID: wfTenant, TemplateKey: domain.TemplateKeySalesDeal, EventAt: wfEventAt}); !errors.Is(err, domain.ErrMissingRequiredField) {
		t.Fatalf("a sale workflow needs its deal, got %v", err)
	}
	created, err := repo.OpenWorkflow(ctx, ports.OpenWorkflowCommand{TenantID: wfTenant, TemplateKey: domain.TemplateKeySalesDeal, EventAt: wfEventAt, SubjectRefID: &ref})
	if err != nil || !created {
		t.Fatalf("open sale workflow: created=%v err=%v", created, err)
	}
	// A redelivered recorded event opens nothing.
	if again, err := repo.OpenWorkflow(ctx, ports.OpenWorkflowCommand{TenantID: wfTenant, TemplateKey: domain.TemplateKeySalesDeal, EventAt: wfEventAt, SubjectRefID: &ref}); err != nil || again {
		t.Fatalf("second open must be a no-op: created=%v err=%v", again, err)
	}
	workflowID, err := repo.WorkflowIDBySubjectRef(ctx, wfTenant, domain.TemplateKeySalesDeal, dealID)
	if err != nil {
		t.Fatal(err)
	}
	detail, err := repo.GetWorkflow(ctx, wfTenant, workflowID, wfEventAt)
	if err != nil {
		t.Fatalf("detail: %v", err)
	}
	if detail.Card.Module != domain.ModuleSales || detail.Card.Subject.GoatID != "" || detail.Card.ActionsTotal != 5 || detail.Card.SubjectRefID != dealID {
		t.Fatalf("card = module %s goat %q total %d ref %q", detail.Card.Module, detail.Card.Subject.GoatID, detail.Card.ActionsTotal, detail.Card.SubjectRefID)
	}
	if detail.Card.SubjectLabel != "Kumar Traders · 12 animals · CBE" {
		t.Fatalf("subject label = %q", detail.Card.SubjectLabel)
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
	// Every step carries its designation, stamped from the SOP, with the catalog's label.
	if tag := byKey(detail, "tag_animals"); tag.OwnerRole != "park_head" || tag.OwnerLabel != "Park Head" || !tag.HasHook(domain.EngineHookSaleTagAnimals) {
		t.Fatalf("tag step = owner %q label %q hook %q", tag.OwnerRole, tag.OwnerLabel, tag.EngineHook)
	}
	if pay := byKey(detail, "full_payment"); pay.OwnerRole != "procurement_director" {
		t.Fatalf("payment step owner = %q", pay.OwnerRole)
	}

	// The tag step is engine-owned: a park head tapping it is refused, an operator is refused
	// for being the wrong designation, and only the allocation confirm completes it.
	tagID := byKey(detail, "tag_animals").ActionID
	_, err = repo.CompleteAction(ctx, domain.CompleteActionCommand{TenantID: wfTenant, WorkflowID: workflowID, ActionID: tagID,
		CompletedAt: wfEventAt.UTC(), IdempotencyKey: "sale-tag-tap", RequestFingerprint: "sale-tag-tap-fp", ActorRoles: []string{"park_head"}})
	if !errors.Is(err, domain.ErrSaleTaggingPending) {
		t.Fatalf("a tap on the tag step must be refused with ErrSaleTaggingPending, got %v", err)
	}
	_, err = repo.CompleteAction(ctx, domain.CompleteActionCommand{TenantID: wfTenant, WorkflowID: workflowID, ActionID: tagID,
		CompletedAt: wfEventAt.UTC(), IdempotencyKey: "sale-tag-op", RequestFingerprint: "sale-tag-op-fp", ActorRoles: []string{"operator"}})
	if !errors.Is(err, domain.ErrStepForOtherRole) {
		t.Fatalf("an operator on the park head's step must be refused, got %v", err)
	}
	if err := repo.CompleteSaleTagStep(ctx, wfTenant, dealID, wfEventAt.UTC()); err != nil {
		t.Fatalf("engine completion: %v", err)
	}
	if err := repo.CompleteSaleTagStep(ctx, wfTenant, dealID, wfEventAt.UTC()); err != nil {
		t.Fatalf("engine completion replay: %v", err)
	}
	if err := repo.CompleteSaleTagStep(ctx, wfTenant, "5a1e5a1e-0000-4000-8000-0000000000ff", wfEventAt.UTC()); err != nil {
		t.Fatalf("a confirm for a sale with no workflow is a no-op, got %v", err)
	}
	detail, _ = repo.GetWorkflow(ctx, wfTenant, workflowID, wfEventAt)
	if byKey(detail, "tag_animals").Status != domain.ActionStatusCompleted || detail.Card.ActionsDone != 1 {
		t.Fatalf("tag step after confirm = %s, done %d", byKey(detail, "tag_animals").Status, detail.Card.ActionsDone)
	}

	complete := func(key, idem string, roles []string, proofs ...domain.ProofItem) (domain.ActionWriteResult, error) {
		t.Helper()
		d, err := repo.GetWorkflow(ctx, wfTenant, workflowID, wfEventAt)
		if err != nil {
			t.Fatal(err)
		}
		return repo.CompleteAction(ctx, domain.CompleteActionCommand{TenantID: wfTenant, WorkflowID: workflowID, ActionID: byKey(d, key).ActionID,
			Proofs: proofs, CompletedAt: wfEventAt.UTC(), IdempotencyKey: idem, RequestFingerprint: idem + "-fp", ActorRoles: roles})
	}
	// The park head loads the animals and photographs the gate pass; the CEO floor could too.
	if _, err := complete("loading_video", "sale-load", []string{"park_head"}, domain.ProofItem{Ref: "proof-video-load", Kind: domain.ProofKindVideo}); err != nil {
		t.Fatalf("park head loading video: %v", err)
	}
	if _, err := complete("dispatch_note", "sale-pass", []string{"ceo_internal"}, domain.ProofItem{Ref: "proof-photo-pass", Kind: domain.ProofKindPhoto}); err != nil {
		t.Fatalf("ceo floor on the park head's step: %v", err)
	}
	// The money question is the sales desk's: a park head is refused, the desk answers YES and
	// the balance step is skipped in the same transaction, completing the sale's work.
	detail, _ = repo.GetWorkflow(ctx, wfTenant, workflowID, wfEventAt)
	payID := byKey(detail, "full_payment").ActionID
	_, err = repo.AnswerAction(ctx, domain.AnswerActionCommand{TenantID: wfTenant, WorkflowID: workflowID, ActionID: payID,
		AnswerValue: "yes", AnsweredAt: wfEventAt.UTC(), IdempotencyKey: "sale-pay-ph", RequestFingerprint: "sale-pay-ph-fp", ActorRoles: []string{"park_head"}})
	if !errors.Is(err, domain.ErrStepForOtherRole) {
		t.Fatalf("a park head on the desk's question must be refused, got %v", err)
	}
	res, err := repo.AnswerAction(ctx, domain.AnswerActionCommand{TenantID: wfTenant, WorkflowID: workflowID, ActionID: payID,
		AnswerValue: "yes", AnsweredAt: wfEventAt.UTC(), IdempotencyKey: "sale-pay", RequestFingerprint: "sale-pay-fp", ActorRoles: []string{"procurement_director"}})
	if err != nil {
		t.Fatalf("desk answers: %v", err)
	}
	if res.Workflow.State != domain.WorkflowStateCompleted || res.Workflow.ActionsTotal != 4 || res.Workflow.ActionsDone != 4 {
		t.Fatalf("after YES the sale's work is done on the short path: state %s total %d done %d", res.Workflow.State, res.Workflow.ActionsTotal, res.Workflow.ActionsDone)
	}
	detail, _ = repo.GetWorkflow(ctx, wfTenant, workflowID, wfEventAt)
	if byKey(detail, "collect_balance").Status != domain.ActionStatusSkipped {
		t.Fatalf("balance step must be skipped on YES, got %s", byKey(detail, "collect_balance").Status)
	}
	// The sales list shows the sale on its day, and the subject read finds it by deal.
	page, err := repo.ListWorkflows(ctx, domain.WorkflowListQuery{TenantID: wfTenant, Module: domain.ModuleSales, EventDate: biztime.BusinessDate(wfEventAt), TodayDate: biztime.BusinessDate(wfEventAt), Filter: domain.FilterAll, PageSize: 20, Now: wfEventAt})
	if err != nil || len(page.Items) != 1 || page.Items[0].SubjectRefID != dealID || page.Items[0].SubjectLabel == "" {
		t.Fatalf("sales list: err=%v items=%d", err, len(page.Items))
	}
	if _, err := repo.WorkflowIDBySubjectRef(ctx, wfTenant, domain.TemplateKeySalesDeal, "5a1e5a1e-0000-4000-8000-0000000000ff"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("an unrecorded sale has no workflow, got %v", err)
	}
}
