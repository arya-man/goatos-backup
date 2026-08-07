package verificationbridge

import (
	"context"

	countsapp "github.com/vgoats/goatos/backend/internal/counts/app"
	countsdomain "github.com/vgoats/goatos/backend/internal/counts/domain"
	verificationdomain "github.com/vgoats/goatos/backend/internal/verification/domain"
)

type ShiftingEnqueuer struct{ verification verificationCreator }

func NewShifting(v verificationCreator) *ShiftingEnqueuer {
	return &ShiftingEnqueuer{verification: v}
}

var _ countsapp.ShiftingVerificationEnqueuer = (*ShiftingEnqueuer)(nil)

func (e *ShiftingEnqueuer) EnqueueShiftingMoveVerification(ctx context.Context, in countsapp.ShiftingVerificationEnqueueRequest) error {
	// The subject label is already composed in the service with location and animal count.
	// It is already composed via oploc.Display() using the backend-populated
	// DestinationShedName and DestinationPartitionLabel from the repository.
	_, err := e.verification.CreateItem(ctx, verificationdomain.CreateItem{
		TenantID:     in.TenantID,
		Vertical:     countsdomain.VerificationVerticalShifting,
		Module:       countsdomain.VerificationModuleShifting,
		Category:     countsdomain.VerificationCategoryShifting,
		SubjectLabel: ptrIfSet(in.SubjectLabel),
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
