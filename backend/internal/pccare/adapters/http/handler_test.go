package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/pccare/app"
	"github.com/vgoats/goatos/backend/internal/pccare/domain"
	"github.com/vgoats/goatos/backend/internal/pccare/ports"
	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
)

const (
	httpTenant = "9c000000-0000-4000-8000-00000000aaaa"
	httpActor  = "9c000000-0000-4000-8000-00000000bbbb"
	httpTask   = "9c000000-0000-4000-8000-00000000cccc"
)

type fakePCCareHTTPService struct {
	lastActor        domain.Actor
	lastTaskProof    app.RegisterTaskProofInput
	lastStockVerdict app.StockVerdictInput
	stockVerdictErr  error
	stockVerdictTask ports.TaskRow
	plannerCatalog   ports.PlannerCatalog
	lastCreate       app.CreateTaskInput
	createErr        error
}

func TestOpenAPIIncludesInventoryVaccineTaskProofContract(t *testing.T) {
	raw, err := os.ReadFile("../../../../../contracts/openapi/app-api.yaml")
	if err != nil {
		t.Fatalf("read app-api.yaml: %v", err)
	}
	spec := string(raw)
	for _, want := range []string{
		"inventory_vaccine",
		"/app/pc-care/tasks/{task_id}/proofs/{slot}:",
		"operationId: appRegisterPCCareTaskProof",
		"stock_fridge_video",
		"task_proof",
		"inventory_requirements:",
		"task_proofs:",
		"PCCareInventoryRequirement:",
		"required_doses:",
	} {
		if !strings.Contains(spec, want) {
			t.Fatalf("contracts/openapi/app-api.yaml missing %q", want)
		}
	}
	for _, stale := range []string{"required_count:", "vaccine_key:"} {
		if strings.Contains(spec, stale) {
			t.Fatalf("contracts/openapi/app-api.yaml still contains stale inventory requirement key %q", stale)
		}
	}
}

func TestTaskDTOIncludesInventoryVaccineWireShape(t *testing.T) {
	dto := taskDTOFrom(ports.TaskRow{
		TaskID:              httpTask,
		Category:            domain.CategoryInventoryVaccine,
		ParkID:              "9c000000-0000-4000-8000-00000000dddd",
		ParkName:            "CPT",
		ShedID:              "9c000000-0000-4000-8000-00000000eeee",
		ShedName:            "Mandela 1",
		PartitionLabel:      "Part 7",
		PlannedBusinessDate: "2026-08-26",
		DueBusinessDate:     "2026-08-26",
		WorkState:           "scheduled",
		Status:              "open",
		RowVersion:          3,
		AnimalCount:         0,
		InventoryRequirements: []ports.InventoryRequirement{{
			VaccineLabel:   "PPR",
			RequiredDoses:  25,
			SourceBatchIDs: []string{"batch-ppr"},
		}},
		TaskProofs: []ports.TaskProofRow{{
			SlotKey:        domain.SlotStockFridgePhoto,
			ProofRef:       "proof-fridge-stock-photo",
			CapturedBy:     httpActor,
			CapturedByName: "Chandrakant",
		}, {
			SlotKey:        domain.SlotStockFridgeVideo,
			ProofRef:       "proof-fridge-stock-video",
			CapturedBy:     httpActor,
			CapturedByName: "Chandrakant",
		}},
	})

	raw, err := json.Marshal(dto)
	if err != nil {
		t.Fatalf("marshal task dto: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal task dto: %v", err)
	}
	if got["category"] != domain.CategoryInventoryVaccine || got["capture_mode"] != domain.CaptureModeTaskProof {
		t.Fatalf("category/capture_mode = %v/%v, want inventory_vaccine/task_proof", got["category"], got["capture_mode"])
	}
	slots, ok := got["expected_slots"].([]any)
	if !ok || len(slots) != 2 ||
		slots[0].(map[string]any)["field_key"] != domain.SlotStockFridgePhoto ||
		slots[1].(map[string]any)["field_key"] != domain.SlotStockFridgeVideo {
		t.Fatalf("expected_slots = %#v, want stock_fridge_photo + stock_fridge_video", got["expected_slots"])
	}
	reqs, ok := got["inventory_requirements"].([]any)
	if !ok || len(reqs) != 1 {
		t.Fatalf("inventory_requirements = %#v, want one requirement", got["inventory_requirements"])
	}
	req := reqs[0].(map[string]any)
	if req["vaccine_label"] != "PPR" || req["required_doses"] != float64(25) {
		t.Fatalf("inventory requirement = %#v, want PPR/25", req)
	}
	if _, bad := req["required_count"]; bad {
		t.Fatalf("inventory requirement used stale required_count key: %#v", req)
	}
	if _, bad := req["vaccine_key"]; bad {
		t.Fatalf("inventory requirement used stale vaccine_key key: %#v", req)
	}
	proofs, ok := got["task_proofs"].([]any)
	if !ok || len(proofs) != 2 {
		t.Fatalf("task_proofs = %#v, want photo and video task proofs", got["task_proofs"])
	}
	firstProof := proofs[0].(map[string]any)
	secondProof := proofs[1].(map[string]any)
	if firstProof["slot_key"] != domain.SlotStockFridgePhoto || firstProof["proof_ref"] != "proof-fridge-stock-photo" ||
		secondProof["slot_key"] != domain.SlotStockFridgeVideo || secondProof["proof_ref"] != "proof-fridge-stock-video" ||
		secondProof["captured_by_name"] != "Chandrakant" {
		t.Fatalf("task proofs = %#v, want independent stock photo/video proofs", proofs)
	}
}

func (f *fakePCCareHTTPService) PlannerCatalog(_ context.Context, actor domain.Actor) (ports.PlannerCatalog, error) {
	f.lastActor = actor
	return f.plannerCatalog, nil
}
func (f *fakePCCareHTTPService) PlannerParkSheds(context.Context, domain.Actor, string, string, string, string, int) (ports.PlannerParkSheds, error) {
	return ports.PlannerParkSheds{}, nil
}
func (f *fakePCCareHTTPService) CreateTask(_ context.Context, _ domain.Actor, in app.CreateTaskInput) (ports.TaskRow, error) {
	f.lastCreate = in
	if f.createErr != nil {
		return ports.TaskRow{}, f.createErr
	}
	return ports.TaskRow{TaskID: httpTask, Category: in.Category}, nil
}
func (f *fakePCCareHTTPService) CancelTask(context.Context, domain.Actor, string, string) error {
	return nil
}
func (f *fakePCCareHTTPService) ListTasks(context.Context, domain.Actor, string, string, string, string, int, bool) (ports.TaskPage, error) {
	return ports.TaskPage{}, nil
}
func (f *fakePCCareHTTPService) Worklist(context.Context, domain.Actor, string, string, string, int) (ports.TaskPage, error) {
	return ports.TaskPage{}, nil
}
func (f *fakePCCareHTTPService) GetTask(context.Context, domain.Actor, string) (ports.TaskRow, error) {
	return ports.TaskRow{}, nil
}
func (f *fakePCCareHTTPService) ListTaskAnimals(context.Context, domain.Actor, string, string, int) ([]ports.AnimalRow, string, error) {
	return nil, "", nil
}
func (f *fakePCCareHTTPService) ScanAnimal(context.Context, domain.Actor, app.ScanAnimalInput) (ports.ScanAnimalResult, error) {
	return ports.ScanAnimalResult{}, nil
}
func (f *fakePCCareHTTPService) RegisterSlotProof(context.Context, domain.Actor, app.RegisterSlotProofInput) error {
	return nil
}
func (f *fakePCCareHTTPService) RegisterTaskProof(_ context.Context, actor domain.Actor, in app.RegisterTaskProofInput) error {
	f.lastActor = actor
	f.lastTaskProof = in
	return nil
}
func (f *fakePCCareHTTPService) SubmitTask(context.Context, domain.Actor, app.SubmitTaskInput) (ports.SubmitTaskResult, error) {
	return ports.SubmitTaskResult{}, nil
}
func (f *fakePCCareHTTPService) TaskRoster(context.Context, domain.Actor, string, string, int) (ports.TaskRosterPage, error) {
	return ports.TaskRosterPage{}, nil
}
func (f *fakePCCareHTTPService) RecordStockVerdict(_ context.Context, actor domain.Actor, in app.StockVerdictInput) (ports.TaskRow, error) {
	f.lastActor = actor
	f.lastStockVerdict = in
	if f.stockVerdictErr != nil {
		return ports.TaskRow{}, f.stockVerdictErr
	}
	return f.stockVerdictTask, nil
}

// The stock-verdict route forwards the director's decision verbatim and maps the module's
// verdict sentinels onto their stable machine codes.
func TestStockVerdictRouteForwardsAndMapsErrors(t *testing.T) {
	do := func(service *fakePCCareHTTPService, body string) *httptest.ResponseRecorder {
		mux := http.NewServeMux()
		Register(mux, NewHandler(service, nil))
		req := httptest.NewRequest(http.MethodPost, "/app/pc-care/tasks/"+httpTask+"/stock-verdict", strings.NewReader(body))
		ctx := httpmiddleware.WithTenantID(req.Context(), httpTenant)
		ctx = httpmiddleware.WithActorID(ctx, httpActor)
		ctx = httpmiddleware.WithAuthGrants(ctx, []permissions.ActiveGrant{{
			Role:      permissions.RolePCDirector,
			ScopeType: "tenant",
			ScopeID:   httpTenant,
		}})
		req = req.WithContext(ctx)
		rec := httptest.NewRecorder()
		httpmiddleware.RequestContext(nil)(mux).ServeHTTP(rec, req)
		return rec
	}

	service := &fakePCCareHTTPService{stockVerdictTask: ports.TaskRow{
		TaskID: httpTask, Category: domain.CategoryInventoryVaccine, Status: domain.StatusCompleted,
	}}
	rec := do(service, `{"verdict":"approve"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("approve status=%d body=%s, want 200", rec.Code, rec.Body.String())
	}
	if service.lastStockVerdict.TaskID != httpTask || service.lastStockVerdict.Verdict != domain.StockVerdictApprove {
		t.Fatalf("forwarded verdict = %+v, want approve on %s", service.lastStockVerdict, httpTask)
	}

	rec = do(service, `{"verdict":"reject","reason":"The video does not show the FMD stock"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("reject status=%d body=%s, want 200", rec.Code, rec.Body.String())
	}
	if service.lastStockVerdict.Verdict != domain.StockVerdictReject ||
		service.lastStockVerdict.Reason != "The video does not show the FMD stock" {
		t.Fatalf("forwarded reject = %+v, want reason carried verbatim", service.lastStockVerdict)
	}

	for _, tc := range []struct {
		err      error
		wantCode int
		wantBody string
	}{
		{domain.ErrNotStockTask, http.StatusUnprocessableEntity, "not_stock_task"},
		{domain.ErrStockVerdictNotPending, http.StatusConflict, "verdict_not_pending"},
		{domain.ErrInvalidStockVerdict, http.StatusUnprocessableEntity, "invalid_verdict"},
		{domain.ErrStockRejectReasonRequired, http.StatusUnprocessableEntity, "reason_required"},
	} {
		rec := do(&fakePCCareHTTPService{stockVerdictErr: tc.err}, `{"verdict":"approve"}`)
		if rec.Code != tc.wantCode || !strings.Contains(rec.Body.String(), tc.wantBody) {
			t.Fatalf("error %v -> status=%d body=%s, want %d %q", tc.err, rec.Code, rec.Body.String(), tc.wantCode, tc.wantBody)
		}
	}
}

func TestPlannerCatalogExcludesKernelOwnedInventoryVaccineCategory(t *testing.T) {
	service := &fakePCCareHTTPService{plannerCatalog: ports.PlannerCatalog{
		Parks:     []ports.PlannerPark{{ParkID: "9c000000-0000-4000-8000-000000001001", ParkName: "CPT"}},
		Operators: []ports.PlannerOperator{{UserID: httpActor, DisplayName: "Amit", ParkIDs: []string{}}},
		// The service narrows this per actor (a CEO gets every planner category); the
		// adapter must label exactly what it is handed and never widen it.
		Categories: domain.PlannerCategories,
	}}
	mux := http.NewServeMux()
	Register(mux, NewHandler(service, nil))

	req := httptest.NewRequest(http.MethodGet, "/app/pc-care/planner/catalog", nil)
	ctx := httpmiddleware.WithTenantID(req.Context(), httpTenant)
	ctx = httpmiddleware.WithActorID(ctx, httpActor)
	ctx = httpmiddleware.WithAuthGrants(ctx, []permissions.ActiveGrant{{
		Role:      permissions.RoleCEOInternal,
		ScopeType: "tenant",
		ScopeID:   httpTenant,
	}})
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	httpmiddleware.RequestContext(nil)(mux).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s, want 200", rec.Code, rec.Body.String())
	}
	var got struct {
		Categories []categoryDTO `json:"categories"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode planner catalog: %v", err)
	}
	seen := map[string]bool{}
	for _, category := range got.Categories {
		seen[category.Key] = true
	}
	if seen[domain.CategoryInventoryVaccine] {
		t.Fatalf("planner catalog categories = %+v, must not offer kernel-owned inventory_vaccine", got.Categories)
	}
	for _, category := range domain.PlannerCategories {
		if !seen[category] {
			t.Fatalf("planner catalog categories = %+v, missing plannable category %s", got.Categories, category)
		}
	}
}

func TestPutTaskProofRoutesInventoryFridgeProofToApp(t *testing.T) {
	service := &fakePCCareHTTPService{}
	mux := http.NewServeMux()
	Register(mux, NewHandler(service, nil))

	req := httptest.NewRequest(
		http.MethodPut,
		"/app/pc-care/tasks/"+httpTask+"/proofs/"+domain.SlotStockFridgeVideo,
		strings.NewReader(`{"proof_ref":"proof-fridge-stock"}`),
	)
	req.Header.Set("Idempotency-Key", "idem-task-proof-1")
	req.Header.Set("traceparent", "trace-task-proof")
	ctx := httpmiddleware.WithTenantID(req.Context(), httpTenant)
	ctx = httpmiddleware.WithActorID(ctx, httpActor)
	ctx = httpmiddleware.WithAuthGrants(ctx, []permissions.ActiveGrant{{
		Role:      permissions.RoleOperator,
		ScopeType: "tenant",
		ScopeID:   httpTenant,
	}})
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	httpmiddleware.RequestContext(nil)(mux).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s, want 200", rec.Code, rec.Body.String())
	}
	if service.lastActor.TenantID != httpTenant || service.lastActor.UserID != httpActor || len(service.lastActor.Roles) != 1 || service.lastActor.Roles[0] != permissions.RoleOperator {
		t.Fatalf("actor = %+v, want operator actor from request context", service.lastActor)
	}
	got := service.lastTaskProof
	if got.TaskID != httpTask ||
		got.SlotKey != domain.SlotStockFridgeVideo ||
		got.ProofRef != "proof-fridge-stock" ||
		got.IdempotencyKey != "idem-task-proof-1" ||
		got.ActorID != httpActor ||
		got.ActorType != "operator" ||
		got.TraceID != "trace-task-proof" {
		t.Fatalf("task proof input = %+v, want Android fridge proof registration", got)
	}
}

// TestActorCarriesThePerPersonPermissionSetIntoTheService pins the adapter half of the
// route/service agreement (PR #181 review finding PC-181-002): when the person's own access
// rows decided the route, AuthMiddleware leaves that permission set on the context, and
// actor() must hand it to the service unchanged -- otherwise a person ticked pc_trimming at
// Configure without the breeding_director job is route-green and service-403.
func TestActorCarriesThePerPersonPermissionSetIntoTheService(t *testing.T) {
	service := &fakePCCareHTTPService{plannerCatalog: ports.PlannerCatalog{Categories: domain.TrimmingCategories}}
	mux := http.NewServeMux()
	Register(mux, NewHandler(service, nil))

	req := httptest.NewRequest(http.MethodGet, "/app/pc-care/planner/catalog", nil)
	ctx := httpmiddleware.WithTenantID(req.Context(), httpTenant)
	ctx = httpmiddleware.WithActorID(ctx, httpActor)
	// The grant roles alone carry NO planning capability...
	ctx = httpmiddleware.WithAuthGrants(ctx, []permissions.ActiveGrant{{Role: permissions.RoleOperator, ScopeType: "park", ScopeID: "9c000000-0000-4000-8000-000000001001"}})
	// ...the person's ticks do.
	ctx = httpmiddleware.WithPersonPermissions(ctx, []string{permissions.PCCareMonitor, permissions.PCCarePlanTrimming})
	rec := httptest.NewRecorder()
	httpmiddleware.RequestContext(nil)(mux).ServeHTTP(rec, req.WithContext(ctx))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !service.lastActor.PermissionsResolved {
		t.Fatal("actor.PermissionsResolved must be true when the middleware attached a per-person set")
	}
	holds := false
	for _, p := range service.lastActor.Permissions {
		holds = holds || p == permissions.PCCarePlanTrimming
	}
	if !holds {
		t.Fatalf("actor.Permissions = %v, want pc_care.plan_trimming carried through", service.lastActor.Permissions)
	}

	// And on the role path nothing is invented: no set attached, none resolved.
	req = httptest.NewRequest(http.MethodGet, "/app/pc-care/planner/catalog", nil)
	ctx = httpmiddleware.WithTenantID(req.Context(), httpTenant)
	ctx = httpmiddleware.WithActorID(ctx, httpActor)
	ctx = httpmiddleware.WithAuthGrants(ctx, []permissions.ActiveGrant{{Role: permissions.RoleCEOInternal, ScopeType: "tenant", ScopeID: httpTenant}})
	httpmiddleware.RequestContext(nil)(mux).ServeHTTP(httptest.NewRecorder(), req.WithContext(ctx))
	if service.lastActor.PermissionsResolved || len(service.lastActor.Permissions) != 0 {
		t.Fatalf("role-path actor must carry no per-person set, got resolved=%v perms=%v", service.lastActor.PermissionsResolved, service.lastActor.Permissions)
	}
}
