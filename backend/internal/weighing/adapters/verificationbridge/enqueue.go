package verificationbridge

import (
	"context"

	verificationdomain "github.com/vgoats/goatos/backend/internal/verification/domain"
	weighingapp "github.com/vgoats/goatos/backend/internal/weighing/app"
	weighingdomain "github.com/vgoats/goatos/backend/internal/weighing/domain"
)

type verificationCreator interface {
	CreateItem(ctx context.Context, in verificationdomain.CreateItem) (verificationdomain.CreateItemResult, error)
}

type Enqueuer struct {
	verification verificationCreator
}

func New(v verificationCreator) *Enqueuer {
	return &Enqueuer{verification: v}
}

var _ weighingapp.VerificationEnqueuer = (*Enqueuer)(nil)

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
		MediaRefs:      in.MediaRefs,
		OperatorID:     ptrIfSet(in.OperatorID),
		ShedID:         ptrIfSet(in.ShedID),
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
