package countsbridge

import (
	"context"
	"fmt"
	"strconv"

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
		ContextRows:  milkPreparationContextRows(in.GoatMilkUsed, in.Answers),
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

// milkPreparationContextRows states what the operator ENTERED with the submission -- the milk
// litres used and the citric acid grams mixed -- so the verifier judges each quantity video
// against a claimed number instead of only confirming a clip exists. Labels are farm language,
// composed here because the backend owns visible copy; clients render them verbatim.
func milkPreparationContextRows(goatMilkUsed bool, a countsdomain.MilkPreparationAnswers) []verificationdomain.ContextRow {
	totalLitres := a.UHTMilkQuantityLitres
	if goatMilkUsed {
		totalLitres += a.GoatMilkQuantityLitres
	}
	if totalLitres <= 0 && a.CitricAcidGrams <= 0 {
		// An older client that sent no answers states nothing rather than claiming zero.
		return nil
	}
	milk := fmt.Sprintf("%s L", trimmedQuantity(totalLitres))
	if goatMilkUsed {
		milk = fmt.Sprintf("%s L (goat %s L + UHT %s L)",
			trimmedQuantity(totalLitres), trimmedQuantity(a.GoatMilkQuantityLitres), trimmedQuantity(a.UHTMilkQuantityLitres))
	}
	return []verificationdomain.ContextRow{
		{Label: "Milk used", Value: milk},
		{Label: "Citric acid", Value: fmt.Sprintf("%s g", trimmedQuantity(a.CitricAcidGrams))},
	}
}

// trimmedQuantity renders an operator-entered quantity without trailing binary-float noise:
// 20 stays "20", 12.5 stays "12.5"; never scientific notation.
func trimmedQuantity(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}
