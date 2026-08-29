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
	platformoutbox "github.com/vgoats/goatos/backend/internal/platform/outbox"
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
	// ConfigureAdoptedShedCohortInTx tags a destination pen with an arriving typed-shifting
	// group's tag ("pen tags follow occupancy", 2026-08-20 rewrite). Counts never writes
	// shed_partitions/shed_profiles itself; the adoption goes through this seam inside the same
	// apply transaction as the relocation.
	ConfigureAdoptedShedCohortInTx(ctx context.Context, tx pgx.Tx, cmd identityports.ConfigureAdoptedShedCohortCommand) error
}

// DeathEvidenceTxGate is implemented by the tasks Postgres repository. The consuming adapter owns
// this seam so Counts does not import tasks storage. It validates both staged videos and moves the
// workflow's internal verifier action to in_review inside the approval transaction.
type DeathEvidenceTxGate interface {
	PrepareDeathEvidenceForApprovalInTx(ctx context.Context, tx pgx.Tx, tenantID, goatID string) (ready bool, err error)
}

// WithIdentityTxWriter injects the identity write seam used to apply approved birth/death/shifting
// effects. A repository without it can still submit and list requests; approving one returns a
// clear error rather than silently skipping the effect.
func (r *Repository) WithIdentityTxWriter(w IdentityTxWriter) *Repository {
	r.identityTx = w
	return r
}

func (r *Repository) WithDeathEvidenceTxGate(g DeathEvidenceTxGate) *Repository {
	r.deathEvidenceTx = g
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
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.ApprovalRequest{}, false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

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

	row := tx.QueryRow(ctx, `
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
		if created.RequestType == domain.ApprovalRequestTypeDeath {
			if err := insertDeathApprovalOutbox(ctx, tx, domain.EventDeathReported, created, ""); err != nil {
				return domain.ApprovalRequest{}, false, err
			}
		}
		if err := tx.Commit(ctx); err != nil {
			return domain.ApprovalRequest{}, false, err
		}
		return created, false, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return domain.ApprovalRequest{}, false, fmt.Errorf("counts: insert approval request: %w", err)
	}

	// The key already existed. Return the original row iff the payload fingerprint matches.
	existing, err := scanApprovalRequest(tx.QueryRow(ctx, `
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
	if err := tx.Commit(ctx); err != nil {
		return domain.ApprovalRequest{}, false, err
	}
	return existing, true, nil
}

// CreateBirthApprovalRequest writes one litter atomically: one count-approval aggregate plus one
// canonical goat per child. The goat_births rows are count-pending, so their insert triggers remove
// them from the herd projection before commit while goat.created remains available to Tasks.
func (r *Repository) CreateBirthApprovalRequest(
	ctx context.Context,
	in domain.ApprovalRequestSubmission,
	children []identityports.CreateAdminGoatCommand,
) (domain.BirthSubmissionResult, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	if r.identityTx == nil {
		return domain.BirthSubmissionResult{}, fmt.Errorf("counts: submit birth: identity write seam is not wired")
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.BirthSubmissionResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	payload := in.Payload
	if len(payload) == 0 {
		payload = json.RawMessage(`{}`)
	}
	raisedAt := in.RaisedAt
	if raisedAt.IsZero() {
		raisedAt = time.Now().In(biztime.DefaultLocation())
	}
	created, err := scanApprovalRequest(tx.QueryRow(ctx, `
INSERT INTO counts_approval_requests (
  tenant_id, request_type, payload, status, raised_by_user_id, raised_at,
  idempotency_key, request_fingerprint
) VALUES ($1::uuid, 'birth', $2::jsonb, 'pending', $3::uuid, $4::timestamptz, $5, $6)
ON CONFLICT (tenant_id, idempotency_key) DO NOTHING
RETURNING `+approvalRequestColumns,
		in.TenantID, string(payload), in.RaisedByUserID, raisedAt.UTC(),
		in.IdempotencyKey, in.RequestFingerprint))
	if errors.Is(err, pgx.ErrNoRows) {
		existing, readErr := scanApprovalRequest(tx.QueryRow(ctx, `
SELECT `+approvalRequestColumns+`
FROM counts_approval_requests
WHERE tenant_id = $1::uuid AND idempotency_key = $2`, in.TenantID, in.IdempotencyKey))
		if readErr != nil {
			if errors.Is(readErr, pgx.ErrNoRows) {
				return domain.BirthSubmissionResult{}, ports.ErrIdempotencyInProgress
			}
			return domain.BirthSubmissionResult{}, readErr
		}
		if existing.RequestFingerprint != in.RequestFingerprint {
			return domain.BirthSubmissionResult{}, ports.ErrIdempotencyConflict
		}
		items, readErr := birthChildren(ctx, tx, existing.TenantID, existing.ApprovalRequestID)
		if readErr != nil {
			return domain.BirthSubmissionResult{}, readErr
		}
		if err := tx.Commit(ctx); err != nil {
			return domain.BirthSubmissionResult{}, err
		}
		return domain.BirthSubmissionResult{Approval: existing, Children: items, Replayed: true}, nil
	}
	if err != nil {
		return domain.BirthSubmissionResult{}, fmt.Errorf("counts: insert birth approval request: %w", err)
	}

	items := make([]domain.BirthChildResult, 0, len(children))
	for i := range children {
		cmd := children[i]
		cmd.BirthEventID = created.ApprovalRequestID
		cmd.BirthChildOrdinal = i + 1
		cmd.BirthCountStatus = domain.ApprovalStatusPending
		result, createErr := r.identityTx.CreateAdminGoatInTx(ctx, tx, cmd)
		if createErr != nil {
			return domain.BirthSubmissionResult{}, createErr
		}
		temp := ""
		for _, identifier := range cmd.Identifiers {
			if identifier.IdentifierType == "temporary_tag" {
				temp = identifier.IdentifierValue
				break
			}
		}
		items = append(items, domain.BirthChildResult{
			GoatID: result.Goat.GoatID, TemporaryIdentifier: temp, ChildOrdinal: i + 1,
		})
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.BirthSubmissionResult{}, err
	}
	return domain.BirthSubmissionResult{Approval: created, Children: items}, nil
}

func birthChildren(ctx context.Context, tx pgx.Tx, tenantID, birthEventID string) ([]domain.BirthChildResult, error) {
	rows, err := tx.Query(ctx, `
SELECT gb.child_goat_id::text, gb.child_ordinal, COALESCE(gi.identifier_value, '')
FROM goat_births gb
LEFT JOIN goat_identifiers gi
  ON gi.tenant_id = gb.tenant_id
 AND gi.goat_id = gb.child_goat_id
 AND gi.identifier_type = 'temporary_tag'
 AND gi.status = 'active'
WHERE gb.tenant_id = $1::uuid AND gb.birth_event_id = $2::uuid
ORDER BY gb.child_ordinal`, tenantID, birthEventID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]domain.BirthChildResult, 0, 3)
	for rows.Next() {
		var item domain.BirthChildResult
		if err := rows.Scan(&item.GoatID, &item.ChildOrdinal, &item.TemporaryIdentifier); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// insertDeathApprovalOutbox keeps submission/rejection and their workflow commands atomic. The
// event id and idempotency key are deterministic per approval request + transition, so a replay
// can never open or cancel duplicate work.
func insertDeathApprovalOutbox(
	ctx context.Context, tx pgx.Tx, eventType string, req domain.ApprovalRequest, reason string,
) error {
	if req.SubjectGoatID == nil || *req.SubjectGoatID == "" {
		return fmt.Errorf("counts: %s request %s has no subject goat", eventType, req.ApprovalRequestID)
	}
	idempotencyKey := eventType + ":" + req.ApprovalRequestID
	eventID := platformoutbox.DeterministicUUID(idempotencyKey)
	occurredAt := req.RaisedAt.UTC()
	if eventType == domain.EventDeathRejected {
		occurredAt = time.Now().UTC()
	}
	payload, err := json.Marshal(map[string]any{
		"event_id":         eventID,
		"event_type":       eventType,
		"schema_version":   countsEventSchemaVersion,
		"schema_ref":       countsEventSchemaRef,
		"aggregate_type":   "counts_approval_request",
		"aggregate_id":     req.ApprovalRequestID,
		"occurred_at":      occurredAt.Format(time.RFC3339Nano),
		"recorded_at":      time.Now().UTC().Format(time.RFC3339Nano),
		"producer":         map[string]any{"service": "goatos-api", "module": "counts", "version": nil},
		"idempotency_key":  idempotencyKey,
		"actor":            map[string]any{"actor_type": "system_rule", "actor_id": nil, "actor_ref": nil},
		"subject_type":     "goat",
		"subject_id":       *req.SubjectGoatID,
		"visibility_scope": map[string]any{"tenant_id": req.TenantID},
		"evidence_refs":    []map[string]string{{"evidence_type": "decision", "evidence_id": req.ApprovalRequestID}},
		"payload": map[string]any{
			"approval_request_id": req.ApprovalRequestID,
			"goat_id":             *req.SubjectGoatID,
			"reason":              reason,
		},
		"trace_id": idempotencyKey,
	})
	if err != nil {
		return fmt.Errorf("counts: marshal %s envelope: %w", eventType, err)
	}
	headers, err := json.Marshal(map[string]any{
		"producer": "counts.ApprovalService", "schema_version": countsEventSchemaVersion,
		"idempotency_key": idempotencyKey, "event_type": eventType,
	})
	if err != nil {
		return fmt.Errorf("counts: marshal %s headers: %w", eventType, err)
	}
	_, err = tx.Exec(ctx, `
INSERT INTO outbox_messages (
  tenant_id, event_id, event_type, schema_version, aggregate_type, aggregate_id,
  topic, payload, headers, idempotency_key, trace_id, status, next_attempt_at
) VALUES (
  $1::uuid, $2::uuid, $3, $4, 'counts_approval_request', $5::uuid,
  $6, $7::jsonb, $8::jsonb, $9, $9, 'pending', now()
)
ON CONFLICT DO NOTHING`, req.TenantID, eventID, eventType, countsEventSchemaVersion,
		req.ApprovalRequestID, countsEventTopic, payload, headers, idempotencyKey)
	if err != nil {
		return fmt.Errorf("counts: insert %s outbox: %w", eventType, err)
	}
	return nil
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

// ApprovalSubjectPark is the indexed, tenant-scoped authority lookup used for death decisions by
// a park-scoped manager. No payload/free-text value is trusted for scope.
func (r *Repository) ApprovalSubjectPark(ctx context.Context, tenantID, goatID string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	var parkID string
	err := r.pool.QueryRow(ctx, `
SELECT park_id::text
FROM goats
WHERE tenant_id = $1::uuid AND goat_id = $2::uuid`, tenantID, goatID).Scan(&parkID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ports.ErrGoatNotFound
	}
	return parkID, err
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
// permissions on the approver queue.
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
  -- P0-2 scope filter: a park-scoped caller ($7 non-empty) sees only requests in their park. The
  -- park lives in the shifting payload (destination_park_id, == source park by P0-1); birth/death
  -- carry no park and are decidable only by no-scope (CEO) callers, so they correctly drop out for
  -- a scoped caller. An empty $7 (no scope) keeps every row.
  AND ($7::text = '' OR (payload->>'destination_park_id') = $7::text)
ORDER BY raised_at DESC, approval_request_id DESC
LIMIT $6`, q.TenantID, q.Status, q.RequestTypes, cursorRaisedAt, cursorID, pageSize+1, q.CallerParkID)
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

	// CR-06: the same-key fingerprint check must run BEFORE the terminal-status early return.
	//
	// Decision-level idempotency: a replay with the same key must not re-run the effect, and a replay
	// that REUSES the same key with a DIFFERENT payload (e.g. an altered reject reason) must be a
	// conflict -- not a silent success. If this ran only after the terminal-status branch below, an
	// already-decided request retried with the same idempotency key but a changed reason would fall
	// into the "same decision" replay path (current.Status == in.Status) and return the original row
	// with replay=true, quietly accepting a payload it never applied. Checking key+fingerprint first
	// makes a same-key/different-payload retry conflict whether the request is pending OR decided.
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

	if current.Status != domain.ApprovalStatusPending {
		// Already decided under a DIFFERENT key (the same-key case was handled above). Repeating the
		// same decision is a no-op replay; flipping it is a conflict.
		if current.Status != in.Status {
			return domain.ApprovalRequest{}, false, ports.ErrApprovalAlreadyDecided
		}
		return current, true, nil
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
	} else if current.RequestType == domain.ApprovalRequestTypeBirth {
		if _, err := tx.Exec(ctx, `
UPDATE goat_births
SET count_status = 'rejected', count_approved_at = NULL, count_approved_by = NULL
WHERE tenant_id = $1::uuid AND birth_event_id = $2::uuid AND count_status = 'pending'`,
			current.TenantID, current.ApprovalRequestID); err != nil {
			return domain.ApprovalRequest{}, false, fmt.Errorf("counts: reject birth count eligibility: %w", err)
		}
	} else if current.RequestType == domain.ApprovalRequestTypeShifting {
		if err := r.rejectShiftingEventInTx(ctx, tx, current); err != nil {
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
	if current.RequestType == domain.ApprovalRequestTypeDeath && in.Status == domain.ApprovalStatusRejected {
		if err := insertDeathApprovalOutbox(ctx, tx, domain.EventDeathRejected, current, in.Reason); err != nil {
			return domain.ApprovalRequest{}, false, err
		}
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
	switch req.RequestType {
	case domain.ApprovalRequestTypeBirth:
		effect := in.Effect.BirthCounts
		if effect == nil || effect.BirthEventID != req.ApprovalRequestID {
			return "", "", fmt.Errorf("counts: approve birth request %s: invalid birth-count effect",
				req.ApprovalRequestID)
		}
		rows, err := tx.Query(ctx, `
SELECT child_goat_id::text, litter_size, count_status
FROM goat_births
WHERE tenant_id = $1::uuid AND birth_event_id = $2::uuid
ORDER BY child_ordinal
FOR UPDATE`, req.TenantID, effect.BirthEventID)
		if err != nil {
			return "", "", err
		}
		childCount, litterSize := 0, 0
		for rows.Next() {
			var childID, status string
			var size int
			if err := rows.Scan(&childID, &size, &status); err != nil {
				rows.Close()
				return "", "", err
			}
			if status != domain.ApprovalStatusPending || (litterSize != 0 && litterSize != size) {
				rows.Close()
				return "", "", ports.ErrApprovalEffectIncomplete
			}
			litterSize = size
			childCount++
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return "", "", err
		}
		if childCount == 0 || childCount != litterSize {
			return "", "", ports.ErrApprovalEffectIncomplete
		}
		if _, err := tx.Exec(ctx, `
UPDATE goat_births
SET count_status = 'approved', count_approved_at = $3::timestamptz, count_approved_by = $4::uuid
WHERE tenant_id = $1::uuid AND birth_event_id = $2::uuid AND count_status = 'pending'`,
			req.TenantID, effect.BirthEventID, in.DecidedAt.UTC(), in.DecidedByUserID); err != nil {
			return "", "", err
		}
		return domain.ApprovalResultTypeBirthEvent, effect.BirthEventID, nil

	case domain.ApprovalRequestTypeDeath:
		if r.identityTx == nil {
			return "", "", fmt.Errorf("counts: approve death request %s: identity write seam is not wired", req.ApprovalRequestID)
		}
		cmd, ok := in.Effect.ExitGoat.(identityports.ExitGoatCommand)
		if !ok {
			return "", "", fmt.Errorf("counts: approve death request %s: effect is not a goat exit command",
				req.ApprovalRequestID)
		}
		if r.deathEvidenceTx == nil {
			return "", "", fmt.Errorf("counts: approve death request %s: death evidence gate is not wired",
				req.ApprovalRequestID)
		}
		ready, err := r.deathEvidenceTx.PrepareDeathEvidenceForApprovalInTx(ctx, tx, req.TenantID, cmd.GoatID)
		if err != nil {
			return "", "", err
		}
		if !ready {
			return "", "", ports.ErrDeathEvidenceIncomplete
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
		if r.identityTx == nil {
			return "", "", fmt.Errorf("counts: approve shifting request %s: identity write seam is not wired", req.ApprovalRequestID)
		}
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
    event_status = CASE WHEN event_status = 'pending' THEN 'authorized' ELSE event_status END,
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
	current, err := lockShiftingEvent(ctx, tx, tenantID, shiftingEventID)
	if err != nil {
		return err
	}
	if current.CompletedAt != nil &&
		(current.EventStatus == domain.ShiftingEventStatusAuthorized || current.EventStatus == domain.ShiftingEventStatusPendingVerification) {
		var approvedGoatIDs []string
		if in.Effect != nil && in.Effect.Shifting != nil {
			approvedGoatIDs = in.Effect.Shifting.GoatIDs
		}
		if _, err := r.applyAuthorizedCompletedShiftingInTx(ctx, tx, tenantID, shiftingEventID,
			in.DecidedAt.UTC(), in.IdempotencyKey, approvedGoatIDs); err != nil {
			return err
		}
	}
	return nil
}

// rejectShiftingEventInTx flips the pending shifting_events row to rejected inside the decision
// transaction, so the raiser's read-only Pending tab stops listing a movement its approver refused
// and the raised-counts feed projection stops feeding the destination for it (rejection is the one
// thing that stops the feed clock). The event_status CASE keeps a legacy completed-before-approval
// row on its current status: only a still-pending movement becomes 'rejected'.
func (r *Repository) rejectShiftingEventInTx(
	ctx context.Context, tx pgx.Tx, req domain.ApprovalRequest,
) error {
	if req.ShiftingEventID == nil || *req.ShiftingEventID == "" {
		return fmt.Errorf("counts: reject shifting request %s: request names no shifting event",
			req.ApprovalRequestID)
	}
	tag, err := tx.Exec(ctx, `
UPDATE shifting_events
SET authorization_state = 'rejected',
    event_status = CASE WHEN event_status = 'pending' THEN 'rejected' ELSE event_status END,
    updated_at = now(),
    row_version = row_version + 1
WHERE tenant_id = $1::uuid AND shifting_event_id = $2::uuid AND authorization_state = 'pending'`,
		req.TenantID, *req.ShiftingEventID)
	if err != nil {
		return fmt.Errorf("counts: reject shifting event: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("%w: shifting event %s was not pending", ports.ErrApprovalAlreadyDecided, *req.ShiftingEventID)
	}
	return nil
}

func stringOrEmpty(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}
