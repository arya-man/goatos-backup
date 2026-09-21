package verificationbridge

import (
	"context"
	"errors"
	"strings"
	"testing"

	verificationapp "github.com/vgoats/goatos/backend/internal/verification/app"
	verificationdomain "github.com/vgoats/goatos/backend/internal/verification/domain"
	weighingdomain "github.com/vgoats/goatos/backend/internal/weighing/domain"
	weighingports "github.com/vgoats/goatos/backend/internal/weighing/ports"
)

// THE BLANK-APPROVE GATE (maintainer decision 2026-09-21).
//
// Weighing's approve is now blind and mandatory: the verifier is not shown the operator's weight,
// and the number she types becomes the recorded weight. Verification enforces that by refusing an
// approve that carries no number -- but only after asking the producer whether a verifier has
// ALREADY set one, so an item measured through the standalone correction route stays approvable.
//
// This applier used to answer that question with a flat `return true`, which was correct only
// while the measurement was optional and the question was therefore never asked. Left as it was,
// it would wave through every unmeasured weighing item: EVERY weighing row carries a weight from
// the moment the operator captured it, so "does this row have a weight" is true before any
// verifier has watched anything. These pin the distinction.

type stubCorrector struct {
	measured    bool
	measuredErr error
	askedRef    verificationdomain.SourceRef
	applied     []weighingdomain.WeightCorrectionCommand
	applyErr    error
}

func (s *stubCorrector) CorrectObservationWeight(_ context.Context, cmd weighingdomain.WeightCorrectionCommand) (weighingdomain.WeightCorrectionResult, error) {
	s.applied = append(s.applied, cmd)
	return weighingdomain.WeightCorrectionResult{}, s.applyErr
}

func (s *stubCorrector) HasVerifierWeight(_ context.Context, _, refType, observationID string) (bool, error) {
	s.askedRef = verificationdomain.SourceRef{RefType: refType, RefID: observationID}
	return s.measured, s.measuredErr
}

// An operator-captured weighing proof nobody has verified yet must report NO recorded measurement,
// so the blank approve is refused and the verifier is made to read the scale.
func TestUnverifiedWeighingProofReportsNoRecordedMeasurement(t *testing.T) {
	corrector := &stubCorrector{measured: false}
	applier := &MeasurementApplier{corrections: corrector}

	got, err := applier.HasRecordedMeasurement(context.Background(), "tenant", verificationdomain.SourceRef{
		RefType: weighingdomain.VerificationRefTypeAnimal,
		RefID:   "22222222-2222-2222-2222-222222222222",
	})
	if err != nil {
		t.Fatalf("HasRecordedMeasurement: %v", err)
	}
	if got {
		t.Fatal("an unverified weighing proof must report no recorded measurement; reporting one " +
			"lets a blank approve record the operator's weight as verified without anyone reading it")
	}
}

// The escape hatch: an item a verifier already corrected through the standalone route stays
// approvable in one tap, rather than stranding an installed APK's work as unapprovable.
func TestAlreadyCorrectedWeighingProofStaysApprovable(t *testing.T) {
	corrector := &stubCorrector{measured: true}
	applier := &MeasurementApplier{corrections: corrector}

	got, err := applier.HasRecordedMeasurement(context.Background(), "tenant", verificationdomain.SourceRef{
		RefType: weighingdomain.VerificationRefTypeShed,
		RefID:   "33333333-3333-3333-3333-333333333333",
	})
	if err != nil {
		t.Fatalf("HasRecordedMeasurement: %v", err)
	}
	if !got {
		t.Fatal("an already-corrected weighing proof must stay approvable without retyping the weight")
	}
	// Addressed from the ITEM's own source, never a target the client named.
	if corrector.askedRef.RefType != weighingdomain.VerificationRefTypeShed ||
		corrector.askedRef.RefID != "33333333-3333-3333-3333-333333333333" {
		t.Fatalf("asked about %+v, want the item's own source", corrector.askedRef)
	}
}

// A producer that cannot answer must FAIL the approve rather than let it through on an assumption.
// Swallowing this error would be the flat `return true` again, one layer down.
func TestUnanswerableMeasurementQuestionFailsTheApprove(t *testing.T) {
	boom := errors.New("database unavailable")
	applier := &MeasurementApplier{corrections: &stubCorrector{measuredErr: boom}}

	if _, err := applier.HasRecordedMeasurement(context.Background(), "tenant", verificationdomain.SourceRef{
		RefType: weighingdomain.VerificationRefTypeAnimal,
		RefID:   "22222222-2222-2222-2222-222222222222",
	}); !errors.Is(err, boom) {
		t.Fatalf("err = %v, want the producer's own error so the approve is refused", err)
	}
}

// A CLOSED BUCKET IS A REFUSAL, NOT A SERVER ERROR.
//
// Found by driving the real verifier drawer against a clone of the live data: she pressed Accept,
// the drawer closed, and NOTHING happened -- the server had returned 500 `internal_error` carrying
// "weighing: bucket closed, correction refused", which no surface renders.
//
// It became reachable because of this very decision. While the measurement was OPTIONAL an approve
// carrying no number never reached the correction write, so a closed bucket's item could still be
// approved; now EVERY weighing approve writes a weight, so every pending item whose bucket is
// already closed hits this path. On the QA clone that was 4 items that could not be approved at
// all, with nothing on screen to say why.
func TestClosedBucketIsRefusedInFarmWordsNotAServerError(t *testing.T) {
	applier := &MeasurementApplier{corrections: &stubCorrector{applyErr: weighingports.ErrCorrectionAfterClose}}

	err := applier.ApplyMeasurement(context.Background(), verificationapp.MeasurementApply{
		TenantID: "tenant",
		Source:   verificationdomain.SourceRef{RefType: weighingdomain.VerificationRefTypeAnimal, RefID: "obs"},
		Value:    27.6,
	})
	var appErr *verificationapp.Error
	if !errors.As(err, &appErr) {
		t.Fatalf("err = %#v, want a rendered verification refusal rather than a raw producer error", err)
	}
	if appErr.Code != "weighing_bucket_closed" {
		t.Fatalf("code = %q, want weighing_bucket_closed", appErr.Code)
	}
	// It must name the REMEDY. "Not editable" would leave her with no next step.
	if !strings.Contains(appErr.Message, "reopen") {
		t.Fatalf("message = %q, want it to name the reopen remedy", appErr.Message)
	}
}

// A superseded (withdrawn) lump-sum record is likewise a refusal she can read, not a 500.
func TestSupersededRecordIsRefusedInFarmWords(t *testing.T) {
	applier := &MeasurementApplier{corrections: &stubCorrector{applyErr: weighingports.ErrImmutable}}
	err := applier.ApplyMeasurement(context.Background(), verificationapp.MeasurementApply{
		TenantID: "tenant",
		Source:   verificationdomain.SourceRef{RefType: weighingdomain.VerificationRefTypeShed, RefID: "obs"},
		Value:    812.5,
	})
	var appErr *verificationapp.Error
	if !errors.As(err, &appErr) || appErr.Code != "weighing_record_superseded" {
		t.Fatalf("err = %#v, want weighing_record_superseded", err)
	}
}

// Anything weighing does NOT name stays raw, so a genuine fault is not disguised as a business
// refusal the verifier could act on.
func TestUnknownProducerFailureIsNotDressedUpAsARefusal(t *testing.T) {
	boom := errors.New("connection reset")
	applier := &MeasurementApplier{corrections: &stubCorrector{applyErr: boom}}
	err := applier.ApplyMeasurement(context.Background(), verificationapp.MeasurementApply{
		TenantID: "tenant",
		Source:   verificationdomain.SourceRef{RefType: weighingdomain.VerificationRefTypeAnimal, RefID: "obs"},
		Value:    27.6,
	})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %#v, want the producer's own error", err)
	}
}
