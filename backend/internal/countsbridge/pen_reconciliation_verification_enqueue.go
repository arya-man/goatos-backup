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
		MediaMeta:      penReconciliationMeta(in.MediaMeta),
		ContextRows:    penReconciliationRows(in.ContextRows),
		OperatorID:     ptrIfSet(in.OperatorID),
		ShedID:         ptrIfSet(in.ShedID),
		PartitionLabel: ptrIfSet(in.PartitionLabel),
		ParkID:         ptrIfSet(in.ParkID),
		CapturedAt:     in.CapturedAt,
		IdempotencyKey: in.IdempotencyKey,
	})
	return err
}

func penReconciliationMeta(meta []countsdomain.PenReconciliationProofMeta) []verificationdomain.MediaMeta {
	if len(meta) == 0 {
		return nil
	}
	captures := make([]verificationdomain.ProofCapture, len(meta))
	for i, m := range meta {
		captures[i] = verificationdomain.ProofCapture{Title: m.Label, Kind: m.Kind}
	}
	return verificationdomain.BuildMediaMeta(captures)
}

func penReconciliationRows(rows []countsdomain.PenReconciliationContextRow) []verificationdomain.ContextRow {
	if len(rows) == 0 {
		return nil
	}
	out := make([]verificationdomain.ContextRow, len(rows))
	for i, r := range rows {
		out[i] = verificationdomain.ContextRow{Label: r.Label, Value: r.Value, Group: r.Group}
	}
	return out
}
