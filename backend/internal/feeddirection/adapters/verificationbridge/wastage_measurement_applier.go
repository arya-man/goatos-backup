package verificationbridge

import (
	"context"

	feeddirectionapp "github.com/vgoats/goatos/backend/internal/feeddirection/app"
	feeddirectionports "github.com/vgoats/goatos/backend/internal/feeddirection/ports"
	verificationapp "github.com/vgoats/goatos/backend/internal/verification/app"
	verificationdomain "github.com/vgoats/goatos/backend/internal/verification/domain"
)

// FEED WASTAGE'S HALF OF AN APPROVE THAT CARRIES A NUMBER (maintainer decision 2026-08-20).
//
// Wastage is the sharper case of the two. The operator submits a VIDEO ONLY, so the leftover weight
// is born on the verifier's screen -- and a save-then-approve pair meant the save relabelled the
// item, bumped row_version, and fenced out the approve she pressed next. Worse, approving without
// having saved completed a pen-day with no wastage recorded at all, and the producer's own
// ErrWastageMeasurementRequired only fired in the consumer AFTER the verdict was durable, leaving
// the item stuck mid-apply with nothing telling her why.
//
// She now types the number and presses Approve once, and an approve with no number is refused
// BEFORE the verdict, with a message naming the field.

// wastageMeasurer is the seam onto the wastage measurement service and the recorded-value read,
// kept as an interface so a test can drive the applier without a database.
type wastageMeasurer interface {
	RecordWastageMeasurement(ctx context.Context, cmd feeddirectionapp.WastageMeasurementCommand) (feeddirectionports.RecordWastageMeasurementResult, error)
}

// wastageMeasurementReader answers whether a completion already carries a number.
type wastageMeasurementReader interface {
	WastageMeasurementRecorded(ctx context.Context, tenantID, completionID string) (bool, error)
}

// WastageMeasurementApplier records the verifier's leftover weight as part of her approve.
type WastageMeasurementApplier struct {
	measurements wastageMeasurer
	recorded     wastageMeasurementReader
}

// NewWastageMeasurementApplier wires the applier over the measurement service and the store read.
func NewWastageMeasurementApplier(
	measurements *feeddirectionapp.WastageMeasurementService,
	recorded wastageMeasurementReader,
) *WastageMeasurementApplier {
	return &WastageMeasurementApplier{measurements: measurements, recorded: recorded}
}

var _ verificationapp.MeasurementApplier = (*WastageMeasurementApplier)(nil)

// ApplyMeasurement stores the measured leftover weight on the completion the item was raised for.
//
// It is the SAME WastageMeasurementService the standalone measurement route calls, so the range
// check, the idempotency reservation, the audit row and the relabel are one implementation. The
// route stays served for installed APKs that still show their own save button.
func (a *WastageMeasurementApplier) ApplyMeasurement(ctx context.Context, in verificationapp.MeasurementApply) error {
	_, err := a.measurements.RecordWastageMeasurement(ctx, feeddirectionapp.WastageMeasurementCommand{
		TenantID: in.TenantID,
		// The completion id is the item's source ref id -- the same id
		// EnqueueFeedWastageVerification put there. The client names a number and nothing else.
		CompletionID:   in.Source.RefID,
		WastageKg:      in.Value,
		RecordedBy:     in.VerifierID,
		IdempotencyKey: in.IdempotencyKey,
	})
	return err
}

// HasRecordedMeasurement reports whether this pen-day was already measured.
//
// Consulted only when an approve arrives with no number, so a pen measured earlier through the
// standalone route -- an installed APK, or the verifier's own earlier save -- is still approvable
// rather than stranded.
func (a *WastageMeasurementApplier) HasRecordedMeasurement(ctx context.Context, tenantID string, source verificationdomain.SourceRef) (bool, error) {
	if a.recorded == nil {
		return false, nil
	}
	return a.recorded.WastageMeasurementRecorded(ctx, tenantID, source.RefID)
}
