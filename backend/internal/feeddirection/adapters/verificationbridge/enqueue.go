// Package verificationbridge holds the composition-layer adapter that connects the feeddirection
// module to the verification module's service API without either module importing the other's storage.
// This mirrors countsbridge (shifting) and sopbridge (vaccination).
package verificationbridge

import (
	"context"

	feeddirectionapp "github.com/vgoats/goatos/backend/internal/feeddirection/app"
	feeddirectiondomain "github.com/vgoats/goatos/backend/internal/feeddirection/domain"
	verificationdomain "github.com/vgoats/goatos/backend/internal/verification/domain"
)

// verificationCreator is the slice of the verification service this bridge needs: enqueue one item.
type verificationCreator interface {
	CreateItem(ctx context.Context, in verificationdomain.CreateItem) (verificationdomain.CreateItemResult, error)
}

// Enqueuer adapts the verification module's CreateItem to the feeddirection
// FeedDistributionVerificationEnqueuer port, so a completed feed distribution (mandatory video + water
// proof) becomes one generic verification item the verifier queue lists. Feeddirection never writes
// verification's tables.
type Enqueuer struct {
	verification verificationCreator
}

// New constructs the bridge over the verification service.
func New(v verificationCreator) *Enqueuer {
	return &Enqueuer{verification: v}
}

var _ feeddirectionapp.FeedDistributionVerificationEnqueuer = (*Enqueuer)(nil)

// EnqueueFeedDistributionVerification maps the feeddirection request to a verification CreateItem.
// Both proofs travel on ONE item (a verifier approves/rejects the pair together). CreateItem is
// idempotent on (tenant, idempotency_key), so a retry after a prior failure heals rather than
// duplicates.
func (e *Enqueuer) EnqueueFeedDistributionVerification(ctx context.Context, in feeddirectionapp.FeedDistributionVerificationEnqueueRequest) error {
	_, err := e.verification.CreateItem(ctx, verificationdomain.CreateItem{
		TenantID: in.TenantID,
		Vertical: feeddirectiondomain.VerificationVerticalFeed,
		Module:   feeddirectiondomain.VerificationModuleFeed,
		Category: feeddirectiondomain.VerificationCategoryFeed,
		Source: verificationdomain.SourceRef{
			Module:  feeddirectiondomain.VerificationModuleFeed,
			RefType: feeddirectiondomain.VerificationRefTypeFeed,
			RefID:   in.CompletionID,
		},
		// Both proofs on one item: the feed-distribution video and the water proof.
		MediaRefs:      []string{in.DistributionProofRef, in.WaterProofRef},
		OperatorID:     ptrIfSet(in.OperatorID),
		ShedID:         ptrIfSet(in.ShedID),
		ParkID:         ptrIfSet(in.ParkID),
		CapturedAt:     in.CapturedAt,
		IdempotencyKey: in.IdempotencyKey,
	})
	return err
}

func ptrIfSet(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
