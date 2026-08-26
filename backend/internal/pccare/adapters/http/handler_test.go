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
	lastActor      domain.Actor
	lastTaskProof  app.RegisterTaskProofInput
	plannerCatalog ports.PlannerCatalog
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

func (f *fakePCCareHTTPService) PlannerCatalog(context.Context, domain.Actor) (ports.PlannerCatalog, error) {
	return f.plannerCatalog, nil
}
func (f *fakePCCareHTTPService) PlannerParkSheds(context.Context, domain.Actor, string, string, string, string, int) (ports.PlannerParkSheds, error) {
	return ports.PlannerParkSheds{}, nil
}
func (f *fakePCCareHTTPService) CreateTask(context.Context, domain.Actor, app.CreateTaskInput) (ports.TaskRow, error) {
	return ports.TaskRow{}, nil
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

func TestPlannerCatalogExcludesKernelOwnedInventoryVaccineCategory(t *testing.T) {
	service := &fakePCCareHTTPService{plannerCatalog: ports.PlannerCatalog{
		Parks:     []ports.PlannerPark{{ParkID: "9c000000-0000-4000-8000-000000001001", ParkName: "CPT"}},
		Operators: []ports.PlannerOperator{{UserID: httpActor, DisplayName: "Amit", ParkIDs: []string{}}},
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
