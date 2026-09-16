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
		MediaMeta:      mediaMeta(in.ProofRefs, in.MediaMeta),
		ContextRows:    contextRows(in.ContextRows),
		OperatorID:     ptrIfSet(in.OperatorID),
		ShedID:         ptrIfSet(in.ShedID),
		PartitionLabel: ptrIfSet(in.PartitionLabel),
		ParkID:         ptrIfSet(in.ParkID),
		CapturedAt:     in.CapturedAt,
		IdempotencyKey: in.IdempotencyKey,
	})
	return err
}

// mediaMeta maps the tasks meta (positional against refs) onto verification's shared builder.
// A request with no meta attaches none, so the registry's positional copy still fills in.
func mediaMeta(refs []string, meta []tasksdomain.MediaMetaItem) []verificationdomain.MediaMeta {
	if len(meta) == 0 || len(meta) != len(refs) {
		return nil
	}
	captures := make([]verificationdomain.ProofCapture, len(meta))
	for i, m := range meta {
		captures[i] = verificationdomain.ProofCapture{Title: m.Label, Kind: m.Kind}
	}
	return verificationdomain.BuildMediaMeta(captures)
}

func contextRows(rows []tasksdomain.EvidenceRow) []verificationdomain.ContextRow {
	if len(rows) == 0 {
		return nil
	}
	out := make([]verificationdomain.ContextRow, len(rows))
	for i, r := range rows {
		out[i] = verificationdomain.ContextRow{Label: r.Label, Value: r.Value, Group: r.Group}
	}
	return out
}

// EnqueueBirthStepVerification maps ONE recorded birth step to ONE verification item (category
// birth_evidence, ref module=counts / ref_type=workflow_birth_action / ref_id=action_id) carrying
// only that step's proofs. The key is per recording (domain.BirthStepReviewKey), so a retry heals
// and a re-shoot after a rejection opens a fresh item. The subject label is composed by the
// service, e.g. "Iodine dipping · Kid CPT-00123 · 2026-09-16 · Godel 1 - Part 3".
func (e *DeathEvidenceEnqueuer) EnqueueBirthStepVerification(ctx context.Context, in tasksapp.BirthStepVerificationEnqueueRequest) error {
	// The service composes MediaMeta positionally against the ordered refs (videos first) from
	// the step title and the register-resolved kinds; an older caller that set only
	// Proofs/ProofLabel still gets each ref named.
	meta := mediaMeta(in.ProofRefs, in.MediaMeta)
	if meta == nil {
		kinds := make(map[string]string, len(in.Proofs))
		for _, proof := range in.Proofs {
			kinds[proof.Ref] = proof.Kind
		}
		captures := make([]verificationdomain.ProofCapture, 0, len(in.ProofRefs))
		for _, ref := range in.ProofRefs {
			kind := kinds[ref]
			if kind == "" {
				kind = tasksdomain.ProofKindVideo
			} // legacy singular video proof
			captures = append(captures, verificationdomain.ProofCapture{Title: in.ProofLabel, Kind: kind})
		}
		meta = verificationdomain.BuildMediaMeta(captures)
	}

	_, err := e.verification.CreateItem(ctx, verificationdomain.CreateItem{
		TenantID:     in.TenantID,
		Vertical:     tasksdomain.VerificationVerticalCounts,
		Module:       tasksdomain.VerificationModuleCounts,
		Category:     tasksdomain.VerificationCategoryBirthEvidence,
		SubjectLabel: ptrIfSet(in.SubjectLabel),
		Source: verificationdomain.SourceRef{
			Module:  tasksdomain.VerificationModuleCounts,
			RefType: tasksdomain.VerificationRefTypeBirthAction,
			RefID:   in.ActionID,
		},
		MediaRefs:      in.ProofRefs,
		MediaMeta:      meta,
		ContextRows:    contextRows(in.ContextRows),
		OperatorID:     ptrIfSet(in.OperatorID),
		ShedID:         ptrIfSet(in.ShedID),
		PartitionLabel: ptrIfSet(in.PartitionLabel),
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
