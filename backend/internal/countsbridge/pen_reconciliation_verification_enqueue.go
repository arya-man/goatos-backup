package countsbridge

import (
	"context"

	countsapp "github.com/vgoats/goatos/backend/internal/counts/app"
	countsdomain "github.com/vgoats/goatos/backend/internal/counts/domain"
	verificationdomain "github.com/vgoats/goatos/backend/internal/verification/domain"
)

// PenReconciliationVerificationEnqueuer adapts the verification module's CreateItem to the
// counts PenReconciliationVerificationEnqueuer port, so a submitted return video becomes one
// generic verification item the verifier queue lists. Counts never writes verification's
// tables.
type PenReconciliationVerificationEnqueuer struct {
	verification verificationCreator
}

// NewPenReconciliationVerificationEnqueuer constructs the bridge over the verification
// service.
func NewPenReconciliationVerificationEnqueuer(v verificationCreator) *PenReconciliationVerificationEnqueuer {
	return &PenReconciliationVerificationEnqueuer{verification: v}
}

var _ countsapp.PenReconciliationVerificationEnqueuer = (*PenReconciliationVerificationEnqueuer)(nil)

// EnqueuePenReconciliationVerification maps the counts request to a verification CreateItem.
// CreateItem is idempotent on (tenant, idempotency_key), so a retry after a prior failure
// heals rather than duplicates.
func (e *PenReconciliationVerificationEnqueuer) EnqueuePenReconciliationVerification(
	ctx context.Context, in countsapp.PenReconciliationVerificationEnqueueRequest,
) error {
	_, err := e.verification.CreateItem(ctx, verificationdomain.CreateItem{
		TenantID:     in.TenantID,
		Vertical:     countsdomain.VerificationVerticalPenReconciliation,
		Module:       countsdomain.VerificationModulePenReconciliation,
		Category:     countsdomain.VerificationCategoryPenReconciliation,
		SubjectLabel: ptrIfSet(in.SubjectLabel),
		Source: verificationdomain.SourceRef{
			Module:  countsdomain.VerificationModulePenReconciliation,
			RefType: countsdomain.VerificationRefTypePenReconciliation,
			RefID:   in.CardID,
		},
		MediaRefs:      in.MediaRefs,
		OperatorID:     ptrIfSet(in.OperatorID),
		ShedID:         ptrIfSet(in.ShedID),
		PartitionLabel: ptrIfSet(in.PartitionLabel),
		ParkID:         ptrIfSet(in.ParkID),
		CapturedAt:     in.CapturedAt,
		IdempotencyKey: in.IdempotencyKey,
	})
	return err
}
