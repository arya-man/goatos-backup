package app

// The vaccine-stock director gate (maintainer decision 2026-09-02): park operators record the
// fridge videos, and the PC Director ALONE approves or rejects the submitted task on the
// module's own stock-verdict path — never verification.verdict, and never an operator
// accepting their own evidence.

import (
	"context"
	"errors"
	"testing"

	"github.com/vgoats/goatos/backend/internal/pccare/domain"
	"github.com/vgoats/goatos/backend/internal/pccare/ports"
	"github.com/vgoats/goatos/backend/internal/permissions"
)

type stockVerdictFakeStore struct {
	ports.TaskStore
	task         ports.TaskRow
	applyResult  bool
	bounceResult bool
	applyCalls   int
	bounceCalls  int
	lastApply    ports.ApplyVerifiedTaskParams
	lastBounce   ports.BounceTaskParams
	// afterWrite mutates the echoed task before the post-verdict re-read (models the DB flip).
	afterWrite func(*ports.TaskRow)
	wrote      bool
}

func (f *stockVerdictFakeStore) GetTask(context.Context, string, string, []string, bool) (ports.TaskRow, error) {
	task := f.task
	if f.wrote && f.afterWrite != nil {
		f.afterWrite(&task)
	}
	return task, nil
}
func (f *stockVerdictFakeStore) ApplyVerifiedTask(_ context.Context, p ports.ApplyVerifiedTaskParams) (bool, error) {
	f.applyCalls++
	f.lastApply = p
	f.wrote = true
	return f.applyResult, nil
}
func (f *stockVerdictFakeStore) BounceTaskForRework(_ context.Context, p ports.BounceTaskParams) (bool, error) {
	f.bounceCalls++
	f.lastBounce = p
	f.wrote = true
	return f.bounceResult, nil
}

func stockTask(status string) ports.TaskRow {
	return ports.TaskRow{
		TaskID:       testTask,
		Category:     domain.CategoryInventoryVaccine,
		Status:       status,
		VaccineLabel: "FMD",
	}
}

func directorActor() domain.Actor {
	return domain.Actor{TenantID: testTenant, UserID: "9c000000-0000-4000-8000-00000000d1d1", Roles: []string{permissions.RolePCDirector}}
}

func TestStockVerdictApproveCompletesTheTaskAsTheDirector(t *testing.T) {
	store := &stockVerdictFakeStore{task: stockTask(domain.StatusPendingVerification), applyResult: true,
		afterWrite: func(task *ports.TaskRow) { task.Status = domain.StatusCompleted }}
	svc := NewService(store)
	task, err := svc.RecordStockVerdict(context.Background(), directorActor(), StockVerdictInput{
		TaskID: testTask, Verdict: domain.StockVerdictApprove, TraceID: "trace-1",
	})
	if err != nil {
		t.Fatalf("approve: %v", err)
	}
	if store.applyCalls != 1 || store.bounceCalls != 0 {
		t.Fatalf("apply/bounce calls = %d/%d, want 1/0", store.applyCalls, store.bounceCalls)
	}
	if store.lastApply.VerifiedBy != directorActor().UserID || store.lastApply.TaskID != testTask {
		t.Fatalf("apply params = %+v, want the director stamped as the approver", store.lastApply)
	}
	if task.Status != domain.StatusCompleted {
		t.Fatalf("echoed status = %q, want completed", task.Status)
	}
}

func TestStockVerdictRejectRequiresAReasonAndBouncesToRework(t *testing.T) {
	store := &stockVerdictFakeStore{task: stockTask(domain.StatusPendingVerification), bounceResult: true,
		afterWrite: func(task *ports.TaskRow) { task.Status = domain.StatusRework }}
	svc := NewService(store)

	if _, err := svc.RecordStockVerdict(context.Background(), directorActor(), StockVerdictInput{
		TaskID: testTask, Verdict: domain.StockVerdictReject,
	}); !errors.Is(err, domain.ErrStockRejectReasonRequired) {
		t.Fatalf("blank-reason reject err = %v, want ErrStockRejectReasonRequired", err)
	}
	if store.bounceCalls != 0 {
		t.Fatalf("bounce calls after refused reject = %d, want 0", store.bounceCalls)
	}

	task, err := svc.RecordStockVerdict(context.Background(), directorActor(), StockVerdictInput{
		TaskID: testTask, Verdict: domain.StockVerdictReject, Reason: "The clip does not show the FMD shelf",
	})
	if err != nil {
		t.Fatalf("reject: %v", err)
	}
	if store.bounceCalls != 1 || store.lastBounce.Reason != "The clip does not show the FMD shelf" {
		t.Fatalf("bounce = %d %+v, want one bounce carrying the reason verbatim", store.bounceCalls, store.lastBounce)
	}
	if task.Status != domain.StatusRework {
		t.Fatalf("echoed status = %q, want rework", task.Status)
	}
}

// Only pc_care.stock_approve holders may decide: an OPERATOR (who records the fridge) and the
// CEO are both refused, so nobody can accept their own evidence and the director's authority
// is not silently widened. Mutation-tested by granting the permission to RoleOperator — this
// test goes red.
func TestStockVerdictIsDirectorOnly(t *testing.T) {
	store := &stockVerdictFakeStore{task: stockTask(domain.StatusPendingVerification), applyResult: true}
	svc := NewService(store)
	for _, role := range []string{permissions.RoleOperator, permissions.RoleCEOInternal, permissions.RoleVerifier} {
		actor := domain.Actor{TenantID: testTenant, UserID: testAssignee, Roles: []string{role}}
		if _, err := svc.RecordStockVerdict(context.Background(), actor, StockVerdictInput{
			TaskID: testTask, Verdict: domain.StockVerdictApprove,
		}); !errors.Is(err, ports.ErrForbidden) {
			t.Fatalf("role %s verdict err = %v, want ErrForbidden", role, err)
		}
	}
	if store.applyCalls != 0 {
		t.Fatalf("apply calls = %d, want 0 after refused verdicts", store.applyCalls)
	}
}

func TestStockVerdictRefusesANonStockTask(t *testing.T) {
	task := stockTask(domain.StatusPendingVerification)
	task.Category = domain.CategoryDeworming
	store := &stockVerdictFakeStore{task: task, applyResult: true}
	svc := NewService(store)
	if _, err := svc.RecordStockVerdict(context.Background(), directorActor(), StockVerdictInput{
		TaskID: testTask, Verdict: domain.StockVerdictApprove,
	}); !errors.Is(err, domain.ErrNotStockTask) {
		t.Fatalf("deworming verdict err = %v, want ErrNotStockTask — the four verifier-reviewed categories stay the verifier's", err)
	}
	if store.applyCalls != 0 {
		t.Fatalf("apply calls = %d, want 0", store.applyCalls)
	}
}

func TestStockVerdictIsIdempotentOnAReplayAndConflictsOtherwise(t *testing.T) {
	// Replayed approve on an already-completed task: state-guarded write no-ops, and the
	// service answers idempotently because the task already sits where the verdict points.
	store := &stockVerdictFakeStore{task: stockTask(domain.StatusCompleted), applyResult: false}
	svc := NewService(store)
	task, err := svc.RecordStockVerdict(context.Background(), directorActor(), StockVerdictInput{
		TaskID: testTask, Verdict: domain.StockVerdictApprove,
	})
	if err != nil || task.Status != domain.StatusCompleted {
		t.Fatalf("replayed approve = (%+v, %v), want idempotent completed echo", task, err)
	}

	// A conflicting verdict (reject after the approve landed) is refused, never applied.
	store = &stockVerdictFakeStore{task: stockTask(domain.StatusCompleted), bounceResult: false}
	svc = NewService(store)
	if _, err := svc.RecordStockVerdict(context.Background(), directorActor(), StockVerdictInput{
		TaskID: testTask, Verdict: domain.StockVerdictReject, Reason: "too late",
	}); !errors.Is(err, domain.ErrStockVerdictNotPending) {
		t.Fatalf("conflicting reject err = %v, want ErrStockVerdictNotPending", err)
	}

	// Replayed reject is idempotent only when it is the same decision, including the reason.
	store = &stockVerdictFakeStore{task: stockTask(domain.StatusRework), bounceResult: false}
	store.task.ReworkReason = "The clip does not show the FMD shelf"
	svc = NewService(store)
	task, err = svc.RecordStockVerdict(context.Background(), directorActor(), StockVerdictInput{
		TaskID: testTask, Verdict: domain.StockVerdictReject, Reason: "The clip does not show the FMD shelf",
	})
	if err != nil || task.Status != domain.StatusRework {
		t.Fatalf("replayed reject = (%+v, %v), want idempotent rework echo", task, err)
	}
	if _, err := svc.RecordStockVerdict(context.Background(), directorActor(), StockVerdictInput{
		TaskID: testTask, Verdict: domain.StockVerdictReject, Reason: "different reason",
	}); !errors.Is(err, domain.ErrStockVerdictNotPending) {
		t.Fatalf("conflicting reject reason err = %v, want ErrStockVerdictNotPending", err)
	}

	// A verdict on a never-submitted task conflicts the same way.
	store = &stockVerdictFakeStore{task: stockTask(domain.StatusOpen), applyResult: false}
	svc = NewService(store)
	if _, err := svc.RecordStockVerdict(context.Background(), directorActor(), StockVerdictInput{
		TaskID: testTask, Verdict: domain.StockVerdictApprove,
	}); !errors.Is(err, domain.ErrStockVerdictNotPending) {
		t.Fatalf("unsubmitted approve err = %v, want ErrStockVerdictNotPending", err)
	}

	if _, err := svc.RecordStockVerdict(context.Background(), directorActor(), StockVerdictInput{
		TaskID: testTask, Verdict: "maybe",
	}); !errors.Is(err, domain.ErrInvalidStockVerdict) {
		t.Fatalf("unknown verdict err = %v, want ErrInvalidStockVerdict", err)
	}
}
