package e2e

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	oblapp "github.com/vgoats/goatos/backend/internal/obligation/app"
	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	prooflocal "github.com/vgoats/goatos/backend/internal/proof/adapters/storage/local"
	proofapp "github.com/vgoats/goatos/backend/internal/proof/app"
	proofdomain "github.com/vgoats/goatos/backend/internal/proof/domain"
	sophttp "github.com/vgoats/goatos/backend/internal/sop/adapters/http"
	soppg "github.com/vgoats/goatos/backend/internal/sop/adapters/postgres"
	sopapp "github.com/vgoats/goatos/backend/internal/sop/app"
	sopdomain "github.com/vgoats/goatos/backend/internal/sop/domain"
	sopports "github.com/vgoats/goatos/backend/internal/sop/ports"
	"github.com/vgoats/goatos/backend/internal/sopbridge"
	vaccapp "github.com/vgoats/goatos/backend/internal/vaccination/app"
)

type storyAAGrantSource struct {
	byActor map[string][]permissions.ActiveGrant
}

func (s storyAAGrantSource) ActiveTenantRoles(_ context.Context, userID, _ string) ([]string, error) {
	roles := make([]string, 0, len(s.byActor[userID]))
	for _, grant := range s.byActor[userID] {
		roles = append(roles, grant.Role)
	}
	return roles, nil
}

func (s storyAAGrantSource) ActiveTenantGrants(_ context.Context, userID, _ string) ([]permissions.ActiveGrant, error) {
	return append([]permissions.ActiveGrant(nil), s.byActor[userID]...), nil
}

const canonicalVaccinationSOPVersion = "b0000000-0000-4000-8000-000000000002"

type storyAATaskCreator struct {
	service *sopapp.Service
	actorID string
}

func (c storyAATaskCreator) CreateTaskForBatch(ctx context.Context, tenantID, batchID, sopVersionID, taskType, title, scopeType, scopeID string) (string, error) {
	versionID := sopVersionID
	response, err := c.service.CreateTask(ctx, sopports.CreateTaskCommand{
		TenantID: tenantID,
		ActorID:  c.actorID,
		Body: sopdomain.CreateTaskRequest{
			SOPVersionID: &versionID,
			TaskType:     taskType,
			Title:        title,
			ScopeType:    scopeType,
			ScopeID:      scopeID,
			Priority:     "normal",
			Context:      map[string]any{"created_by": "obligation-sweeper", "obligation_batch_id": batchID},
		},
	}, "story-aa-sweeper")
	if err != nil {
		return "", err
	}
	return response.Task.TaskID, nil
}

// TestKernelStoryAA_OrphanSingletonShedDrive drives Batch story B layer 3: when only one goat is
// due in a shed and no park merge partner exists, SM-4's shed fallback still creates a micro-drive.
func TestKernelStoryAA_OrphanSingletonShedDrive(t *testing.T) {
	fx := NewFixture(t)
	story := NewStory(t, "story-aa", "Orphan singleton: shed micro-drive fallback",
		"Only one goat is due in its shed — below the minimum for a normal shed drive and with no "+
			"park partner to merge. SM-4 layer 3 must still batch it via the orphan singleton shed fallback.")
	story.Certify("authenticated admin-web HTTP + backend kernel + durable outbox envelope + domain consumer")
	defer story.Finish()

	const (
		shedID             = "ed000000-0000-4000-8000-000000000001"
		stageID            = "ed000000-0000-4000-8000-000000000002"
		goatID             = "ed000000-0000-4000-8000-000000000010"
		itemID             = "ed000000-0000-4000-8000-000000000020"
		stockID            = "ed000000-0000-4000-8000-000000000021"
		operatorID         = "ed000000-0000-4000-8000-000000000031"
		verifierID         = "ed000000-0000-4000-8000-000000000032"
		managerID          = "ed000000-0000-4000-8000-000000000033"
		wrongParkManagerID = "ed000000-0000-4000-8000-000000000034"
	)

	fx.SeedShed(shedID, "E2E-AA", stageID)
	versionID, _ := fx.PublishScheduleProtocol("vaccination.e2e.story_aa", `{}`, []RuleSpec{{
		DoseCode: "annual", Sequence: 1, TriggerType: "birth_age", OffsetDays: 21,
		DueWindowDays: 7, Repeat: "yearly", CatchUp: "pc_approval",
	}})

	dob := time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC)
	fx.SeedGoat(GoatSpec{GoatID: goatID, ShedID: shedID, DOB: &dob})
	fx.exec("vaccine item",
		`INSERT INTO inventory_items (item_id, tenant_id, item_code, name, category, base_unit)
		 VALUES ($1, $2, 'VAC-E2E-AA', 'E2E Story AA vaccine', 'vaccine', 'dose')`, itemID, fxTenant)
	fx.exec("vaccine stock",
		`INSERT INTO inventory_stock (stock_id, tenant_id, item_id, location_id, quantity_in_stock, quantity_reserved, quantity_unit, expiry_date)
		 VALUES ($1, $2, $3, $4, 10, 0, 'dose', CURRENT_DATE + INTERVAL '180 days')`,
		stockID, fxTenant, itemID, shedID)

	due := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	fx.PublishGoatEvent(vaccapp.EventGoatCreated, goatID, due)

	story.Step("Sweep with park consolidation enabled but no merge partner",
		"Layer 1 skips the singleton; layer 2 finds no park merge; layer 3 creates a shed micro-drive.")
	sopRepo := soppg.NewRepository(fx.Pool, 5*time.Second)
	sopService := sopapp.NewService(sopRepo)
	sweepCfg := defaultParkSweepConfig()
	sweepCfg.SOPVersionID = canonicalVaccinationSOPVersion
	sweepCfg.VaccineItemID = itemID
	sweepCfg.DosesPerGoat = 1
	sweeper := oblapp.NewSweeperService(fx.Obl, storyAATaskCreator{service: sopService, actorID: operatorID}, fx.Inv)
	sweepRes, err := sweeper.SweepVersion(fx.Ctx, fxTenant, versionID, sweepCfg, time.Date(2026, 7, 5, 0, 0, 0, 0, time.UTC))
	story.Assert("sweep ran without error", err == nil, "err=%v", err)
	story.Assert("singleton goat batched", sweepRes.Obligations == 1, "obligations=%d", sweepRes.Obligations)

	batchID := fx.scanText(`SELECT COALESCE(batch_id::text, '') FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2`, fxTenant, goatID)
	story.Assert("orphan goat is on a batch", batchID != "", "batch_id=%q", batchID)

	scopeType := fx.scanText(`SELECT scope_type FROM obligation_batches WHERE tenant_id=$1 AND batch_id=$2::uuid`, fxTenant, batchID)
	story.Assert("fallback drive is shed-scoped", scopeType == "shed", "scope_type=%q", scopeType)

	taskID := fx.scanText(`SELECT COALESCE(sop_task_id::text, '') FROM obligation_batches WHERE tenant_id=$1 AND batch_id=$2::uuid`, fxTenant, batchID)
	story.Assert("production sweeper created executable SOP task", taskID != "", "task_id=%q", taskID)

	story.Step("Operator uploads proof and submits the exact micro-drive task",
		"The canonical SOP validates three completed task-bound proof artifacts and materializes the recorded dose from the per-goat submission item.")
	proofService := proofapp.NewService(fx.Proof, prooflocal.New(t.TempDir(), "story-aa-proof-secret"))
	proofRefs := make([]sopdomain.ProofReference, 0, 3)
	proofIDs := make(map[string]string, 3)
	for _, subject := range []string{"shed", "vial_lot", "administration"} {
		var subjectID *string
		if subject == "shed" {
			value := shedID
			subjectID = &value
		}
		target, uploadErr := proofService.CreateUpload(fx.Ctx, proofdomain.CreateUpload{
			TenantID: fxTenant, ProofType: "video", MimeType: "video/mp4",
			ScopeType: "task", ScopeID: taskID, SubjectType: subject, SubjectID: subjectID,
			UploadedBy: storyAAPtrString(operatorID), Metadata: map[string]any{"story": "AA"},
		})
		story.Assert("proof upload registered for "+subject, uploadErr == nil, "err=%v", uploadErr)
		if uploadErr != nil {
			continue
		}
		_, uploadErr = proofService.StoreUpload(fx.Ctx, fxTenant, target.Proof.ProofID, "video/mp4", bytes.NewBufferString("story-aa-"+subject))
		story.Assert("proof binary completed for "+subject, uploadErr == nil, "err=%v", uploadErr)
		proofRefs = append(proofRefs, sopdomain.ProofReference{ProofID: target.Proof.ProofID})
		proofIDs[subject] = target.Proof.ProofID
	}

	vaccinationService := vaccapp.NewService(fx.Vacc)
	verificationBus := eventbus.NewInProcessBus()
	vaccapp.NewVerificationHandler(vaccapp.NewCompletionService(vaccinationService, fx.Obl, fx.Inv)).Register(verificationBus)
	sopService.WithProofValidator(proofService).
		WithSubmissionHook(sopbridge.NewVaccinationSubmissionBridge(vaccinationService)).
		WithTaskReviewFanout(sopbridge.NewVerifyFanout(vaccinationService, verificationBus))
	administeredAt := time.Date(2026, 7, 1, 9, 30, 0, 0, time.UTC)
	submitted, submitErr := sopService.SubmitTask(fx.Ctx, sopports.SubmitTaskCommand{
		TenantID: fxTenant, ActorID: operatorID, TaskID: taskID,
		Body: sopdomain.SubmitTaskRequest{
			SOPVersionID:   canonicalVaccinationSOPVersion,
			IdempotencyKey: "story-aa-submit",
			Answers: map[string]any{
				"vaccine_lot_id": stockID, "cold_chain_verified": true,
				"shed_video": proofIDs["shed"], "vial_lot_video": proofIDs["vial_lot"],
				"administration_video": proofIDs["administration"],
				"goat_ids":             []any{goatID}, "dose_ml_given": 1.0, "doses": 1,
				"route_site": "subcutaneous", "administered_at": administeredAt.Format(time.RFC3339),
				"adverse_reaction": false,
			},
			ProofRefs: proofRefs,
		},
	}, "story-aa-submit")
	story.Assert("canonical SOP submission succeeded", submitErr == nil, "err=%v", submitErr)
	if submitErr != nil {
		return
	}
	story.Assert("submission awaits independent review", submitted.Task.State == "needs_review", "state=%q", submitted.Task.State)
	recorded := fx.countRows(`SELECT count(*) FROM vaccination_completions WHERE tenant_id=$1 AND obligation_id=(SELECT obligation_id FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2) AND status='recorded'`, fxTenant, goatID)
	story.Assert("submission fanout created one recorded completion", recorded == 1, "count=%d", recorded)

	story.Step("Role-safe admin-web review accepts and recurrence is projected",
		"Operator and park-scoped leadership grant attempts are denied before mutation. A tenant-scoped Park Head then accepts through the real authenticated admin-web review route; SOP review fans out through SM-5 and writes vaccination.completed.")
	grantSource := storyAAGrantSource{byActor: map[string][]permissions.ActiveGrant{
		operatorID:         {{Role: permissions.RoleOperator, ScopeType: "tenant", ScopeID: fxTenant}},
		managerID:          {{Role: permissions.RoleParkHead, ScopeType: "tenant", ScopeID: fxTenant}},
		wrongParkManagerID: {{Role: permissions.RoleParkHead, ScopeType: "park", ScopeID: "ed000000-0000-4000-8000-000000000099"}},
		verifierID:         {{Role: permissions.RoleParkHead, ScopeType: "park", ScopeID: fxPark}},
	}}
	auth, authErr := httpmiddleware.NewAuthMiddleware(httpmiddleware.AuthConfig{
		Mode: httpmiddleware.AuthModeDevHeaders, DevHeadersAllowed: true, Environment: "test",
	}, nil, grantSource, nil)
	if authErr != nil {
		t.Fatalf("build auth middleware: %v", authErr)
	}
	mux := http.NewServeMux()
	sophttp.Register(mux, sophttp.NewHandler(sopService))
	app := auth.Wrap(mux)
	verify := func(actor string) *httptest.ResponseRecorder {
		body := fmt.Sprintf(`{"reason":"proof accepted","row_version":%d}`, submitted.Task.RowVersion)
		req := httptest.NewRequest(http.MethodPost, "/admin/tasks/"+taskID+"/verify", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set(httpmiddleware.TenantContextHeader, fxTenant)
		req.Header.Set("X-GoatOS-Actor-ID", actor)
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, req)
		return rec
	}
	for _, denied := range []struct{ name, actor string }{{"operator", operatorID}, {"park-scoped leadership grant", wrongParkManagerID}} {
		rec := verify(denied.actor)
		story.Assert(denied.name+" cannot close the drive", rec.Code == http.StatusForbidden, "HTTP=%d body=%s", rec.Code, rec.Body.String())
		story.Assert(denied.name+" denial made no state change", fx.countRows(`SELECT count(*) FROM vaccination_completions WHERE tenant_id=$1 AND batch_id=$2::uuid AND status='accepted'`, fxTenant, batchID) == 0, "batch=%s", batchID)
	}
	acceptedResponse := verify(managerID)
	story.Assert("tenant-scoped Park Head closes through admin-web review route", acceptedResponse.Code == http.StatusOK, "HTTP=%d body=%s", acceptedResponse.Code, acceptedResponse.Body.String())
	if acceptedResponse.Code != http.StatusOK {
		return
	}
	story.Assert("task is accepted", fx.scanText(`SELECT state FROM sop_tasks WHERE tenant_id=$1 AND task_id=$2::uuid`, fxTenant, taskID) == "accepted", "task=%s", taskID)
	obligationID := fx.scanText(`SELECT obligation_id::text FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2 ORDER BY created_at LIMIT 1`, fxTenant, goatID)
	accepted := fx.countRows(`SELECT count(*) FROM vaccination_completions WHERE tenant_id=$1 AND obligation_id=$2::uuid AND status='accepted'`, fxTenant, obligationID)
	story.Assert("SM-5 accepted exactly one completion", accepted == 1, "count=%d", accepted)
	fx.DispatchVaccinationCompleted(obligationID)
	nextCount := fx.countRows(`SELECT count(*) FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2 AND status IN ('scheduled','due')`, fxTenant, goatID)
	story.Assert("SM-7 scheduled exactly one next obligation", nextCount == 1, "count=%d", nextCount)
	nextDue := fx.scanTime(`SELECT due_at FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2 AND status IN ('scheduled','due') ORDER BY due_at LIMIT 1`, fxTenant, goatID)
	story.Assert("yearly recurrence uses administration date plus one year", sameDay(nextDue, administeredAt.AddDate(1, 0, 0)), "due=%s", nextDue)
}

func storyAAPtrString(value string) *string { return &value }
