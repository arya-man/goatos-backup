package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/toxin/domain"
	"github.com/vgoats/goatos/backend/internal/toxin/ports"
)

type fakeRepo struct {
	completeStepCalls int
	submitCalls       int
	verdictCalls      int
	lastStep          ports.CompleteStepParams
	lastSubmit        ports.SubmitParams
	lastVerdict       ports.VerdictParams
}

func (f *fakeRepo) CreateTaskFromPurchase(context.Context, ports.CreateTaskParams) error { return nil }
func (f *fakeRepo) ListTasks(context.Context, ports.ListTasksParams) (ports.TaskPage, error) {
	return ports.TaskPage{}, nil
}
func (f *fakeRepo) GetTask(context.Context, string, string) (ports.TaskRow, error) {
	return ports.TaskRow{}, nil
}
func (f *fakeRepo) CompleteStep(_ context.Context, p ports.CompleteStepParams) (ports.TaskRow, error) {
	f.completeStepCalls++
	f.lastStep = p
	return ports.TaskRow{}, nil
}
func (f *fakeRepo) SubmitReading(_ context.Context, p ports.SubmitParams) (ports.TaskRow, error) {
	f.submitCalls++
	f.lastSubmit = p
	return ports.TaskRow{}, nil
}
func (f *fakeRepo) RecordVerdict(_ context.Context, p ports.VerdictParams) (ports.TaskRow, error) {
	f.verdictCalls++
	f.lastVerdict = p
	return ports.TaskRow{}, nil
}

// LoadReport satisfies the port; the report read has its own DB-backed proof
// (TestReportCountsLoadsNotRounds), because its whole content is a SQL grain collapse
// that an in-memory fake cannot exercise honestly.
func (f *fakeRepo) LoadReport(_ context.Context, _ ports.ReportParams) (ports.ReportPage, error) {
	return ports.ReportPage{}, nil
}

type fakeProofs struct {
	videoErr, photoErr   error
	videoRefs, photoRefs []string
}

func (f *fakeProofs) ValidateToxinStepVideo(_ context.Context, _ string, ref string) error {
	f.videoRefs = append(f.videoRefs, ref)
	return f.videoErr
}
func (f *fakeProofs) ValidateToxinStripPhoto(_ context.Context, _ string, ref string) error {
	f.photoRefs = append(f.photoRefs, ref)
	return f.photoErr
}

var fixedNow = time.Date(2026, 8, 25, 10, 0, 0, 0, time.UTC)

func newTestService(repo *fakeRepo, proofs *fakeProofs) *Service {
	return NewService(repo, proofs).WithClock(func() time.Time { return fixedNow })
}

func TestCompleteStepValidatesBeforeWriting(t *testing.T) {
	ctx := context.Background()

	// No idempotency key: refused before any proof read or repo call.
	repo, proofs := &fakeRepo{}, &fakeProofs{}
	svc := newTestService(repo, proofs)
	if _, err := svc.CompleteStep(ctx, ports.CompleteStepParams{TenantID: "t", TaskID: "task", StepNo: 1, ProofRef: "p"}); !errors.Is(err, ErrIdempotencyKeyRequired) {
		t.Fatalf("missing key: err = %v", err)
	}
	// Blank proof: refused.
	if _, err := svc.CompleteStep(ctx, ports.CompleteStepParams{TenantID: "t", TaskID: "task", StepNo: 1, ProofRef: " ", IdempotencyKey: "k"}); !errors.Is(err, domain.ErrProofRequired) {
		t.Fatalf("blank proof: err = %v", err)
	}
	// The wait row and the reading step are not completable through the step route.
	for _, stepNo := range []int{4, 7} {
		if _, err := svc.CompleteStep(ctx, ports.CompleteStepParams{TenantID: "t", TaskID: "task", StepNo: stepNo, ProofRef: "p", IdempotencyKey: "k"}); !errors.Is(err, domain.ErrStepNotCompletable) {
			t.Fatalf("step %d: err = %v", stepNo, err)
		}
	}
	// A bad video rejects BEFORE the repo write — a submit whose capture is unusable
	// must not consume the step.
	proofs.videoErr = ports.ErrInvalidProof
	if _, err := svc.CompleteStep(ctx, ports.CompleteStepParams{TenantID: "t", TaskID: "task", StepNo: 2, ProofRef: "p", IdempotencyKey: "k"}); !errors.Is(err, ports.ErrInvalidProof) {
		t.Fatalf("bad video: err = %v", err)
	}
	if repo.completeStepCalls != 0 {
		t.Fatalf("repo written %d times before validation passed", repo.completeStepCalls)
	}
	// A good call reaches the repo with the SERVICE clock pinned onto the params.
	proofs.videoErr = nil
	if _, err := svc.CompleteStep(ctx, ports.CompleteStepParams{TenantID: "t", TaskID: "task", StepNo: 2, ProofRef: "p", IdempotencyKey: "k"}); err != nil {
		t.Fatalf("good step: err = %v", err)
	}
	if repo.completeStepCalls != 1 || !repo.lastStep.Now.Equal(fixedNow) {
		t.Fatalf("repo calls = %d, now = %v", repo.completeStepCalls, repo.lastStep.Now)
	}
}

func TestSubmitReadingValidatesBeforeWriting(t *testing.T) {
	ctx := context.Background()
	repo, proofs := &fakeRepo{}, &fakeProofs{}
	svc := newTestService(repo, proofs)

	if _, err := svc.SubmitReading(ctx, ports.SubmitParams{TenantID: "t", TaskID: "task", Outcome: domain.OutcomeNegative, StripPhotoRef: "p"}); !errors.Is(err, ErrIdempotencyKeyRequired) {
		t.Fatalf("missing key: err = %v", err)
	}
	if _, err := svc.SubmitReading(ctx, ports.SubmitParams{TenantID: "t", TaskID: "task", Outcome: "faint", StripPhotoRef: "p", IdempotencyKey: "k"}); !errors.Is(err, domain.ErrInvalidOutcome) {
		t.Fatalf("bad outcome: err = %v", err)
	}
	proofs.photoErr = ports.ErrInvalidProof
	if _, err := svc.SubmitReading(ctx, ports.SubmitParams{TenantID: "t", TaskID: "task", Outcome: domain.OutcomeNegative, StripPhotoRef: "p", IdempotencyKey: "k"}); !errors.Is(err, ports.ErrInvalidProof) {
		t.Fatalf("bad photo: err = %v", err)
	}
	if repo.submitCalls != 0 {
		t.Fatal("repo written before validation passed")
	}
	proofs.photoErr = nil
	if _, err := svc.SubmitReading(ctx, ports.SubmitParams{TenantID: "t", TaskID: "task", Outcome: domain.OutcomeInvalid, StripPhotoRef: "p", IdempotencyKey: "k"}); err != nil {
		t.Fatalf("invalid reading is still a VALID submit: err = %v", err)
	}
	if repo.submitCalls != 1 || !repo.lastSubmit.Now.Equal(fixedNow) {
		t.Fatalf("repo calls = %d, now = %v", repo.submitCalls, repo.lastSubmit.Now)
	}
}

func TestRecordVerdictValidatesBeforeWriting(t *testing.T) {
	ctx := context.Background()
	repo := &fakeRepo{}
	svc := newTestService(repo, &fakeProofs{})

	if _, err := svc.RecordVerdict(ctx, ports.VerdictParams{TenantID: "t", TaskID: "task", Decision: domain.VerdictAccept}); !errors.Is(err, ErrIdempotencyKeyRequired) {
		t.Fatalf("missing key: err = %v", err)
	}
	if _, err := svc.RecordVerdict(ctx, ports.VerdictParams{TenantID: "t", TaskID: "task", Decision: domain.VerdictReject, Reason: " ", IdempotencyKey: "k"}); !errors.Is(err, domain.ErrRejectReasonRequired) {
		t.Fatalf("blank reject reason: err = %v", err)
	}
	if repo.verdictCalls != 0 {
		t.Fatal("repo written before validation passed")
	}
	if _, err := svc.RecordVerdict(ctx, ports.VerdictParams{TenantID: "t", TaskID: "task", Decision: domain.VerdictReject, Reason: " blur ", IdempotencyKey: "k"}); err != nil {
		t.Fatalf("good reject: err = %v", err)
	}
	if repo.verdictCalls != 1 || repo.lastVerdict.Reason != "blur" {
		t.Fatalf("repo calls = %d, reason = %q", repo.verdictCalls, repo.lastVerdict.Reason)
	}
}
