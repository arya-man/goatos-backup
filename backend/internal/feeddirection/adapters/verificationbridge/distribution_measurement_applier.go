package verificationbridge

import (
	"context"
	"strings"

	feeddirectiondomain "github.com/vgoats/goatos/backend/internal/feeddirection/domain"
	feeddirectionports "github.com/vgoats/goatos/backend/internal/feeddirection/ports"
	verificationapp "github.com/vgoats/goatos/backend/internal/verification/app"
	verificationdomain "github.com/vgoats/goatos/backend/internal/verification/domain"
)

// FEED DISTRIBUTION'S HALF OF AN APPROVE THAT CARRIES THE NUMBER (maintainer decision 2026-09-28).
//
// The distribution verifier used to only look at the operator's feed-weight photo. Her item now
// carries ONE blind entry box, "Total feed given (kg)" -- the feed is mixed by the time it reaches
// the trough, so there is one combined quantity, never one per feed item -- and her approve carries
// the number she reads off the photo / video.
//
// It rides the per-field measurement path packing already uses (one field, key "total_feed"), so both
// clients' existing blind entry box, Approve hold and "I checked the video again" confirmation serve
// it unchanged. What differs is the RULE, and that stays here: the reading is checked against the
// planned pen-session total with a 5% band (domain.DistributionEntryVariance), never packing's 500 g.
//
// Runs BEFORE the verdict, so a refusal (unknown completion, out-of-range weight, an unconfirmed
// variance) stops the whole approve rather than approving beside a reading that never landed.

// distributionFeedStore is the narrow slice of the distribution store this applier needs.
type distributionFeedStore interface {
	DistributionPlannedFeedKg(ctx context.Context, tenantID, completionID string) (float64, bool, error)
	RecordDistributionVerifiedFeed(ctx context.Context, p feeddirectionports.RecordDistributionVerifiedFeedParams) error
	DistributionVerifiedFeedRecorded(ctx context.Context, tenantID, completionID string) (bool, error)
}

// DistributionMeasurementApplier records the verifier's total-feed reading as part of her approve.
type DistributionMeasurementApplier struct {
	store distributionFeedStore
}

// NewDistributionMeasurementApplier wires the applier over the distribution completion store.
func NewDistributionMeasurementApplier(store distributionFeedStore) *DistributionMeasurementApplier {
	return &DistributionMeasurementApplier{store: store}
}

var _ verificationapp.MeasurementApplier = (*DistributionMeasurementApplier)(nil)

// ApplyMeasurement checks the reading against the plan, then writes it onto the completion the item
// was raised for. The completion id is the item's source ref id -- the client names a number and
// nothing else. Verification has already checked that the one declared box is filled and that no
// unknown key rides along.
func (a *DistributionMeasurementApplier) ApplyMeasurement(ctx context.Context, in verificationapp.MeasurementApply) error {
	var (
		entered float64
		found   bool
	)
	for _, entry := range in.Entries {
		if strings.TrimSpace(entry.Key) == feeddirectiondomain.DistributionTotalFeedKey {
			entered, found = entry.Value, true
			break
		}
	}
	if !found {
		// Verification validates entries against the item's own field list, so this is reachable
		// only for an item enqueued without the box. Refuse by name rather than store nothing.
		return verificationapp.UnprocessableField(
			"measurement_required", "record the total feed before approving",
			"measurement.entries", "required", feeddirectiondomain.DistributionTotalFeedLabel,
		)
	}

	if entered > feeddirectiondomain.MaxDistributionTotalFeedKg {
		// A slipped extra digit. Named here as a field problem she can fix, rather than reaching the
		// store's range check as a server error.
		return verificationapp.UnprocessableField(
			"invalid_measurement", "the total feed is not a usable weight",
			"measurement.entries", "value_out_of_range", "enter the total feed weight you can read in the video",
		)
	}

	// The plan is read BEFORE anything is written, so a refusal below leaves no row behind.
	plannedKg, hasPlan, err := a.store.DistributionPlannedFeedKg(ctx, in.TenantID, in.Source.RefID)
	if err != nil {
		return err
	}
	params := feeddirectionports.RecordDistributionVerifiedFeedParams{
		TenantID:       in.TenantID,
		CompletionID:   in.Source.RefID,
		EnteredKg:      entered,
		RecordedBy:     in.VerifierID,
		IdempotencyKey: in.IdempotencyKey,
		TraceID:        in.TraceID,
	}
	if hasPlan {
		plan := plannedKg
		params.PlannedKg = &plan
		if direction, exceeds := feeddirectiondomain.DistributionEntryVariance(entered, plannedKg); exceeds {
			if !in.VarianceAcknowledged {
				// Warned, not told: a direction and a sentence, never the plan or the gap.
				return &verificationapp.MeasurementConfirmationRequired{
					Message: feeddirectiondomain.DistributionEntryConfirmMessage,
					Fields: []verificationapp.MeasurementFieldNotice{{
						Key:     feeddirectiondomain.DistributionTotalFeedKey,
						Code:    direction,
						Message: feeddirectiondomain.DistributionEntryVarianceMessage(direction),
					}},
				}
			}
			// Her second press: she looked again and stands by the number.
			params.VarianceAcknowledged = true
		}
	}
	return a.store.RecordDistributionVerifiedFeed(ctx, params)
}

// HasRecordedMeasurement reports whether the completion's current submission already carries a
// reading -- consulted only when an approve arrives without one, so an approve replayed after a
// prior successful apply is not stranded unapprovable.
func (a *DistributionMeasurementApplier) HasRecordedMeasurement(ctx context.Context, tenantID string, source verificationdomain.SourceRef) (bool, error) {
	return a.store.DistributionVerifiedFeedRecorded(ctx, tenantID, source.RefID)
}

// distributionMeasurementFields is the one box every distribution item carries.
func distributionMeasurementFields() []verificationdomain.MeasurementField {
	return []verificationdomain.MeasurementField{{
		Key:   feeddirectiondomain.DistributionTotalFeedKey,
		Label: feeddirectiondomain.DistributionTotalFeedLabel,
	}}
}
