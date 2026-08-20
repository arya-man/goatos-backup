package verificationbridge

import (
	"context"

	verificationapp "github.com/vgoats/goatos/backend/internal/verification/app"
	verificationdomain "github.com/vgoats/goatos/backend/internal/verification/domain"
	weighingapp "github.com/vgoats/goatos/backend/internal/weighing/app"
	weighingdomain "github.com/vgoats/goatos/backend/internal/weighing/domain"
)

// WEIGHING'S HALF OF AN APPROVE THAT CARRIES A WEIGHT (maintainer decision 2026-08-20).
//
// The verifier used to save the corrected weight and then approve, two acts for one judgement --
// and the save relabelled the item, bumping row_version, so the approve she pressed next was
// fenced out and silently did nothing. The weight now rides the approve, and verification reaches
// the write through this adapter.
//
// Nothing here is new behaviour: it is the SAME WeightCorrectionService the correction route calls,
// so the range checks, the lump-sum-only head count, the closed-bucket refusal, the audit row and
// the relabel are one implementation. The route stays served for installed APKs that still show
// their own save button.

// weightCorrector is the seam onto weighing's own correction service, kept as an interface so a
// test can drive the applier without a database.
type weightCorrector interface {
	CorrectObservationWeight(ctx context.Context, cmd weighingdomain.WeightCorrectionCommand) (weighingdomain.WeightCorrectionResult, error)
}

// MeasurementApplier applies a verifier's corrected weight as part of her approve.
type MeasurementApplier struct {
	corrections weightCorrector
}

// NewMeasurementApplier wires the applier over weighing's correction service.
func NewMeasurementApplier(corrections *weighingapp.WeightCorrectionService) *MeasurementApplier {
	return &MeasurementApplier{corrections: corrections}
}

var _ verificationapp.MeasurementApplier = (*MeasurementApplier)(nil)

// ApplyMeasurement writes the corrected weight onto the observation the item was raised for.
//
// The observation id and ref type come from the ITEM's source, which is the same pair
// EnqueueWeighingVerification put there -- the verifier's client names a number and nothing else.
func (a *MeasurementApplier) ApplyMeasurement(ctx context.Context, in verificationapp.MeasurementApply) error {
	// Zero means "leave the recorded count alone" in the weighing command, which is exactly what a
	// nil count means here. The service refuses a count outright on an individual capture, so a
	// stray one fails loudly rather than being dropped.
	animalCount := 0
	if in.Count != nil {
		animalCount = *in.Count
	}
	_, err := a.corrections.CorrectObservationWeight(ctx, weighingdomain.WeightCorrectionCommand{
		TenantID:       in.TenantID,
		ObservationID:  in.Source.RefID,
		RefType:        in.Source.RefType,
		WeightKg:       in.Value,
		AnimalCount:    animalCount,
		Reason:         in.Reason,
		CorrectedBy:    in.VerifierID,
		IdempotencyKey: in.IdempotencyKey,
	})
	return err
}

// HasRecordedMeasurement always reports true for weighing.
//
// It is only consulted for a category whose spec is RequiredForApprove, and weighing's is not: the
// OPERATOR already recorded a weight when he captured the animal, so there is never a weighing item
// with no number on it. The blank field means "his weight is right", which stays a single tap.
func (a *MeasurementApplier) HasRecordedMeasurement(context.Context, string, verificationdomain.SourceRef) (bool, error) {
	return true, nil
}
