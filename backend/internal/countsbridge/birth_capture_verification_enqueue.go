package countsbridge

import (
	"context"
	"errors"

	countsapp "github.com/vgoats/goatos/backend/internal/counts/app"
	countsdomain "github.com/vgoats/goatos/backend/internal/counts/domain"
	tasksdomain "github.com/vgoats/goatos/backend/internal/tasks/domain"
	verificationdomain "github.com/vgoats/goatos/backend/internal/verification/domain"
)

// BirthCaptureVerificationEnqueuer adapts verification's CreateItem to the counts
// BirthCaptureVerificationEnqueuer port: ONE birth_evidence item per litter carrying the Add
// birth form's own proofs (ref module=counts, ref_type=birth_capture, ref_id=birth_event_id),
// each named by its authored slot title and register kind, the answers as grouped context rows.
type BirthCaptureVerificationEnqueuer struct {
	verification verificationCreator
}

// NewBirthCaptureVerificationEnqueuer constructs the bridge.
func NewBirthCaptureVerificationEnqueuer(v verificationCreator) *BirthCaptureVerificationEnqueuer {
	return &BirthCaptureVerificationEnqueuer{verification: v}
}

var _ countsapp.BirthCaptureVerificationEnqueuer = (*BirthCaptureVerificationEnqueuer)(nil)
var _ countsapp.BirthCaptureVerificationWithdrawer = (*BirthCaptureVerificationEnqueuer)(nil)

// sourceWithdrawer is verification's producer retire seam (pending items only). Both the
// verification app service and its Postgres repository implement it.
type sourceWithdrawer interface {
	WithdrawItemsBySource(ctx context.Context, tenantID, sourceModule, sourceRefType string, sourceRefIDs []string) (int, error)
}

// WithdrawBirthCaptureVerification withdraws the still-pending birth_capture items of one litter
// when its birth is rejected (maintainer decision 2026-09-25). A verdict already cast is untouched.
func (e *BirthCaptureVerificationEnqueuer) WithdrawBirthCaptureVerification(ctx context.Context, tenantID, birthEventID string) error {
	w, ok := e.verification.(sourceWithdrawer)
	if !ok {
		return errors.New("countsbridge: verification withdraw seam is not wired")
	}
	_, err := w.WithdrawItemsBySource(ctx, tenantID, tasksdomain.VerificationModuleCounts, countsdomain.VerificationRefTypeBirthCapture, []string{birthEventID})
	return err
}

// EnqueueBirthCaptureVerification is idempotent on the refs-keyed idempotency key.
func (e *BirthCaptureVerificationEnqueuer) EnqueueBirthCaptureVerification(ctx context.Context, in countsapp.BirthCaptureVerificationEnqueueRequest) error {
	var meta []verificationdomain.MediaMeta
	if len(in.MediaMeta) == len(in.ProofRefs) && len(in.MediaMeta) > 0 {
		captures := make([]verificationdomain.ProofCapture, len(in.MediaMeta))
		for i, m := range in.MediaMeta {
			captures[i] = verificationdomain.ProofCapture{Title: m.Label, Kind: m.Kind}
		}
		meta = verificationdomain.BuildMediaMeta(captures)
	}
	var rows []verificationdomain.ContextRow
	for _, r := range in.ContextRows {
		rows = append(rows, verificationdomain.ContextRow{Label: r.Label, Value: r.Value, Group: r.Group})
	}
	_, err := e.verification.CreateItem(ctx, verificationdomain.CreateItem{
		TenantID:     in.TenantID,
		Vertical:     tasksdomain.VerificationVerticalCounts,
		Module:       tasksdomain.VerificationModuleCounts,
		Category:     tasksdomain.VerificationCategoryBirthEvidence,
		SubjectLabel: ptrIfSet(in.SubjectLabel),
		ContextRows:  rows,
		Source: verificationdomain.SourceRef{
			Module:  countsdomain.VerificationModuleCounts,
			RefType: countsdomain.VerificationRefTypeBirthCapture,
			RefID:   in.BirthEventID,
		},
		MediaRefs:      in.ProofRefs,
		MediaMeta:      meta,
		OperatorID:     ptrIfSet(in.OperatorID),
		ShedID:         ptrIfSet(in.ShedID),
		PartitionLabel: ptrIfSet(in.PartitionLabel),
		ParkID:         ptrIfSet(in.ParkID),
		CapturedAt:     in.CapturedAt,
		IdempotencyKey: in.IdempotencyKey,
	})
	return err
}
