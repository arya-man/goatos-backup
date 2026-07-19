package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

// CompleteShiftingEvent executes an authorized movement.
//
// ATOMICITY -- the point of this method. One transaction contains, in order:
//
//  1. SELECT ... FOR UPDATE of the shifting_events row (serializes two operators pressing
//     "Completed" on the same movement),
//  2. the animal set read back from the approval request that authorized the movement,
//  3. the RELOCATION, through identity's transaction-scoped seam,
//  4. the flip to event_status='applied' plus the applied_at/applied_by stamp and the completion
//     idempotency pair.
//
// Any failure in 3 rolls back 1, 2 and 4 together. There is no window in which the movement reads
// 'applied' while the animals did not move, and none in which the animals moved while the movement
// still reads 'authorized' -- which is what the atomic transition rule in AGENTS.md requires. The
// schema backs it up: shifting_events_applied_shape_check forbids an 'applied' row with no
// completion stamp.
//
// IDEMPOTENCY. Two distinct replays are handled, because a phone in a park WILL retry this:
//
//   - Same completion key on an already-applied movement: the stored key/fingerprint pair is
//     compared. An exact replay returns the original result with replay=true and relocates nobody
//     again; a same-key/different-payload replay is ErrIdempotencyConflict.
//   - DIFFERENT key on an already-applied movement: also returns the original result with
//     replay=true. Completion is a confirmation of a physical fact, not a command that may run
//     twice -- a second operator confirming the same movement must not move the herd onward, and
//     the animals are already at the destination, so the correct answer is the original one.
func (r *Repository) CompleteShiftingEvent(
	ctx context.Context, in domain.ShiftingCompletionCommand,
) (domain.ShiftingExecutionResult, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	if r.identityTx == nil {
		return domain.ShiftingExecutionResult{}, false, fmt.Errorf(
			"counts: complete shifting event %s: identity write seam is not wired", in.ShiftingEventID)
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

	// Already executed. Answer with the original result rather than relocating a second time.
	if current.EventStatus == domain.ShiftingEventStatusApplied {
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
		// Fail closed. A movement naming nobody cannot be "completed": flipping it to applied would
		// record that animals moved while relocating none, which is the count-moves-nothing shape
		// the 2026-07-19 decision retired.
		return domain.ShiftingExecutionResult{}, false, fmt.Errorf(
			"%w: shifting event %s names no animals to move",
			ports.ErrShiftingExecutionIncomplete, in.ShiftingEventID)
	}

	// THE RELOCATION. This is the only place in the shifting flow that writes an animal's canonical
	// location, and it runs here -- at completion -- rather than at approval, because only now has
	// somebody asserted that the animals are physically standing in the destination shed.
	moved, err := r.identityTx.RelocateGoatsToShedInTx(ctx, tx, identityports.RelocateGoatsCommand{
		TenantID: in.TenantID,
		ActorID:  in.CompletedByUserID,
		GoatIDs:  goatIDs,
		ToParkID: destParkID,
		ToShedID: destShedID,
		Reason:   "counts shifting completion " + in.ShiftingEventID,
		// The relocation is stamped with the moment of COMPLETION, not of approval: the animals'
		// location history must read when they moved, not when someone permitted it.
		OccurredAt: in.CompletedAt,
		// Keyed to the EVENT, not to the caller's client key, so two operators completing the same
		// movement with different client keys still collapse onto one outbox message per animal.
		OutboxIdempotencyPrefix: "counts-shifting-completion:" + in.ShiftingEventID,
	})
	if err != nil {
		return domain.ShiftingExecutionResult{}, false, err
	}
	// Fail closed on a shortfall, exactly as the approve path used to: if any named animal was not
	// movable (exited, merged, wrong tenant) the completion must not half-apply. Rolling back
	// leaves the movement authorized so a human can resolve the animal and retry -- far better than
	// an 'applied' movement whose herd register disagrees with the shed.
	if len(moved.MovedGoatIDs) != len(goatIDs) {
		return domain.ShiftingExecutionResult{}, false, fmt.Errorf(
			"%w: shifting event %s named %d animals but %d were movable",
			ports.ErrShiftingExecutionIncomplete, in.ShiftingEventID, len(goatIDs), len(moved.MovedGoatIDs))
	}

	// The status flip. `AND event_status = 'authorized'` makes the transition itself the
	// concurrency guard: if anything completed or canceled this movement since the lock, zero rows
	// update and the whole transaction -- including the relocation above -- rolls back.
	var (
		appliedAt time.Time
		appliedBy string
	)
	if err := tx.QueryRow(ctx, `
UPDATE shifting_events
SET event_status = 'applied',
    applied_at = $3::timestamptz,
    applied_by = $4::uuid,
    completion_idempotency_key = nullif($5, ''),
    completion_request_fingerprint = nullif($6, ''),
    updated_at = now(),
    row_version = row_version + 1
WHERE tenant_id = $1::uuid AND shifting_event_id = $2::uuid AND event_status = 'authorized'
RETURNING applied_at, applied_by::text`,
		in.TenantID, in.ShiftingEventID, in.CompletedAt.UTC(), in.CompletedByUserID,
		in.IdempotencyKey, in.RequestFingerprint).Scan(&appliedAt, &appliedBy); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ShiftingExecutionResult{}, false, ports.ErrShiftingNotAuthorized
		}
		return domain.ShiftingExecutionResult{}, false, fmt.Errorf("counts: complete shifting event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return domain.ShiftingExecutionResult{}, false, err
	}
	committed = true

	return domain.ShiftingExecutionResult{
		ShiftingEventID:   in.ShiftingEventID,
		EventStatus:       domain.ShiftingEventStatusApplied,
		DestinationParkID: destParkID,
		DestinationShedID: destShedID,
		MovedGoatIDs:      moved.MovedGoatIDs,
		AppliedAt:         &appliedAt,
		AppliedBy:         &appliedBy,
	}, false, nil
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

// shiftingMovementSet reads the animals a movement covers, plus its destination.
//
// The animal set is the goat_ids captured on the APPROVED approval request that authorized this
// movement. Restricting to status='approved' is load-bearing rather than cosmetic: it means a
// completion can only ever relocate the animal set an approver actually signed off on, so editing
// a request after approval (or a second, still-pending request naming other animals) cannot widen
// what completion moves.
func (r *Repository) shiftingMovementSet(
	ctx context.Context, tx pgx.Tx, tenantID, shiftingEventID string,
) (goatIDs []string, destParkID, destShedID string, err error) {
	var payload []byte
	err = tx.QueryRow(ctx, `
SELECT car.payload, se.destination_park_id::text, se.destination_shed_id::text
FROM shifting_events se
LEFT JOIN counts_approval_requests car
       ON car.tenant_id = se.tenant_id
      AND car.shifting_event_id = se.shifting_event_id
      AND car.status = 'approved'
WHERE se.tenant_id = $1::uuid AND se.shifting_event_id = $2::uuid`,
		tenantID, shiftingEventID).Scan(&payload, &destParkID, &destShedID)
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
// than a filter over every movement the tenant has ever recorded.
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
	)
	if q.Cursor != nil {
		cursorAuthorizedAt = q.Cursor.AuthorizedAt.UTC()
		cursorID = q.Cursor.ShiftingEventID
	}
	if q.SourceParkID != "" {
		parkFilter = q.SourceParkID
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
		domain.MaxShiftingExecutionAnimalPreview)
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
