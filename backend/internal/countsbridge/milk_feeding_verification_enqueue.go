package countsbridge

import (
	"context"
	"fmt"

	countsapp "github.com/vgoats/goatos/backend/internal/counts/app"
	countsdomain "github.com/vgoats/goatos/backend/internal/counts/domain"
	verificationdomain "github.com/vgoats/goatos/backend/internal/verification/domain"
)

type MilkFeedingVerificationEnqueuer struct{ verification verificationCreator }

func NewMilkFeedingVerificationEnqueuer(v verificationCreator) *MilkFeedingVerificationEnqueuer {
	return &MilkFeedingVerificationEnqueuer{verification: v}
}

var _ countsapp.MilkFeedingVerificationEnqueuer = (*MilkFeedingVerificationEnqueuer)(nil)

func (e *MilkFeedingVerificationEnqueuer) EnqueueMilkFeedingVerification(ctx context.Context, in countsapp.MilkFeedingVerificationEnqueueRequest) error {
	refs := make([]string, 0, len(in.StepProofs))
	for _, step := range in.StepProofs {
		refs = append(refs, step.ProofRef)
	}
	subject := fmt.Sprintf("Milk feeding · attempt %d · clean bottles + mixing/filling", in.AttemptNo)
	_, err := e.verification.CreateItem(ctx, verificationdomain.CreateItem{TenantID: in.TenantID, Vertical: countsdomain.VerificationVerticalMilkFeeding, Module: countsdomain.VerificationModuleMilkFeeding, Category: countsdomain.VerificationCategoryMilkFeeding, SubjectLabel: &subject, Source: verificationdomain.SourceRef{Module: countsdomain.VerificationModuleMilkFeeding, RefType: countsdomain.VerificationRefTypeMilkFeeding, RefID: in.CompletionID}, MediaRefs: refs, OperatorID: ptrIfSet(in.OperatorID), ParkID: ptrIfSet(in.ParkID), CapturedAt: in.CapturedAt, IdempotencyKey: in.IdempotencyKey})
	return err
}
