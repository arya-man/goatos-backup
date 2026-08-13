package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/vgoats/goatos/backend/internal/counts/domain"
	"github.com/vgoats/goatos/backend/internal/counts/ports"
	identityports "github.com/vgoats/goatos/backend/internal/identity/ports"
)

// Shifting EXECUTION -- completion, cancellation, and the operator's pending-execution queue.
//
// This file owns the second half of a movement's life. Approval (approval_repository.go) decides
// whether a movement MAY happen; this decides that it DID, or that it never will.
//
// The state machine, and who may drive each edge:
//
//	                      approve (CountsApproveShifting)
//	 pending ────────────────────────────────────────────► authorized      NOTHING MOVES
//	    │                                                     │  │
//	    │ reject (CountsApproveShifting)                       │  │ cancel (CountsWrite, any operator)
//	    ▼                                                      │  ▼
//	 rejected                                                  │ canceled  NOTHING MOVES
//	                                                           │
//	                       complete (CountsWrite, any operator)│
//	                                                           ▼
//	                                                        applied        ◄── THE ANIMALS RELOCATE
//
// 'applied' and 'rejected' and 'canceled' are terminal. Completion is reachable ONLY from
// 'authorized' (or from evidence rework on an already-approved movement) -- enforced here by the
// `authorization_state = 'authorized'` transition predicate, and independently by
// shifting_events_applied_requires_authorization_check in the schema, so a caller addressing a
// pending movement's id cannot execute an unapproved relocation.
//
// APPROVE-FIRST (maintainer decision 2026-08-09). This RETIRES the 2026-07-28 rule under which the
// two gates were independent and order-free, and under which a raised movement appeared in the
// operator's Actions queue immediately. A movement is now invisible to the operator's work list and
// non-completable until a Park Head authorizes it; it is reachable only through the read-only
// Pending tab, which reports "Awaiting Park Head approval" and offers no action.
//
// The approval-arrives-second branch in authorizeShiftingEventInTx is deliberately KEPT: rows
// completed under the superseded rule are still in flight, and dropping it would strand them
// approved-but-never-applied. It is compatibility, not a supported new path.

// ---------------------------------------------------------------------------
// Complete
// ---------------------------------------------------------------------------

// CompleteShiftingEvent records the operator-completion gate with mandatory video evidence.
//
// It requires Park Head approval to already exist (maintainer decision 2026-08-09): an unapproved
// movement is refused with ErrShiftingNotAuthorized and nothing is written. Approval therefore
// always lands first, and this transaction is the one that applies the move.
//
// A blank ProofRef is rejected with ErrShiftingProofRequired before any state changes -- a move with
// no video has nothing for a verifier to approve.
//
// IDEMPOTENCY. A phone in a park WILL retry this:
//
//   - Already 'pending_verification' (this movement's video was already submitted): the stored
//     completion key/fingerprint is compared. An exact replay returns the original result with
//     replay=true and enqueues nothing new; a same-key/different-payload replay is
//     ErrIdempotencyConflict.
//   - Already 'applied' (a verifier already approved it and the animals moved): return the original
//     result with replay=true. Completion is a confirmation of a physical fact, not a command that
//     may run twice.
func (r *Repository) CompleteShiftingEvent(
	ctx context.Context, in domain.ShiftingCompletionCommand,
) (domain.ShiftingExecutionResult, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	// The video is mandatory. Reject before opening a transaction so a proofless completion changes
	// nothing.
	if strings.TrimSpace(in.ProofRef) == "" {
		return domain.ShiftingExecutionResult{}, false, ports.ErrShiftingProofRequired
	}

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return domain.ShiftingExecutionResult{}, false, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()

	current, err := lockShiftingEvent(ctx, tx, in.TenantID, in.ShiftingEventID)
	if err != nil {
		return domain.ShiftingExecutionResult{}, false, err
	}

	// The animal set. It lives in the approval request's stored payload -- the same place the
	// approve path read it from -- because shifting_events itself is an aggregate, breed-grain
	// model (shifting_event_impacts counts heads, it does not name animals). Reading it here rather
	// than carrying it from the approval keeps completion self-contained: the operator's phone
	// posts an id, not a herd.
	goatIDs, destParkID, destShedID, err := r.shiftingProposedMovementSet(ctx, tx, in.TenantID, in.ShiftingEventID)
	if err != nil {
		return domain.ShiftingExecutionResult{}, false, err
	}
	// Already submitted for verification, or already applied. Answer with the original result rather
	// than enqueuing / relocating a second time.
	if (current.CompletedAt != nil || current.EventStatus == domain.ShiftingEventStatusPendingVerification ||
		current.EventStatus == domain.ShiftingEventStatusApplied) &&
		current.VerificationState != "rejected" {
		if in.IdempotencyKey != "" && current.CompletionIdempotencyKey != nil &&
			*current.CompletionIdempotencyKey == in.IdempotencyKey {
			if current.CompletionRequestFingerprint == nil ||
				*current.CompletionRequestFingerprint != in.RequestFingerprint {
				return domain.ShiftingExecutionResult{}, false, ports.ErrIdempotencyConflict
			}
		}
		destShedName := ""
		if shedName, err := r.fetchShedName(ctx, in.TenantID, destShedID); err == nil {
			destShedName = shedName
		}
		return domain.ShiftingExecutionResult{
			ShiftingEventID:           in.ShiftingEventID,
			EventStatus:               current.EventStatus,
			DestinationParkID:         destParkID,
			DestinationShedID:         destShedID,
			DestinationShedName:       destShedName,
			DestinationPartitionLabel: derefOrEmpty(current.DestinationPartitionLabel),
			MovedGoatIDs:              goatIDs,
			RaiseComment:              current.RaiseComment,
			AppliedAt:                 current.AppliedAt,
			AppliedBy:                 current.AppliedBy,
		}, true, nil
	}

	// ORDER MATTERS from here down: each check below is more expensive and more specific than the
	// last, and whichever fires first is the reason the operator is shown. Authorization is the
	// cheapest and the most fundamental, so it answers first -- otherwise an unapproved HIGH-priority
	// movement is told to "record the feed videos" (and pays for a full feed-requirement resolution)
	// when the real answer is that nobody has approved it yet.
	//
	// APPROVE-FIRST (maintainer decision 2026-08-09). authorization_state is the ground truth of
	// "approved", not event_status: a legacy pre-000049 row can sit in 'pending_verification' while
	// still unapproved, and gating on event_status alone would let that row through.
	//
	// This is checked AFTER the replay branch above deliberately, so a movement completed under the
	// superseded order-free rule still answers its own retries instead of turning a stored, in-flight
	// completion into a hard error on the phone that made it.
	if current.AuthorizationState != "authorized" {
		return domain.ShiftingExecutionResult{}, false, fmt.Errorf(
			"%w: shifting event %s has not been approved by a park head, and completion may only start "+
				"from an approved movement or evidence rework",
			ports.ErrShiftingNotAuthorized, in.ShiftingEventID,
		)
	}
	if current.EventStatus != domain.ShiftingEventStatusAuthorized &&
		current.VerificationState != "rejected" {
		return domain.ShiftingExecutionResult{}, false, fmt.Errorf(
			"%w: shifting event %s is %q, and completion may only start from authorized or evidence rework",
			ports.ErrShiftingNotAuthorized, in.ShiftingEventID, current.EventStatus,
		)
	}

	if len(goatIDs) == 0 {
		// Fail closed. A movement naming nobody cannot be "completed": submitting it for verification
		// would queue a video that proves the relocation of no animals.
		//
		// Ahead of the feed block for the same precedence reason: the feed requirement is priced from
		// this very animal set, so an empty movement would otherwise surface as a confusing
		// "feed config blocked: movement or approval animal set is missing" instead of naming the
		// actual problem.
		return domain.ShiftingExecutionResult{}, false, fmt.Errorf(
			"%w: shifting event %s names no animals to move",
			ports.ErrShiftingExecutionIncomplete, in.ShiftingEventID)
	}

	var feedSnapshot []byte
	if current.Priority == "high" {
		if strings.TrimSpace(in.FeedPackingProofRef) == "" || strings.TrimSpace(in.FeedGivenProofRef) == "" {
			return domain.ShiftingExecutionResult{}, false, ports.ErrShiftingFeedProofsRequired
		}
		requirements, err := loadShiftingFeedRequirements(ctx, tx, in.TenantID,
			[]string{in.ShiftingEventID}, in.CompletedAt)
		if err != nil {
			return domain.ShiftingExecutionResult{}, false, err
		}
		requirement := requirements[in.ShiftingEventID]
		if requirement.Status != "ready" || requirement.Fingerprint == "" {
			return domain.ShiftingExecutionResult{}, false, fmt.Errorf("%w: %s",
				ports.ErrShiftingFeedConfigBlocked, requirement.BlockedReason)
		}
		if strings.TrimSpace(in.FeedConfigFingerprint) == "" ||
			strings.TrimSpace(in.FeedConfigFingerprint) != requirement.Fingerprint {
			return domain.ShiftingExecutionResult{}, false, ports.ErrShiftingFeedConfigChanged
		}
		feedSnapshot, err = json.Marshal(requirement)
		if err != nil {
			return domain.ShiftingExecutionResult{}, false, fmt.Errorf("counts: encode shifting feed requirement: %w", err)
		}
	}

	// Record the operator fact first. For a rejected proof on an already-applied movement, preserve
	// the applied state and original completion actor/time while replacing only the evidence.
	tag, err := tx.Exec(ctx, `
UPDATE shifting_events
SET event_status = event_status,
    verification_state = 'unverified',
    proof_ref = $3,
    feed_packing_proof_ref = CASE WHEN priority = 'high' THEN nullif($9, '') ELSE NULL END,
    feed_given_proof_ref = CASE WHEN priority = 'high' THEN nullif($10, '') ELSE NULL END,
    feed_config_fingerprint = CASE WHEN priority = 'high' THEN nullif($11, '') ELSE NULL END,
    feed_requirement_snapshot = CASE WHEN priority = 'high' THEN $12::jsonb ELSE NULL END,
    completion_destination_tag = nullif($6, ''),
    completion_idempotency_key = nullif($4, ''),
    completion_request_fingerprint = nullif($5, ''),
    completed_at = COALESCE(completed_at, $7::timestamptz),
    completed_by = COALESCE(completed_by, $8::uuid),
    updated_at = now(),
    row_version = row_version + 1
WHERE tenant_id = $1::uuid AND shifting_event_id = $2::uuid
  AND authorization_state = 'authorized'
  AND (event_status = 'authorized' OR verification_state = 'rejected')`,
		in.TenantID, in.ShiftingEventID, strings.TrimSpace(in.ProofRef),
		in.IdempotencyKey, in.RequestFingerprint, strings.TrimSpace(in.DestinationTag),
		in.CompletedAt.UTC(), in.CompletedByUserID,
		strings.TrimSpace(in.FeedPackingProofRef), strings.TrimSpace(in.FeedGivenProofRef),
		strings.TrimSpace(in.FeedConfigFingerprint), feedSnapshot)
	if err != nil {
		return domain.ShiftingExecutionResult{}, false, fmt.Errorf("counts: submit shifting for verification: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ShiftingExecutionResult{}, false, ports.ErrShiftingNotAuthorized
	}

	updated, err := lockShiftingEvent(ctx, tx, in.TenantID, in.ShiftingEventID)
	if err != nil {
		return domain.ShiftingExecutionResult{}, false, err
	}
	if updated.AuthorizationState == "authorized" && updated.CompletedAt != nil &&
		(updated.EventStatus == domain.ShiftingEventStatusAuthorized || updated.EventStatus == domain.ShiftingEventStatusPendingVerification) {
		result, err := r.applyAuthorizedCompletedShiftingInTx(ctx, tx, in.TenantID, in.ShiftingEventID,
			in.CompletedAt.UTC(), in.TraceID, nil)
		if err != nil {
			return domain.ShiftingExecutionResult{}, false, err
		}
		if err := tx.Commit(ctx); err != nil {
			return domain.ShiftingExecutionResult{}, false, err
		}
		committed = true
		return result, false, nil
	}

	if err := tx.Commit(ctx); err != nil {
		return domain.ShiftingExecutionResult{}, false, err
	}
	committed = true

	destShedName := ""
	if shedName, err := r.fetchShedName(ctx, in.TenantID, destShedID); err == nil {
		destShedName = shedName
	}

	return domain.ShiftingExecutionResult{
		ShiftingEventID:           in.ShiftingEventID,
		EventStatus:               updated.EventStatus,
		DestinationParkID:         destParkID,
		DestinationShedID:         destShedID,
		DestinationShedName:       destShedName,
		DestinationPartitionLabel: derefOrEmpty(updated.DestinationPartitionLabel),
		MovedGoatIDs:              goatIDs,
		RaiseComment:              current.RaiseComment,
	}, false, nil
}

// ApplyVerifiedShiftingEvent records an approved evidence verdict. For new rows it changes only
// verification_state: Park Head approval + operator completion have already applied the move (or
// will do so when the missing gate arrives). The historic method name remains for event-consumer
// compatibility.
//
// A pre-000049 pending_verification row is rollout debt from the superseded verifier gate. If its
// approval and legacy completion both exist, this method applies that row once before recording the
// evidence verdict so an in-flight movement is not stranded. New rows never rely on this branch.
// Re-delivered verdicts are idempotent and cannot relocate an already-applied movement again.
func (r *Repository) ApplyVerifiedShiftingEvent(
	ctx context.Context, in domain.ShiftingVerifiedApplyCommand,
) (domain.ShiftingExecutionResult, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return domain.ShiftingExecutionResult{}, false, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()

	current, err := lockShiftingEvent(ctx, tx, in.TenantID, in.ShiftingEventID)
	if err != nil {
		return domain.ShiftingExecutionResult{}, false, err
	}

	goatIDs, destParkID, destShedID, err := r.shiftingProposedMovementSet(ctx, tx, in.TenantID, in.ShiftingEventID)
	if err != nil {
		return domain.ShiftingExecutionResult{}, false, err
	}

	if current.EventStatus != domain.ShiftingEventStatusPending &&
		current.EventStatus != domain.ShiftingEventStatusPendingVerification &&
		current.EventStatus != domain.ShiftingEventStatusApplied {
		return domain.ShiftingExecutionResult{
			ShiftingEventID: in.ShiftingEventID,
			EventStatus:     current.EventStatus,
		}, false, nil
	}

	// Compatibility for a pre-000049 row that already had approval + completion but was waiting on
	// the old verifier gate: apply the two satisfied business gates before recording the verdict.
	if current.EventStatus == domain.ShiftingEventStatusPendingVerification && current.AuthorizationState == "authorized" {
		if current.CompletedAt == nil || current.CompletedBy == nil {
			if _, err := tx.Exec(ctx, `
UPDATE shifting_events
SET completed_at = COALESCE(completed_at, $3::timestamptz),
    completed_by = COALESCE(completed_by, $4::uuid)
WHERE tenant_id = $1::uuid AND shifting_event_id = $2::uuid`,
				in.TenantID, in.ShiftingEventID, in.VerifiedAt.UTC(), in.VerifiedByUserID); err != nil {
				return domain.ShiftingExecutionResult{}, false, fmt.Errorf("counts: stamp legacy shifting completion: %w", err)
			}
		}
		if _, err := r.applyAuthorizedCompletedShiftingInTx(ctx, tx, in.TenantID, in.ShiftingEventID,
			in.VerifiedAt.UTC(), in.TraceID, nil); err != nil {
			return domain.ShiftingExecutionResult{}, false, err
		}
	}

	tag, err := tx.Exec(ctx, `
UPDATE shifting_events
SET verification_state = 'verified',
    updated_at = now(),
    row_version = row_version + 1
WHERE tenant_id = $1::uuid AND shifting_event_id = $2::uuid
  AND event_status IN ('pending', 'pending_verification', 'applied')
  AND verification_state <> 'verified'`, in.TenantID, in.ShiftingEventID)
	if err != nil {
		return domain.ShiftingExecutionResult{}, false, fmt.Errorf("counts: mark shifting verification approved: %w", err)
	}

	final, err := lockShiftingEvent(ctx, tx, in.TenantID, in.ShiftingEventID)
	if err != nil {
		return domain.ShiftingExecutionResult{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.ShiftingExecutionResult{}, false, err
	}
	committed = true
	return domain.ShiftingExecutionResult{
		ShiftingEventID:   in.ShiftingEventID,
		EventStatus:       final.EventStatus,
		DestinationParkID: destParkID,
		DestinationShedID: destShedID,
		MovedGoatIDs:      goatIDs,
		AppliedAt:         final.AppliedAt,
		AppliedBy:         final.AppliedBy,
	}, tag.RowsAffected() == 1, nil
}

// BounceShiftingEventForRework marks rejected evidence for re-shoot while preserving movement state.
// Applied goat location and census truth are never rolled back.
//
// Re-delivered verdicts are idempotent. A verdict for a terminal rejected/canceled movement is a
// no-op; an applied row remains applied while its evidence is reopened for re-shoot.
func (r *Repository) BounceShiftingEventForRework(
	ctx context.Context, in domain.ShiftingReworkCommand,
) error {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	tag, err := r.pool.Exec(ctx, `
UPDATE shifting_events
SET verification_state = 'rejected',
    completion_idempotency_key = NULL,
    completion_request_fingerprint = NULL,
    updated_at = now(),
    row_version = row_version + 1
WHERE tenant_id = $1::uuid AND shifting_event_id = $2::uuid
  AND event_status IN ('pending', 'pending_verification', 'applied')`,
		in.TenantID, in.ShiftingEventID)
	if err != nil {
		return fmt.Errorf("counts: bounce shifting event for rework: %w", err)
	}
	_ = tag // zero rows affected is an accepted stale/duplicate verdict; no error.
	return nil
}

// ---------------------------------------------------------------------------
// Cancel
// ---------------------------------------------------------------------------

// CancelShiftingEvent retires an authorized movement that will never be executed. It MOVES NOTHING.
//
// Reachable ONLY from 'authorized' -- deliberately NOT from 'pending'. A pending movement already
// has a retirement path that records a decision by somebody with the authority to make it: the
// approver REJECTS it. Letting an operator cancel a pending movement instead would leave the linked
// counts_approval_requests row still 'pending' and still approvable, and approval's transition
// guard keys on authorization_state (which a cancel does not touch) -- so an approver could
// re-authorize a movement that had been canceled underneath them. It would also let an operator
// holding only CountsWrite dispose of a request they lack CountsApproveShifting to decide. Cancel
// therefore covers exactly the gap the owner described: approved movements that pile up because
// nobody ever walks them.
//
// Idempotent on the same terms as completion: an exact replay of a cancel returns the original
// result, and a cancel addressing an already-canceled movement is a no-op success rather than a
// conflict.
func (r *Repository) CancelShiftingEvent(
	ctx context.Context, in domain.ShiftingCancellationCommand,
) (domain.ShiftingExecutionResult, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return domain.ShiftingExecutionResult{}, false, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()

	current, err := lockShiftingEvent(ctx, tx, in.TenantID, in.ShiftingEventID)
	if err != nil {
		return domain.ShiftingExecutionResult{}, false, err
	}

	if current.EventStatus == domain.ShiftingEventStatusCanceled {
		if in.IdempotencyKey != "" && current.CancelIdempotencyKey != nil &&
			*current.CancelIdempotencyKey == in.IdempotencyKey {
			if current.CancelRequestFingerprint == nil ||
				*current.CancelRequestFingerprint != in.RequestFingerprint {
				return domain.ShiftingExecutionResult{}, false, ports.ErrIdempotencyConflict
			}
		}
		return domain.ShiftingExecutionResult{
			ShiftingEventID:   in.ShiftingEventID,
			EventStatus:       current.EventStatus,
			DestinationParkID: current.DestinationParkID,
			DestinationShedID: current.DestinationShedID,
			CanceledAt:        current.CanceledAt,
			CanceledBy:        current.CanceledBy,
			CancelReason:      current.CancelReason,
		}, true, nil
	}

	if current.EventStatus != domain.ShiftingEventStatusAuthorized {
		return domain.ShiftingExecutionResult{}, false, fmt.Errorf(
			"%w: shifting event %s is %q, and cancellation may only start from %q (a %q movement is "+
				"retired by rejecting its approval request instead)",
			ports.ErrShiftingNotAuthorized, in.ShiftingEventID, current.EventStatus,
			domain.ShiftingEventStatusAuthorized, domain.ShiftingEventStatusPending)
	}

	var (
		canceledAt time.Time
		canceledBy string
		reason     string
	)
	// authorization_state is deliberately LEFT ALONE: the movement genuinely was authorized, and
	// that is history rather than something a cancellation revises. Leaving it 'authorized' also
	// keeps approval's `authorization_state = 'pending'` guard permanently closed against this row.
	if err := tx.QueryRow(ctx, `
UPDATE shifting_events
SET event_status = 'canceled',
    canceled_at = $3::timestamptz,
    canceled_by = $4::uuid,
    cancel_reason = $5,
    cancel_idempotency_key = nullif($6, ''),
    cancel_request_fingerprint = nullif($7, ''),
    updated_at = now(),
    row_version = row_version + 1
WHERE tenant_id = $1::uuid AND shifting_event_id = $2::uuid AND event_status = 'authorized'
RETURNING canceled_at, canceled_by::text, cancel_reason`,
		in.TenantID, in.ShiftingEventID, in.CanceledAt.UTC(), in.CanceledByUserID, in.Reason,
		in.IdempotencyKey, in.RequestFingerprint).Scan(&canceledAt, &canceledBy, &reason); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ShiftingExecutionResult{}, false, ports.ErrShiftingNotAuthorized
		}
		return domain.ShiftingExecutionResult{}, false, fmt.Errorf("counts: cancel shifting event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return domain.ShiftingExecutionResult{}, false, err
	}
	committed = true

	return domain.ShiftingExecutionResult{
		ShiftingEventID:   in.ShiftingEventID,
		EventStatus:       domain.ShiftingEventStatusCanceled,
		DestinationParkID: current.DestinationParkID,
		DestinationShedID: current.DestinationShedID,
		CanceledAt:        &canceledAt,
		CanceledBy:        &canceledBy,
		CancelReason:      &reason,
	}, false, nil
}

// ---------------------------------------------------------------------------
// Locked read
// ---------------------------------------------------------------------------

// lockedShiftingEvent is the narrow row the execution transitions need under lock.
type lockedShiftingEvent struct {
	EventStatus        string
	AuthorizationState string
	VerificationState  string
	Priority           string

	DestinationParkID         string
	DestinationShedID         string
	DestinationPartitionLabel *string

	AppliedAt                *time.Time
	AppliedBy                *string
	CompletedAt              *time.Time
	CompletedBy              *string
	CompletionDestinationTag *string
	ManagementStageMode      *string
	TargetManagementStage    *string
	// RaiseComment is read under the SAME row lock as everything else, so the note handed to the
	// verifier is the one stored on the movement being completed, not a value re-read afterwards.
	RaiseComment *string

	CanceledAt   *time.Time
	CanceledBy   *string
	CancelReason *string

	CompletionIdempotencyKey     *string
	CompletionRequestFingerprint *string
	CancelIdempotencyKey         *string
	CancelRequestFingerprint     *string
}

// lockShiftingEvent takes a row lock so two operators driving the same movement serialize here
// rather than both running a transition.
func lockShiftingEvent(ctx context.Context, tx pgx.Tx, tenantID, shiftingEventID string) (lockedShiftingEvent, error) {
	var out lockedShiftingEvent
	err := tx.QueryRow(ctx, `
SELECT event_status, authorization_state, verification_state, priority,
       destination_park_id::text, destination_shed_id::text, destination_partition_label,
       applied_at, applied_by::text, completed_at, completed_by::text,
       completion_destination_tag, management_stage_mode, target_management_stage,
       raise_comment,
       canceled_at, canceled_by::text, cancel_reason,
       completion_idempotency_key, completion_request_fingerprint,
       cancel_idempotency_key, cancel_request_fingerprint
FROM shifting_events
WHERE tenant_id = $1::uuid AND shifting_event_id = $2::uuid
FOR UPDATE`, tenantID, shiftingEventID).Scan(
		&out.EventStatus, &out.AuthorizationState, &out.VerificationState, &out.Priority,
		&out.DestinationParkID, &out.DestinationShedID, &out.DestinationPartitionLabel,
		&out.AppliedAt, &out.AppliedBy, &out.CompletedAt, &out.CompletedBy,
		&out.CompletionDestinationTag, &out.ManagementStageMode, &out.TargetManagementStage,
		&out.RaiseComment,
		&out.CanceledAt, &out.CanceledBy, &out.CancelReason,
		&out.CompletionIdempotencyKey, &out.CompletionRequestFingerprint,
		&out.CancelIdempotencyKey, &out.CancelRequestFingerprint)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return lockedShiftingEvent{}, ports.ErrShiftingEventNotFound
		}
		return lockedShiftingEvent{}, fmt.Errorf("counts: lock shifting event: %w", err)
	}
	return out, nil
}

// applyAuthorizedCompletedShiftingInTx is the single relocation writer. Its caller already holds
// the shifting row lock. It runs only after both business gates are durable and commits goat
// identity, stage/location events, outbox, and the shifting applied state in one transaction.
func (r *Repository) applyAuthorizedCompletedShiftingInTx(
	ctx context.Context, tx pgx.Tx, tenantID, shiftingEventID string, appliedAt time.Time, traceID string,
	approvedGoatIDs []string,
) (domain.ShiftingExecutionResult, error) {
	if r.identityTx == nil {
		return domain.ShiftingExecutionResult{}, fmt.Errorf(
			"counts: apply authorized completed shifting event %s: identity write seam is not wired", shiftingEventID)
	}
	current, err := lockShiftingEvent(ctx, tx, tenantID, shiftingEventID)
	if err != nil {
		return domain.ShiftingExecutionResult{}, err
	}
	if current.AuthorizationState != "authorized" ||
		(current.EventStatus != domain.ShiftingEventStatusAuthorized && current.EventStatus != domain.ShiftingEventStatusPendingVerification) ||
		current.CompletedAt == nil || current.CompletedBy == nil {
		return domain.ShiftingExecutionResult{}, fmt.Errorf(
			"%w: shifting event %s does not have both approval and operator completion",
			ports.ErrShiftingExecutionIncomplete, shiftingEventID)
	}
	goatIDs := approvedGoatIDs
	destParkID, destShedID := current.DestinationParkID, current.DestinationShedID
	if len(goatIDs) == 0 {
		goatIDs, destParkID, destShedID, err = r.shiftingMovementSet(ctx, tx, tenantID, shiftingEventID)
		if err != nil {
			return domain.ShiftingExecutionResult{}, err
		}
	}
	if len(goatIDs) == 0 {
		return domain.ShiftingExecutionResult{}, fmt.Errorf(
			"%w: shifting event %s names no approved animals", ports.ErrShiftingExecutionIncomplete, shiftingEventID)
	}
	sourceParkID, sourceShedID, sourcePartitionLabel, err := r.readShiftingEventSourceLocation(ctx, tx, tenantID, shiftingEventID)
	if err != nil {
		return domain.ShiftingExecutionResult{}, err
	}
	destinationTag := ""
	if current.TargetManagementStage != nil {
		destinationTag = *current.TargetManagementStage
	} else if current.CompletionDestinationTag != nil { // legacy pre-selection row
		destinationTag = *current.CompletionDestinationTag
	}
	// Operational location is park + physical shed + OPTIONAL partition, so the
	// partition raised with the movement has to survive to the applying
	// transaction. Shifting applies at whichever of park-head-approval /
	// operator-completion arrives SECOND, and the raised
	// destination_partition_label was previously validated at raise time and then
	// dropped here -- which is exactly why "Yashoda 1 -> Yashoda 2" committed as a
	// plain "Yashoda" move and the partition silently reverted.
	//
	// Re-validate against the CURRENT shed_partitions catalog rather than trusting
	// the raise-time check: the two gates are independent and may be hours apart,
	// so a partition can be retired in between. Fail closed instead of writing a
	// label that no longer names a real place.
	destShedName, err := validateDestinationPartitionAgainstCatalogTx(
		ctx, tx, tenantID, destShedID, current.DestinationPartitionLabel,
	)
	if err != nil {
		return domain.ShiftingExecutionResult{}, err
	}
	moved, err := r.identityTx.RelocateGoatsToShedInTx(ctx, tx, identityports.RelocateGoatsCommand{
		TenantID: tenantID, ActorID: *current.CompletedBy, GoatIDs: goatIDs,
		FromParkID: sourceParkID, FromShedID: sourceShedID, FromPartitionLabel: sourcePartitionLabel,
		ToParkID: destParkID, ToShedID: destShedID, DestinationTag: destinationTag,
		DestinationPartitionLabel: current.DestinationPartitionLabel,
		DestinationShedName:       destShedName,
		TraceID:                   traceID,
		Reason:                    "counts shifting approved and operator-completed " + shiftingEventID,
		OccurredAt:                appliedAt.UTC(),
		OutboxIdempotencyPrefix:   "counts-shifting-applied:" + shiftingEventID,
	})
	if err != nil {
		return domain.ShiftingExecutionResult{}, err
	}
	if len(moved.MovedGoatIDs) != len(goatIDs) {
		return domain.ShiftingExecutionResult{}, fmt.Errorf(
			"%w: shifting event %s named %d animals but %d were movable",
			ports.ErrShiftingExecutionIncomplete, shiftingEventID, len(goatIDs), len(moved.MovedGoatIDs))
	}
	var stampedAt time.Time
	var stampedBy string
	if err := tx.QueryRow(ctx, `
UPDATE shifting_events
SET event_status = 'applied', applied_at = $3::timestamptz, applied_by = $4::uuid,
    updated_at = now(), row_version = row_version + 1
WHERE tenant_id = $1::uuid AND shifting_event_id = $2::uuid
  AND authorization_state = 'authorized' AND event_status IN ('authorized', 'pending_verification')
RETURNING applied_at, applied_by::text`, tenantID, shiftingEventID, appliedAt.UTC(), *current.CompletedBy).
		Scan(&stampedAt, &stampedBy); err != nil {
		return domain.ShiftingExecutionResult{}, fmt.Errorf("counts: apply authorized completed shifting: %w", err)
	}
	srcPark, srcShed := "", ""
	if sourceParkID != nil {
		srcPark = *sourceParkID
	}
	if sourceShedID != nil {
		srcShed = *sourceShedID
	}
	return domain.ShiftingExecutionResult{
		ShiftingEventID:           shiftingEventID,
		EventStatus:               domain.ShiftingEventStatusApplied,
		SourceParkID:              srcPark,
		SourceShedID:              srcShed,
		DestinationParkID:         destParkID,
		DestinationShedID:         destShedID,
		DestinationShedName:       destShedName,
		DestinationPartitionLabel: derefOrEmpty(current.DestinationPartitionLabel),
		MovedGoatIDs:              moved.MovedGoatIDs,
		AppliedAt:                 &stampedAt,
		AppliedBy:                 &stampedBy,
	}, nil
}

// readShiftingEventSourceLocation reads the expected source park and shed for a shifting event.
// P1 follow-up #1: Used to guard against stale location overwrites.
func (r *Repository) readShiftingEventSourceLocation(
	ctx context.Context, tx pgx.Tx, tenantID, shiftingEventID string,
) (*string, *string, *string, error) {
	var sourceParkID, sourceShedID, sourcePartitionLabel *string
	err := tx.QueryRow(ctx, `
SELECT source_park_id::text, source_shed_id::text, source_partition_label
FROM shifting_events
WHERE tenant_id = $1::uuid AND shifting_event_id = $2::uuid`,
		tenantID, shiftingEventID).Scan(&sourceParkID, &sourceShedID, &sourcePartitionLabel)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil, nil, ports.ErrShiftingEventNotFound
		}
		return nil, nil, nil, fmt.Errorf("counts: read shifting source location: %w", err)
	}
	return sourceParkID, sourceShedID, sourcePartitionLabel, nil
}

// shiftingMovementSet reads the animals a movement covers, plus its destination and source.
//
// The animal set is the goat_ids captured on the APPROVED approval request that authorized this
// movement. Restricting to status='approved' is load-bearing rather than cosmetic: it means a
// completion can only ever relocate the animal set an approver actually signed off on, so editing
// a request after approval (or a second, still-pending request naming other animals) cannot widen
// what completion moves.
//
// P1 follow-up #1: Also reads the expected source park and shed to guard against stale location
// overwrites (an animal moved after approval but before completion).
func (r *Repository) shiftingMovementSet(
	ctx context.Context, tx pgx.Tx, tenantID, shiftingEventID string,
) (goatIDs []string, destParkID, destShedID string, err error) {
	var payload []byte
	var sourceParkID, sourceShedID *string
	err = tx.QueryRow(ctx, `
SELECT car.payload, se.destination_park_id::text, se.destination_shed_id::text,
       se.source_park_id::text, se.source_shed_id::text
FROM shifting_events se
LEFT JOIN counts_approval_requests car
       ON car.tenant_id = se.tenant_id
      AND car.shifting_event_id = se.shifting_event_id
      AND car.status = 'approved'
WHERE se.tenant_id = $1::uuid AND se.shifting_event_id = $2::uuid`,
		tenantID, shiftingEventID).Scan(&payload, &destParkID, &destShedID, &sourceParkID, &sourceShedID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, "", "", ports.ErrShiftingEventNotFound
		}
		return nil, "", "", fmt.Errorf("counts: read shifting movement set: %w", err)
	}
	if len(payload) == 0 {
		return nil, destParkID, destShedID, nil
	}
	var decoded struct {
		GoatIDs []string `json:"goat_ids"`
	}
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return nil, "", "", fmt.Errorf("counts: decode shifting movement set: %w", err)
	}
	if len(decoded.GoatIDs) > identityports.MaxRelocateGoatsPerCommand {
		return nil, "", "", fmt.Errorf("%w: shifting event %s names %d animals, above the %d bulk-relocate bound",
			ports.ErrShiftingExecutionIncomplete, shiftingEventID,
			len(decoded.GoatIDs), identityports.MaxRelocateGoatsPerCommand)
	}
	return decoded.GoatIDs, destParkID, destShedID, nil
}

// shiftingProposedMovementSet reads the one linked approval payload whether its Park Head decision
// is still pending or already approved. It is used for Actions and operator completion only; the
// relocation writer above deliberately calls shiftingMovementSet, which accepts approved payloads
// exclusively.
func (r *Repository) shiftingProposedMovementSet(
	ctx context.Context, tx pgx.Tx, tenantID, shiftingEventID string,
) (goatIDs []string, destParkID, destShedID string, err error) {
	var payload []byte
	err = tx.QueryRow(ctx, `
SELECT car.payload, se.destination_park_id::text, se.destination_shed_id::text
FROM shifting_events se
LEFT JOIN LATERAL (
    SELECT ar.payload
    FROM counts_approval_requests ar
    WHERE ar.tenant_id = se.tenant_id AND ar.shifting_event_id = se.shifting_event_id
      AND ar.status IN ('pending', 'approved')
    ORDER BY CASE ar.status WHEN 'approved' THEN 0 ELSE 1 END, ar.raised_at DESC
    LIMIT 1
) car ON true
WHERE se.tenant_id = $1::uuid AND se.shifting_event_id = $2::uuid`, tenantID, shiftingEventID).
		Scan(&payload, &destParkID, &destShedID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, "", "", ports.ErrShiftingEventNotFound
		}
		return nil, "", "", fmt.Errorf("counts: read proposed shifting movement set: %w", err)
	}
	if len(payload) == 0 {
		return nil, destParkID, destShedID, nil
	}
	var decoded struct {
		GoatIDs []string `json:"goat_ids"`
	}
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return nil, "", "", fmt.Errorf("counts: decode proposed shifting movement set: %w", err)
	}
	if len(decoded.GoatIDs) > identityports.MaxRelocateGoatsPerCommand {
		return nil, "", "", fmt.Errorf("%w: shifting event %s names %d animals, above the %d bulk-relocate bound",
			ports.ErrShiftingExecutionIncomplete, shiftingEventID, len(decoded.GoatIDs), identityports.MaxRelocateGoatsPerCommand)
	}
	return decoded.GoatIDs, destParkID, destShedID, nil
}

// ---------------------------------------------------------------------------
// Pending-execution queue
// ---------------------------------------------------------------------------

// shiftingActionsVisibleSQL is the ACTIONS LEAD TIME (maintainer decision 2026-08-09), mirroring
// counts/domain.ShiftingActionsDueFrom. nowParam is the placeholder carrying the caller's clock, so
// the filter is deterministic in tests and cannot drift from the app's business clock.
//
// It hides only a row still AWAITING OPERATOR WORK -- event_status='authorized'. A movement that is
// already applied (completed, or applied-and-in-evidence-rework) is history: hiding it because its
// planned day has not arrived would erase work an operator demonstrably already did. A 'pending'
// row is likewise never hidden, so a raiser always sees the movement they just raised sitting in
// the read-only Pending tab.
//
// The date arithmetic is IST wall clock on both sides, because a Goat OS business day is an India
// business day: `(raised_at AT TIME ZONE 'Asia/Kolkata')::time < '13:30'` asks what the clock on the
// wall read when the operator raised it, which is the question the rule is actually about.
//
// SCALE. This is a computed predicate on raised_at, so it cannot use an index by itself. That is
// bounded here rather than exempted: the surrounding query has already narrowed to one tenant, one
// business-date range and one status through shifting_events_actions_history_idx, so the expression
// is evaluated over an index range that is one business day wide in the normal mobile case. With no
// date filter the planner still walks raised_at DESC and stops at LIMIT; the only rows it discards
// are ones raised inside the lead window, so the extra work is bounded by two days of raise volume,
// never by the tenant's history.
//
// Note for whoever changes this: `make scale-guard` does NOT check this construct -- a computed
// timezone expression in a WHERE is one of its blind spots, verified by deleting the reasoning and
// re-running the guard, which still passed. Do not read a green scale-guard as proof that a future
// version of this predicate is index-safe; check the plan.
func shiftingActionsVisibleSQL(nowParam string) string {
	return `(se.event_status <> 'authorized' OR se.priority = 'high'
	         OR (` + nowParam + `::timestamptz AT TIME ZONE 'Asia/Kolkata') >=
	            ((se.raised_at AT TIME ZONE 'Asia/Kolkata')::date
	             + (CASE WHEN (se.raised_at AT TIME ZONE 'Asia/Kolkata')::time < TIME '13:30'
	                     THEN 1 ELSE 2 END)))`
}

// shiftingOutstandingActionSQL is the ONE definition of "this movement still needs operator work".
//
// It is the predicate behind primary_action_key='execute', and the previous-dates badge counts
// exactly the rows it matches. Both callers share it deliberately: they drifted apart once and the
// badge became a lie.
//
// A movement is outstanding when it is APPROVED but not yet executed (proof_ref IS NULL — the
// completion writes the proof, so its presence IS the record of the work), or when a verifier sent
// its evidence back for a re-shoot (verification_state='rejected'). Everything else is not work:
//   - 'pending' is unapproved, and by the APPROVE-FIRST gate (maintainer decision 2026-08-09) the
//     operator cannot act on it at all -- it is visible read-only in its own tab;
//   - 'applied' with no rejection is DONE;
//   - 'canceled' is dropped everywhere.
//
// The canceled exclusion is redundant for primary_action_key (the page query already drops canceled
// rows before this expression is reached) and load-bearing for the badge, which has no such
// enclosing filter.
//
// Do NOT reintroduce a bare `event_status <> 'canceled'` for the badge. That counted every
// non-canceled movement raised on a prior date -- completed ones included -- so a finished day kept
// advertising work for the full 90-day lookback. Observed 2026-08-13: a movement raised 08-12,
// approved, executed with proof, still showed "1" while every status tab for that date read
// authorized=0 rework=0 completed=1.
func shiftingOutstandingActionSQL() string {
	return `(se.event_status NOT IN ('canceled', 'pending')
	         AND ((se.event_status = 'authorized' AND se.proof_ref IS NULL)
	              OR se.verification_state = 'rejected'))`
}

// ListShiftingEventsPendingExecution returns one keyset page of date-scoped Actions history.
//
// projection-review: membership=date-and-status-scoped shifting_events plus one preferred request per event; group_key=shifting_event_id; join_cardinality=request and preview lateral joins reduce to at most one row per event; pagination=keyset over raised_at and shifting_event_id with limit plus one; scope=tenant_id plus business-date status park and shed filters
// BUCKETS (maintainer decision 2026-08-09). The five buckets stay disjoint, but 'all' now means
// "the operator's work list" and EXCLUDES unapproved movements: a raised movement is reachable only
// through the read-only 'pending' bucket until a Park Head authorizes it, and 'rework' likewise
// excludes 'pending' so an unapproved movement cannot re-enter the work list through an evidence
// verdict. Every non-canceled row still lands in exactly one bucket, so nothing becomes unreachable.
//
// This is a canonical-source read, not a projection. GRAIN = one row per shifting_event,
// which is the queue's natural unit of work (an operator executes a movement, not an animal). The
// only aggregate is animal_count, computed from ONE selected pending/approved request payload per
// event. The LATERAL selector is bounded to LIMIT 1 and prefers approved over pending, so
// there is no many-side to fan out and no possibility of a join multiplying the count. The animal
// preview is a LATERAL over a SLICE of that same array, so it cannot inflate the row either, and
// the count is read from the full array rather than from the preview, keeping the displayed total
// independent of the preview bound and of page size.
//
// Keyset, not OFFSET: rows may transition while an operator pages. The business date is converted
// to one inclusive/exclusive UTC range by the service; indexed columns stay bare. ORDER BY and the
// cursor match shifting_events_actions_history_idx, so the selected date/status reads an index
// range bounded by page size instead of scanning the tenant's full movement history.
func (r *Repository) ListShiftingEventsPendingExecution(
	ctx context.Context, q domain.ShiftingExecutionQuery,
) (domain.ShiftingExecutionPage, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	pageSize := q.PageSize
	if pageSize <= 0 || pageSize > domain.MaxShiftingExecutionPageSize {
		pageSize = domain.MaxShiftingExecutionPageSize
	}

	var (
		cursorRaisedAt any
		cursorID       any
		raisedFrom     any
		raisedBefore   any
	)
	if q.Cursor != nil {
		cursorRaisedAt = q.Cursor.AuthorizedAt.UTC()
		cursorID = q.Cursor.ShiftingEventID
	}
	if q.RaisedFrom != nil {
		raisedFrom = q.RaisedFrom.UTC()
	}
	if q.RaisedBefore != nil {
		raisedBefore = q.RaisedBefore.UTC()
	}
	status := q.Status
	if status == "" {
		status = "all"
	}
	var sourceParkID, sourceShedID any
	if q.SourceParkID != "" {
		sourceParkID = q.SourceParkID
	}
	if q.SourceShedID != "" {
		sourceShedID = q.SourceShedID
	}

	now := q.Now
	if now.IsZero() {
		now = time.Now()
	}

	// Fetch one extra row to decide whether a next page exists, without a second COUNT query.
	rows, err := r.pool.Query(ctx, `
WITH page AS (
	    SELECT se.shifting_event_id, se.event_status, se.verification_state,
	           -- An UNAPPROVED movement is never executable (maintainer decision 2026-08-09,
	           -- retiring the order-free gates). event_status='pending' is exactly
	           -- authorization_state='pending': authorizeShiftingEventInTx flips both in one
	           -- statement, so a row cannot be approved while still reading 'pending' here.
	           CASE WHEN `+shiftingOutstandingActionSQL()+`
	                THEN 'execute' ELSE 'none' END AS primary_action_key,
	           se.priority, se.category,
           se.source_park_id, se.source_shed_id, se.source_partition_label,
           se.destination_park_id, se.destination_shed_id, se.destination_partition_label,
           se.authorized_by, se.authorized_at, se.raised_at, se.effective_at
    FROM shifting_events se
    WHERE se.tenant_id = $1::uuid
	      AND se.event_status <> 'canceled'
	      AND ($2::timestamptz IS NULL OR se.raised_at >= $2::timestamptz)
	      AND ($3::timestamptz IS NULL OR se.raised_at < $3::timestamptz)
	      AND (($4::text = 'all' AND se.event_status <> 'pending')
	           OR ($4::text = 'pending' AND se.event_status = 'pending')
	           OR ($4::text = 'authorized' AND se.event_status = 'authorized' AND se.verification_state <> 'rejected')
	           OR ($4::text = 'rework' AND se.verification_state = 'rejected' AND se.event_status <> 'pending')
	           OR ($4::text = 'completed' AND se.event_status = 'applied' AND se.verification_state <> 'rejected'))
	      AND ($9::uuid IS NULL OR se.source_park_id = $9::uuid)
	      AND ($10::uuid IS NULL OR se.source_shed_id = $10::uuid)
	      AND `+shiftingActionsVisibleSQL("$11")+`
	      AND ($5::timestamptz IS NULL
	           OR (se.raised_at, se.shifting_event_id) < ($5::timestamptz, $6::uuid))
	    ORDER BY se.raised_at DESC, se.shifting_event_id DESC
	    LIMIT $7
), req AS (
    SELECT p.shifting_event_id,
           car.raised_by_user_id,
           ARRAY(SELECT jsonb_array_elements_text(car.payload -> 'goat_ids'))::uuid[] AS goat_ids
    FROM page p
    LEFT JOIN LATERAL (
        SELECT ar.raised_by_user_id, ar.payload
        FROM counts_approval_requests ar
        WHERE ar.tenant_id = $1::uuid AND ar.shifting_event_id = p.shifting_event_id
          AND ar.status IN ('pending', 'approved')
        ORDER BY CASE ar.status WHEN 'approved' THEN 0 ELSE 1 END, ar.raised_at DESC
        LIMIT 1
    ) car ON true
)
	SELECT p.shifting_event_id::text, p.event_status, p.verification_state, p.primary_action_key,
	       p.priority, p.category,
       p.source_park_id::text, src_park.name, p.source_shed_id::text, src_shed.name,
       p.source_partition_label,
       p.destination_park_id::text, dst_park.name,
       p.destination_shed_id::text, dst_shed.name,
       p.destination_partition_label,
       p.authorized_by::text, p.authorized_at,
       r.raised_by_user_id::text, p.raised_at, p.effective_at,
       coalesce(array_length(r.goat_ids, 1), 0) AS animal_count,
       coalesce(preview.animals, '[]'::jsonb) AS animals
FROM page p
LEFT JOIN req r ON r.shifting_event_id = p.shifting_event_id
-- projection-review: membership=the pending shifting events this operator may execute, one row per event (the aggregate below counts each event's animals, never the events themselves); group_key=shifting_event_id, so the four locations joins added here are label lookups on an already-unique row and change no grain; join_cardinality=each locations join is 1:{0,1} on (tenant_id, location_id) -- the added status='active' predicate only NARROWS a label lookup, and it is load-bearing: without it a source or destination could resolve to an inactive partition-alias location row and a movement would name a place no animal can live; pagination=keyset over the event list, and the per-event animal counts are computed per row rather than summed across the page; scope=tenant plus the operator's park scope
LEFT JOIN locations src_park ON src_park.location_id = p.source_park_id AND src_park.status = 'active'
LEFT JOIN locations src_shed ON src_shed.location_id = p.source_shed_id AND src_shed.status = 'active'
LEFT JOIN locations dst_park ON dst_park.location_id = p.destination_park_id AND dst_park.status = 'active'
LEFT JOIN locations dst_shed ON dst_shed.location_id = p.destination_shed_id AND dst_shed.status = 'active'
LEFT JOIN LATERAL (
    SELECT jsonb_agg(jsonb_build_object(
               'goat_id', g.goat_id::text,
               'display_id', g.display_id,
               'tag', tag.identifier_value
           ) ORDER BY g.display_id) AS animals
	    FROM unnest(r.goat_ids[1:$8]) AS gid
    JOIN goats g ON g.tenant_id = $1::uuid AND g.goat_id = gid
    LEFT JOIN LATERAL (
        SELECT gi.identifier_value
        FROM goat_identifiers gi
        WHERE gi.tenant_id = $1::uuid AND gi.goat_id = g.goat_id
          AND gi.status = 'active' AND gi.is_primary_for_goat
        ORDER BY gi.valid_from DESC
        LIMIT 1
    ) tag ON true
) preview ON true
ORDER BY p.raised_at DESC, p.shifting_event_id DESC`,
		q.TenantID, raisedFrom, raisedBefore, status, cursorRaisedAt, cursorID, pageSize+1,
		domain.MaxShiftingExecutionAnimalPreview, sourceParkID, sourceShedID, now.UTC())
	if err != nil {
		return domain.ShiftingExecutionPage{}, fmt.Errorf("counts: list shifting events pending execution: %w", err)
	}
	defer rows.Close()

	items := make([]domain.ShiftingExecutionRow, 0, pageSize)
	for rows.Next() {
		var (
			item       domain.ShiftingExecutionRow
			raisedBy   *string
			animalsRaw []byte
		)
		if err := rows.Scan(
			&item.ShiftingEventID, &item.EventStatus, &item.VerificationState, &item.PrimaryActionKey,
			&item.Priority, &item.Category,
			&item.SourceParkID, &item.SourceParkName, &item.SourceShedID, &item.SourceShedName,
			&item.SourcePartitionLabel,
			&item.DestinationParkID, &item.DestinationParkName,
			&item.DestinationShedID, &item.DestinationShedName,
			&item.DestinationPartitionLabel,
			&item.AuthorizedByUserID, &item.AuthorizedAt,
			&raisedBy, &item.RaisedAt, &item.EffectiveAt,
			&item.AnimalCount, &animalsRaw,
		); err != nil {
			return domain.ShiftingExecutionPage{}, fmt.Errorf("counts: scan pending-execution row: %w", err)
		}
		if raisedBy != nil {
			item.RaisedByUserID = *raisedBy
		}
		if len(animalsRaw) > 0 {
			if err := json.Unmarshal(animalsRaw, &item.Animals); err != nil {
				return domain.ShiftingExecutionPage{}, fmt.Errorf("counts: decode pending-execution animals: %w", err)
			}
		}
		if item.Animals == nil {
			item.Animals = []domain.ShiftingExecutionAnimal{}
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return domain.ShiftingExecutionPage{}, fmt.Errorf("counts: list shifting events pending execution: %w", err)
	}
	highIDs := make([]string, 0, len(items))
	for i := range items {
		if items[i].Priority == "high" {
			highIDs = append(highIDs, items[i].ShiftingEventID)
		}
	}
	if len(highIDs) > 0 {
		requirements, err := loadShiftingFeedRequirements(ctx, r.pool, q.TenantID, highIDs, time.Now())
		if err != nil {
			return domain.ShiftingExecutionPage{}, err
		}
		for i := range items {
			if req, ok := requirements[items[i].ShiftingEventID]; ok {
				items[i].FeedRequirement = &req
			}
		}
	}

	page := domain.ShiftingExecutionPage{}
	if len(items) > pageSize {
		last := items[pageSize-1]
		cursor, err := domain.EncodeShiftingExecutionCursor(domain.ShiftingExecutionCursor{
			AuthorizedAt:    last.RaisedAt,
			ShiftingEventID: last.ShiftingEventID,
		})
		if err != nil {
			return domain.ShiftingExecutionPage{}, err
		}
		page.NextCursor = cursor
		items = items[:pageSize]
	}
	page.Items = items
	if q.RaisedFrom != nil && q.RaisedBefore != nil {
		// Each FILTER mirrors ONE branch of the page predicate above, so a tab's count is exactly what
		// that tab lists. 'all' excludes 'pending' because an unapproved movement is not in the
		// operator's work list; it is reachable only through its own read-only Pending tab. 'rework'
		// excludes 'pending' for the same reason and 'canceled' because the page query drops canceled
		// rows globally -- without that the Rework tab could count a row it cannot show.
		if err := r.pool.QueryRow(ctx, `SELECT
 count(*) FILTER (WHERE event_status NOT IN ('canceled', 'pending')),
 count(*) FILTER (WHERE event_status='pending'),
 count(*) FILTER (WHERE event_status='authorized' AND verification_state <> 'rejected'),
 count(*) FILTER (WHERE verification_state='rejected' AND event_status NOT IN ('canceled', 'pending')),
 count(*) FILTER (WHERE event_status='applied' AND verification_state <> 'rejected')
FROM shifting_events se WHERE tenant_id=$1::uuid AND raised_at >= $2 AND raised_at < $3
  AND ($5::uuid IS NULL OR se.source_park_id = $5::uuid)
  AND ($6::uuid IS NULL OR se.source_shed_id = $6::uuid)
  AND `+shiftingActionsVisibleSQL("$4"),
			q.TenantID, q.RaisedFrom.UTC(), q.RaisedBefore.UTC(), now.UTC(), sourceParkID, sourceShedID).Scan(
			&page.StatusCounts.All, &page.StatusCounts.Pending, &page.StatusCounts.Authorized,
			&page.StatusCounts.Rework, &page.StatusCounts.Completed); err != nil {
			return domain.ShiftingExecutionPage{}, fmt.Errorf("counts: shifting actions summary: %w", err)
		}
		// Same visibility filter as the page and the counts: a previous date must not advertise work
		// the operator cannot yet see when they navigate to it.
		//
		// And the same OUTSTANDING-WORK filter as primary_action_key, via the one shared predicate:
		// this badge is a call to action, so it counts only what the operator still has to do. It
		// previously carried a bare `event_status <> 'canceled'`, which also counted completed and
		// unapproved movements.
		prevRows, err := r.pool.Query(ctx, `SELECT to_char((raised_at AT TIME ZONE 'Asia/Kolkata')::date, 'YYYY-MM-DD'), count(*)
FROM shifting_events se
WHERE tenant_id=$1::uuid AND `+shiftingOutstandingActionSQL()+`
  AND raised_at < $2 AND raised_at >= $2 - interval '90 days'
  AND ($4::uuid IS NULL OR se.source_park_id = $4::uuid)
  AND ($5::uuid IS NULL OR se.source_shed_id = $5::uuid)
  AND `+shiftingActionsVisibleSQL("$3")+`
GROUP BY (raised_at AT TIME ZONE 'Asia/Kolkata')::date
ORDER BY (raised_at AT TIME ZONE 'Asia/Kolkata')::date DESC LIMIT 5`, q.TenantID, q.RaisedFrom.UTC(), now.UTC(), sourceParkID, sourceShedID)
		if err != nil {
			return domain.ShiftingExecutionPage{}, fmt.Errorf("counts: shifting previous dates: %w", err)
		}
		defer prevRows.Close()
		page.PreviousDates = []domain.ShiftingPreviousDate{}
		for prevRows.Next() {
			var d domain.ShiftingPreviousDate
			if err := prevRows.Scan(&d.Date, &d.ActionCount); err != nil {
				return domain.ShiftingExecutionPage{}, err
			}
			page.PreviousDates = append(page.PreviousDates, d)
		}
		if err := prevRows.Err(); err != nil {
			return domain.ShiftingExecutionPage{}, err
		}
	}
	return page, nil
}

// validateDestinationPartitionAgainstCatalogTx re-checks, inside the applying
// transaction, that the destination the operator raised still names a real
// place, and returns the destination shed's display name.
//
// Why re-check at all: shifting applies at whichever of park-head-approval /
// operator-completion arrives SECOND, and the two gates are independent and may
// be far apart in time. A partition validated at raise time can be retired
// before the move commits. Writing the stale label would file animals into a
// partition that no longer exists.
//
// The authority is `shed_partitions` (migration 000112), NOT the per-goat
// `goat_shed_partitions` table: the latter only knows partitions that currently
// hold animals, so an EMPTY partition would be wrongly rejected here — that is
// precisely the destination an operator is trying to fill.
//
// Dual-shape rule, both directions must work:
//   - destination shed HAS active catalog partitions -> a label is required and
//     must match one of them (compared with the shared normalizer, so 'Part 3'
//     and '3' are the same partition).
//   - destination shed has NO catalog partitions -> a NULL label is correct and
//     is left NULL. Never coerce it to the 'whole' sentinel here; 'whole' is a
//     matching key, not a location.
func validateDestinationPartitionAgainstCatalogTx(
	ctx context.Context,
	tx pgx.Tx,
	tenantID string,
	destShedID string,
	label *string,
) (string, error) {
	var shedName string
	var partitionCount int
	var matches int
	if err := tx.QueryRow(ctx, `
SELECT COALESCE(NULLIF(shed.name, ''), shed.location_code, ''),
       (SELECT count(*) FROM shed_partitions sp
         WHERE sp.tenant_id = $1::uuid AND sp.shed_id = $2::uuid AND sp.status = 'active')::int,
       (SELECT count(*) FROM shed_partitions sp
         WHERE sp.tenant_id = $1::uuid AND sp.shed_id = $2::uuid AND sp.status = 'active'
           AND sp.normalized_label = regexp_replace(lower(btrim($3::text)), '^part[[:space:]]+', ''))::int
FROM locations shed
WHERE shed.tenant_id = $1::uuid AND shed.location_id = $2::uuid`,
		tenantID, destShedID, strings.TrimSpace(derefOrEmpty(label)),
	).Scan(&shedName, &partitionCount, &matches); err != nil {
		return "", fmt.Errorf("resolve destination shed %s partition catalog: %w", destShedID, err)
	}

	trimmed := strings.TrimSpace(derefOrEmpty(label))
	isBlank := trimmed == "" || strings.EqualFold(trimmed, "whole")

	if partitionCount == 0 {
		// Non-partitioned shed. A blank label is the correct answer; a label that
		// names a partition this shed does not have is a real error, not something
		// to silently drop.
		if !isBlank {
			return "", fmt.Errorf(
				"%w: destination shed %s has no partitions but movement names partition %q",
				ports.ErrShiftingExecutionIncomplete, destShedID, trimmed)
		}
		return shedName, nil
	}

	if isBlank {
		return "", fmt.Errorf(
			"%w: destination shed %s is partitioned (%d partitions) but movement carries no destination partition",
			ports.ErrShiftingExecutionIncomplete, destShedID, partitionCount)
	}
	if matches == 0 {
		return "", fmt.Errorf(
			"%w: destination partition %q is no longer in the catalog for shed %s (retired between raise and apply)",
			ports.ErrShiftingExecutionIncomplete, trimmed, destShedID)
	}
	return shedName, nil
}

func derefOrEmpty(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// fetchShedName reads the display name of a shed location.
func (r *Repository) fetchShedName(ctx context.Context, tenantID, shedID string) (string, error) {
	var shedName string
	err := r.pool.QueryRow(ctx, `
SELECT COALESCE(NULLIF(shed.name, ''), shed.location_code, '')
FROM locations shed
WHERE shed.tenant_id = $1::uuid AND shed.location_id = $2::uuid`,
		tenantID, shedID).Scan(&shedName)
	if err != nil {
		return "", fmt.Errorf("counts: fetch shed name %s: %w", shedID, err)
	}
	return shedName, nil
}
