package verificationbridge

import (
	"context"

	verificationdomain "github.com/vgoats/goatos/backend/internal/verification/domain"
	weighingapp "github.com/vgoats/goatos/backend/internal/weighing/app"
	weighingdomain "github.com/vgoats/goatos/backend/internal/weighing/domain"
)

type verificationCreator interface {
	CreateItem(ctx context.Context, in verificationdomain.CreateItem) (verificationdomain.CreateItemResult, error)
	// WithdrawItemsBySource is verification's own retire seam. Weighing calls it
	// instead of touching verification_items so a reopened bucket's superseded
	// submission stops being decidable without this module writing another
	// module's tables.
	WithdrawItemsBySource(ctx context.Context, tenantID, sourceModule, sourceRefType string, sourceRefIDs []string) (int, error)
	// MarkVerdictApplied is verification's ack seam. Same reason as the withdraw seam above:
	// weighing tells verification something happened rather than reaching into its tables.
	MarkVerdictApplied(ctx context.Context, tenantID, sourceModule, sourceRefType string, sourceRefIDs []string, appliedByModule string) (int, error)
	// RelabelItemBySource is verification's relabel seam, used after the verifier corrects a
	// weight: the subject label states the weight, so the label has to be recomposed or the
	// queue keeps advertising the number that was just replaced. Same reason as the seams
	// above -- weighing hands verification the new sentence rather than writing its table.
	RelabelItemBySource(ctx context.Context, tenantID, sourceModule, sourceRefType, sourceRefID, subjectLabel string) (int, error)
}

type Enqueuer struct {
	verification verificationCreator
}

func New(v verificationCreator) *Enqueuer {
	return &Enqueuer{verification: v}
}

var _ weighingapp.VerificationEnqueuer = (*Enqueuer)(nil)
var _ weighingapp.VerificationWithdrawer = (*Enqueuer)(nil)
var _ weighingapp.VerificationApplyAcker = (*Enqueuer)(nil)
var _ weighingapp.VerificationRelabeler = (*Enqueuer)(nil)

// RelabelWeighingVerification restates the verification item's subject label after
// the VERIFIER corrects the weight it names.
//
// The label is composed at enqueue and carries the weight itself ("Godel 1 - Part 3
// · Tag 9010 · 120.0 kg"), so a corrected observation whose item is not relabelled
// leaves the verifier reading the number she just replaced. RefID is the observation
// id -- the same id EnqueueWeighingVerification put in Source.RefID.
func (e *Enqueuer) RelabelWeighingVerification(ctx context.Context, tenantID, refType, observationID, subjectLabel string) error {
	if observationID == "" || subjectLabel == "" {
		return nil
	}
	_, err := e.verification.RelabelItemBySource(
		ctx,
		tenantID,
		weighingdomain.VerificationModuleWeighing,
		refType,
		observationID,
		subjectLabel,
	)
	return err
}

// AckWeighingVerificationApplied reports that weighing's verdict applier has written the
// verifier's decision onto the observation itself. Until this lands, the item reads as
// domain.VerdictStateApplying on the verifier's surface -- decided, not yet in effect --
// rather than silently disappearing out of the pending queue as if the work were done.
//
// RefID is the observation id, the same id EnqueueWeighingVerification put in Source.RefID.
func (e *Enqueuer) AckWeighingVerificationApplied(ctx context.Context, tenantID, refType string, observationIDs []string) error {
	if len(observationIDs) == 0 {
		return nil
	}
	_, err := e.verification.MarkVerdictApplied(
		ctx,
		tenantID,
		weighingdomain.VerificationModuleWeighing,
		refType,
		observationIDs,
		weighingdomain.VerificationModuleWeighing,
	)
	return err
}

// WithdrawWeighingVerification retires the still-pending items raised for weighing
// source records that no longer represent the bucket's work (ReopenScope stamps
// withdrawn_at on the lump-sum submission). RefID is the observation id -- the same
// id EnqueueWeighingVerification put in Source.RefID.
func (e *Enqueuer) WithdrawWeighingVerification(ctx context.Context, tenantID, refType string, observationIDs []string) error {
	if len(observationIDs) == 0 {
		return nil
	}
	_, err := e.verification.WithdrawItemsBySource(ctx, tenantID, weighingdomain.VerificationModuleWeighing, refType, observationIDs)
	return err
}

// EnqueueWeighingVerification raises ONE verification item per piece of EVIDENCE, which for
// individual weighing means one per animal (each animal has its own video) and for lump-sum means
// one per shed (one video covers the shed).
//
// DO NOT "batch" this to one item per submission. It has been raised twice as a scale defect —
// "5k kids means 5,000 videos for one verifier" — and ruled NOT A BUG both times (maintainer,
// 2026-08-03; see context/repo-audits/weighing-implementation-do-not-reopen-ledger.md → B-5).
// The grain is consistent across the whole product: vaccination is one item per SOP submission
// because it records one proof per submission, not because submission is the universal grain.
// Batching separately-recorded videos into one review item would mean a verifier approving
// footage they never watched. If the volume is a problem, the question is whether per-animal
// video is still required — a decision about what to CAPTURE, never about how to queue it.
func (e *Enqueuer) EnqueueWeighingVerification(ctx context.Context, in weighingapp.VerificationEnqueueRequest) error {
	// The item's queue CATEGORY follows the evidence kind. Weigh captures
	// (animal / shed observations) share weighing_proof; the fasting task's
	// feed & water removal videos get their own weighing_fasting queue page, so
	// a verifier reviewing weights is not handed removal footage under a weight
	// label. in.Category stays the SOURCE ref_type either way.
	category := weighingdomain.VerificationCategoryWeighing
	if in.Category == weighingdomain.VerificationRefTypeFasting {
		category = weighingdomain.VerificationCategoryFasting
	}
	_, err := e.verification.CreateItem(ctx, verificationdomain.CreateItem{
		TenantID:     in.TenantID,
		Vertical:     weighingdomain.VerificationVerticalWeighing,
		Module:       weighingdomain.VerificationModuleWeighing,
		Category:     category,
		SubjectLabel: ptrIfSet(in.SubjectLabel),
		Source: verificationdomain.SourceRef{
			Module:  weighingdomain.VerificationModuleWeighing,
			RefType: in.Category,
			RefID:   in.ObservationID,
		},
		MediaRefs:  in.MediaRefs,
		OperatorID: ptrIfSet(in.OperatorID),
		ShedID:     ptrIfSet(in.ShedID),
		// ParkID is the notification routing key: the verification pending consumer resolves the
		// park's verify-duty holders from it and no-ops on a blank park (see the sibling feed and
		// shifting bridges, which pass it for the same reason).
		ParkID:         ptrIfSet(in.ParkID),
		CapturedAt:     in.CapturedAt,
		IdempotencyKey: in.IdempotencyKey,
		// Weighing runs a verdict applier (weighing/app.VerificationVerdictHandler ->
		// weighingpg.ApplyVerificationVerdict) and acks it via
		// AckWeighingVerificationApplied above, so its items participate in the
		// awaiting-application state. Do NOT set this on a producer that has no ack:
		// its decided items would sit in "applying" forever.
		ApplierAckExpected: true,
	})
	return err
}

func ptrIfSet(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
