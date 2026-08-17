package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/weighing/domain"
	"github.com/vgoats/goatos/backend/internal/weighing/ports"
)

type fakeCorrectionStore struct {
	calls  []domain.WeightCorrectionCommand
	result domain.WeightCorrectionResult
	err    error
}

func (f *fakeCorrectionStore) CorrectObservationWeight(_ context.Context, cmd domain.WeightCorrectionCommand) (domain.WeightCorrectionResult, error) {
	f.calls = append(f.calls, cmd)
	if f.err != nil {
		return domain.WeightCorrectionResult{}, f.err
	}
	return f.result, nil
}

type fakeRelabeler struct {
	calls []relabelArgs
	err   error
}

type relabelArgs struct {
	tenantID, refType, observationID, subjectLabel string
}

func (f *fakeRelabeler) RelabelWeighingVerification(_ context.Context, tenantID, refType, observationID, subjectLabel string) error {
	f.calls = append(f.calls, relabelArgs{tenantID, refType, observationID, subjectLabel})
	return f.err
}

func lumpSumCommand() domain.WeightCorrectionCommand {
	return domain.WeightCorrectionCommand{
		TenantID:       "11111111-1111-1111-1111-111111111111",
		ObservationID:  "22222222-2222-2222-2222-222222222222",
		RefType:        domain.VerificationRefTypeShed,
		WeightKg:       732,
		AnimalCount:    31,
		CorrectedBy:    "33333333-3333-3333-3333-333333333333",
		IdempotencyKey: "correction-1",
	}
}

// A corrected observation whose verification item is NOT relabelled leaves the
// verifier reading the weight she just replaced -- her own correction looks like it
// never landed. The label the store recomposed must reach the item.
func TestCorrectionRelabelsTheVerificationItemWithTheCorrectedWeight(t *testing.T) {
	store := &fakeCorrectionStore{result: domain.WeightCorrectionResult{
		ObservationID: "22222222-2222-2222-2222-222222222222",
		RefType:       domain.VerificationRefTypeShed,
		WeightKg:      732,
		AnimalCount:   31,
		SubjectLabel:  "Godel 1 - Part 3 · 732.0 kg · 31 goats",
		CorrectedAt:   time.Now(),
	}}
	relabeler := &fakeRelabeler{}
	svc := NewWeightCorrectionService(store, nil).WithVerificationRelabeler(relabeler)

	if _, err := svc.CorrectObservationWeight(context.Background(), lumpSumCommand()); err != nil {
		t.Fatalf("correction must succeed: %v", err)
	}
	if len(relabeler.calls) != 1 {
		t.Fatalf("expected exactly one relabel, got %d", len(relabeler.calls))
	}
	got := relabeler.calls[0]
	if got.subjectLabel != "Godel 1 - Part 3 · 732.0 kg · 31 goats" {
		t.Fatalf("relabel must carry the recomposed label, got %q", got.subjectLabel)
	}
	// The relabel has to ADDRESS the same record the item points at. A relabel sent
	// with the wrong grain or id silently updates nothing and leaves the stale weight
	// on screen -- a failure with no error to notice.
	if got.refType != domain.VerificationRefTypeShed || got.observationID != "22222222-2222-2222-2222-222222222222" {
		t.Fatalf("relabel must address the corrected observation, got %+v", got)
	}
}

// The relabel is a receipt for a write that already committed. Failing the request
// on it would tell the verifier her correction did not land while the farm's records
// say it did -- trading a stale label for a false negative.
func TestARelabelFailureDoesNotFailTheCorrection(t *testing.T) {
	store := &fakeCorrectionStore{result: domain.WeightCorrectionResult{
		ObservationID: "22222222-2222-2222-2222-222222222222",
		RefType:       domain.VerificationRefTypeShed,
		WeightKg:      732,
		SubjectLabel:  "Godel 1 - Part 3 · 732.0 kg",
	}}
	svc := NewWeightCorrectionService(store, nil).
		WithVerificationRelabeler(&fakeRelabeler{err: errors.New("verification unreachable")})

	result, err := svc.CorrectObservationWeight(context.Background(), lumpSumCommand())
	if err != nil {
		t.Fatalf("a relabel failure must not fail the correction: %v", err)
	}
	if result.WeightKg != 732 {
		t.Fatalf("the corrected result must still be returned, got %v", result.WeightKg)
	}
}

// Without a relabeler wired the correction still lands and every weighing read model
// shows the new number. Only the item's label lags -- never a reason to refuse a
// correction the verifier is entitled to make.
func TestCorrectionWorksWithNoRelabelerWired(t *testing.T) {
	store := &fakeCorrectionStore{result: domain.WeightCorrectionResult{WeightKg: 732}}
	if _, err := NewWeightCorrectionService(store, nil).
		CorrectObservationWeight(context.Background(), lumpSumCommand()); err != nil {
		t.Fatalf("correction must not require a relabeler: %v", err)
	}
}

// The service refuses an invalid correction BEFORE the store, and carries the
// field-level code the HTTP layer renders. A flat "request is invalid" would leave
// the verifier with no idea which field to fix.
func TestServiceRefusesAnInvalidCorrectionBeforeWriting(t *testing.T) {
	store := &fakeCorrectionStore{}
	cmd := lumpSumCommand()
	cmd.RefType = domain.VerificationRefTypeAnimal // an individual capture carries no head count

	_, err := NewWeightCorrectionService(store, nil).CorrectObservationWeight(context.Background(), cmd)
	if err == nil {
		t.Fatal("an individual correction carrying a head count must be refused")
	}
	if code := CorrectionCode(err); code != "animal_count_not_applicable" {
		t.Fatalf("the refusal must carry its field-level code, got %q", code)
	}
	// Refused BEFORE the store: nothing may be written on a command the domain rejects.
	if len(store.calls) != 0 {
		t.Fatalf("an invalid correction must not reach the store, got %d writes", len(store.calls))
	}
	// Still ErrInvalidArgument to every existing handler branch.
	if !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatal("the refusal must stay errors.Is-comparable to ErrInvalidArgument")
	}
}

// A store failure is the verifier's failure: the correction did NOT land, and
// reporting success would leave a wrong weight on the record with nobody looking.
func TestAStoreFailureFailsTheCorrection(t *testing.T) {
	store := &fakeCorrectionStore{err: ports.ErrCorrectionAfterClose}
	relabeler := &fakeRelabeler{}
	_, err := NewWeightCorrectionService(store, nil).
		WithVerificationRelabeler(relabeler).
		CorrectObservationWeight(context.Background(), lumpSumCommand())
	if !errors.Is(err, ports.ErrCorrectionAfterClose) {
		t.Fatalf("the store's refusal must reach the caller, got %v", err)
	}
	// And nothing may be relabelled for a correction that never happened.
	if len(relabeler.calls) != 0 {
		t.Fatalf("a failed correction must not relabel, got %d calls", len(relabeler.calls))
	}
}
