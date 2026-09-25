package postgres

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/tasks/domain"
	"github.com/vgoats/goatos/backend/internal/tasks/domain/sopseed"
	"github.com/vgoats/goatos/backend/internal/tasks/ports"
)

// SALE WORKFLOW CONDITIONS (maintainer decision 2026-09-25, docs/decisions/sales-sop.md -> "A sale
// without animals" and "A failed sale"), on real Postgres through the production repository path.

// publishV1SaleSOPAndApply000432 installs the 000369 v1 document as the tenant's PUBLISHED
// sales.deal version -- the state every live tenant is in on deploy day -- then runs migration
// 000432's own Up SQL over it, so the test proves the in-place patch, not a hand-written copy.
func publishV1SaleSOPAndApply000432(t *testing.T, repo *Repository) {
	t.Helper()
	ctx := t.Context()
	v1, err := os.ReadFile(filepath.Join("..", "..", "domain", "testdata", "sales_deal_000369.json"))
	if err != nil {
		t.Fatal(err)
	}
	// An AUTHORED version compiles against the tenant's registry rows (the migrations seed them for
	// tenants that existed then; this test tenant is created after), so seed the same rows.
	types, err := sopseed.TaskTypes()
	if err != nil {
		t.Fatal(err)
	}
	for i, tt := range types {
		scope, _ := json.Marshal(tt.CategoryScope)
		schema := string(tt.ParameterSchema)
		if schema == "" {
			schema = "{}"
		}
		if _, err := repo.pool.Exec(ctx, `
INSERT INTO sop_task_types (tenant_id, task_type_key, name, description, category_scope, answer_kind, engine_hook, parameter_schema, sort_order)
VALUES ($1::uuid, $2, $3, $4, $5::jsonb, $6, $7, $8::jsonb, $9)
ON CONFLICT (tenant_id, task_type_key) DO NOTHING`, wfTenant, tt.Key, tt.Name, tt.Description, string(scope), tt.AnswerKind, tt.EngineHook, schema, i+1); err != nil {
			t.Fatalf("seed task type %s: %v", tt.Key, err)
		}
	}
	if _, err := repo.pool.Exec(ctx, `
INSERT INTO sop_definitions (tenant_id, code, name, description, status, category_key, kind, module_key)
VALUES ($1::uuid, 'sales.deal', 'Sale', 'Sale steps', 'active', 'action', 'module', 'sales')
ON CONFLICT (tenant_id, code) DO NOTHING`, wfTenant); err != nil {
		t.Fatalf("seed sop definition: %v", err)
	}
	if _, err := repo.pool.Exec(ctx, `
INSERT INTO sop_versions (tenant_id, sop_id, version, version_label, status, form_dsl, proof_policy, compatibility, validation_report, published_at)
SELECT sd.tenant_id, sd.sop_id, 1, 'Sale v1', 'published',
       jsonb_build_object('schema_version', 'goatos.sop-form.v1', 'sop_code', 'sales.deal', 'title', 'Sale',
                          'fields', jsonb_build_array(), 'follow_up', $2::jsonb),
       '{"subject_scope": "task", "types": ["video", "photo"], "required": false, "minimum_count": 0, "verify_before_apply": false, "approval_before_apply": false}'::jsonb,
       '{"min_app_version": "0.2.0"}'::jsonb, '{"valid": true, "errors": [], "warnings": []}'::jsonb, now()
FROM sop_definitions sd
WHERE sd.tenant_id = $1::uuid AND sd.code = 'sales.deal'
ON CONFLICT (tenant_id, sop_id, version) DO NOTHING`, wfTenant, string(v1)); err != nil {
		t.Fatalf("seed v1 sale sop: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "migrations", "postgres", "000432_sales_sop_sale_has_animals.sql"))
	if err != nil {
		t.Fatal(err)
	}
	up := strings.SplitN(strings.SplitN(string(raw), "-- +goose Down", 2)[0], "-- +goose Up", 2)[1]
	for i := 0; i < 2; i++ { // twice: the patch is idempotent
		if _, err := repo.pool.Exec(ctx, up); err != nil {
			t.Fatalf("apply 000432 (pass %d): %v", i+1, err)
		}
	}
	var conditioned int
	if err := repo.pool.QueryRow(ctx, `
SELECT count(*)::int
FROM sop_versions v
JOIN sop_definitions sd ON sd.tenant_id = v.tenant_id AND sd.sop_id = v.sop_id AND sd.code = 'sales.deal'
CROSS JOIN LATERAL jsonb_array_elements(v.form_dsl->'follow_up'->'tracks') t(track)
CROSS JOIN LATERAL jsonb_array_elements(t.track->'steps') st(step)
WHERE v.tenant_id = $1::uuid AND st.step->>'when' = 'sale_has_animals'`, wfTenant).Scan(&conditioned); err != nil {
		t.Fatal(err)
	}
	if conditioned != 3 {
		t.Fatalf("000432 conditioned %d steps on the published version, want 3", conditioned)
	}
}

func saleActionKeys(t *testing.T, repo *Repository, dealID string) (string, []string) {
	t.Helper()
	ctx := t.Context()
	workflowID, err := repo.WorkflowIDBySubjectRef(ctx, wfTenant, domain.TemplateKeySalesDeal, dealID)
	if err != nil {
		t.Fatalf("workflow for %s: %v", dealID, err)
	}
	detail, err := repo.GetWorkflow(ctx, wfTenant, workflowID, wfEventAt)
	if err != nil {
		t.Fatal(err)
	}
	keys := make([]string, 0, len(detail.Actions))
	for _, a := range detail.Actions {
		keys = append(keys, a.ActionKey)
	}
	return workflowID, keys
}

// TestSaleWithoutAnimalsOpensWithoutAnUnfinishableTagStep: a manure / feed / other-item sale, or
// an animal sale with no head count, opened with the tag step it could never finish -- the card
// stayed open and overdue forever. After the fix the patched published version opens such a sale
// with its payment steps only; an animal sale and an OLD event (no has_live_animals key) open
// with every step.
func TestSaleWithoutAnimalsOpensWithoutAnUnfinishableTagStep(t *testing.T) {
	repo, _, ctx := newWorkflowRepo(t)
	publishV1SaleSOPAndApply000432(t, repo)
	no, yes := false, true
	cases := []struct {
		deal       string
		hasAnimals *bool
		want       string
	}{
		{"5a1e5a1e-0000-4000-8000-00000000a001", &no, "full_payment,collect_balance"},
		{"5a1e5a1e-0000-4000-8000-00000000a002", &yes, "tag_animals,loading_video,dispatch_note,full_payment,collect_balance"},
		{"5a1e5a1e-0000-4000-8000-00000000a003", nil, "tag_animals,loading_video,dispatch_note,full_payment,collect_balance"},
	}
	for _, c := range cases {
		ref := c.deal
		if _, err := repo.OpenWorkflow(ctx, ports.OpenWorkflowCommand{TenantID: wfTenant, TemplateKey: domain.TemplateKeySalesDeal,
			EventAt: wfEventAt, SubjectRefID: &ref, SaleHasAnimals: c.hasAnimals}); err != nil {
			t.Fatalf("open %s: %v", c.deal, err)
		}
		_, keys := saleActionKeys(t, repo, c.deal)
		if got := strings.Join(keys, ","); got != c.want {
			t.Fatalf("deal %s (has animals %v) opened %s, want %s", c.deal, c.hasAnimals, got, c.want)
		}
	}
	// The manure sale's card counts only the payment question (the balance step is branch-gated).
	workflowID, _ := saleActionKeys(t, repo, cases[0].deal)
	detail, err := repo.GetWorkflow(ctx, wfTenant, workflowID, wfEventAt)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Card.NextAction == nil || detail.Card.NextAction.Key != "full_payment" {
		t.Fatalf("manure sale's next step = %+v, want full_payment", detail.Card.NextAction)
	}
}

// TestDealFailedCancelsItsSaleWorkflow: "Deal Failed" never reached the tasks engine, so a failed
// deal's workflow stayed open. The cancel keeps a finished step's record, cancels every unfinished
// one, closes the card, and is idempotent; a deal with no workflow is a no-op.
func TestDealFailedCancelsItsSaleWorkflow(t *testing.T) {
	repo, _, ctx := newWorkflowRepo(t)
	const dealID = "5a1e5a1e-0000-4000-8000-00000000b001"
	ref := dealID
	if _, err := repo.OpenWorkflow(ctx, ports.OpenWorkflowCommand{TenantID: wfTenant, TemplateKey: domain.TemplateKeySalesDeal, EventAt: wfEventAt, SubjectRefID: &ref}); err != nil {
		t.Fatal(err)
	}
	workflowID, _ := saleActionKeys(t, repo, dealID)
	detail, _ := repo.GetWorkflow(ctx, wfTenant, workflowID, wfEventAt)
	var loadID string
	for _, a := range detail.Actions {
		if a.ActionKey == "loading_video" {
			loadID = a.ActionID
		}
	}
	// The tagging confirm lands (a finished step), then the loading video.
	if err := repo.CompleteSaleTagStep(ctx, wfTenant, dealID, wfEventAt.UTC()); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.CompleteAction(ctx, domain.CompleteActionCommand{TenantID: wfTenant, WorkflowID: workflowID, ActionID: loadID,
		Proofs: []domain.ProofItem{{Ref: "proof-video-load", Kind: domain.ProofKindVideo}}, CompletedAt: wfEventAt.UTC(),
		IdempotencyKey: "fail-load", RequestFingerprint: "fail-load-fp", ActorRoles: []string{"park_head"}}); err != nil {
		t.Fatalf("loading video: %v", err)
	}

	for i := 0; i < 2; i++ { // replay is a no-op
		if err := repo.CancelSaleWorkflow(ctx, wfTenant, dealID); err != nil {
			t.Fatalf("cancel (pass %d): %v", i+1, err)
		}
	}
	if err := repo.CancelSaleWorkflow(ctx, wfTenant, "5a1e5a1e-0000-4000-8000-0000000000ee"); err != nil {
		t.Fatalf("a deal with no workflow must be a no-op, got %v", err)
	}
	detail, err := repo.GetWorkflow(ctx, wfTenant, workflowID, wfEventAt)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Card.State != domain.WorkflowStateCanceled || detail.Card.NextAction != nil {
		t.Fatalf("failed deal's card = state %s next %+v, want canceled with no next step", detail.Card.State, detail.Card.NextAction)
	}
	for _, a := range detail.Actions {
		want := domain.ActionStatusCanceled
		if a.ActionKey == "loading_video" || a.ActionKey == "tag_animals" {
			want = domain.ActionStatusCompleted
		}
		if a.Status != want {
			t.Fatalf("step %s = %s, want %s", a.ActionKey, a.Status, want)
		}
	}
	// A cancelled sale step can no longer be worked.
	var payID string
	for _, a := range detail.Actions {
		if a.ActionKey == "full_payment" {
			payID = a.ActionID
		}
	}
	if _, err := repo.AnswerAction(ctx, domain.AnswerActionCommand{TenantID: wfTenant, WorkflowID: workflowID, ActionID: payID,
		AnswerValue: "yes", AnsweredAt: wfEventAt.UTC(), IdempotencyKey: "fail-pay", RequestFingerprint: "fail-pay-fp", ActorRoles: []string{"procurement_director"}}); !errors.Is(err, domain.ErrActionCanceled) {
		t.Fatalf("a step of a cancelled sale workflow must refuse an answer with ErrActionCanceled, got %v", err)
	}
}

// TestRepair000433UnsticksExistingSaleWorkflows runs migration 000433's own Up SQL over workflows
// opened the OLD way (every step, whatever was sold): a manure sale (legacy animal_count = 1 on an
// 'other' line) and an animal line with no head count lose their unfinished tag / loading / gate
// pass steps, a failed deal's workflow is cancelled, a real animal sale is untouched, and a second
// run changes nothing.
func TestRepair000433UnsticksExistingSaleWorkflows(t *testing.T) {
	repo, pool, ctx := newWorkflowRepo(t)
	deals := []struct {
		id, product, kind, status string
		count                     *int
	}{
		{"5a1e5a1e-0000-4000-8000-00000000c001", "Manure", "other", "Deal Closed", intPtr(1)},
		{"5a1e5a1e-0000-4000-8000-00000000c002", "Goat", "animal", "Deal Closed", nil},
		{"5a1e5a1e-0000-4000-8000-00000000c003", "Goat", "animal", "Deal Failed", intPtr(4)},
		{"5a1e5a1e-0000-4000-8000-00000000c004", "Goat", "animal", "Deal Closed", intPtr(3)},
	}
	for _, d := range deals {
		if _, err := pool.Exec(ctx, `
INSERT INTO sales_deals (id, tenant_id, sale_date, farm, buyer_name, product_type, breed, animal_count, sales_value, status)
VALUES ($1::uuid, $2::uuid, DATE '2026-09-20', 'CBE', 'Repair buyer', $3, 'Any', $4, 1000, $5)`, d.id, wfTenant, d.product, d.count, d.status); err != nil {
			t.Fatalf("seed deal: %v", err)
		}
		if _, err := pool.Exec(ctx, `
INSERT INTO sales_deal_lines (tenant_id, deal_id, line_no, product_type, breed, animal_count, sales_value, product_code, product_kind)
VALUES ($1::uuid, $2::uuid, 1, $3, 'Any', $4, 1000, lower($3), $5)`, wfTenant, d.id, d.product, d.count, d.kind); err != nil {
			t.Fatalf("seed line: %v", err)
		}
		ref := d.id
		if _, err := repo.OpenWorkflow(ctx, ports.OpenWorkflowCommand{TenantID: wfTenant, TemplateKey: domain.TemplateKeySalesDeal, EventAt: wfEventAt, SubjectRefID: &ref}); err != nil {
			t.Fatalf("open %s: %v", d.id, err)
		}
	}
	// Adversarial date shift: the no-animal sale's payment step carries its OWN scheduled date,
	// three days after the tag step it replaces as "next", so a card that copied the skipped
	// step's due date (or no date) is caught below.
	if _, err := pool.Exec(ctx, `
UPDATE workflow_actions a SET due_at = a.due_at + interval '3 days'
FROM workflow_instances wi
WHERE wi.tenant_id = a.tenant_id AND wi.workflow_id = a.workflow_id
  AND wi.subject_ref_id = $2::uuid AND a.tenant_id = $1::uuid AND a.action_key = 'full_payment'`, wfTenant, deals[0].id); err != nil {
		t.Fatalf("shift payment due date: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "migrations", "postgres", "000433_sales_workflow_repair_stuck_steps.sql"))
	if err != nil {
		t.Fatal(err)
	}
	up := strings.SplitN(strings.SplitN(string(raw), "-- +goose Down", 2)[0], "-- +goose Up", 2)[1]
	if _, err := pool.Exec(ctx, up); err != nil {
		t.Fatalf("apply 000433: %v", err)
	}
	type card struct {
		state, statuses string
		total, done     int
		next            *string
		version         int
	}
	read := func(dealID string) card {
		t.Helper()
		var c card
		if err := pool.QueryRow(ctx, `
SELECT wi.state, wi.actions_total, wi.actions_done, wi.next_action_key, wi.row_version,
       (SELECT string_agg(a.action_key || '=' || a.status, ',' ORDER BY a.seq) FROM workflow_actions a
         WHERE a.tenant_id = wi.tenant_id AND a.workflow_id = wi.workflow_id)
FROM workflow_instances wi
WHERE wi.tenant_id = $1::uuid AND wi.template_key = 'sales_deal' AND wi.subject_ref_id = $2::uuid`, wfTenant, dealID).
			Scan(&c.state, &c.total, &c.done, &c.next, &c.version, &c.statuses); err != nil {
			t.Fatal(err)
		}
		return c
	}
	noAnimals := "tag_animals=skipped,loading_video=skipped,dispatch_note=skipped,full_payment=pending,collect_balance=pending"
	for _, d := range deals[:2] {
		c := read(d.id)
		if c.state != "open" || c.statuses != noAnimals || c.total != 2 || c.done != 0 || c.next == nil || *c.next != "full_payment" {
			t.Fatalf("deal %s (%s) after repair = %+v (next %v), want the payment steps only", d.id, d.product, c, c.next)
		}
	}
	var nextDue, paymentScheduledDate *time.Time
	if err := pool.QueryRow(ctx, `
SELECT wi.next_due_at, a.due_at FROM workflow_instances wi
JOIN workflow_actions a ON a.tenant_id = wi.tenant_id AND a.workflow_id = wi.workflow_id AND a.action_key = 'full_payment'
WHERE wi.tenant_id = $1::uuid AND wi.subject_ref_id = $2::uuid`, wfTenant, deals[0].id).Scan(&nextDue, &paymentScheduledDate); err != nil {
		t.Fatal(err)
	}
	if (nextDue == nil) != (paymentScheduledDate == nil) || (nextDue != nil && !nextDue.Equal(*paymentScheduledDate)) {
		t.Fatalf("repaired card next_due_at = %v, want the payment step's own ScheduledDate %v", nextDue, paymentScheduledDate)
	}
	if c := read(deals[2].id); c.state != "canceled" || c.next != nil || strings.Contains(c.statuses, "pending") {
		t.Fatalf("failed deal after repair = %+v, want cancelled with no step owed", c)
	}
	animal := read(deals[3].id)
	if animal.state != "open" || !strings.HasPrefix(animal.statuses, "tag_animals=pending") || animal.total != 5 {
		t.Fatalf("a real animal sale must be untouched, got %+v", animal)
	}
	before := map[string]card{}
	for _, d := range deals {
		before[d.id] = read(d.id)
	}
	if _, err := pool.Exec(ctx, up); err != nil {
		t.Fatalf("re-apply 000433: %v", err)
	}
	for _, d := range deals {
		after, was := read(d.id), before[d.id]
		if after.state != was.state || after.statuses != was.statuses || after.total != was.total ||
			after.done != was.done || after.version != was.version || (after.next == nil) != (was.next == nil) ||
			(after.next != nil && *after.next != *was.next) {
			t.Fatalf("a second run changed deal %s: %+v -> %+v", d.id, before[d.id], after)
		}
	}
}

func intPtr(v int) *int { return &v }
