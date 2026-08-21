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
	submitResult   ports.SubmitTaskResult
	applyCalls     int
	bounceCalls    int
	lastBounce     ports.BounceTaskParams
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

type fakeEnqueuer struct {
	calls    int
	lastKeys []string
}

func (f *fakeEnqueuer) EnqueuePCCareVerification(_ context.Context, in VerificationEnqueueRequest) error {
	f.calls++
	f.lastKeys = append(f.lastKeys, in.IdempotencyKey)
	return nil
}

func operatorActor(userID string) domain.Actor {
	return domain.Actor{TenantID: testTenant, UserID: userID, Roles: []string{permissions.RoleOperator}}
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
