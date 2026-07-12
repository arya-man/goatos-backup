package e2e

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	oblapp "github.com/vgoats/goatos/backend/internal/obligation/app"
	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	sophttp "github.com/vgoats/goatos/backend/internal/sop/adapters/http"
	vaccapp "github.com/vgoats/goatos/backend/internal/vaccination/app"
)

// TestKernelStoryAJ_LeadershipReviewRoutes proves the current admin-web leadership review closure
// path without bypassing the vaccination kernel. A real drive is generated, proof-backed
// submission records the dose, operator/scoped-grant HTTP attempts are denied before mutation, a
// Director reworks it via the admin-web route, the operator re-submits, and CEO/CXO accepts via
// the same admin-web review route. The accept path fans out through SM-5, emits durable
// vaccination.completed, and SM-7 creates exactly one next yearly obligation.
func TestKernelStoryAJ_LeadershipReviewRoutes(t *testing.T) {
	fx := NewFixture(t)
	story := NewStory(t, "story-aj", "Leadership admin-web review closes real vaccination drives",
		"A real vaccination drive goes through proof-backed SOP submission. Operators and park-scoped "+
			"leadership grants cannot close it through tenant-level admin review routes. A Director can "+
			"request rework through the admin-web review route; after the operator re-submits, CEO/CXO "+
			"accepts through the same admin-web route. Only the "+
			"accepted leadership review completes the obligation and schedules the next yearly cycle.")
	story.Certify("authenticated admin-web HTTP + SOP proof/submission/rework/review + vaccination.completed consumer")
	defer story.Finish()

	const (
		shedID     = "a9000000-0000-4000-8000-000000000001"
		stageID    = "a9000000-0000-4000-8000-000000000002"
		goatID     = "a9000000-0000-4000-8000-000000000003"
		operatorID = "a9000000-0000-4000-8000-000000000004"
		managerID  = "a9000000-0000-4000-8000-000000000005"
		directorID = "a9000000-0000-4000-8000-000000000006"
		cxoID      = "a9000000-0000-4000-8000-000000000007"
		itemID     = "a9000000-0000-4000-8000-000000000008"
		lotID      = "a9000000-0000-4000-8000-000000000009"
	)

	story.Step("Generate one yearly vaccination drive through production generation and sweep",
		"The story seeds only input facts: shed, goat, rule, and vaccine stock. The obligation, batch, "+
			"and SOP task are produced by the generation kernel and SM-4 sweeper.")
	fx.SeedShed(shedID, "E2E-AJ", stageID)
	dob := time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC)
	fx.SeedGoat(GoatSpec{GoatID: goatID, ShedID: shedID, DOB: &dob})
	versionID, ruleIDs := fx.PublishScheduleProtocol("vaccination.e2e.story_aj", "{}", []RuleSpec{{
		DoseCode: "annual", Sequence: 1, TriggerType: "birth_age", OffsetDays: 21,
		DueWindowDays: 7, Repeat: "yearly", CatchUp: "pc_approval",
	}})
	fx.exec("vaccine item",
		`INSERT INTO inventory_items (item_id, tenant_id, item_code, name, category, base_unit)
		 VALUES ($1, $2, 'VAC-E2E-AJ', 'E2E Story AJ vaccine', 'vaccine', 'dose')`, itemID, fxTenant)
	fx.exec("vaccine stock",
		`INSERT INTO inventory_stock (stock_id, tenant_id, item_id, location_id, quantity_in_stock, quantity_reserved, quantity_unit, expiry_date)
		 VALUES ($1, $2, $3, $4, 10, 0, 'dose', CURRENT_DATE + INTERVAL '10 years')`, lotID, fxTenant, itemID, shedID)

	due := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	gen := vaccapp.NewGenerationService(fx.Proto, fx.Vacc, fx.Obl)
	genRes, err := gen.GenerateForVersion(fx.Ctx, fxTenant, versionID, due)
	story.Assert("generation produced exactly one due obligation", err == nil && genRes.Generated == 1, "generated=%d err=%v", genRes.Generated, err)
	if err != nil {
		return
	}
	h := newVaccinationSOPHarness(t, fx)
	sweeper := oblapp.NewSweeperService(fx.Obl, storyAATaskCreator{service: h.Service, actorID: operatorID}, fx.Inv)
	sweepRes, err := sweeper.SweepVersion(fx.Ctx, fxTenant, versionID, oblapp.SweepConfig{
		SOPVersionID: canonicalVaccinationSOPVersion, VaccineItemID: itemID, DosesPerGoat: 1,
	}, due.AddDate(0, 0, 1))
	story.Assert("sweeper created one executable drive", err == nil && sweepRes.Batches == 1 && sweepRes.Obligations == 1,
		"batches=%d obligations=%d err=%v", sweepRes.Batches, sweepRes.Obligations, err)
	if err != nil {
		return
	}
	obligationID := fx.scanText(`SELECT obligation_id::text FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2 AND rule_id=$3`, fxTenant, goatID, ruleIDs["annual"])
	taskID := fx.scanText(`SELECT ob.sop_task_id::text FROM obligation_instances oi JOIN obligation_batches ob ON ob.tenant_id=oi.tenant_id AND ob.batch_id=oi.batch_id WHERE oi.tenant_id=$1 AND oi.obligation_id=$2::uuid`, fxTenant, obligationID)
	batchID := fx.scanText(`SELECT batch_id::text FROM obligation_instances WHERE tenant_id=$1 AND obligation_id=$2::uuid`, fxTenant, obligationID)
	story.Assert("drive has obligation, batch, and task ids", obligationID != "" && batchID != "" && taskID != "", "obligation=%s batch=%s task=%s", obligationID, batchID, taskID)

	story.Step("Operator submits proof-backed vaccination through SOP",
		"The operator supplies the exact goat, lot, cold-chain, route-site, administered-at, and proof refs. "+
			"Submission fanout creates a recorded completion but does not complete the obligation yet.")
	administeredAt := due.Add(9 * time.Hour)
	first, err := h.submit(taskID, shedID, operatorID, lotID, []string{goatID}, administeredAt, "story-aj-submit-1", "subcutaneous")
	firstState := ""
	firstRowVersion := 0
	if first != nil {
		firstState = first.Task.State
		firstRowVersion = first.Task.RowVersion
	}
	story.Assert("first proof-backed submission succeeded", err == nil && first != nil && first.Task.State == "needs_review", "task_state=%q row_version=%d err=%v", firstState, firstRowVersion, err)
	if err != nil {
		return
	}
	story.Assert("submission recorded one unaccepted completion",
		fx.countRows(`SELECT count(*) FROM vaccination_completions WHERE tenant_id=$1 AND obligation_id=$2::uuid AND status='recorded'`, fxTenant, obligationID) == 1,
		"obligation=%s", obligationID)

	story.Step("Admin-web review gates reject operator and park-scoped leadership grant before mutation",
		"The current review surface is the admin-web SOP task verify/rework route. Operators cannot close a vaccination drive there, and park-scoped grants do not authorize tenant-level admin review routes.")
	grantSource := storyAAGrantSource{byActor: map[string][]permissions.ActiveGrant{
		operatorID: {{Role: permissions.RoleOperator, ScopeType: "tenant", ScopeID: fxTenant}},
		managerID:  {{Role: permissions.RoleParkHead, ScopeType: "park", ScopeID: fxPark}},
		directorID: {{Role: permissions.RolePCDirector, ScopeType: "tenant", ScopeID: fxTenant}},
		cxoID:      {{Role: permissions.RoleCEOInternal, ScopeType: "tenant", ScopeID: fxTenant}},
	}}
	auth, authErr := httpmiddleware.NewAuthMiddleware(httpmiddleware.AuthConfig{
		Mode: httpmiddleware.AuthModeDevHeaders, DevHeadersAllowed: true, Environment: "test",
	}, nil, grantSource, nil)
	if authErr != nil {
		t.Fatalf("build auth middleware: %v", authErr)
	}
	mux := http.NewServeMux()
	sophttp.Register(mux, sophttp.NewHandler(h.Service))
	app := auth.Wrap(mux)
	review := func(method, path, actor string, rowVersion int, reason string) *httptest.ResponseRecorder {
		body := fmt.Sprintf(`{"reason":%q,"row_version":%d}`, reason, rowVersion)
		req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set(httpmiddleware.TenantContextHeader, fxTenant)
		req.Header.Set("X-GoatOS-Actor-ID", actor)
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, req)
		return rec
	}
	for _, denied := range []struct {
		label, actor, route string
	}{
		{"operator admin verify", operatorID, "/admin/tasks/" + taskID + "/verify"},
		{"park-scoped leadership grant admin rework", managerID, "/admin/tasks/" + taskID + "/rework"},
	} {
		rec := review(http.MethodPost, denied.route, denied.actor, first.Task.RowVersion, "not authorized")
		story.Assert(denied.label+" denied", rec.Code == http.StatusForbidden, "HTTP=%d body=%s", rec.Code, rec.Body.String())
		story.Assert(denied.label+" made no accepted/rejected mutation",
			fx.countRows(`SELECT count(*) FROM vaccination_completions WHERE tenant_id=$1 AND obligation_id=$2::uuid AND status IN ('accepted','rejected')`, fxTenant, obligationID) == 0,
			"obligation=%s", obligationID)
	}

	story.Step("Director reworks through the admin-web review route",
		"The admin-web review route writes the real SOP review state. Rework rejects the recorded completion, leaves the obligation open, and consumes no stock.")
	rec := review(http.MethodPost, "/admin/tasks/"+taskID+"/rework", directorID, first.Task.RowVersion, "director requests clearer proof")
	story.Assert("Director can rework through admin-web route", rec.Code == http.StatusOK, "HTTP=%d body=%s", rec.Code, rec.Body.String())
	if rec.Code != http.StatusOK {
		return
	}
	story.Assert("task moved to rework_requested", fx.scanText(`SELECT state FROM sop_tasks WHERE tenant_id=$1 AND task_id=$2::uuid`, fxTenant, taskID) == "rework_requested", "task=%s", taskID)
	story.Assert("rework rejected exactly one recorded completion",
		fx.countRows(`SELECT count(*) FROM vaccination_completions WHERE tenant_id=$1 AND obligation_id=$2::uuid AND status='rejected'`, fxTenant, obligationID) == 1,
		"obligation=%s", obligationID)
	story.Assert("rework did not complete the obligation or consume stock",
		fx.scanText(`SELECT status FROM obligation_instances WHERE tenant_id=$1 AND obligation_id=$2::uuid`, fxTenant, obligationID) != "completed" &&
			fx.countRows(`SELECT count(*) FROM inventory_stock_movements WHERE tenant_id=$1 AND batch_id=$2::uuid AND movement_type='consume'`, fxTenant, batchID) == 0,
		"obligation=%s batch=%s", obligationID, batchID)

	story.Step("Operator re-submits and CEO/CXO verifies through the admin-web route",
		"The second submission is a fresh proof-backed attempt. CEO/CXO closes it through the current admin-web review route; SM-5 accepts the completion and completes the obligation.")
	second, err := h.submit(taskID, shedID, operatorID, lotID, []string{goatID}, administeredAt.Add(30*time.Minute), "story-aj-submit-2", "subcutaneous")
	secondState := ""
	secondRowVersion := 0
	if second != nil {
		secondState = second.Task.State
		secondRowVersion = second.Task.RowVersion
	}
	story.Assert("second proof-backed submission succeeded", err == nil && second != nil && second.Task.State == "needs_review", "task_state=%q row_version=%d err=%v", secondState, secondRowVersion, err)
	if err != nil {
		return
	}
	rec = review(http.MethodPost, "/admin/tasks/"+taskID+"/verify", cxoID, second.Task.RowVersion, "CEO/CXO accepted corrected proof")
	story.Assert("CEO/CXO can verify through admin-web route", rec.Code == http.StatusOK, "HTTP=%d body=%s", rec.Code, rec.Body.String())
	if rec.Code != http.StatusOK {
		return
	}
	story.Assert("task accepted, obligation completed, stock consumed once",
		fx.scanText(`SELECT state FROM sop_tasks WHERE tenant_id=$1 AND task_id=$2::uuid`, fxTenant, taskID) == "accepted" &&
			fx.scanText(`SELECT status FROM obligation_instances WHERE tenant_id=$1 AND obligation_id=$2::uuid`, fxTenant, obligationID) == "completed" &&
			fx.countRows(`SELECT count(*) FROM inventory_stock_movements WHERE tenant_id=$1 AND batch_id=$2::uuid AND movement_type='consume'`, fxTenant, batchID) == 1,
		"task=%s obligation=%s batch=%s", taskID, obligationID, batchID)

	story.Step("Durable vaccination.completed schedules exactly one next yearly cycle",
		"After leadership acceptance, the next obligation is created by SM-7 from the actual corrected administration time; no test SQL seeds the next cycle.")
	fx.DispatchVaccinationCompleted(obligationID)
	nextCount := fx.countRows(`SELECT count(*) FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2 AND rule_id=$3 AND status IN ('scheduled','due')`, fxTenant, goatID, ruleIDs["annual"])
	nextDue := fx.scanTime(`SELECT due_at FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2 AND rule_id=$3 AND status IN ('scheduled','due') ORDER BY due_at LIMIT 1`, fxTenant, goatID, ruleIDs["annual"])
	wantNext := administeredAt.Add(30*time.Minute).AddDate(1, 0, 0)
	story.Assert("SM-7 created exactly one yearly recurrence from the accepted administration date",
		nextCount == 1 && sameDay(nextDue, wantNext),
		"count=%d due=%s want=%s", nextCount, nextDue.Format("2006-01-02"), wantNext.Format("2006-01-02"))
}
