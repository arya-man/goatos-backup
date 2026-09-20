package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/toxin/domain"
	"github.com/vgoats/goatos/backend/internal/toxin/ports"
)

// versionedRepo hands back a task stamped with a chosen procedure version.
type versionedRepo struct {
	fakeRepo
	version int
}

func (r *versionedRepo) GetTask(context.Context, string, string) (ports.TaskRow, error) {
	return ports.TaskRow{Task: domain.Task{TaskID: "task-1", Status: domain.StatusInProgress, SOPVersion: r.version}}, nil
}

// shortProcedure is a four-step test: sample, mix, fill the well, read. It exists to prove the
// engine follows the DOCUMENT and not the seven constants it used to carry.
func shortProcedure(version int) domain.Procedure {
	return domain.CompileProcedure(domain.ToxinDSL{SchemaVersion: domain.ToxinSchemaVersion, Steps: []domain.ToxinDSLStep{
		{No: 1, Kind: domain.StepKindVideo, Title: "Take the sample", Instruction: "On camera."},
		{No: 2, Kind: domain.StepKindVideo, Title: "Mix", Instruction: "On camera."},
		{No: 3, Kind: domain.StepKindVideo, Title: "Fill the well", Instruction: "On camera."},
		{No: 4, Kind: domain.StepKindPhotoReading, Title: "Read the strip", Instruction: "Photograph it."},
	}}, version)
}

type fakeProcedures struct {
	byVersion map[int]domain.Procedure
	asked     []int
}

func (f *fakeProcedures) PublishedProcedure(context.Context, string) (domain.Procedure, error) {
	return shortProcedure(9), nil
}

func (f *fakeProcedures) ProcedureVersion(_ context.Context, _ string, version int) (domain.Procedure, error) {
	f.asked = append(f.asked, version)
	if proc, ok := f.byVersion[version]; ok {
		return proc, nil
	}
	return domain.Procedure{}, ports.ErrProcedureVersionUnknown
}

// TestAStepIsJudgedAgainstTheRoundsOwnProcedure is the safety property of authoring the toxin
// test: a round runs the version it was OPENED on, whatever has been published since. Step 6
// exists in the seeded seven-step document and does not exist in this round's four-step one, so
// it is refused -- and the version the repository is handed is the round's, not the latest.
func TestAStepIsJudgedAgainstTheRoundsOwnProcedure(t *testing.T) {
	repo := &versionedRepo{version: 2}
	procs := &fakeProcedures{byVersion: map[int]domain.Procedure{2: shortProcedure(2)}}
	svc := NewService(repo, &fakeProofs{}).WithProcedureSource(procs).WithClock(func() time.Time { return fixedNow })

	if _, err := svc.CompleteStep(context.Background(), ports.CompleteStepParams{
		TenantID: "t1", TaskID: "task-1", StepNo: 6, ProofRef: "proof-1", IdempotencyKey: "k1",
	}); !errors.Is(err, domain.ErrUnknownStep) {
		t.Fatalf("a step outside this round's procedure must be refused, got %v", err)
	}
	if repo.completeStepCalls != 0 {
		t.Fatal("a refused step reached the repository")
	}

	if _, err := svc.CompleteStep(context.Background(), ports.CompleteStepParams{
		TenantID: "t1", TaskID: "task-1", StepNo: 3, ProofRef: "proof-1", IdempotencyKey: "k2",
	}); err != nil {
		t.Fatal(err)
	}
	if repo.lastStep.Procedure.Version != 2 {
		t.Fatalf("repository was handed procedure v%d, want the round's v2", repo.lastStep.Procedure.Version)
	}
	if len(procs.asked) == 0 || procs.asked[0] != 2 {
		t.Fatalf("service resolved versions %v, want the round's own", procs.asked)
	}

	// The READING step of THIS procedure is step 4, not the seeded document's 7.
	if _, err := svc.SubmitReading(context.Background(), ports.SubmitParams{
		TenantID: "t1", TaskID: "task-1", Outcome: domain.OutcomeNegative, StripPhotoRef: "photo-1", IdempotencyKey: "k3",
	}); err != nil {
		t.Fatal(err)
	}
	if repo.lastSubmit.Procedure.FinalStepNo() != 4 {
		t.Fatalf("submit ran against final step %d, want 4", repo.lastSubmit.Procedure.FinalStepNo())
	}
}

// TestAnUnreadableProcedureFallsBackToTheSeededOne: a round whose version the library no longer
// carries still runs -- on the seeded seven steps, the ones it was running before anything was
// authored -- rather than becoming a test nobody can finish.
func TestAnUnreadableProcedureFallsBackToTheSeededOne(t *testing.T) {
	repo := &versionedRepo{version: 7}
	svc := NewService(repo, &fakeProofs{}).WithProcedureSource(&fakeProcedures{byVersion: map[int]domain.Procedure{}}).
		WithClock(func() time.Time { return fixedNow })
	if _, err := svc.CompleteStep(context.Background(), ports.CompleteStepParams{
		TenantID: "t1", TaskID: "task-1", StepNo: 2, ProofRef: "proof-1", IdempotencyKey: "k1",
	}); err != nil {
		t.Fatal(err)
	}
	if got := len(repo.lastStep.Procedure.Steps); got != len(domain.Steps()) {
		t.Fatalf("fallback procedure has %d steps, want the seeded %d", got, len(domain.Steps()))
	}
}
