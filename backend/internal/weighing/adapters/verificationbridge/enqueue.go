package verificationbridge

import (
	"context"

	verificationdomain "github.com/vgoats/goatos/backend/internal/verification/domain"
	weighingapp "github.com/vgoats/goatos/backend/internal/weighing/app"
	weighingdomain "github.com/vgoats/goatos/backend/internal/weighing/domain"
)

type verificationCreator interface {
	CreateItem(ctx context.Context, in verificationdomain.CreateItem) (verificationdomain.CreateItemResult, error)
	// WithdrawItemsBySource is verification's own retire seam. Weighing calls it
	// instead of touching verification_items so a reopened bucket's superseded
	// submission stops being decidable without this module writing another
	// module's tables.
	WithdrawItemsBySource(ctx context.Context, tenantID, sourceModule, sourceRefType string, sourceRefIDs []string) (int, error)
}

type Enqueuer struct {
	verification verificationCreator
}

func New(v verificationCreator) *Enqueuer {
	return &Enqueuer{verification: v}
}

var _ weighingapp.VerificationEnqueuer = (*Enqueuer)(nil)
var _ weighingapp.VerificationWithdrawer = (*Enqueuer)(nil)

// WithdrawWeighingVerification retires the still-pending items raised for weighing
// source records that no longer represent the bucket's work (ReopenScope stamps
// withdrawn_at on the lump-sum submission). RefID is the observation id -- the same
// id EnqueueWeighingVerification put in Source.RefID.
func (e *Enqueuer) WithdrawWeighingVerification(ctx context.Context, tenantID, refType string, observationIDs []string) error {
	if len(observationIDs) == 0 {
		return nil
	}
	_, err := e.verification.WithdrawItemsBySource(ctx, tenantID, weighingdomain.VerificationModuleWeighing, refType, observationIDs)
	return err
}

func (e *Enqueuer) EnqueueWeighingVerification(ctx context.Context, in weighingapp.VerificationEnqueueRequest) error {
	_, err := e.verification.CreateItem(ctx, verificationdomain.CreateItem{
		TenantID:     in.TenantID,
		Vertical:     weighingdomain.VerificationVerticalWeighing,
		Module:       weighingdomain.VerificationModuleWeighing,
		Category:     weighingdomain.VerificationCategoryWeighing,
		SubjectLabel: ptrIfSet(in.SubjectLabel),
		Source: verificationdomain.SourceRef{
			Module:  weighingdomain.VerificationModuleWeighing,
			RefType: in.Category,
			RefID:   in.ObservationID,
		},
		MediaRefs:  in.MediaRefs,
		OperatorID: ptrIfSet(in.OperatorID),
		ShedID:     ptrIfSet(in.ShedID),
		// ParkID is the notification routing key: the verification pending consumer resolves the
		// park's verify-duty holders from it and no-ops on a blank park (see the sibling feed and
		// shifting bridges, which pass it for the same reason).
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
