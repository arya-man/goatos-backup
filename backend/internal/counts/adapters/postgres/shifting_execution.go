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
// 'authorized' -- enforced here by the `AND event_status = 'authorized'` transition predicate, and
// independently by shifting_events_applied_requires_authorization_check in the schema, so a caller
// addressing a pending movement's id cannot execute an unapproved relocation.

// ---------------------------------------------------------------------------
// Complete
// ---------------------------------------------------------------------------

// CompleteShiftingEvent SUBMITS an authorized movement for verification (maintainer decision,
// 2026-07-26, superseding the 2026-07-19 completion-applies-the-move rule for shifting).
//
// The operator confirms the animals walked AND records a MANDATORY video (in.ProofRef). This method
// does NOT relocate anybody and does NOT move the count: it flips the movement to
// 'pending_verification', stores the video reference, and stamps the completion idempotency pair.
// The animals are physically in the destination shed while the census still reads the source shed;
// the relocation runs later, in ApplyVerifiedShiftingEvent, only when a verifier approves the video.
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
	goatIDs, destParkID, destShedID, err := r.shiftingMovementSet(ctx, tx, in.TenantID, in.ShiftingEventID)
	if err != nil {
		return domain.ShiftingExecutionResult{}, false, err
	}

	// Already submitted for verification, or already applied. Answer with the original result rather
	// than enqueuing / relocating a second time.
	if current.EventStatus == domain.ShiftingEventStatusPendingVerification ||
		current.EventStatus == domain.ShiftingEventStatusApplied {
		if in.IdempotencyKey != "" && current.CompletionIdempotencyKey != nil &&
			*current.CompletionIdempotencyKey == in.IdempotencyKey {
			if current.CompletionRequestFingerprint == nil ||
				*current.CompletionRequestFingerprint != in.RequestFingerprint {
				return domain.ShiftingExecutionResult{}, false, ports.ErrIdempotencyConflict
			}
		}
		return domain.ShiftingExecutionResult{
			ShiftingEventID:   in.ShiftingEventID,
			EventStatus:       current.EventStatus,
			DestinationParkID: destParkID,
			DestinationShedID: destShedID,
			MovedGoatIDs:      goatIDs,
			AppliedAt:         current.AppliedAt,
			AppliedBy:         current.AppliedBy,
		}, true, nil
	}

	if current.EventStatus != domain.ShiftingEventStatusAuthorized {
		return domain.ShiftingExecutionResult{}, false, fmt.Errorf(
			"%w: shifting event %s is %q, and completion may only start from %q",
			ports.ErrShiftingNotAuthorized, in.ShiftingEventID, current.EventStatus,
			domain.ShiftingEventStatusAuthorized)
	}

	if len(goatIDs) == 0 {
		// Fail closed. A movement naming nobody cannot be "completed": submitting it for verification
		// would queue a video that proves the relocation of no animals.
		return domain.ShiftingExecutionResult{}, false, fmt.Errorf(
			"%w: shifting event %s names no animals to move",
			ports.ErrShiftingExecutionIncomplete, in.ShiftingEventID)
	}

	// The status flip: authorized -> pending_verification. NOTHING relocates here. The mandatory
	// video (proof_ref) is stored so the verification enqueue can carry it, and the completion
	// idempotency pair is stamped. `AND event_status = 'authorized'` makes the transition its own
	// concurrency guard: if anything canceled/re-decided this movement since the lock, zero rows
	// update and the transaction rolls back.
	tag, err := tx.Exec(ctx, `
UPDATE shifting_events
SET event_status = 'pending_verification',
    verification_state = 'unverified',
    proof_ref = $3,
    completion_idempotency_key = nullif($4, ''),
    completion_request_fingerprint = nullif($5, ''),
    updated_at = now(),
    row_version = row_version + 1
WHERE tenant_id = $1::uuid AND shifting_event_id = $2::uuid AND event_status = 'authorized'`,
		in.TenantID, in.ShiftingEventID, strings.TrimSpace(in.ProofRef),
		in.IdempotencyKey, in.RequestFingerprint)
	if err != nil {
		return domain.ShiftingExecutionResult{}, false, fmt.Errorf("counts: submit shifting for verification: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ShiftingExecutionResult{}, false, ports.ErrShiftingNotAuthorized
	}

	if err := tx.Commit(ctx); err != nil {
		return domain.ShiftingExecutionResult{}, false, err
	}
	committed = true

	return domain.ShiftingExecutionResult{
		ShiftingEventID:   in.ShiftingEventID,
		EventStatus:       domain.ShiftingEventStatusPendingVerification,
		DestinationParkID: destParkID,
		DestinationShedID: destShedID,
		MovedGoatIDs:      goatIDs,
	}, false, nil
}

// ApplyVerifiedShiftingEvent relocates the animals of a movement whose video a verifier has APPROVED,
// and flips it 'pending_verification' -> 'applied'. This is the consumer half of the 2026-07-26
// rule: it runs from the verification.verdict.approved consumer, NOT from the operator's phone, and
// it is the ONLY place a shifting movement writes an animal's canonical location.
//
// ATOMICITY mirrors the old completion path: one transaction locks the row, relocates through
// identity's transaction-scoped seam, and flips the status; any failure rolls the whole thing back,
// so there is never an 'applied' row whose animals did not move.
//
// IDEMPOTENCY: a re-delivered verdict on an already-'applied' movement returns applied=false and
// relocates nobody. A verdict that arrives when the movement is no longer 'pending_verification'
// (canceled, bounced back to authorized, superseded) is ignored as a stale delivery.
func (r *Repository) ApplyVerifiedShiftingEvent(
	ctx context.Context, in domain.ShiftingVerifiedApplyCommand,
) (domain.ShiftingExecutionResult, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	if r.identityTx == nil {
		return domain.ShiftingExecutionResult{}, false, fmt.Errorf(
			"counts: apply verified shifting event %s: identity write seam is not wired", in.ShiftingEventID)
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

	goatIDs, destParkID, destShedID, err := r.shiftingMovementSet(ctx, tx, in.TenantID, in.ShiftingEventID)
	if err != nil {
		return domain.ShiftingExecutionResult{}, false, err
	}

	// Already applied: a re-delivered verdict must not move the herd onward.
	if current.EventStatus == domain.ShiftingEventStatusApplied {
		return domain.ShiftingExecutionResult{
			ShiftingEventID:   in.ShiftingEventID,
			EventStatus:       current.EventStatus,
			DestinationParkID: destParkID,
			DestinationShedID: destShedID,
			MovedGoatIDs:      goatIDs,
			AppliedAt:         current.AppliedAt,
			AppliedBy:         current.AppliedBy,
		}, false, nil
	}

	// A verdict for a movement that is no longer awaiting verification is a stale delivery (the move
	// was canceled or bounced back before the verifier's approval landed). Ignore it rather than
	// resurrecting a retired movement.
	if current.EventStatus != domain.ShiftingEventStatusPendingVerification {
		return domain.ShiftingExecutionResult{
			ShiftingEventID: in.ShiftingEventID,
			EventStatus:     current.EventStatus,
		}, false, nil
	}

	if len(goatIDs) == 0 {
		return domain.ShiftingExecutionResult{}, false, fmt.Errorf(
			"%w: shifting event %s names no animals to move",
			ports.ErrShiftingExecutionIncomplete, in.ShiftingEventID)
	}

	sourceParkID, sourceShedID, err := r.readShiftingEventSourceLocation(ctx, tx, in.TenantID, in.ShiftingEventID)
	if err != nil {
		return domain.ShiftingExecutionResult{}, false, fmt.Errorf("counts: read shifting source location: %w", err)
	}

	moved, err := r.identityTx.RelocateGoatsToShedInTx(ctx, tx, identityports.RelocateGoatsCommand{
		TenantID:       in.TenantID,
		ActorID:        in.VerifiedByUserID,
		GoatIDs:        goatIDs,
		FromParkID:     sourceParkID,
		FromShedID:     sourceShedID,
		ToParkID:       destParkID,
		ToShedID:       destShedID,
		DestinationTag: in.DestinationTag,
		TraceID:        in.TraceID,
		Reason:         "counts shifting verified " + in.ShiftingEventID,
		// The relocation is stamped with the moment of VERIFICATION -- when the move became real.
		OccurredAt:              in.VerifiedAt,
		OutboxIdempotencyPrefix: "counts-shifting-verified:" + in.ShiftingEventID,
	})
	if err != nil {
		return domain.ShiftingExecutionResult{}, false, err
	}
	if len(moved.MovedGoatIDs) != len(goatIDs) {
		return domain.ShiftingExecutionResult{}, false, fmt.Errorf(
			"%w: shifting event %s named %d animals but %d were movable",
			ports.ErrShiftingExecutionIncomplete, in.ShiftingEventID, len(goatIDs), len(moved.MovedGoatIDs))
	}

	var (
		appliedAt time.Time
		appliedBy string
	)
	if err := tx.QueryRow(ctx, `
UPDATE shifting_events
SET event_status = 'applied',
    verification_state = 'verified',
    applied_at = $3::timestamptz,
    applied_by = $4::uuid,
    updated_at = now(),
    row_version = row_version + 1
WHERE tenant_id = $1::uuid AND shifting_event_id = $2::uuid AND event_status = 'pending_verification'
RETURNING applied_at, applied_by::text`,
		in.TenantID, in.ShiftingEventID, in.VerifiedAt.UTC(), in.VerifiedByUserID).Scan(&appliedAt, &appliedBy); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ShiftingExecutionResult{}, false, ports.ErrShiftingNotAuthorized
		}
		return domain.ShiftingExecutionResult{}, false, fmt.Errorf("counts: apply verified shifting event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return domain.ShiftingExecutionResult{}, false, err
	}
	committed = true

	srcPark, srcShed := "", ""
	if sourceParkID != nil {
		srcPark = *sourceParkID
	}
	if sourceShedID != nil {
		srcShed = *sourceShedID
	}
	return domain.ShiftingExecutionResult{
		ShiftingEventID:   in.ShiftingEventID,
		EventStatus:       domain.ShiftingEventStatusApplied,
		DestinationParkID: destParkID,
		DestinationShedID: destShedID,
		SourceParkID:      srcPark,
		SourceShedID:      srcShed,
		MovedGoatIDs:      moved.MovedGoatIDs,
		AppliedAt:         &appliedAt,
		AppliedBy:         &appliedBy,
	}, true, nil
}

// BounceShiftingEventForRework returns a movement whose video a verifier REJECTED to 'authorized',
// so the operator sees it again in the pending-execution queue and re-records the video. NOTHING
// relocates. It runs from the verification.verdict.rework consumer.
//
// Idempotent: only a 'pending_verification' row is bounced; a verdict re-delivered after the
// movement already moved on (re-completed, applied, canceled) is a no-op.
func (r *Repository) BounceShiftingEventForRework(
	ctx context.Context, in domain.ShiftingReworkCommand,
) error {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	tag, err := r.pool.Exec(ctx, `
UPDATE shifting_events
SET event_status = 'authorized',
    verification_state = 'rejected',
    completion_idempotency_key = NULL,
    completion_request_fingerprint = NULL,
    updated_at = now(),
    row_version = row_version + 1
WHERE tenant_id = $1::uuid AND shifting_event_id = $2::uuid AND event_status = 'pending_verification'`,
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

	DestinationParkID string
	DestinationShedID string

	AppliedAt *time.Time
	AppliedBy *string

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
SELECT event_status, authorization_state,
       destination_park_id::text, destination_shed_id::text,
       applied_at, applied_by::text,
       canceled_at, canceled_by::text, cancel_reason,
       completion_idempotency_key, completion_request_fingerprint,
       cancel_idempotency_key, cancel_request_fingerprint
FROM shifting_events
WHERE tenant_id = $1::uuid AND shifting_event_id = $2::uuid
FOR UPDATE`, tenantID, shiftingEventID).Scan(
		&out.EventStatus, &out.AuthorizationState,
		&out.DestinationParkID, &out.DestinationShedID,
		&out.AppliedAt, &out.AppliedBy,
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

// readShiftingEventSourceLocation reads the expected source park and shed for a shifting event.
// P1 follow-up #1: Used to guard against stale location overwrites.
func (r *Repository) readShiftingEventSourceLocation(
	ctx context.Context, tx pgx.Tx, tenantID, shiftingEventID string,
) (*string, *string, error) {
	var sourceParkID, sourceShedID *string
	err := tx.QueryRow(ctx, `
SELECT source_park_id::text, source_shed_id::text
FROM shifting_events
WHERE tenant_id = $1::uuid AND shifting_event_id = $2::uuid`,
		tenantID, shiftingEventID).Scan(&sourceParkID, &sourceShedID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil, ports.ErrShiftingEventNotFound
		}
		return nil, nil, fmt.Errorf("counts: read shifting source location: %w", err)
	}
	return sourceParkID, sourceShedID, nil
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

// ---------------------------------------------------------------------------
// Pending-execution queue
// ---------------------------------------------------------------------------

// ListShiftingEventsPendingExecution returns one keyset page of AUTHORIZED movements.
//
// projection-review: canonical-source read, not a projection. GRAIN = one row per shifting_event,
// which is the queue's natural unit of work (an operator executes a movement, not an animal). The
// only aggregate is animal_count, and it is computed from ONE array on ONE approval request per
// event -- the LEFT JOIN to counts_approval_requests is 1:1 by (tenant_id, shifting_event_id, and
// status='approved'), which counts_approval_requests_shifting_link_check keeps single-valued -- so
// there is no many-side to fan out and no possibility of a join multiplying the count. The animal
// preview is a LATERAL over a SLICE of that same array, so it cannot inflate the row either, and
// the count is read from the full array rather than from the preview, keeping the displayed total
// independent of the preview bound and of page size.
//
// Keyset, not OFFSET: this queue is drained by several operators at once, so an offset page would
// skip or repeat movements while one of them pages. The predicate and ORDER BY match
// shifting_events_pending_execution_idx (and ..._park_idx when park_id is supplied), both partial
// on event_status='authorized', so a page is an index range scan bounded by the page size rather
// than a filter over every movement the tenant has ever recorded. The optional source_shed_id is a
// residual equality on top of that same authorized index range -- the farm -> shed cascade always
// pins the park too, so the shed narrows an already park-bounded set, not the whole tenant.
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
		cursorAuthorizedAt any
		cursorID           any
		parkFilter         any
		shedFilter         any
	)
	if q.Cursor != nil {
		cursorAuthorizedAt = q.Cursor.AuthorizedAt.UTC()
		cursorID = q.Cursor.ShiftingEventID
	}
	if q.SourceParkID != "" {
		parkFilter = q.SourceParkID
	}
	if q.SourceShedID != "" {
		shedFilter = q.SourceShedID
	}

	// Fetch one extra row to decide whether a next page exists, without a second COUNT query.
	rows, err := r.pool.Query(ctx, `
WITH page AS (
    SELECT se.shifting_event_id, se.priority, se.category,
           se.source_park_id, se.source_shed_id,
           se.destination_park_id, se.destination_shed_id,
           se.authorized_by, se.authorized_at, se.raised_at, se.effective_at
    FROM shifting_events se
    WHERE se.tenant_id = $1::uuid
      AND se.event_status = 'authorized'
      AND ($2::uuid IS NULL OR se.source_park_id = $2::uuid)
      AND ($7::uuid IS NULL OR se.source_shed_id = $7::uuid)
      AND ($3::timestamptz IS NULL
           OR (se.authorized_at, se.shifting_event_id) < ($3::timestamptz, $4::uuid))
    ORDER BY se.authorized_at DESC, se.shifting_event_id DESC
    LIMIT $5
), req AS (
    SELECT p.shifting_event_id,
           car.raised_by_user_id,
           ARRAY(SELECT jsonb_array_elements_text(car.payload -> 'goat_ids'))::uuid[] AS goat_ids
    FROM page p
    LEFT JOIN counts_approval_requests car
           ON car.tenant_id = $1::uuid
          AND car.shifting_event_id = p.shifting_event_id
          AND car.status = 'approved'
)
SELECT p.shifting_event_id::text, p.priority, p.category,
       p.source_park_id::text, src_park.name, p.source_shed_id::text, src_shed.name,
       p.destination_park_id::text, dst_park.name,
       p.destination_shed_id::text, dst_shed.name,
       p.authorized_by::text, p.authorized_at,
       r.raised_by_user_id::text, p.raised_at, p.effective_at,
       coalesce(array_length(r.goat_ids, 1), 0) AS animal_count,
       coalesce(preview.animals, '[]'::jsonb) AS animals
FROM page p
LEFT JOIN req r ON r.shifting_event_id = p.shifting_event_id
LEFT JOIN locations src_park ON src_park.location_id = p.source_park_id
LEFT JOIN locations src_shed ON src_shed.location_id = p.source_shed_id
LEFT JOIN locations dst_park ON dst_park.location_id = p.destination_park_id
LEFT JOIN locations dst_shed ON dst_shed.location_id = p.destination_shed_id
LEFT JOIN LATERAL (
    SELECT jsonb_agg(jsonb_build_object(
               'goat_id', g.goat_id::text,
               'display_id', g.display_id,
               'tag', tag.identifier_value
           ) ORDER BY g.display_id) AS animals
    FROM unnest(r.goat_ids[1:$6]) AS gid
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
ORDER BY p.authorized_at DESC, p.shifting_event_id DESC`,
		q.TenantID, parkFilter, cursorAuthorizedAt, cursorID, pageSize+1,
		domain.MaxShiftingExecutionAnimalPreview, shedFilter)
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
			&item.ShiftingEventID, &item.Priority, &item.Category,
			&item.SourceParkID, &item.SourceParkName, &item.SourceShedID, &item.SourceShedName,
			&item.DestinationParkID, &item.DestinationParkName,
			&item.DestinationShedID, &item.DestinationShedName,
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

	page := domain.ShiftingExecutionPage{}
	if len(items) > pageSize {
		last := items[pageSize-1]
		var authorizedAt time.Time
		if last.AuthorizedAt != nil {
			authorizedAt = *last.AuthorizedAt
		}
		cursor, err := domain.EncodeShiftingExecutionCursor(domain.ShiftingExecutionCursor{
			AuthorizedAt:    authorizedAt,
			ShiftingEventID: last.ShiftingEventID,
		})
		if err != nil {
			return domain.ShiftingExecutionPage{}, err
		}
		page.NextCursor = cursor
		items = items[:pageSize]
	}
	page.Items = items
	return page, nil
}
