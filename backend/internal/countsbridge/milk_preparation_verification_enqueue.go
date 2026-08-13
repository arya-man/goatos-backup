package countsbridge

import (
	"context"
	"fmt"

	countsapp "github.com/vgoats/goatos/backend/internal/counts/app"
	countsdomain "github.com/vgoats/goatos/backend/internal/counts/domain"
	verificationdomain "github.com/vgoats/goatos/backend/internal/verification/domain"
)

// MilkPreparationVerificationEnqueuer maps one farm-day preparation attempt to one verifier item.
// All applicable videos stay together and retain the domain's fixed step order.
type MilkPreparationVerificationEnqueuer struct {
	verification verificationCreator
}

func NewMilkPreparationVerificationEnqueuer(v verificationCreator) *MilkPreparationVerificationEnqueuer {
	return &MilkPreparationVerificationEnqueuer{verification: v}
}

var _ countsapp.MilkPreparationVerificationEnqueuer = (*MilkPreparationVerificationEnqueuer)(nil)

func (e *MilkPreparationVerificationEnqueuer) EnqueueMilkPreparationVerification(ctx context.Context, in countsapp.MilkPreparationVerificationEnqueueRequest) error {
	refs := make([]string, 0, len(in.StepProofs))
	for _, step := range in.StepProofs {
		refs = append(refs, step.ProofRef)
	}
	subject := fmt.Sprintf("Milk preparation · attempt %d · %d step videos", in.AttemptNo, len(refs))
	_, err := e.verification.CreateItem(ctx, verificationdomain.CreateItem{
		TenantID:     in.TenantID,
		Vertical:     countsdomain.VerificationVerticalMilkPreparation,
		Module:       countsdomain.VerificationModuleMilkPreparation,
		Category:     countsdomain.VerificationCategoryMilkPreparation,
		SubjectLabel: &subject,
		Source: verificationdomain.SourceRef{
			Module:  countsdomain.VerificationModuleMilkPreparation,
			RefType: countsdomain.VerificationRefTypeMilkPreparation,
			RefID:   in.CompletionID,
		},
		MediaRefs:      refs,
		OperatorID:     ptrIfSet(in.OperatorID),
		ParkID:         ptrIfSet(in.ParkID),
		CapturedAt:     in.CapturedAt,
		IdempotencyKey: in.IdempotencyKey,
	})
	return err
}
