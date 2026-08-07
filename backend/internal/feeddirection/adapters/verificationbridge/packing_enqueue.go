package verificationbridge

import (
	"context"
	"fmt"

	feeddirectionapp "github.com/vgoats/goatos/backend/internal/feeddirection/app"
	feeddirectiondomain "github.com/vgoats/goatos/backend/internal/feeddirection/domain"
	"github.com/vgoats/goatos/backend/internal/platform/oploc"
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
	loc := oploc.OperationalLocation{ShedName: in.ShedName, PartitionLabel: in.PartitionLabel}
	locDisplay := loc.Display()
	baseLabel := ""
	if in.SessionNo > 0 {
		baseLabel = fmt.Sprintf("Session %d", in.SessionNo)
	}
	var label *string
	if baseLabel != "" && locDisplay != "" {
		fullLabel := baseLabel + " · " + locDisplay
		label = &fullLabel
	} else if baseLabel != "" {
		label = &baseLabel
	} else if locDisplay != "" {
		label = &locDisplay
	}
	_, err := e.verification.CreateItem(ctx, verificationdomain.CreateItem{
		TenantID: in.TenantID,
		Vertical: feeddirectiondomain.VerificationVerticalFeed,
		Module:   feeddirectiondomain.VerificationModuleFeed,
		Category: feeddirectiondomain.VerificationCategoryPacking,
		// The verifier's subject for a feed-packing item is the shed-session being packed; surface it
		// so the detail view shows which session's video is under review (shed/park/operator ride on
		// their own item fields). Backend owns this display string (dumb-renderer rule).
		SubjectLabel: label,
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

// packingSubjectLabel is the human subject shown to the verifier for a packing item: the shed-session
// number. Returns nil when the session is unset so the field stays omitted rather than reading "Session 0".
func packingSubjectLabel(sessionNo int32) *string {
	if sessionNo <= 0 {
		return nil
	}
	s := fmt.Sprintf("Session %d", sessionNo)
	return &s
}
