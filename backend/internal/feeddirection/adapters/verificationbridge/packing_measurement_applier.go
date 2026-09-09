package verificationbridge

import (
	"context"

	feeddirectiondomain "github.com/vgoats/goatos/backend/internal/feeddirection/domain"
	feeddirectionports "github.com/vgoats/goatos/backend/internal/feeddirection/ports"
	verificationapp "github.com/vgoats/goatos/backend/internal/verification/app"
	verificationdomain "github.com/vgoats/goatos/backend/internal/verification/domain"
)

// FEED PACKING'S HALF OF AN APPROVE THAT CARRIES THE NUMBERS (maintainer decision 2026-08-21).
//
// The packing verifier no longer judges the video against a printed expected ration. Her item
// carries one entry box per feed item (names only -- blind entry), she types the packed weight she
// can see for each, and presses Approve ONCE; the approve carries every reading (the 2026-08-20
// "THE APPROVE CARRIES THE NUMBER" rule, extended from one value to one per field). A verifier who
// cannot see a usable video rejects, which sends the bag to rework exactly as before.
//
// This applier is the producer's write seam: verification hands the validated entries here BEFORE
// the verdict is recorded, so a refusal (unknown completion, out-of-range weight) stops the whole
// approve rather than leaving an approved item beside readings that never landed. The
// intended-vs-entered variance is computed by the feed analytics execution read, never shown to
// the verifier.
//
// THE VERIFIER IS WARNED, NOT TOLD (maintainer decision 2026-09-09). Before the write, every reading
// is checked against the plan it was packed against (the completion's snapshot, else the frozen
// sheet). A reading more than domain.PackingEntryConfirmToleranceKg away is refused ONCE with a
// MeasurementConfirmationRequired that names the entry and a DIRECTION only -- "more than 500 g
// above/below the plan" -- never the planned figure or the gap, so blind entry survives. The same
// approve re-sent with VarianceAcknowledged records the readings, marking each flagged one as
// confirmed, so the record says she was warned and chose to stand by her reading.

// packingQuantitiesStore is the narrow slice of the packing store this applier needs.
type packingQuantitiesStore interface {
	RecordPackingVerifiedQuantities(ctx context.Context, p feeddirectionports.RecordPackingVerifiedQuantitiesParams) error
	PackingVerifiedQuantitiesRecorded(ctx context.Context, tenantID, completionID string) (bool, error)
	PackingPlannedQuantities(ctx context.Context, tenantID, completionID string) (map[string]float64, error)
}

// PackingMeasurementApplier records the verifier's per-item packed weights as part of her approve.
type PackingMeasurementApplier struct {
	store packingQuantitiesStore
}

// NewPackingMeasurementApplier wires the applier over the packing completion store.
func NewPackingMeasurementApplier(store packingQuantitiesStore) *PackingMeasurementApplier {
	return &PackingMeasurementApplier{store: store}
}

var _ verificationapp.MeasurementApplier = (*PackingMeasurementApplier)(nil)

// ApplyMeasurement checks the readings against the plan, then upserts them onto the completion the
// item was raised for. The completion id is the item's source ref id -- the client names numbers
// and nothing else. Labels are resolved from the item's own enqueued field list so the stored rows
// stay renderable.
func (a *PackingMeasurementApplier) ApplyMeasurement(ctx context.Context, in verificationapp.MeasurementApply) error {
	labels := make(map[string]string, len(in.Fields))
	for _, field := range in.Fields {
		labels[field.Key] = field.Label
	}
	// The plan is read BEFORE anything is written, so a refusal below leaves no row behind. An
	// error here (unknown completion) stops the approve the same way the write itself would.
	planned, err := a.store.PackingPlannedQuantities(ctx, in.TenantID, in.Source.RefID)
	if err != nil {
		return err
	}
	entries := make([]feeddirectionports.PackingVerifiedQuantity, 0, len(in.Entries))
	var notices []verificationapp.MeasurementFieldNotice
	for _, entry := range in.Entries {
		reading := feeddirectionports.PackingVerifiedQuantity{
			FeedItemKey:   entry.Key,
			FeedItemLabel: labels[entry.Key],
			EnteredKg:     entry.Value,
		}
		if plannedKg, ok := planned[entry.Key]; ok {
			plan := plannedKg
			reading.PlannedKg = &plan
			if direction, exceeds := feeddirectiondomain.PackingEntryVariance(entry.Value, plannedKg); exceeds {
				if in.VarianceAcknowledged {
					// Her second press: she looked again and stands by the number. Recorded as
					// such, so the reading carries the fact that it was questioned.
					reading.VarianceAcknowledged = true
				} else {
					notices = append(notices, verificationapp.MeasurementFieldNotice{
						Key:     entry.Key,
						Code:    direction,
						Message: feeddirectiondomain.PackingEntryVarianceMessage(direction),
					})
				}
			}
		}
		entries = append(entries, reading)
	}
	if len(notices) > 0 {
		return &verificationapp.MeasurementConfirmationRequired{
			Message: feeddirectiondomain.PackingEntryConfirmMessage,
			Fields:  notices,
		}
	}
	return a.store.RecordPackingVerifiedQuantities(ctx, feeddirectionports.RecordPackingVerifiedQuantitiesParams{
		TenantID:       in.TenantID,
		CompletionID:   in.Source.RefID,
		Entries:        entries,
		RecordedBy:     in.VerifierID,
		IdempotencyKey: in.IdempotencyKey,
	})
}

// HasRecordedMeasurement reports whether this completion already carries readings -- consulted
// only when an approve arrives without any, so an approve replayed after a prior successful apply
// is not stranded unapprovable.
func (a *PackingMeasurementApplier) HasRecordedMeasurement(ctx context.Context, tenantID string, source verificationdomain.SourceRef) (bool, error) {
	return a.store.PackingVerifiedQuantitiesRecorded(ctx, tenantID, source.RefID)
}
