// Package verificationbridge holds the composition-layer adapter that connects the feeddirection
// module to the verification module's service API without either module importing the other's storage.
// This mirrors countsbridge (shifting) and sopbridge (vaccination).
package verificationbridge

import (
	"context"
	"fmt"

	feeddirectionapp "github.com/vgoats/goatos/backend/internal/feeddirection/app"
	feeddirectiondomain "github.com/vgoats/goatos/backend/internal/feeddirection/domain"
	verificationdomain "github.com/vgoats/goatos/backend/internal/verification/domain"
)

// verificationCreator is the slice of the verification service this bridge needs: enqueue one item.
type verificationCreator interface {
	CreateItem(ctx context.Context, in verificationdomain.CreateItem) (verificationdomain.CreateItemResult, error)
}

// Enqueuer adapts the verification module's CreateItem to the feeddirection
// FeedDistributionVerificationEnqueuer port, so a completed feed distribution (weight photo +
// distribution video + water video) becomes one generic verification item the verifier queue lists.
// Feeddirection never writes verification's tables.
type Enqueuer struct {
	verification verificationCreator
}

// New constructs the bridge over the verification service.
func New(v verificationCreator) *Enqueuer {
	return &Enqueuer{verification: v}
}

var _ feeddirectionapp.FeedDistributionVerificationEnqueuer = (*Enqueuer)(nil)

// EnqueueFeedDistributionVerification maps the feeddirection request to a verification CreateItem.
// All THREE proofs travel on ONE item (a verifier approves/rejects the set together). CreateItem is
// idempotent on (tenant, idempotency_key), so a retry after a prior failure heals rather than
// duplicates.
func (e *Enqueuer) EnqueueFeedDistributionVerification(ctx context.Context, in feeddirectionapp.FeedDistributionVerificationEnqueueRequest) error {
	subjectLabel := feedDistributionSubjectLabel(in.SessionNo)
	_, err := e.verification.CreateItem(ctx, verificationdomain.CreateItem{
		TenantID:     in.TenantID,
		Vertical:     feeddirectiondomain.VerificationVerticalFeed,
		Module:       feeddirectiondomain.VerificationModuleFeed,
		Category:     feeddirectiondomain.VerificationCategoryFeed,
		SubjectLabel: subjectLabel,
		Source: verificationdomain.SourceRef{
			Module:  feeddirectiondomain.VerificationModuleFeed,
			RefType: feeddirectiondomain.VerificationRefTypeFeed,
			RefID:   in.CompletionID,
		},
		// All three proofs on ONE item, in CAPTURE ORDER -- weight photo, distribution video, water
		// video. The verifier reviews them in the order the work happened, and the weight leads because
		// it is the only capture that can be checked against the expected ration the item carries.
		//
		// A blank weight ref is DROPPED rather than sent as an empty entry: a grandfathered row
		// (migration 000151) re-enqueued after a rework verdict genuinely has no weight photo, and an
		// empty string would reach the verifier as a media slot that can never load.
		MediaRefs:      mediaRefs(in.FeedWeightProofRef, in.DistributionProofRef, in.WaterProofRef),
		OperatorID:     ptrIfSet(in.OperatorID),
		ShedID:         ptrIfSet(in.ShedID),
		PartitionLabel: ptrIfSet(in.PartitionLabel),
		ParkID:         ptrIfSet(in.ParkID),
		CapturedAt:     in.CapturedAt,
		IdempotencyKey: in.IdempotencyKey,
	})
	return err
}

// feedDistributionSubjectLabel is the SESSION and nothing else.
//
// It used to append the raw shed UUID and a "Pen N" fragment, so a verifier's card read
// "Session 2 · 62241795-628e-58ef-9591-aa384fb0f0f7 · Pen 1" -- a database id rendered as farm copy
// (the never-render-a-UUID rule), next to a pen the card was ALREADY showing from its own
// partition_label field. Location rides on ShedID/PartitionLabel and is composed once at the wire
// boundary by oploc.Display(); repeating it here duplicated it and, when the shed name could not be
// resolved, leaked the id instead.
func feedDistributionSubjectLabel(sessionNo int32) *string {
	if sessionNo <= 0 {
		return nil
	}
	label := fmt.Sprintf("Session %d", sessionNo)
	return &label
}

// mediaRefs keeps the supplied proof references in order and drops the blanks, so the item's media
// list never carries a slot the verifier cannot open.
func mediaRefs(refs ...string) []string {
	out := make([]string, 0, len(refs))
	for _, ref := range refs {
		if ref != "" {
			out = append(out, ref)
		}
	}
	return out
}

func ptrIfSet(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
