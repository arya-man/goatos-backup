// Package countsbridge holds composition-layer adapters that connect the counts module to other
// modules' service APIs without either module importing the other's storage. This mirrors sopbridge.
package countsbridge

import (
	"context"

	countsapp "github.com/vgoats/goatos/backend/internal/counts/app"
	countsdomain "github.com/vgoats/goatos/backend/internal/counts/domain"
	verificationdomain "github.com/vgoats/goatos/backend/internal/verification/domain"
)

// verificationCreator is the slice of the verification service this bridge needs: enqueue one item.
type verificationCreator interface {
	CreateItem(ctx context.Context, in verificationdomain.CreateItem) (verificationdomain.CreateItemResult, error)
}

// ShiftingVerificationEnqueuer adapts the verification module's CreateItem to the counts
// ShiftingVerificationEnqueuer port, so a completed shed move (mandatory video) becomes one generic
// verification item the verifier queue lists. Counts never writes verification's tables.
type ShiftingVerificationEnqueuer struct {
	verification verificationCreator
}

// NewShiftingVerificationEnqueuer constructs the bridge over the verification service.
func NewShiftingVerificationEnqueuer(v verificationCreator) *ShiftingVerificationEnqueuer {
	return &ShiftingVerificationEnqueuer{verification: v}
}

var _ countsapp.ShiftingVerificationEnqueuer = (*ShiftingVerificationEnqueuer)(nil)

// EnqueueShiftingMoveVerification maps the counts request to a verification CreateItem. CreateItem is
// idempotent on (tenant, idempotency_key), so a retry after a prior failure heals rather than
// duplicates.
func (e *ShiftingVerificationEnqueuer) EnqueueShiftingMoveVerification(ctx context.Context, in countsapp.ShiftingVerificationEnqueueRequest) error {
	_, err := e.verification.CreateItem(ctx, verificationdomain.CreateItem{
		TenantID:     in.TenantID,
		Vertical:     countsdomain.VerificationVerticalShifting,
		Module:       countsdomain.VerificationModuleShifting,
		Category:     countsdomain.VerificationCategoryShifting,
		SubjectLabel: ptrIfSet(in.SubjectLabel),
		SubjectNote:  ptrIfSet(in.SubjectNote),
		Source: verificationdomain.SourceRef{
			Module:  countsdomain.VerificationModuleShifting,
			RefType: countsdomain.VerificationRefTypeShifting,
			RefID:   in.ShiftingEventID,
		},
		MediaRefs:      in.MediaRefs,
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
