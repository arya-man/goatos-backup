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
// so the range checks, the closed-bucket refusal, the audit row and the relabel are one
// implementation. The route stays served for installed APKs that still show their own save button.

// weightCorrector is the seam onto weighing's own correction service, kept as an interface so a
// test can drive the applier without a database.
type weightCorrector interface {
	CorrectObservationWeight(ctx context.Context, cmd weighingdomain.WeightCorrectionCommand) (weighingdomain.WeightCorrectionResult, error)
	HasVerifierWeight(ctx context.Context, tenantID, refType, observationID string) (bool, error)
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
	// The head count is not editable by anyone since 2026-08-24 (it is snapshotted from the herd
	// register at submit and frozen). Verification's own validateMeasurement already refuses a
	// count for weighing because the category spec carries no CountLabel; the pass-through below
	// stays so a value that somehow arrives fails loudly in ValidateWeightCorrection rather than
	// being dropped.
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

// HasRecordedMeasurement reports whether a VERIFIER has already set this observation's weight.
//
// IT MUST NOT ANSWER "the row has a weight" (maintainer decision 2026-09-21). It used to return a
// flat true, which was correct while weighing's measurement was OPTIONAL -- the question was never
// asked, because verification only consults it for a RequiredForApprove category. Weighing's
// approve is now blind and mandatory, so this IS the gate on a blank approve, and a flat true would
// wave every unmeasured item through: every weighing row carries a weight from the moment the
// operator captured it, so "has a weight" is true before any verifier has looked at the video.
//
// The real question is whether the weight was replaced by a verifier, and weighing answers it from
// operator_weight_kg -- written on the first correction and never again. A false answer means the
// approve is refused with "record the value before approving", which is exactly right for an item
// nobody has read the scale for.
//
// The escape hatch this preserves: an item measured earlier through the standalone correction route
// (an installed APK still showing its own save button) answers true and stays approvable in one tap.
func (a *MeasurementApplier) HasRecordedMeasurement(ctx context.Context, tenantID string, source verificationdomain.SourceRef) (bool, error) {
	return a.corrections.HasVerifierWeight(ctx, tenantID, source.RefType, source.RefID)
}
