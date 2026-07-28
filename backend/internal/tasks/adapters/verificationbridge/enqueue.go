// Package verificationbridge is the composition-layer adapter that connects the tasks module's
// death evidence trail to the generic verification module without either module importing the
// other's storage (mirrors countsbridge / feeddirection's verificationbridge).
package verificationbridge

import (
	"context"

	tasksapp "github.com/vgoats/goatos/backend/internal/tasks/app"
	tasksdomain "github.com/vgoats/goatos/backend/internal/tasks/domain"
	verificationdomain "github.com/vgoats/goatos/backend/internal/verification/domain"
)

// verificationCreator is the slice of the verification service this bridge needs: enqueue one item.
type verificationCreator interface {
	CreateItem(ctx context.Context, in verificationdomain.CreateItem) (verificationdomain.CreateItemResult, error)
}

// DeathEvidenceEnqueuer adapts verification's CreateItem to the tasks DeathVerificationEnqueuer
// port: ONE item (category death_evidence) carries BOTH death videos, ref
// (module=counts, ref_type=workflow_death_signoff, ref_id=workflow_id), so one verifier verdict
// covers the pair. Tasks never writes verification's tables.
type DeathEvidenceEnqueuer struct {
	verification verificationCreator
}

// New constructs the bridge over the verification service.
func New(v verificationCreator) *DeathEvidenceEnqueuer {
	return &DeathEvidenceEnqueuer{verification: v}
}

var _ tasksapp.DeathVerificationEnqueuer = (*DeathEvidenceEnqueuer)(nil)

// EnqueueDeathEvidenceVerification maps the tasks request to a verification CreateItem. CreateItem
// is idempotent on (tenant, idempotency_key), and the caller's key carries the workflow AND the
// proof pair, so a retry after a prior failure heals rather than duplicates while a post-rejection
// re-shoot still opens a fresh review item.
func (e *DeathEvidenceEnqueuer) EnqueueDeathEvidenceVerification(ctx context.Context, in tasksapp.DeathVerificationEnqueueRequest) error {
	_, err := e.verification.CreateItem(ctx, verificationdomain.CreateItem{
		TenantID:     in.TenantID,
		Vertical:     tasksdomain.VerificationVerticalCounts,
		Module:       tasksdomain.VerificationModuleCounts,
		Category:     tasksdomain.VerificationCategoryDeathEvidence,
		SubjectLabel: ptrIfSet(in.SubjectLabel),
		Source: verificationdomain.SourceRef{
			Module:  tasksdomain.VerificationModuleCounts,
			RefType: tasksdomain.VerificationRefTypeDeathSignoff,
			RefID:   in.WorkflowID,
		},
		MediaRefs:      in.ProofRefs,
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
