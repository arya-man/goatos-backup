package app

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/pccare/domain"
	"github.com/vgoats/goatos/backend/internal/pccare/ports"
	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
)

const (
	testTenant   = "9c000000-0000-4000-8000-00000000aaaa"
	testTask     = "9c000000-0000-4000-8000-00000000bbbb"
	testAssignee = "9c000000-0000-4000-8000-00000000cccc"
	testOutsider = "9c000000-0000-4000-8000-00000000dddd"
	testAnimal   = "9c000000-0000-4000-8000-00000000eeee"
)

// fakeStore is a minimal TaskStore for app-gate tests.
type fakeStore struct {
	ports.TaskStore
	assignees      map[string]bool
	scanCalls      int
	submitCalls    int
	slotCalls      int
	taskProofCalls int
	lastTaskProof  ports.RegisterTaskProofParams
	submitResult   ports.SubmitTaskResult
	applyCalls     int
	bounceCalls    int
	lastBounce     ports.BounceTaskParams
	lastList       ports.ListTasksQuery
	listResult     ports.TaskPage
	submitOverride func() (ports.SubmitTaskResult, error)
}

func (f *fakeStore) IsAssignee(_ context.Context, _, _, userID string) (bool, error) {
	return f.assignees[userID], nil
}
func (f *fakeStore) ScanAnimal(_ context.Context, _ ports.ScanAnimalParams) (ports.ScanAnimalResult, error) {
	f.scanCalls++
	return ports.ScanAnimalResult{AnimalRowID: testAnimal}, nil
}
func (f *fakeStore) RegisterSlotProof(_ context.Context, _ ports.RegisterSlotProofParams) error {
	f.slotCalls++
	return nil
}
func (f *fakeStore) RegisterTaskProof(_ context.Context, p ports.RegisterTaskProofParams) error {
	f.taskProofCalls++
	f.lastTaskProof = p
	return nil
}
func (f *fakeStore) SubmitTask(_ context.Context, _ ports.SubmitTaskParams) (ports.SubmitTaskResult, error) {
	f.submitCalls++
	if f.submitOverride != nil {
		return f.submitOverride()
	}
	return f.submitResult, nil
}
func (f *fakeStore) ApplyVerifiedTask(_ context.Context, _ ports.ApplyVerifiedTaskParams) (bool, error) {
	f.applyCalls++
	return true, nil
}
func (f *fakeStore) BounceTaskForRework(_ context.Context, p ports.BounceTaskParams) (bool, error) {
	f.bounceCalls++
	f.lastBounce = p
	return true, nil
}
func (f *fakeStore) ListTasks(_ context.Context, q ports.ListTasksQuery) (ports.TaskPage, error) {
	f.lastList = q
	return f.listResult, nil
}

type fakeEnqueuer struct {
	calls    int
	lastKeys []string
	last     VerificationEnqueueRequest
}

func (f *fakeEnqueuer) EnqueuePCCareVerification(_ context.Context, in VerificationEnqueueRequest) error {
	f.calls++
	f.lastKeys = append(f.lastKeys, in.IdempotencyKey)
	f.last = in
	return nil
}

type fakeProofValidator struct {
	mediaCalls int
	videoCalls int
	lastTenant string
	lastProofs []string
	err        error
}

func (f *fakeProofValidator) ValidateLiveCameraVideos(_ context.Context, tenantID string, proofIDs []string) error {
	f.videoCalls++
	f.lastTenant = tenantID
	f.lastProofs = append([]string(nil), proofIDs...)
	return f.err
}

func (f *fakeProofValidator) ValidateLiveCameraMedia(_ context.Context, tenantID string, proofIDs []string) error {
	f.mediaCalls++
	f.lastTenant = tenantID
	f.lastProofs = append([]string(nil), proofIDs...)
	return f.err
}

func operatorActor(userID string) domain.Actor {
	return domain.Actor{TenantID: testTenant, UserID: userID, Roles: []string{permissions.RoleOperator}}
}

func TestCreateTaskRejectsKernelOwnedInventoryVaccineCategory(t *testing.T) {
	store := &fakeStore{}
	svc := NewService(store)
	ctx := httpmiddleware.WithAuthGrants(context.Background(), []permissions.ActiveGrant{{
		Role: permissions.RoleCEOInternal, ScopeType: "tenant", ScopeID: testTenant,
	}})
	actor := domain.Actor{TenantID: testTenant, UserID: "ceo-1", Roles: []string{permissions.RoleCEOInternal}}

	_, err := svc.CreateTask(ctx, actor, CreateTaskInput{
		Category:            domain.CategoryInventoryVaccine,
		ParkID:              "9c000000-0000-4000-8000-000000001001",
		ShedID:              "9c000000-0000-4000-8000-000000001002",
		PlannedBusinessDate: "2026-08-26",
		AssigneeUserIDs:     []string{testAssignee},
		IdempotencyKey:      "pc-inventory-manual-create",
		ActorID:             "ceo-1",
		ActorType:           "human",
	})
	if !errors.Is(err, domain.ErrKernelOwnedCategory) {
		t.Fatalf("CreateTask inventory err = %v, want ErrKernelOwnedCategory", err)
	}
}

func TestPlannerParkShedsRejectsKernelOwnedInventoryVaccineCategory(t *testing.T) {
	store := &fakeStore{}
	svc := NewService(store)
	ctx := httpmiddleware.WithAuthGrants(context.Background(), []permissions.ActiveGrant{{
		Role: permissions.RoleCEOInternal, ScopeType: "tenant", ScopeID: testTenant,
	}})
	actor := domain.Actor{TenantID: testTenant, UserID: "ceo-1", Roles: []string{permissions.RoleCEOInternal}}

	_, err := svc.PlannerParkSheds(
		ctx,
		actor,
		"9c000000-0000-4000-8000-000000001001",
		domain.CategoryInventoryVaccine,
		"2026-08-26",
		"",
		25,
	)
	if !errors.Is(err, domain.ErrKernelOwnedCategory) {
		t.Fatalf("PlannerParkSheds inventory err = %v, want ErrKernelOwnedCategory", err)
	}
}

func TestCEOCanMonitorInventoryVaccineTasksTenantWide(t *testing.T) {
	store := &fakeStore{listResult: ports.TaskPage{Items: []ports.TaskRow{{
		TaskID: testTask, Category: domain.CategoryInventoryVaccine, Status: domain.StatusPendingVerification,
		InventoryRequirements: []ports.InventoryRequirement{{VaccineLabel: "PPR", RequiredDoses: 4}},
	}}}}
	svc := NewService(store)
	ctx := httpmiddleware.WithAuthGrants(context.Background(), []permissions.ActiveGrant{{
		Role: permissions.RoleCEOInternal, ScopeType: "tenant", ScopeID: testTenant,
	}})
	actor := domain.Actor{TenantID: testTenant, UserID: "ceo-1", Roles: []string{permissions.RoleCEOInternal}}

	page, err := svc.ListTasks(ctx, actor, "", domain.CategoryInventoryVaccine, "2026-08-19", "", 25, false)
	if err != nil {
		t.Fatalf("CEO ListTasks inventory: %v", err)
	}
	if len(page.Items) != 1 || page.Items[0].Category != domain.CategoryInventoryVaccine || page.Items[0].Status != domain.StatusPendingVerification {
		t.Fatalf("CEO inventory monitor page = %+v, want pending inventory task", page.Items)
	}
	if !store.lastList.TenantWide || len(store.lastList.AuthorizedParkIDs) != 0 || store.lastList.AssigneeUserID != "" || store.lastList.CurrentOrCarry {
		t.Fatalf("CEO monitor query = %+v, want tenant-wide monitor list, not assigned current/carry worklist", store.lastList)
	}
	if store.lastList.Category != domain.CategoryInventoryVaccine || store.lastList.DueBusinessDate != "2026-08-19" {
		t.Fatalf("CEO monitor category/date = %q/%q", store.lastList.Category, store.lastList.DueBusinessDate)
	}
}

// The assigned-only rule: pc_care.execute alone never authorizes a write — a non-assignee
// holder is refused with the typed error, and no store write runs.
func TestExecuteWritesRequireAssigneeMembership(t *testing.T) {
	store := &fakeStore{assignees: map[string]bool{testAssignee: true}}
	svc := NewService(store).WithVerificationEnqueuer(&fakeEnqueuer{})

	_, err := svc.ScanAnimal(context.Background(), operatorActor(testOutsider), ScanAnimalInput{
		TaskID: testTask, ScannedIdentifier: "RFID-1", IdempotencyKey: "scan-key-1",
	})
	if !errors.Is(err, domain.ErrTaskNotAssigned) {
		t.Fatalf("outsider scan err = %v, want ErrTaskNotAssigned", err)
	}
	if err := svc.RegisterSlotProof(context.Background(), operatorActor(testOutsider), RegisterSlotProofInput{
		TaskID: testTask, AnimalRowID: testAnimal, SlotFieldKey: domain.SlotVideo,
		ProofRef: "proof-1", IdempotencyKey: "slot-key-1",
	}); !errors.Is(err, domain.ErrTaskNotAssigned) {
		t.Fatalf("outsider slot err = %v, want ErrTaskNotAssigned", err)
	}
	if _, err := svc.SubmitTask(context.Background(), operatorActor(testOutsider), SubmitTaskInput{
		TaskID: testTask, IdempotencyKey: "submit-key-1",
	}); !errors.Is(err, domain.ErrTaskNotAssigned) {
		t.Fatalf("outsider submit err = %v, want ErrTaskNotAssigned", err)
	}
	if store.scanCalls+store.slotCalls+store.submitCalls != 0 {
		t.Fatalf("store writes ran for a non-assignee: %+v", store)
	}

	// An ASSIGNEE with the same role passes.
	if _, err := svc.ScanAnimal(context.Background(), operatorActor(testAssignee), ScanAnimalInput{
		TaskID: testTask, ScannedIdentifier: "RFID-1", IdempotencyKey: "scan-key-2",
	}); err != nil {
		t.Fatalf("assignee scan err = %v", err)
	}
}

// A role without pc_care.execute is refused before the membership check.
func TestExecuteWritesRequireThePermission(t *testing.T) {
	store := &fakeStore{assignees: map[string]bool{testAssignee: true}}
	svc := NewService(store).WithVerificationEnqueuer(&fakeEnqueuer{})
	verifier := domain.Actor{TenantID: testTenant, UserID: testAssignee, Roles: []string{permissions.RoleVerifier}}
	if _, err := svc.ScanAnimal(context.Background(), verifier, ScanAnimalInput{
		TaskID: testTask, ScannedIdentifier: "RFID-1", IdempotencyKey: "scan-key-3",
	}); !errors.Is(err, ports.ErrForbidden) {
		t.Fatalf("verifier scan err = %v, want ErrForbidden", err)
	}
}

func TestRegisterTaskProofRequiresAssigneeAndValidatesLiveCameraMedia(t *testing.T) {
	store := &fakeStore{assignees: map[string]bool{testAssignee: true}}
	proofs := &fakeProofValidator{}
	svc := NewService(store).WithProofValidator(proofs)

	err := svc.RegisterTaskProof(context.Background(), operatorActor(testOutsider), RegisterTaskProofInput{
		TaskID:         testTask,
		SlotKey:        domain.SlotStockFridgeVideo,
		ProofRef:       "proof-fridge-stock",
		IdempotencyKey: "task-proof-1",
		ActorID:        testOutsider,
		ActorType:      "operator",
		TraceID:        "trace-task-proof-outsider",
	})
	if !errors.Is(err, domain.ErrTaskNotAssigned) {
		t.Fatalf("outsider task proof err = %v, want ErrTaskNotAssigned", err)
	}
	if store.taskProofCalls != 0 || proofs.mediaCalls != 0 || proofs.videoCalls != 0 {
		t.Fatalf("outsider reached proof/store writes: store=%d media=%d video=%d", store.taskProofCalls, proofs.mediaCalls, proofs.videoCalls)
	}

	err = svc.RegisterTaskProof(context.Background(), operatorActor(testAssignee), RegisterTaskProofInput{
		TaskID:         testTask,
		SlotKey:        domain.SlotStockFridgeVideo,
		ProofRef:       "proof-fridge-stock",
		IdempotencyKey: "task-proof-2",
		ActorID:        testAssignee,
		ActorType:      "operator",
		TraceID:        "trace-task-proof-assignee",
	})
	if err != nil {
		t.Fatalf("assignee task proof err = %v", err)
	}
	if proofs.mediaCalls != 1 || proofs.videoCalls != 0 {
		t.Fatalf("proof validator calls = media %d video %d, want media-only", proofs.mediaCalls, proofs.videoCalls)
	}
	if proofs.lastTenant != testTenant || len(proofs.lastProofs) != 1 || proofs.lastProofs[0] != "proof-fridge-stock" {
		t.Fatalf("proof validation = tenant %q proofs %+v, want task proof media", proofs.lastTenant, proofs.lastProofs)
	}
	if store.taskProofCalls != 1 {
		t.Fatalf("task proof store calls = %d, want 1", store.taskProofCalls)
	}
	got := store.lastTaskProof
	if got.TenantID != testTenant || got.TaskID != testTask || got.SlotKey != domain.SlotStockFridgeVideo || got.ProofRef != "proof-fridge-stock" {
		t.Fatalf("task proof params = %+v, want inventory stock proof params", got)
	}
	if got.CapturedBy != testAssignee || got.IdempotencyKey != "task-proof-2" || got.TraceID != "trace-task-proof-assignee" {
		t.Fatalf("task proof actor/idempotency = %+v", got)
	}
}

// The durable submit event is what enqueues verification now: keyed to task+row_version so a
// rework re-submit mints a fresh item while a retry collapses.
func TestPendingVerificationHandlerEnqueuesKeyedByRowVersion(t *testing.T) {
	enq := &fakeEnqueuer{}
	handler := NewPCCarePendingVerificationHandler(enq, nil)
	payload, _ := json.Marshal(map[string]any{
		"task_id":         testTask,
		"category":        domain.CategoryDeworming,
		"row_version":     4,
		"animal_count":    2,
		"operator_id":     testAssignee,
		"media_refs":      []map[string]string{{"proof_ref": "proof-1", "label": "RFID-1 · Video"}},
		"shed_name":       "Castro",
		"partition_label": "2",
	})
	if err := handler.HandleEvent(context.Background(), eventbus.Event{
		Type:       "pc_care.task.pending_verification",
		TenantID:   testTenant,
		OccurredAt: time.Unix(0, 0),
		Payload:    payload,
	}); err != nil {
		t.Fatalf("pending verification: %v", err)
	}

	if enq.calls != 1 {
		t.Fatalf("enqueue calls = %d, want 1", enq.calls)
	}
	if got, want := enq.lastKeys[0], "pc-care-verification:"+testTask+":4"; got != want {
		t.Fatalf("enqueue key = %q, want %q", got, want)
	}
}

func TestPendingVerificationHandlerCarriesInventoryVaccineFridgeProof(t *testing.T) {
	enq := &fakeEnqueuer{}
	handler := NewPCCarePendingVerificationHandler(enq, nil)
	occurredAt := time.Unix(123, 0)
	payload, _ := json.Marshal(map[string]any{
		"task_id":               testTask,
		"category":              domain.CategoryInventoryVaccine,
		"park_id":               "9c000000-0000-4000-8000-00000000f001",
		"shed_id":               "9c000000-0000-4000-8000-00000000f002",
		"shed_name":             "Mandela",
		"partition_label":       "7",
		"planned_business_date": "2026-08-26",
		"row_version":           8,
		"animal_count":          0,
		"operator_id":           testAssignee,
		"media_refs": []map[string]string{{
			"proof_ref": "proof-fridge-stock",
			"label":     "Fridge stock proof",
		}},
	})
	if err := handler.HandleEvent(context.Background(), eventbus.Event{
		Type:       "pc_care.task.pending_verification",
		TenantID:   testTenant,
		OccurredAt: occurredAt,
		Payload:    payload,
	}); err != nil {
		t.Fatalf("pending inventory verification: %v", err)
	}
	if enq.calls != 1 {
		t.Fatalf("enqueue calls = %d, want 1", enq.calls)
	}
	got := enq.last
	if got.TenantID != testTenant || got.TaskID != testTask || got.Category != domain.CategoryInventoryVaccine {
		t.Fatalf("enqueue identity = tenant %q task %q category %q, want inventory task", got.TenantID, got.TaskID, got.Category)
	}
	if got.AnimalCount != 0 {
		t.Fatalf("inventory animal count = %d, want 0", got.AnimalCount)
	}
	if len(got.MediaRefs) != 1 || got.MediaRefs[0].ProofRef != "proof-fridge-stock" || got.MediaRefs[0].Label != "Fridge stock proof" {
		t.Fatalf("inventory media refs = %+v, want fridge stock proof", got.MediaRefs)
	}
	if got.ShedName != "Mandela" || got.PartitionLabel != "7" || got.PlannedBusinessDate != "2026-08-26" {
		t.Fatalf("inventory context = shed %q partition %q planned %q", got.ShedName, got.PartitionLabel, got.PlannedBusinessDate)
	}
	if got.OperatorID != testAssignee || !got.CapturedAt.Equal(occurredAt.UTC()) {
		t.Fatalf("operator/captured_at = %q/%s, want event values", got.OperatorID, got.CapturedAt)
	}
	if got.IdempotencyKey != "pc-care-verification:"+testTask+":8" {
		t.Fatalf("idempotency key = %q, want task row-version key", got.IdempotencyKey)
	}
}

// The verdict consumer filters strictly on source.module + ref_type: a feed/vaccination
// verdict passes through untouched, our module's verdicts route to the matching write.
func TestVerdictHandlerFiltersOnModuleAndRefType(t *testing.T) {
	store := &fakeStore{}
	handler := NewPCCareVerificationHandler(store, nil)

	payload := func(module, refType, refID, reason string) []byte {
		raw, _ := json.Marshal(map[string]any{
			"verified_by": "v-1",
			"reason":      reason,
			"source":      map[string]string{"module": module, "ref_type": refType, "ref_id": refID},
		})
		return raw
	}

	// A foreign module's verdict is ignored.
	if err := handler.HandleEvent(context.Background(), eventbus.Event{
		Type: "verification.verdict.approved", TenantID: testTenant,
		Payload: payload("feed", "feed_packing_completion", testTask, ""),
	}); err != nil {
		t.Fatalf("foreign verdict: %v", err)
	}
	if store.applyCalls != 0 {
		t.Fatal("foreign module verdict reached ApplyVerifiedTask")
	}

	// Ours: approved -> apply.
	if err := handler.HandleEvent(context.Background(), eventbus.Event{
		Type: "verification.verdict.approved", TenantID: testTenant, ID: "evt-1",
		Payload: payload(domain.VerificationModulePCCare, domain.VerificationRefTypeTask, testTask, ""),
	}); err != nil {
		t.Fatalf("approved verdict: %v", err)
	}
	if store.applyCalls != 1 {
		t.Fatalf("applyCalls = %d, want 1", store.applyCalls)
	}

	// Ours: rework -> bounce, carrying the verifier's reason.
	if err := handler.HandleEvent(context.Background(), eventbus.Event{
		Type: "verification.verdict.rework", TenantID: testTenant, ID: "evt-2",
		Payload: payload(domain.VerificationModulePCCare, domain.VerificationRefTypeTask, testTask, "Too dark"),
	}); err != nil {
		t.Fatalf("rework verdict: %v", err)
	}
	if store.bounceCalls != 1 || store.lastBounce.Reason != "Too dark" {
		t.Fatalf("bounce = %d/%+v, want 1 with the reason", store.bounceCalls, store.lastBounce)
	}
}
