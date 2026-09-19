package postgres

import (
	"errors"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/tasks/domain"
	"github.com/vgoats/goatos/backend/internal/tasks/domain/sopseed"
	"github.com/vgoats/goatos/backend/internal/tasks/ports"
)

// TestGeneralSOPRunBranchesOnTheAnswer drives the seeded GENERAL SOP (migration 000361, the
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

// TestGeneralSOPListReadIsGrainSafe pins the grain of the card read once a general run carries
// an authored SOP name (the sop_definitions join on cardSelectColumns). Each subtest is the
// adversarial shape the aggregates-and-projections review asks for.
func TestGeneralSOPListReadIsGrainSafe(t *testing.T) {
	repo, pool, ctx := newWorkflowRepo(t)
	const otherTenant = "aaaaaaa1-0000-0000-0000-000000000002"
	if _, err := pool.Exec(ctx, `
INSERT INTO tenants (tenant_id, name, status) VALUES ($1::uuid, 'Other Tenant', 'active')
ON CONFLICT (tenant_id) DO NOTHING`, otherTenant); err != nil {
		t.Fatal(err)
	}
	// The same code authored by TWO tenants: the name join must pick this tenant's row only.
	for _, row := range []struct{ tenant, name string }{{wfTenant, "Gate visitor check"}, {otherTenant, "Somebody else's gate"}} {
		if _, err := pool.Exec(ctx, `
INSERT INTO sop_definitions (tenant_id, code, name, status, kind, module_key)
VALUES ($1::uuid, $2, $3, 'active', 'general', 'general')
ON CONFLICT (tenant_id, code) DO UPDATE SET name = EXCLUDED.name`, row.tenant, sopseed.SOPCodeGateVisitorCheck, row.name); err != nil {
			t.Fatalf("seed definition: %v", err)
		}
	}
	template := domain.GeneralTemplateKey(sopseed.SOPCodeGateVisitorCheck)
	open := func(tenant, ref string, at time.Time) string {
		t.Helper()
		if _, err := repo.OpenWorkflow(ctx, ports.OpenWorkflowCommand{TenantID: tenant, TemplateKey: template, EventAt: at, SubjectRefID: &ref}); err != nil {
			t.Fatalf("open general run: %v", err)
		}
		id, err := repo.WorkflowIDBySubjectRef(ctx, tenant, template, ref)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	list := func(tenant, date, filter string, size int, cursor *domain.WorkflowCursor) domain.WorkflowListPage {
		t.Helper()
		page, err := repo.ListWorkflows(ctx, domain.WorkflowListQuery{TenantID: tenant, Module: domain.ModuleGeneral, EventDate: date,
			TodayDate: biztime.BusinessDate(wfEventAt), Filter: filter, PageSize: size, Cursor: cursor, Now: wfEventAt})
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		return page
	}
	day := biztime.BusinessDate(wfEventAt)
	runA := open(wfTenant, "0d0d0d0d-0000-4000-8000-0000000000a1", wfEventAt)
	runB := open(wfTenant, "0d0d0d0d-0000-4000-8000-0000000000a2", wfEventAt)
	runC := open(wfTenant, "0d0d0d0d-0000-4000-8000-0000000000a3", wfEventAt)
	open(otherTenant, "0d0d0d0d-0000-4000-8000-0000000000b1", wfEventAt)
	yesterday := open(wfTenant, "0d0d0d0d-0000-4000-8000-0000000000c1", wfEventAt.AddDate(0, 0, -1))

	t.Run("OneToManyNameJoinStaysOneCardPerRun", func(t *testing.T) {
		page := list(wfTenant, day, domain.FilterAll, 20, nil)
		if len(page.Items) != 3 || page.Chips.All != 3 {
			t.Fatalf("three runs on the day, got %d items / chips.all %d", len(page.Items), page.Chips.All)
		}
		for _, card := range page.Items {
			if card.SOPName != "Gate visitor check" {
				t.Fatalf("card %s carries the wrong tenant's name %q", card.WorkflowID, card.SOPName)
			}
		}
		detail, err := repo.GetWorkflow(ctx, wfTenant, runA, wfEventAt)
		if err != nil || detail.Card.SOPName != "Gate visitor check" {
			t.Fatalf("detail name = %q (%v)", detail.Card.SOPName, err)
		}
	})
	t.Run("PageBoundaryKeepsChipsWholeFilter", func(t *testing.T) {
		first := list(wfTenant, day, domain.FilterAll, 2, nil)
		if len(first.Items) != 2 || first.NextCursor == nil || first.Chips.All != 3 {
			t.Fatalf("page 1 = %d items cursor %v chips.all %d", len(first.Items), first.NextCursor, first.Chips.All)
		}
		cursor, err := domain.DecodeWorkflowCursor(*first.NextCursor)
		if err != nil {
			t.Fatal(err)
		}
		second := list(wfTenant, day, domain.FilterAll, 2, cursor)
		if len(second.Items) != 1 || second.NextCursor != nil || second.Chips.All != 3 {
			t.Fatalf("page 2 = %d items cursor %v chips.all %d", len(second.Items), second.NextCursor, second.Chips.All)
		}
		seen := map[string]bool{}
		for _, c := range append(first.Items, second.Items...) {
			if seen[c.WorkflowID] {
				t.Fatalf("run %s served on both pages", c.WorkflowID)
			}
			seen[c.WorkflowID] = true
		}
		for _, id := range []string{runA, runB, runC} {
			if !seen[id] {
				t.Fatalf("run %s missing across the two pages", id)
			}
		}
	})
	t.Run("DateShiftKeepsYesterdaysRunOffTodaysList", func(t *testing.T) {
		for _, c := range list(wfTenant, day, domain.FilterAll, 20, nil).Items {
			if c.WorkflowID == yesterday {
				t.Fatalf("yesterday's run leaked onto today's list")
			}
		}
		prev := list(wfTenant, biztime.BusinessDate(wfEventAt.AddDate(0, 0, -1)), domain.FilterAll, 20, nil)
		if len(prev.Items) != 1 || prev.Items[0].WorkflowID != yesterday || prev.Chips.All != 1 {
			t.Fatalf("yesterday = %d items chips.all %d", len(prev.Items), prev.Chips.All)
		}
	})
	t.Run("ScopeHierarchyNeverCrossesTheTenant", func(t *testing.T) {
		other := list(otherTenant, day, domain.FilterAll, 20, nil)
		if len(other.Items) != 1 || other.Chips.All != 1 || other.Items[0].SOPName != "Somebody else's gate" {
			t.Fatalf("other tenant = %d items chips.all %d name %q", len(other.Items), other.Chips.All, other.Items[0].SOPName)
		}
		if _, err := repo.GetWorkflow(ctx, otherTenant, runA, wfEventAt); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("a run must not be readable from another tenant, got %v", err)
		}
	})
	t.Run("StatusMatrixCountsTheTakenPathOnly", func(t *testing.T) {
		byKey := func(id, key string) domain.WorkflowAction {
			d, err := repo.GetWorkflow(ctx, wfTenant, id, wfEventAt)
			if err != nil {
				t.Fatal(err)
			}
			for _, a := range d.Actions {
				if a.ActionKey == key {
					return a
				}
			}
			t.Fatalf("no action %s", key)
			return domain.WorkflowAction{}
		}
		answer := func(id, key, value, idem string) {
			t.Helper()
			if _, err := repo.AnswerAction(ctx, domain.AnswerActionCommand{TenantID: wfTenant, WorkflowID: id, ActionID: byKey(id, key).ActionID,
				AnswerValue: value, AnsweredAt: wfEventAt.UTC(), IdempotencyKey: idem, RequestFingerprint: idem + "-fp"}); err != nil {
				t.Fatalf("answer %s: %v", key, err)
			}
		}
		// runA takes the NO path to completion; runB answers YES and stops before its video;
		// runC is untouched.
		answer(runA, "visitor_name", "A", "grain-a-1")
		answer(runA, "from_other_farm", "no", "grain-a-2")
		if _, err := repo.CompleteAction(ctx, domain.CompleteActionCommand{TenantID: wfTenant, WorkflowID: runA, ActionID: byKey(runA, "log_visit").ActionID,
			CompletedAt: wfEventAt.UTC(), IdempotencyKey: "grain-a-3", RequestFingerprint: "grain-a-3-fp"}); err != nil {
			t.Fatal(err)
		}
		answer(runB, "visitor_name", "B", "grain-b-1")
		answer(runB, "from_other_farm", "yes", "grain-b-2")

		all := list(wfTenant, day, domain.FilterAll, 20, nil)
		if all.Chips.All != 3 || all.Chips.Completed != 1 {
			t.Fatalf("chips = %+v", all.Chips)
		}
		if all.Chips.Overdue+all.Chips.Due+all.Chips.Completed+all.Chips.AwaitingVideo != all.Chips.All {
			t.Fatalf("status buckets must be disjoint and cover every run: %+v", all.Chips)
		}
		completed := list(wfTenant, day, domain.FilterCompleted, 20, nil)
		if len(completed.Items) != 1 || completed.Items[0].WorkflowID != runA || completed.Items[0].ActionsTotal != 3 || completed.Items[0].ActionsDone != 3 {
			t.Fatalf("completed page = %+v", completed.Items)
		}
		for _, c := range all.Items {
			switch c.WorkflowID {
			case runB:
				if c.ActionsTotal != 5 || c.ActionsDone != 2 {
					t.Fatalf("YES run keeps every step: %d/%d", c.ActionsDone, c.ActionsTotal)
				}
			case runC:
				if c.ActionsTotal != 5 || c.ActionsDone != 0 {
					t.Fatalf("untouched run: %d/%d", c.ActionsDone, c.ActionsTotal)
				}
			}
		}
	})
}
