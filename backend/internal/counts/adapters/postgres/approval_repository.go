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
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

// IdentityTxWriter is the slice of the identity module's Postgres repository that the Counts
// approval workflow applies INSIDE its own transaction.
//
// It is declared here, in the consuming package, so identity does not have to know that Counts
// exists. *identitypostgres.Repository satisfies it; bootstrap wires the real one in.
//
// Why a transaction-scoped seam rather than just calling identity's app service: the app service
// methods are transaction-terminal (they open a pool transaction and commit it). Calling one from
// an approval would give two independent transactions, and a crash between them would leave a
// request reading 'approved' with no goat -- or a goat with no approval. Threading the caller's
// transaction is what makes the status flip and its effect a single atomic unit.
//
// Every rule still lives in identity: these methods run the same validation, idempotency, decision
// records, audit rows, and outbox events as the pool-owned calls. Counts never writes `goats`.
type IdentityTxWriter interface {
	CreateAdminGoatInTx(ctx context.Context, tx pgx.Tx, cmd identityports.CreateAdminGoatCommand) (*identityports.AdminGoatMutationResult, error)
	ExitGoatInTx(ctx context.Context, tx pgx.Tx, cmd identityports.ExitGoatCommand) (*identityports.AdminGoatMutationResult, error)
	RelocateGoatsToShedInTx(ctx context.Context, tx pgx.Tx, cmd identityports.RelocateGoatsCommand) (identityports.RelocateGoatsResult, error)
}

// WithIdentityTxWriter injects the identity write seam used to apply approved birth/death/shifting
// effects. A repository without it can still submit and list requests; approving one returns a
// clear error rather than silently skipping the effect.
func (r *Repository) WithIdentityTxWriter(w IdentityTxWriter) *Repository {
	r.identityTx = w
	return r
}

const approvalRequestColumns = `
    approval_request_id::text, tenant_id::text, request_type, payload,
    shifting_event_id::text, subject_goat_id::text, status,
    raised_by_user_id::text, raised_at,
    decided_by_user_id::text, decided_at, decision_reason,
    applied_result_type, applied_result_id::text,
    idempotency_key, request_fingerprint, row_version`

func scanApprovalRequest(row pgx.Row) (domain.ApprovalRequest, error) {
	var (
		out             domain.ApprovalRequest
		payload         []byte
		shiftingEventID *string
		subjectGoatID   *string
		decidedBy       *string
		decidedAt       *time.Time
		decisionReason  *string
		resultType      *string
		resultID        *string
	)
	if err := row.Scan(
		&out.ApprovalRequestID, &out.TenantID, &out.RequestType, &payload,
		&shiftingEventID, &subjectGoatID, &out.Status,
		&out.RaisedByUserID, &out.RaisedAt,
		&decidedBy, &decidedAt, &decisionReason,
		&resultType, &resultID,
		&out.IdempotencyKey, &out.RequestFingerprint, &out.RowVersion,
	); err != nil {
		return domain.ApprovalRequest{}, err
	}
	out.Payload = json.RawMessage(payload)
	out.ShiftingEventID = shiftingEventID
	out.SubjectGoatID = subjectGoatID
	out.DecidedByUserID = decidedBy
	out.DecidedAt = decidedAt
	out.DecisionReason = decisionReason
	out.AppliedResultType = resultType
	out.AppliedResultID = resultID
	return out, nil
}

// ---------------------------------------------------------------------------
// Submit
// ---------------------------------------------------------------------------

// CreateApprovalRequest persists a PENDING request and applies nothing.
//
// Idempotency contract: the (tenant, idempotency_key) unique index makes a duplicate submit
// collapse onto the original row. The stored request_fingerprint is then compared -- an exact
// replay returns the original request with replay=true and no second row; a same-key/different-
// payload replay is ErrIdempotencyConflict.
func (r *Repository) CreateApprovalRequest(ctx context.Context, in domain.ApprovalRequestSubmission) (domain.ApprovalRequest, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	payload := in.Payload
	if len(payload) == 0 {
		payload = json.RawMessage(`{}`)
	}
	raisedAt := in.RaisedAt
	if raisedAt.IsZero() {
		// Matches the app-tier handler's raised_at: a business timestamp in the business location,
		// so a submission that omits it buckets into the same business day as one that sends it.
		raisedAt = time.Now().In(biztime.DefaultLocation())
	}

	row := r.pool.QueryRow(ctx, `
INSERT INTO counts_approval_requests (
  tenant_id, request_type, payload, shifting_event_id, subject_goat_id,
  status, raised_by_user_id, raised_at, idempotency_key, request_fingerprint
) VALUES (
  $1::uuid, $2, $3::jsonb, nullif($4, '')::uuid, nullif($5, '')::uuid,
  'pending', $6::uuid, $7::timestamptz, $8, $9
)
ON CONFLICT (tenant_id, idempotency_key) DO NOTHING
RETURNING `+approvalRequestColumns,
		in.TenantID, in.RequestType, string(payload),
		stringOrEmpty(in.ShiftingEventID), stringOrEmpty(in.SubjectGoatID),
		in.RaisedByUserID, raisedAt.UTC(), in.IdempotencyKey, in.RequestFingerprint)

	created, err := scanApprovalRequest(row)
	if err == nil {
		return created, false, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return domain.ApprovalRequest{}, false, fmt.Errorf("counts: insert approval request: %w", err)
	}

	// The key already existed. Return the original row iff the payload fingerprint matches.
	existing, err := scanApprovalRequest(r.pool.QueryRow(ctx, `
SELECT `+approvalRequestColumns+`
FROM counts_approval_requests
WHERE tenant_id = $1::uuid AND idempotency_key = $2`, in.TenantID, in.IdempotencyKey))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ApprovalRequest{}, false, ports.ErrIdempotencyInProgress
		}
		return domain.ApprovalRequest{}, false, fmt.Errorf("counts: read approval request replay: %w", err)
	}
	if existing.RequestFingerprint != in.RequestFingerprint {
		return domain.ApprovalRequest{}, false, ports.ErrIdempotencyConflict
	}
	return existing, true, nil
}

// ---------------------------------------------------------------------------
// Read
// ---------------------------------------------------------------------------

func (r *Repository) GetApprovalRequest(ctx context.Context, tenantID, approvalRequestID string) (domain.ApprovalRequest, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	out, err := scanApprovalRequest(r.pool.QueryRow(ctx, `
SELECT `+approvalRequestColumns+`
FROM counts_approval_requests
WHERE tenant_id = $1::uuid AND approval_request_id = $2::uuid`, tenantID, approvalRequestID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ApprovalRequest{}, ports.ErrApprovalRequestNotFound
		}
		return domain.ApprovalRequest{}, fmt.Errorf("counts: read approval request: %w", err)
	}
	return out, nil
}

// ListApprovalRequests returns one keyset page ordered by (raised_at, approval_request_id) DESC.
//
// Keyset, not OFFSET: the approvals queue is appended to continuously, so an offset page would
// skip or repeat rows as new requests arrive while an approver pages. The predicate matches
// counts_approval_requests_pending_queue_idx (partial on status='pending') for the hot pending
// list and counts_approval_requests_status_queue_idx for decided history, so both are index scans
// bounded by the page size rather than filters over the table.
//
// RequestTypes is caller-authority, not a client filter: it is derived from the caller's
// permissions, so the page can only ever contain rows this approver is allowed to decide.
func (r *Repository) ListApprovalRequests(ctx context.Context, q domain.ApprovalRequestQuery) (domain.ApprovalRequestPage, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	if len(q.RequestTypes) == 0 {
		// No decidable types means no visible rows. Fail closed rather than dropping the predicate.
		return domain.ApprovalRequestPage{Items: []domain.ApprovalRequestSummary{}}, nil
	}
	pageSize := q.PageSize
	if pageSize <= 0 || pageSize > domain.MaxApprovalPageSize {
		pageSize = domain.MaxApprovalPageSize
	}

	var (
		cursorRaisedAt any
		cursorID       any
	)
	if q.Cursor != nil {
		cursorRaisedAt = q.Cursor.RaisedAt.UTC()
		cursorID = q.Cursor.ApprovalRequestID
	}

	// Fetch one extra row to decide whether a next page exists, without a second COUNT query.
	rows, err := r.pool.Query(ctx, `
SELECT approval_request_id::text, request_type, status,
       raised_by_user_id::text, raised_at,
       shifting_event_id::text, subject_goat_id::text, payload,
       decided_by_user_id::text, decided_at, decision_reason
FROM counts_approval_requests
WHERE tenant_id = $1::uuid
  AND status = $2
  AND request_type = ANY($3::text[])
  AND ($4::timestamptz IS NULL OR (raised_at, approval_request_id) < ($4::timestamptz, $5::uuid))
ORDER BY raised_at DESC, approval_request_id DESC
LIMIT $6`, q.TenantID, q.Status, q.RequestTypes, cursorRaisedAt, cursorID, pageSize+1)
	if err != nil {
		return domain.ApprovalRequestPage{}, fmt.Errorf("counts: list approval requests: %w", err)
	}
	defer rows.Close()

	items := make([]domain.ApprovalRequestSummary, 0, pageSize)
	for rows.Next() {
		var (
			item    domain.ApprovalRequestSummary
			payload []byte
		)
		if err := rows.Scan(
			&item.ApprovalRequestID, &item.RequestType, &item.Status,
			&item.RaisedByUserID, &item.RaisedAt,
			&item.ShiftingEventID, &item.SubjectGoatID, &payload,
			&item.DecidedByUserID, &item.DecidedAt, &item.DecisionReason,
		); err != nil {
			return domain.ApprovalRequestPage{}, fmt.Errorf("counts: scan approval request: %w", err)
		}
		item.Summary = json.RawMessage(payload)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return domain.ApprovalRequestPage{}, fmt.Errorf("counts: list approval requests: %w", err)
	}

	page := domain.ApprovalRequestPage{}
	if len(items) > pageSize {
		last := items[pageSize-1]
		cursor, err := domain.EncodeApprovalRequestCursor(domain.ApprovalRequestCursor{
			RaisedAt:          last.RaisedAt,
			ApprovalRequestID: last.ApprovalRequestID,
		})
		if err != nil {
			return domain.ApprovalRequestPage{}, err
		}
		page.NextCursor = cursor
		items = items[:pageSize]
	}
	page.Items = items
	return page, nil
}

// ---------------------------------------------------------------------------
// Decide
// ---------------------------------------------------------------------------

// DecideApprovalRequest is the atomic approve/reject.
//
// ATOMICITY -- the whole point of this method. One transaction contains, in order:
//
//  1. the decision idempotency claim,
//  2. SELECT ... FOR UPDATE of the request row (serializes concurrent deciders),
//  3. the side effect (goat create / goat exit / shifting AUTHORIZE -- authorization only, since a
//     shifting's animals move at completion, not here), applied through the identity module's
//     transaction-scoped seam,
//  4. the status flip to approved/rejected, stamped with the effect's result id.
//
// Any failure in 3 rolls back 1, 2 and 4 together. There is no window in which the request reads
// 'approved' while its effect did not save, and none in which the effect saved while the request
// still reads 'pending' -- which is what the atomic transition rule in AGENTS.md requires. The
// schema backs this up: counts_approval_requests_applied_result_check forbids an 'approved' row
// that does not name the result the transaction produced.
//
// IDEMPOTENCY. Two distinct replays are handled:
//
//   - Same decision idempotency key: the stored key/fingerprint pair is compared. An exact replay
//     returns the original decided row with replay=true and applies nothing again; a
//     same-key/different-payload replay is ErrIdempotencyConflict.
//   - Different key, request already decided the SAME way: returns the existing row with
//     replay=true. A second approve therefore cannot double-apply, which matters because the effect
//     (creating a kid, exiting an animal) is not something the caller may run twice. Deciding a
//     request the OTHER way after it is decided is ErrApprovalAlreadyDecided.
func (r *Repository) DecideApprovalRequest(ctx context.Context, in domain.ApprovalDecision) (domain.ApprovalRequest, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return domain.ApprovalRequest{}, false, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()

	// Lock the request first so two approvers racing on the same row serialize here rather than
	// both running the effect.
	current, err := scanApprovalRequest(tx.QueryRow(ctx, `
SELECT `+approvalRequestColumns+`
FROM counts_approval_requests
WHERE tenant_id = $1::uuid AND approval_request_id = $2::uuid
FOR UPDATE`, in.TenantID, in.ApprovalRequestID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ApprovalRequest{}, false, ports.ErrApprovalRequestNotFound
		}
		return domain.ApprovalRequest{}, false, fmt.Errorf("counts: lock approval request: %w", err)
	}

	if current.Status != domain.ApprovalStatusPending {
		// Already decided. Repeating the same decision is a no-op replay; flipping it is a conflict.
		if current.Status != in.Status {
			return domain.ApprovalRequest{}, false, ports.ErrApprovalAlreadyDecided
		}
		return current, true, nil
	}

	// Decision-level idempotency: a replay with the same key must not re-run the effect.
	if in.IdempotencyKey != "" {
		var (
			storedKey         *string
			storedFingerprint *string
		)
		if err := tx.QueryRow(ctx, `
SELECT decision_idempotency_key, decision_request_fingerprint
FROM counts_approval_requests
WHERE tenant_id = $1::uuid AND approval_request_id = $2::uuid`,
			in.TenantID, in.ApprovalRequestID).Scan(&storedKey, &storedFingerprint); err != nil {
			return domain.ApprovalRequest{}, false, fmt.Errorf("counts: read decision idempotency: %w", err)
		}
		if storedKey != nil && *storedKey == in.IdempotencyKey {
			if storedFingerprint == nil || *storedFingerprint != in.RequestFingerprint {
				return domain.ApprovalRequest{}, false, ports.ErrIdempotencyConflict
			}
			return current, true, nil
		}
	}

	var (
		resultType string
		resultID   string
	)
	if in.Status == domain.ApprovalStatusApproved {
		resultType, resultID, err = r.applyApprovalEffect(ctx, tx, current, in)
		if err != nil {
			return domain.ApprovalRequest{}, false, err
		}
	}

	// The status flip. `AND status = 'pending'` makes the transition itself the concurrency guard:
	// if anything decided this row since the lock, zero rows update and the whole transaction --
	// including the effect above -- rolls back.
	decided, err := scanApprovalRequest(tx.QueryRow(ctx, `
UPDATE counts_approval_requests
SET status = $3,
    decided_by_user_id = $4::uuid,
    decided_at = $5::timestamptz,
    decision_reason = nullif($6, ''),
    applied_result_type = nullif($7, ''),
    applied_result_id = nullif($8, '')::uuid,
    decision_idempotency_key = nullif($9, ''),
    decision_request_fingerprint = nullif($10, ''),
    updated_at = now(),
    row_version = row_version + 1
WHERE tenant_id = $1::uuid AND approval_request_id = $2::uuid AND status = 'pending'
RETURNING `+approvalRequestColumns,
		in.TenantID, in.ApprovalRequestID, in.Status,
		in.DecidedByUserID, in.DecidedAt.UTC(), in.Reason,
		resultType, resultID, in.IdempotencyKey, in.RequestFingerprint))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ApprovalRequest{}, false, ports.ErrApprovalAlreadyDecided
		}
		return domain.ApprovalRequest{}, false, fmt.Errorf("counts: decide approval request: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return domain.ApprovalRequest{}, false, err
	}
	committed = true
	return decided, false, nil
}

// applyApprovalEffect runs the approved request's side effect inside the decision transaction and
// returns the (result_type, result_id) pair stamped onto the approved row.
func (r *Repository) applyApprovalEffect(
	ctx context.Context, tx pgx.Tx, req domain.ApprovalRequest, in domain.ApprovalDecision,
) (string, string, error) {
	if in.Effect == nil {
		return "", "", fmt.Errorf("counts: approve %s request %s: no prepared effect supplied",
			req.RequestType, req.ApprovalRequestID)
	}
	if r.identityTx == nil {
		return "", "", fmt.Errorf("counts: approve %s request %s: identity write seam is not wired",
			req.RequestType, req.ApprovalRequestID)
	}

	switch req.RequestType {
	case domain.ApprovalRequestTypeBirth:
		cmd, ok := in.Effect.CreateGoat.(identityports.CreateAdminGoatCommand)
		if !ok {
			return "", "", fmt.Errorf("counts: approve birth request %s: effect is not a goat create command",
				req.ApprovalRequestID)
		}
		// Creates the kid AND emits goat.created in this transaction, so the kid's vaccination
		// obligations are generated if and only if the approval commits.
		result, err := r.identityTx.CreateAdminGoatInTx(ctx, tx, cmd)
		if err != nil {
			return "", "", err
		}
		return domain.ApprovalResultTypeGoat, result.Goat.GoatID, nil

	case domain.ApprovalRequestTypeDeath:
		cmd, ok := in.Effect.ExitGoat.(identityports.ExitGoatCommand)
		if !ok {
			return "", "", fmt.Errorf("counts: approve death request %s: effect is not a goat exit command",
				req.ApprovalRequestID)
		}
		// The dead+died guardrail is re-checked inside identity's adapter on this exact path; a
		// command that did not come from the guarded prepare step fails here rather than exiting an
		// animal. goat.exited is emitted in this transaction, so the animal's open obligations are
		// cancelled only on approval.
		result, err := r.identityTx.ExitGoatInTx(ctx, tx, cmd)
		if err != nil {
			return "", "", err
		}
		return domain.ApprovalResultTypeGoat, result.Goat.GoatID, nil

	case domain.ApprovalRequestTypeShifting:
		effect := in.Effect.Shifting
		if effect == nil {
			return "", "", fmt.Errorf("counts: approve shifting request %s: missing shifting effect",
				req.ApprovalRequestID)
		}
		// Approving a shifting AUTHORIZES it and MOVES NOTHING (maintainer decision, 2026-07-19).
		//
		// An approval is a manager's permission slip. It is not evidence that anyone walked the
		// animals to the destination shed, and writing their canonical location at approval time
		// asserted exactly that -- the herd register, the census, and every shed-scoped vaccination
		// obligation would name a shed the animals were not standing in, for however long the real
		// movement took (or forever, if it never happened).
		//
		// The relocation now runs at COMPLETION -- CompleteShiftingEvent, when an operator
		// confirms the animals physically moved -- atomically with the flip to event_status
		// 'applied'. The animal set travels no further than this row: it stays in the approval
		// request's stored payload and is re-read there by the completion path.
		//
		// The empty-set check stays here even though nothing moves: goat_ids is required at submit
		// and re-checked when the stored payload is decoded, so an effect that reaches this point
		// naming nobody was built by something that bypassed both. Authorizing it would queue a
		// movement that completion can never execute, so it fails closed at the earlier gate.
		if len(effect.GoatIDs) == 0 {
			return "", "", fmt.Errorf("%w: shifting request %s named no animals to move",
				ports.ErrApprovalEffectIncomplete, req.ApprovalRequestID)
		}
		if err := r.authorizeShiftingEventInTx(ctx, tx, req.TenantID, effect.ShiftingEventID, in); err != nil {
			return "", "", err
		}
		return domain.ApprovalResultTypeShiftingEvent, effect.ShiftingEventID, nil

	default:
		return "", "", fmt.Errorf("counts: approve request %s: unknown request type %q",
			req.ApprovalRequestID, req.RequestType)
	}
}

// authorizeShiftingEventInTx flips the pending shifting_events row to authorized inside the
// decision transaction. `AND authorization_state = 'pending'` keeps the transition idempotent and
// prevents re-authorizing an already-decided movement.
func (r *Repository) authorizeShiftingEventInTx(
	ctx context.Context, tx pgx.Tx, tenantID, shiftingEventID string, in domain.ApprovalDecision,
) error {
	tag, err := tx.Exec(ctx, `
UPDATE shifting_events
SET authorization_state = 'authorized',
    event_status = 'authorized',
    authorized_at = $3::timestamptz,
    authorized_by = $4::uuid,
    updated_at = now(),
    row_version = row_version + 1
WHERE tenant_id = $1::uuid AND shifting_event_id = $2::uuid AND authorization_state = 'pending'`,
		tenantID, shiftingEventID, in.DecidedAt.UTC(), in.DecidedByUserID)
	if err != nil {
		return fmt.Errorf("counts: authorize shifting event: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("%w: shifting event %s was not pending", ports.ErrApprovalAlreadyDecided, shiftingEventID)
	}
	return nil
}

func stringOrEmpty(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}
