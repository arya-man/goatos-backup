package verificationbridge

import (
	"context"

	feeddirectionapp "github.com/vgoats/goatos/backend/internal/feeddirection/app"
	feeddirectiondomain "github.com/vgoats/goatos/backend/internal/feeddirection/domain"
	verificationdomain "github.com/vgoats/goatos/backend/internal/verification/domain"
)

// PackingEnqueuer adapts the verification module's CreateItem to the feeddirection
// FeedPackingVerificationEnqueuer port, so a completed feed packing session (one mandatory video)
// becomes one generic verification item the verifier queue lists. It reuses the same verificationCreator
// slice as the distribution Enqueuer; only the category/ref_type and single media ref differ.
// Feeddirection never writes verification's tables.
type PackingEnqueuer struct {
	verification verificationCreator
}

// NewPacking constructs the packing bridge over the verification service.
func NewPacking(v verificationCreator) *PackingEnqueuer {
	return &PackingEnqueuer{verification: v}
}

var _ feeddirectionapp.FeedPackingVerificationEnqueuer = (*PackingEnqueuer)(nil)

// EnqueueFeedPackingVerification maps the feeddirection request to a verification CreateItem. The single
// packing video travels on ONE item. CreateItem is idempotent on (tenant, idempotency_key), so a retry
// after a prior failure heals rather than duplicates.
func (e *PackingEnqueuer) EnqueueFeedPackingVerification(ctx context.Context, in feeddirectionapp.FeedPackingVerificationEnqueueRequest) error {
	_, err := e.verification.CreateItem(ctx, verificationdomain.CreateItem{
		TenantID: in.TenantID,
		Vertical: feeddirectiondomain.VerificationVerticalFeed,
		Module:   feeddirectiondomain.VerificationModuleFeed,
		Category: feeddirectiondomain.VerificationCategoryPacking,
		Source: verificationdomain.SourceRef{
			Module:  feeddirectiondomain.VerificationModuleFeed,
			RefType: feeddirectiondomain.VerificationRefTypePacking,
			RefID:   in.CompletionID,
		},
		// One media ref: the packing video.
		MediaRefs:      []string{in.PackingProofRef},
		OperatorID:     ptrIfSet(in.OperatorID),
		ShedID:         ptrIfSet(in.ShedID),
		ParkID:         ptrIfSet(in.ParkID),
		CapturedAt:     in.CapturedAt,
		IdempotencyKey: in.IdempotencyKey,
	})
	return err
}
