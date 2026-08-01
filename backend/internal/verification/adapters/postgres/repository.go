// Package postgres implements the Verification module's persistence, including the outbox insert
// for the status-event seam (item pending / verdict approved / verdict rework), written in the SAME
// transaction as the state change (backend/AGENTS.md atomic transition + read-model/event rule).
package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	platformoutbox "github.com/vgoats/goatos/backend/internal/platform/outbox"
	"github.com/vgoats/goatos/backend/internal/verification/domain"
	"github.com/vgoats/goatos/backend/internal/verification/ports"
)

const defaultQueryTimeout = 3 * time.Second

// Status-event types emitted on the existing outbox bus (build-handover-20260713.md §1 P0 1a — "the
// Max seam"). The notification producer session consumes these to fan out pushes; this module only
// publishes.
const (
	EventItemPending                  = "verification.item.pending"
	EventVerdictApproved              = "verification.verdict.approved"
	EventVerdictRework                = "verification.verdict.rework"
	EventItemClosed                   = "verification.item.closed"
	EventVaccinationDriveReady        = "verification.vaccination_drive.ready"
	EventVaccinationDriveClosed       = "verification.vaccination_drive.closed"
	verificationTopic                 = "verification"
	vaccinationCompletedEventType     = "vaccination.completed"
	vaccinationCompletedSchemaVersion = "1.0.0"
	vaccinationCompletedSchemaRef     = "domain-event-envelope.v1"
	vaccinationCompletedTopic         = "vaccination.events"
	// verificationSchemaVer must be semver to satisfy the domain-event-envelope schema
	// (schema_version pattern ^[0-9]+\.[0-9]+\.[0-9]+$) enforced by the outbox relay's
	// EnvelopeValidator before publish. A non-semver value (was "1") fails validation, so the
	// event is marked invalid_event_envelope and NEVER delivered to any consumer.
	verificationSchemaVer = "1.0.0"
	// verificationSchemaRef is the required schema_ref pointer for the envelope. Mirrors the
	// notification.exhausted producer's "<schema file>#<event_type>" convention.
	verificationSchemaRef = "contracts/jsonschema/domain-event-envelope.schema.json"
)

type Repository struct {
	pool    *pgxpool.Pool
	timeout time.Duration
}

func NewRepository(pool *pgxpool.Pool, queryTimeout time.Duration) *Repository {
	if queryTimeout <= 0 {
		queryTimeout = defaultQueryTimeout
	}
	return &Repository{pool: pool, timeout: queryTimeout}
}

var _ ports.Repository = (*Repository)(nil)

const itemColumns = `item_id::text, tenant_id::text, vertical, module, category, source_module,
  source_task_id::text, source_submission_id::text, source_ref_type, source_ref_id::text, subject_label, media_refs,
  status, verdict_reason, operator_id::text, shed_id::text, park_id::text, captured_at, verified_by::text,
  verified_at, closed_by::text, closed_at, row_version, created_at, updated_at`

const itemColumnsWithLabels = `vi.item_id::text, vi.tenant_id::text, vi.vertical, vi.module, vi.category, vi.source_module,
  vi.source_task_id::text, vi.source_submission_id::text, vi.source_ref_type, vi.source_ref_id::text, vi.subject_label, vi.media_refs,
  vi.status, vi.verdict_reason, vi.operator_id::text, vi.shed_id::text, vi.park_id::text, vi.captured_at, vi.verified_by::text,
  vi.verified_at, vi.closed_by::text, vi.closed_at, vi.row_version, vi.created_at, vi.updated_at,
  COALESCE(wm_member.display_name, wm_user.display_name)::text, shed_loc.name::text, park_loc.name::text`

func (r *Repository) CreateItem(ctx context.Context, in domain.CreateItem) (domain.CreateItemResult, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	mediaJSON, err := json.Marshal(nonNilStrings(in.MediaRefs))
	if err != nil {
		return domain.CreateItemResult{}, fmt.Errorf("verification: marshal media_refs: %w", err)
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.CreateItemResult{}, err
	}
	defer rollback(ctx, tx)

	var itemID string
	err = tx.QueryRow(ctx, `
INSERT INTO verification_items (
  tenant_id, vertical, module, category, source_module, source_task_id, source_submission_id,
  source_ref_type, source_ref_id, subject_label, media_refs, status, operator_id, shed_id, park_id,
  captured_at, idempotency_key
) VALUES (
  $1::uuid, $2, $3, $4, $5, nullif($6, '')::uuid, nullif($7, '')::uuid, $8, $9::uuid, nullif($10, ''),
  $11::jsonb, 'pending', nullif($12, '')::uuid, nullif($13, '')::uuid, nullif($14, '')::uuid, $15, $16
)
ON CONFLICT (tenant_id, idempotency_key) DO NOTHING
RETURNING item_id::text`,
		in.TenantID, in.Vertical, in.Module, in.Category, in.Source.Module,
		derefStr(in.Source.TaskID), derefStr(in.Source.SubmissionID), in.Source.RefType, in.Source.RefID,
		derefStr(in.SubjectLabel), string(mediaJSON), derefStr(in.OperatorID), derefStr(in.ShedID), derefStr(in.ParkID),
		in.CapturedAt.UTC(), in.IdempotencyKey,
	).Scan(&itemID)
	created := true
	if errors.Is(err, pgx.ErrNoRows) {
		created = false
	} else if err != nil {
		return domain.CreateItemResult{}, mapWriteErr(err)
	}

	if created {
		if err := insertOutboxEvent(ctx, tx, in.TenantID, EventItemPending, itemID,
			"verification.item.pending:"+itemID, verificationItemPendingPayload(itemID, in)); err != nil {
			return domain.CreateItemResult{}, err
		}
	}

	var item domain.Item
	if created {
		item, err = scanItemRow(tx.QueryRow(ctx, "SELECT "+itemColumns+" FROM verification_items WHERE tenant_id = $1::uuid AND item_id = $2::uuid", in.TenantID, itemID))
	} else {
		item, err = scanItemRow(tx.QueryRow(ctx, "SELECT "+itemColumns+" FROM verification_items WHERE tenant_id = $1::uuid AND idempotency_key = $2", in.TenantID, in.IdempotencyKey))
	}
	if err != nil {
		return domain.CreateItemResult{}, mapWriteErr(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.CreateItemResult{}, err
	}
	return domain.CreateItemResult{Item: item, Created: created}, nil
}

func (r *Repository) GetItem(ctx context.Context, tenantID, itemID string) (domain.Item, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	item, err := scanItemRow(r.pool.QueryRow(ctx, "SELECT "+itemColumns+" FROM verification_items WHERE tenant_id = $1::uuid AND item_id = $2::uuid", tenantID, itemID))
	if err != nil {
		return domain.Item{}, mapWriteErr(err)
	}
	return item, nil
}

func (r *Repository) GetSubmissionItems(ctx context.Context, tenantID, submissionID string) ([]domain.Item, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	rows, err := r.pool.Query(ctx, `
SELECT `+itemColumns+`
FROM verification_items
WHERE tenant_id = $1::uuid
  AND source_submission_id = $2::uuid
ORDER BY captured_at, item_id`, tenantID, submissionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]domain.Item, 0, 20)
	for rows.Next() {
		item, scanErr := scanItemRow(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, ports.ErrNotFound
	}
	return items, nil
}

func (r *Repository) ListQueue(ctx context.Context, params ports.ListQueueParams) ([]domain.Item, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	var cursorCapturedAt any
	var cursorItemID any
	if params.Cursor != nil {
		cursorCapturedAt = params.Cursor.CapturedAt
		cursorItemID = params.Cursor.ItemID
	}
	rows, err := r.pool.Query(ctx, `
SELECT `+itemColumnsWithLabels+`
FROM verification_items vi
LEFT JOIN workforce_members wm_member
  ON vi.tenant_id = wm_member.tenant_id AND vi.operator_id = wm_member.workforce_member_id
LEFT JOIN workforce_members wm_user
  ON vi.tenant_id = wm_user.tenant_id AND vi.operator_id = wm_user.user_id
LEFT JOIN locations shed_loc ON vi.tenant_id = shed_loc.tenant_id AND vi.shed_id = shed_loc.location_id
LEFT JOIN locations park_loc ON vi.tenant_id = park_loc.tenant_id AND vi.park_id = park_loc.location_id
WHERE vi.tenant_id = $1::uuid
  AND ($2 = '' OR vi.status = $2)
  AND ($3 = '' OR vi.category = $3)
  AND ($4 = '' OR vi.vertical = $4)
  AND ($5 = '' OR vi.module = $5)
  AND (NOT $6::boolean OR vi.park_id = ANY($7::uuid[]))
  AND ($14 = '' OR vi.park_id = $14::uuid)
  AND ($15 = '' OR vi.shed_id = $15::uuid)
  AND ($8::timestamptz IS NULL OR (vi.captured_at, vi.item_id) > ($8::timestamptz, $9::uuid))
  AND (
    NOT $10::boolean
    OR (
      vi.source_submission_id IS NOT NULL
      AND vi.closed_at IS NULL
      AND NOT EXISTS (
        SELECT 1
        FROM verification_items sibling
        WHERE sibling.tenant_id = vi.tenant_id
          AND sibling.source_submission_id = vi.source_submission_id
          AND (sibling.status <> 'approved' OR sibling.closed_at IS NOT NULL)
      )
    )
  )
  AND (NOT $12::boolean OR vi.source_submission_id IS NOT NULL)
  AND (NOT $13::boolean OR vi.closed_at IS NULL)
ORDER BY vi.captured_at ASC, vi.item_id ASC
LIMIT $11`,
		params.TenantID, params.Status, params.Category, params.Vertical, params.Module,
		params.ScopeRestricted, params.ParkIDs, cursorCapturedAt, cursorItemID,
		params.ReadyForClosure, params.Limit, params.SubmissionScopedOnly, params.OpenOnly,
		params.ParkID, params.ShedID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]domain.Item, 0, params.Limit)
	for rows.Next() {
		item, err := scanItemWithLabels(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) ListQueueFilterOptions(ctx context.Context, params ports.ListQueueParams) (domain.QueueFilterOptions, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	options := domain.QueueFilterOptions{}
	parkRows, err := r.pool.Query(ctx, `
SELECT vi.park_id::text, COALESCE(park_loc.name, vi.park_id::text) AS label
FROM verification_items vi
LEFT JOIN locations park_loc ON vi.tenant_id = park_loc.tenant_id AND vi.park_id = park_loc.location_id
WHERE vi.tenant_id = $1::uuid
  AND vi.park_id IS NOT NULL
  AND ($2 = '' OR vi.status = $2)
  AND ($3 = '' OR vi.category = $3)
  AND ($4 = '' OR vi.vertical = $4)
  AND ($5 = '' OR vi.module = $5)
  AND (NOT $6::boolean OR vi.park_id = ANY($7::uuid[]))
  AND (NOT $8::boolean OR vi.source_submission_id IS NOT NULL)
  AND (NOT $9::boolean OR vi.closed_at IS NULL)
GROUP BY vi.park_id, park_loc.name
ORDER BY label, vi.park_id::text`,
		params.TenantID, params.Status, params.Category, params.Vertical, params.Module,
		params.ScopeRestricted, params.ParkIDs, params.SubmissionScopedOnly, params.OpenOnly,
	)
	if err != nil {
		return options, err
	}
	defer parkRows.Close()
	for parkRows.Next() {
		var id, label string
		if err := parkRows.Scan(&id, &label); err != nil {
			return options, err
		}
		options.Parks = append(options.Parks, domain.LocationFilterOption{ID: id, Label: label})
	}
	if err := parkRows.Err(); err != nil {
		return options, err
	}
	shedRows, err := r.pool.Query(ctx, `
SELECT vi.shed_id::text, COALESCE(shed_loc.name, vi.shed_id::text) AS label
FROM verification_items vi
LEFT JOIN locations shed_loc ON vi.tenant_id = shed_loc.tenant_id AND vi.shed_id = shed_loc.location_id
WHERE vi.tenant_id = $1::uuid
  AND vi.shed_id IS NOT NULL
  AND ($2 = '' OR vi.status = $2)
  AND ($3 = '' OR vi.category = $3)
  AND ($4 = '' OR vi.vertical = $4)
  AND ($5 = '' OR vi.module = $5)
  AND (NOT $6::boolean OR vi.park_id = ANY($7::uuid[]))
  AND ($8 = '' OR vi.park_id = $8::uuid)
  AND (NOT $9::boolean OR vi.source_submission_id IS NOT NULL)
  AND (NOT $10::boolean OR vi.closed_at IS NULL)
GROUP BY vi.shed_id, shed_loc.name
ORDER BY label, vi.shed_id::text`,
		params.TenantID, params.Status, params.Category, params.Vertical, params.Module,
		params.ScopeRestricted, params.ParkIDs, params.ParkID, params.SubmissionScopedOnly, params.OpenOnly,
	)
	if err != nil {
		return options, err
	}
	defer shedRows.Close()
	for shedRows.Next() {
		var id, label string
		if err := shedRows.Scan(&id, &label); err != nil {
			return options, err
		}
		options.Sheds = append(options.Sheds, domain.LocationFilterOption{ID: id, Label: label})
	}
	if err := shedRows.Err(); err != nil {
		return options, err
	}
	return options, nil
}

func (r *Repository) CloseItem(ctx context.Context, in domain.CloseAction) (domain.Item, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.Item{}, err
	}
	defer rollback(ctx, tx)
	reservation, err := reserveIdempotency(ctx, tx, in.TenantID, "verification.close-item", in.IdempotencyKey,
		requestFingerprint(in.ItemID, in.ActorID, fmt.Sprintf("%d", in.RowVersion)))
	if err != nil {
		return domain.Item{}, err
	}
	if !reservation.proceed {
		item, err := scanItemRow(tx.QueryRow(ctx, "SELECT "+itemColumns+" FROM verification_items WHERE tenant_id = $1::uuid AND item_id = $2::uuid", in.TenantID, reservation.resultID))
		if err != nil {
			return domain.Item{}, mapWriteErr(err)
		}
		if err := tx.Commit(ctx); err != nil {
			return domain.Item{}, err
		}
		return item, nil
	}
	tag, err := tx.Exec(ctx, `
UPDATE verification_items
SET closed_by = $1::uuid, closed_at = now(), row_version = row_version + 1
WHERE tenant_id = $2::uuid
  AND item_id = $3::uuid
  AND row_version = $4
  AND status = 'approved'
  AND closed_at IS NULL
  AND source_submission_id IS NULL`,
		in.ActorID, in.TenantID, in.ItemID, in.RowVersion)
	if err != nil {
		return domain.Item{}, mapWriteErr(err)
	}
	if tag.RowsAffected() == 0 {
		var exists bool
		if scanErr := tx.QueryRow(ctx, `
SELECT true
FROM verification_items
WHERE tenant_id = $1::uuid
  AND item_id = $2::uuid`, in.TenantID, in.ItemID).Scan(&exists); scanErr != nil {
			if errors.Is(scanErr, pgx.ErrNoRows) {
				return domain.Item{}, ports.ErrNotFound
			}
			return domain.Item{}, scanErr
		}
		return domain.Item{}, ports.ErrConflict
	}
	item, err := scanItemRow(tx.QueryRow(ctx,
		"SELECT "+itemColumns+" FROM verification_items WHERE tenant_id = $1::uuid AND item_id = $2::uuid",
		in.TenantID, in.ItemID))
	if err != nil {
		return domain.Item{}, mapWriteErr(err)
	}
	idempotencyKey := fmt.Sprintf("%s:%s:%d", EventItemClosed, in.ItemID, item.RowVersion)
	if err := insertOutboxEvent(ctx, tx, in.TenantID, EventItemClosed, in.ItemID, idempotencyKey, verificationVerdictPayload(item)); err != nil {
		return domain.Item{}, err
	}
	if err := completeIdempotency(ctx, tx, in.TenantID, "verification.close-item", in.IdempotencyKey, "verification_item", item.ItemID); err != nil {
		return domain.Item{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Item{}, err
	}
	return item, nil
}

func (r *Repository) CloseSubmission(ctx context.Context, in domain.CloseSubmissionAction) ([]domain.Item, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer rollback(ctx, tx)
	reservation, err := reserveIdempotency(ctx, tx, in.TenantID, "verification.close-submission", in.IdempotencyKey,
		requestFingerprint(in.SubmissionID, in.ActorID))
	if err != nil {
		return nil, err
	}

	rows, err := tx.Query(ctx, `
SELECT `+itemColumns+`
FROM verification_items
WHERE tenant_id = $1::uuid
  AND source_submission_id = $2::uuid
ORDER BY captured_at, item_id
FOR UPDATE`, in.TenantID, in.SubmissionID)
	if err != nil {
		return nil, err
	}
	items := make([]domain.Item, 0, 20)
	for rows.Next() {
		item, scanErr := scanItemRow(rows)
		if scanErr != nil {
			rows.Close()
			return nil, scanErr
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	if len(items) == 0 {
		return nil, ports.ErrNotFound
	}
	if !reservation.proceed {
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		return items, nil
	}

	allClosed := true
	for _, item := range items {
		if item.Status != domain.StatusApproved {
			return nil, ports.ErrConflict
		}
		if item.ClosedAt == nil {
			allClosed = false
		}
	}
	acceptVaccinationCompletions := func() error {
		if _, err := tx.Exec(ctx, `
UPDATE vaccination_completions vc
SET status = 'accepted',
    verified_by = $1::uuid,
    verified_at = now(),
    row_version = vc.row_version + 1,
    updated_at = now()
FROM sop_submission_items si
WHERE vc.tenant_id = $2::uuid
  AND si.tenant_id = vc.tenant_id
  AND si.submission_id = $3::uuid
  AND vc.sop_submission_item_id = si.item_id
  AND vc.status = 'recorded'
  AND EXISTS (
    SELECT 1
    FROM verification_items vi
    WHERE vi.tenant_id = vc.tenant_id
      AND vi.source_submission_id = si.submission_id
      AND (
        (vi.source_ref_type = 'sop_submission' AND vi.source_ref_id = si.submission_id)
        OR (vi.source_ref_type = 'vaccination_goat' AND vi.source_ref_id = si.goat_id)
      )
      AND vi.status = 'approved'
      AND vi.closed_at IS NOT NULL
)`, in.ActorID, in.TenantID, in.SubmissionID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
UPDATE obligation_instances oi
SET status = 'completed',
    completed_at = COALESCE(oi.completed_at, now()),
    updated_at = now()
WHERE oi.tenant_id = $1::uuid
  AND oi.status <> 'completed'
  AND EXISTS (
    SELECT 1
    FROM vaccination_completions vc
    JOIN sop_submission_items si
      ON si.tenant_id = vc.tenant_id
     AND si.item_id = vc.sop_submission_item_id
    WHERE vc.tenant_id = oi.tenant_id
      AND vc.obligation_id = oi.obligation_id
      AND vc.status = 'accepted'
      AND si.submission_id = $2::uuid
  )`, in.TenantID, in.SubmissionID); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `
SELECT DISTINCT vc.obligation_id::text
FROM vaccination_completions vc
JOIN obligation_instances oi
  ON oi.tenant_id = vc.tenant_id
 AND oi.obligation_id = vc.obligation_id
JOIN sop_submission_items si
  ON si.tenant_id = vc.tenant_id
 AND si.item_id = vc.sop_submission_item_id
WHERE vc.tenant_id = $1::uuid
  AND si.submission_id = $2::uuid
  AND vc.status = 'accepted'
  AND oi.status = 'completed'`, in.TenantID, in.SubmissionID)
		if err != nil {
			return err
		}
		obligationIDs := make([]string, 0, len(items))
		for rows.Next() {
			var obligationID string
			if err := rows.Scan(&obligationID); err != nil {
				rows.Close()
				return err
			}
			obligationIDs = append(obligationIDs, obligationID)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
		for _, obligationID := range obligationIDs {
			if _, err := insertVaccinationCompletedOutbox(ctx, tx, in.TenantID, obligationID); err != nil {
				return err
			}
		}
		_, err = tx.Exec(ctx, `
UPDATE obligation_batches ob
SET status = 'completed',
    updated_at = now()
WHERE ob.tenant_id = $1::uuid
  AND EXISTS (
    SELECT 1
    FROM vaccination_completions vc
    JOIN sop_submission_items si
      ON si.tenant_id = vc.tenant_id
     AND si.item_id = vc.sop_submission_item_id
    WHERE vc.tenant_id = ob.tenant_id
      AND vc.batch_id = ob.batch_id
      AND vc.status = 'accepted'
      AND si.submission_id = $2::uuid
  )
  AND NOT EXISTS (
    SELECT 1
    FROM obligation_instances oi
    WHERE oi.tenant_id = ob.tenant_id
      AND oi.batch_id = ob.batch_id
      AND oi.status <> 'completed'
  )`, in.TenantID, in.SubmissionID)
		return err
	}
	// A replay after a lost response is a read-only success. A partially closed drive can only be
	// legacy/item-level state and is failed closed rather than silently mixing authority actions.
	if allClosed {
		if err := acceptVaccinationCompletions(); err != nil {
			return nil, mapWriteErr(err)
		}
		if err := completeIdempotency(ctx, tx, in.TenantID, "verification.close-submission", in.IdempotencyKey, "verification_submission", in.SubmissionID); err != nil {
			return nil, err
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		return items, nil
	}
	for _, item := range items {
		if item.ClosedAt != nil {
			return nil, ports.ErrConflict
		}
	}

	if _, err := tx.Exec(ctx, `
UPDATE verification_items
SET closed_by = $1::uuid, closed_at = now(), row_version = row_version + 1
WHERE tenant_id = $2::uuid
  AND source_submission_id = $3::uuid
  AND status = 'approved'
  AND closed_at IS NULL`, in.ActorID, in.TenantID, in.SubmissionID); err != nil {
		return nil, mapWriteErr(err)
	}
	if err := acceptVaccinationCompletions(); err != nil {
		return nil, mapWriteErr(err)
	}
	closedRows, err := tx.Query(ctx, `
SELECT `+itemColumns+`
FROM verification_items
WHERE tenant_id = $1::uuid
  AND source_submission_id = $2::uuid
ORDER BY captured_at, item_id`, in.TenantID, in.SubmissionID)
	if err != nil {
		return nil, err
	}
	closedItems := make([]domain.Item, 0, len(items))
	for closedRows.Next() {
		item, scanErr := scanItemRow(closedRows)
		if scanErr != nil {
			closedRows.Close()
			return nil, scanErr
		}
		closedItems = append(closedItems, item)
	}
	if err := closedRows.Err(); err != nil {
		closedRows.Close()
		return nil, err
	}
	closedRows.Close()
	if len(closedItems) != len(items) {
		return nil, ports.ErrConflict
	}
	for _, item := range closedItems {
		idempotencyKey := fmt.Sprintf("%s:%s:%d", EventItemClosed, item.ItemID, item.RowVersion)
		if err := insertOutboxEvent(ctx, tx, in.TenantID, EventItemClosed, item.ItemID, idempotencyKey, verificationVerdictPayload(item)); err != nil {
			return nil, err
		}
	}
	if err := completeIdempotency(ctx, tx, in.TenantID, "verification.close-submission", in.IdempotencyKey, "verification_submission", in.SubmissionID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return closedItems, nil
}

func (r *Repository) ListReadyVaccinationBatchClosures(ctx context.Context, params ports.ListQueueParams) ([]domain.VaccinationBatchClosure, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	rows, err := r.pool.Query(ctx, `
WITH expected AS (
  SELECT
    oi.batch_id,
    COUNT(*)::int AS total_count,
    ob.protocol_version_id::text AS protocol_version_id,
	    COALESCE(MIN(vda.park_id::text), '') AS park_id,
	    COALESCE(MIN(park.name), '') AS park_label,
	    string_agg(DISTINCT NULLIF(vda.physical_shed, ''), ', ' ORDER BY NULLIF(vda.physical_shed, '')) AS shed_labels,
	    COUNT(DISTINCT NULLIF(vda.physical_shed, ''))::int AS planned_shed_count,
	    MIN(COALESCE(vda.planned_date, ob.planned_date)) AS start_date,
	    MAX(COALESCE(vda.planned_date, ob.planned_date)) AS end_date
  FROM obligation_instances oi
  JOIN obligation_batches ob
    ON ob.tenant_id = oi.tenant_id
   AND ob.batch_id = oi.batch_id
  LEFT JOIN vaccination_drive_assignments vda
    ON vda.tenant_id = oi.tenant_id
   AND vda.batch_id = oi.batch_id
  LEFT JOIN locations park
    ON park.tenant_id = vda.tenant_id
   AND park.location_id = vda.park_id
  WHERE oi.tenant_id = $1::uuid
    AND oi.batch_id IS NOT NULL
    AND ($8 = '' OR vda.park_id = $8::uuid)
    AND ($9 = '' OR vda.shed_id = $9::uuid)
  GROUP BY oi.batch_id, ob.protocol_version_id
),
proofs AS (
  SELECT
    vc.batch_id,
    vi.item_id,
    vi.status,
    vi.closed_at,
    vi.park_id,
    vi.shed_id
  FROM vaccination_completions vc
  JOIN sop_submission_items si
    ON si.tenant_id = vc.tenant_id
   AND si.item_id = vc.sop_submission_item_id
  JOIN verification_items vi
    ON vi.tenant_id = vc.tenant_id
   AND vi.source_submission_id = si.submission_id
   AND (
     (vi.source_ref_type = 'sop_submission' AND vi.source_ref_id = si.submission_id)
     OR (vi.source_ref_type = 'vaccination_goat' AND vi.source_ref_id = si.goat_id)
   )
  WHERE vc.tenant_id = $1::uuid
    AND vi.category = $2
    AND ($3 = '' OR vi.vertical = $3)
    AND ($4 = '' OR vi.module = $4)
    AND (NOT $7::boolean OR vi.closed_at IS NULL)
    AND (NOT $5::boolean OR vi.park_id = ANY($6::uuid[]))
    AND ($8 = '' OR vi.park_id = $8::uuid)
    AND ($9 = '' OR vi.shed_id = $9::uuid)
),
rollup AS (
  SELECT
    e.batch_id::text,
    CASE
      WHEN e.park_id = '' THEN 'vaccination:' || e.protocol_version_id
      ELSE 'vaccination:' || e.protocol_version_id || ':park:' || e.park_id
    END AS drive_key,
    trim(both ' · ' FROM concat_ws(
      ' · ',
      NULLIF(e.park_label, ''),
      CASE
        WHEN e.start_date IS NULL THEN ''
        WHEN e.end_date IS NULL OR e.start_date = e.end_date THEN to_char(e.start_date, 'DD Mon YYYY')
        ELSE to_char(e.start_date, 'DD Mon') || ' - ' || to_char(e.end_date, 'DD Mon YYYY')
      END
    )) AS drive_label,
    trim(both ' · ' FROM concat_ws(
      ' · ',
      CASE
        WHEN e.start_date IS NULL THEN ''
	        WHEN e.end_date IS NULL OR e.start_date = e.end_date THEN to_char(e.start_date, 'DD Mon YYYY')
	        ELSE to_char(e.start_date, 'DD Mon') || ' - ' || to_char(e.end_date, 'DD Mon YYYY')
	      END,
	      CASE
	        WHEN e.planned_shed_count = 1 THEN NULLIF(e.shed_labels, '')
	        WHEN e.planned_shed_count > 1 THEN e.planned_shed_count::text || ' sheds'
	        ELSE ''
	      END
	    )) AS batch_label,
    e.park_id,
    e.park_label,
    COALESCE(e.start_date::text, '') AS start_date,
    COALESCE(e.end_date::text, '') AS end_date,
    e.total_count,
    COUNT(p.*)::int AS proof_count,
    COUNT(*) FILTER (WHERE p.status = 'approved')::int AS approved_count,
    COUNT(*) FILTER (WHERE p.status = 'rejected')::int AS rejected_count,
    COUNT(*) FILTER (WHERE p.status = 'pending')::int AS pending_count,
    COUNT(DISTINCT p.item_id)::int AS video_count,
    COUNT(DISTINCT p.item_id) FILTER (WHERE p.status = 'approved')::int AS approved_videos,
    COUNT(DISTINCT p.item_id) FILTER (WHERE p.status = 'rejected')::int AS rejected_videos,
    COUNT(DISTINCT p.item_id) FILTER (WHERE p.status = 'pending')::int AS pending_videos,
    COUNT(DISTINCT p.shed_id)::int AS shed_count
  FROM expected e
  JOIN proofs p ON p.batch_id = e.batch_id
	  GROUP BY e.batch_id, e.protocol_version_id, e.park_id, e.park_label, e.shed_labels, e.planned_shed_count, e.start_date, e.end_date, e.total_count
)
SELECT batch_id, drive_key, drive_label, batch_label, park_id, park_label, start_date, end_date,
       total_count, approved_count, rejected_count, pending_count,
       video_count, approved_videos, rejected_videos, pending_videos, shed_count
FROM rollup
WHERE proof_count = total_count
  AND approved_count = total_count
  AND rejected_count = 0
  AND pending_count = 0
ORDER BY batch_id
LIMIT 20`,
		params.TenantID, params.Category, params.Vertical, params.Module,
		params.ScopeRestricted, params.ParkIDs, params.OpenOnly, params.ParkID, params.ShedID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.VaccinationBatchClosure, 0, 4)
	for rows.Next() {
		var c domain.VaccinationBatchClosure
		if err := rows.Scan(
			&c.BatchID,
			&c.DriveKey,
			&c.DriveLabel,
			&c.BatchLabel,
			&c.ParkID,
			&c.ParkLabel,
			&c.StartDate,
			&c.EndDate,
			&c.TotalCount,
			&c.ApprovedCount,
			&c.RejectedCount,
			&c.PendingCount,
			&c.VideoCount,
			&c.ApprovedVideos,
			&c.RejectedVideos,
			&c.PendingVideos,
			&c.ShedCount,
		); err != nil {
			return nil, err
		}
		c.Ready = true
		out = append(out, c)
	}
	return out, rows.Err()
}

func (r *Repository) CloseVaccinationBatch(ctx context.Context, in domain.CloseVaccinationBatchAction) ([]domain.Item, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer rollback(ctx, tx)
	reservation, err := reserveIdempotency(ctx, tx, in.TenantID, "verification.close-vaccination-batch", in.IdempotencyKey,
		requestFingerprint(in.BatchID, in.ActorID))
	if err != nil {
		return nil, err
	}

	var expectedCount int
	if err := tx.QueryRow(ctx, `
SELECT COUNT(*)::int
FROM obligation_instances
WHERE tenant_id = $1::uuid
  AND batch_id = $2::uuid`, in.TenantID, in.BatchID).Scan(&expectedCount); err != nil {
		return nil, err
	}
	if expectedCount == 0 {
		return nil, ports.ErrNotFound
	}

	rows, err := tx.Query(ctx, `
SELECT `+itemColumns+`
FROM verification_items vi
WHERE vi.tenant_id = $1::uuid
  AND vi.source_submission_id IS NOT NULL
  AND EXISTS (
    SELECT 1
    FROM vaccination_completions vc
    JOIN sop_submission_items si
      ON si.tenant_id = vc.tenant_id
     AND si.item_id = vc.sop_submission_item_id
    WHERE vc.tenant_id = vi.tenant_id
      AND vc.batch_id = $2::uuid
      AND si.submission_id = vi.source_submission_id
      AND (
        (vi.source_ref_type = 'sop_submission' AND vi.source_ref_id = si.submission_id)
        OR (vi.source_ref_type = 'vaccination_goat' AND vi.source_ref_id = si.goat_id)
      )
  )
ORDER BY vi.captured_at, vi.item_id
FOR UPDATE`, in.TenantID, in.BatchID)
	if err != nil {
		return nil, err
	}
	items := make([]domain.Item, 0, expectedCount)
	for rows.Next() {
		item, scanErr := scanItemRow(rows)
		if scanErr != nil {
			rows.Close()
			return nil, scanErr
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	if len(items) != expectedCount {
		var coveredCount int
		if err := tx.QueryRow(ctx, `
SELECT COUNT(*)::int
FROM vaccination_completions vc
JOIN sop_submission_items si
  ON si.tenant_id = vc.tenant_id
 AND si.item_id = vc.sop_submission_item_id
WHERE vc.tenant_id = $1::uuid
  AND vc.batch_id = $2::uuid
  AND EXISTS (
    SELECT 1
    FROM verification_items vi
    WHERE vi.tenant_id = vc.tenant_id
      AND vi.source_submission_id = si.submission_id
      AND (
        (vi.source_ref_type = 'sop_submission' AND vi.source_ref_id = si.submission_id)
        OR (vi.source_ref_type = 'vaccination_goat' AND vi.source_ref_id = si.goat_id)
      )
  )`, in.TenantID, in.BatchID).Scan(&coveredCount); err != nil {
			return nil, err
		}
		if coveredCount != expectedCount {
			return nil, ports.ErrConflict
		}
	}
	if len(items) == 0 {
		return nil, ports.ErrConflict
	}
	allClosed := true
	for _, item := range items {
		if item.Status != domain.StatusApproved {
			return nil, ports.ErrConflict
		}
		if item.ClosedAt == nil {
			allClosed = false
		}
	}
	if allClosed {
		if err := r.acceptVaccinationBatch(ctx, tx, in.TenantID, in.BatchID, in.ActorID); err != nil {
			return nil, mapWriteErr(err)
		}
		if reservation.proceed {
			if err := completeIdempotency(ctx, tx, in.TenantID, "verification.close-vaccination-batch", in.IdempotencyKey, "vaccination_batch", in.BatchID); err != nil {
				return nil, err
			}
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		return items, nil
	}
	if !reservation.proceed && reservation.resultID != in.BatchID {
		return nil, ports.ErrConflict
	}

	if _, err := tx.Exec(ctx, `
UPDATE verification_items vi
SET closed_by = $1::uuid, closed_at = now(), row_version = row_version + 1
WHERE vi.tenant_id = $2::uuid
  AND vi.source_submission_id IS NOT NULL
  AND vi.status = 'approved'
  AND vi.closed_at IS NULL
  AND EXISTS (
    SELECT 1
    FROM vaccination_completions vc
    JOIN sop_submission_items si
      ON si.tenant_id = vc.tenant_id
     AND si.item_id = vc.sop_submission_item_id
    WHERE vc.tenant_id = vi.tenant_id
      AND vc.batch_id = $3::uuid
      AND si.submission_id = vi.source_submission_id
      AND (
        (vi.source_ref_type = 'sop_submission' AND vi.source_ref_id = si.submission_id)
        OR (vi.source_ref_type = 'vaccination_goat' AND vi.source_ref_id = si.goat_id)
      )
  )`, in.ActorID, in.TenantID, in.BatchID); err != nil {
		return nil, mapWriteErr(err)
	}
	if err := r.acceptVaccinationBatch(ctx, tx, in.TenantID, in.BatchID, in.ActorID); err != nil {
		return nil, mapWriteErr(err)
	}

	closedRows, err := tx.Query(ctx, `
SELECT `+itemColumns+`
FROM verification_items vi
WHERE vi.tenant_id = $1::uuid
  AND vi.source_submission_id IS NOT NULL
  AND EXISTS (
    SELECT 1
    FROM vaccination_completions vc
    JOIN sop_submission_items si
      ON si.tenant_id = vc.tenant_id
     AND si.item_id = vc.sop_submission_item_id
    WHERE vc.tenant_id = vi.tenant_id
      AND vc.batch_id = $2::uuid
      AND si.submission_id = vi.source_submission_id
      AND (
        (vi.source_ref_type = 'sop_submission' AND vi.source_ref_id = si.submission_id)
        OR (vi.source_ref_type = 'vaccination_goat' AND vi.source_ref_id = si.goat_id)
      )
  )
ORDER BY vi.captured_at, vi.item_id`, in.TenantID, in.BatchID)
	if err != nil {
		return nil, err
	}
	closedItems := make([]domain.Item, 0, len(items))
	for closedRows.Next() {
		item, scanErr := scanItemRow(closedRows)
		if scanErr != nil {
			closedRows.Close()
			return nil, scanErr
		}
		closedItems = append(closedItems, item)
	}
	if err := closedRows.Err(); err != nil {
		closedRows.Close()
		return nil, err
	}
	closedRows.Close()
	for _, item := range closedItems {
		idempotencyKey := fmt.Sprintf("%s:%s:%d", EventItemClosed, item.ItemID, item.RowVersion)
		if err := insertOutboxEvent(ctx, tx, in.TenantID, EventItemClosed, item.ItemID, idempotencyKey, verificationVerdictPayload(item)); err != nil {
			return nil, err
		}
	}
	if err := insertVaccinationDriveClosedOutbox(ctx, tx, in.TenantID, in.BatchID, in.ActorID); err != nil {
		return nil, err
	}
	if err := completeIdempotency(ctx, tx, in.TenantID, "verification.close-vaccination-batch", in.IdempotencyKey, "vaccination_batch", in.BatchID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return closedItems, nil
}

func (r *Repository) acceptVaccinationBatch(ctx context.Context, tx pgx.Tx, tenantID, batchID, actorID string) error {
	if _, err := tx.Exec(ctx, `
UPDATE vaccination_completions vc
SET status = 'accepted',
    verified_by = $1::uuid,
    verified_at = now(),
    row_version = vc.row_version + 1,
    updated_at = now()
FROM sop_submission_items si
WHERE vc.tenant_id = $2::uuid
  AND vc.batch_id = $3::uuid
  AND si.tenant_id = vc.tenant_id
  AND vc.sop_submission_item_id = si.item_id
  AND vc.status = 'recorded'
  AND EXISTS (
    SELECT 1
    FROM verification_items vi
    WHERE vi.tenant_id = vc.tenant_id
      AND vi.source_submission_id = si.submission_id
      AND (
        (vi.source_ref_type = 'sop_submission' AND vi.source_ref_id = si.submission_id)
        OR (vi.source_ref_type = 'vaccination_goat' AND vi.source_ref_id = si.goat_id)
      )
      AND vi.status = 'approved'
      AND vi.closed_at IS NOT NULL
)`, actorID, tenantID, batchID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
	UPDATE obligation_instances oi
	SET status = 'completed',
	    completed_at = COALESCE(
	      oi.completed_at,
	      (
	        SELECT min(vc.administered_at)
	        FROM vaccination_completions vc
	        WHERE vc.tenant_id = oi.tenant_id
	          AND vc.obligation_id = oi.obligation_id
	          AND vc.status = 'accepted'
	      ),
	      now()
	    ),
	    updated_at = now()
	WHERE oi.tenant_id = $1::uuid
	  AND oi.batch_id = $2::uuid
  AND oi.status <> 'completed'
  AND EXISTS (
    SELECT 1
    FROM vaccination_completions vc
    WHERE vc.tenant_id = oi.tenant_id
      AND vc.obligation_id = oi.obligation_id
      AND vc.status = 'accepted'
  )`, tenantID, batchID); err != nil {
		return err
	}
	rows, err := tx.Query(ctx, `
SELECT obligation_id::text
FROM obligation_instances
WHERE tenant_id = $1::uuid
  AND batch_id = $2::uuid
  AND status = 'completed'`, tenantID, batchID)
	if err != nil {
		return err
	}
	obligationIDs := make([]string, 0)
	for rows.Next() {
		var obligationID string
		if err := rows.Scan(&obligationID); err != nil {
			rows.Close()
			return err
		}
		obligationIDs = append(obligationIDs, obligationID)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, obligationID := range obligationIDs {
		if _, err := insertVaccinationCompletedOutbox(ctx, tx, tenantID, obligationID); err != nil {
			return err
		}
	}
	if err := r.acceptSOPBatch(ctx, tx, tenantID, batchID, actorID); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
UPDATE obligation_batches ob
SET status = 'completed',
    updated_at = now()
WHERE ob.tenant_id = $1::uuid
  AND ob.batch_id = $2::uuid
  AND NOT EXISTS (
    SELECT 1
    FROM obligation_instances oi
    WHERE oi.tenant_id = ob.tenant_id
      AND oi.batch_id = ob.batch_id
      AND oi.status <> 'completed'
  )`, tenantID, batchID)
	return err
}

func (r *Repository) acceptSOPBatch(ctx context.Context, tx pgx.Tx, tenantID, batchID, actorID string) error {
	if _, err := tx.Exec(ctx, `
WITH batch_items AS (
  SELECT DISTINCT si.submission_id, si.task_id, si.item_id
  FROM vaccination_completions vc
  JOIN sop_submission_items si
    ON si.tenant_id = vc.tenant_id
   AND si.item_id = vc.sop_submission_item_id
  WHERE vc.tenant_id = $1::uuid
    AND vc.batch_id = $2::uuid
    AND vc.status = 'accepted'
)
UPDATE sop_submission_items si
SET state = 'accepted',
    result = COALESCE(si.result, '{}'::jsonb) || jsonb_build_object(
      'verification_closed_at', now(),
      'verification_closed_by', $3::text
    )
FROM batch_items bi
WHERE si.tenant_id = $1::uuid
  AND si.item_id = bi.item_id
  AND si.state = 'needs_review'`, tenantID, batchID, actorID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
WITH candidate_submissions AS (
  SELECT DISTINCT si.submission_id, si.task_id
  FROM vaccination_completions vc
  JOIN sop_submission_items si
    ON si.tenant_id = vc.tenant_id
   AND si.item_id = vc.sop_submission_item_id
  WHERE vc.tenant_id = $1::uuid
    AND vc.batch_id = $2::uuid
    AND vc.status = 'accepted'
)
UPDATE sop_submissions ss
SET state = 'accepted',
    accepted_at = COALESCE(ss.accepted_at, now()),
    row_version = ss.row_version + 1
FROM candidate_submissions cs
WHERE ss.tenant_id = $1::uuid
  AND ss.submission_id = cs.submission_id
  AND ss.state IN ('submitted', 'needs_review')
  AND NOT EXISTS (
    SELECT 1
    FROM sop_submission_items remaining
    WHERE remaining.tenant_id = ss.tenant_id
      AND remaining.submission_id = ss.submission_id
      AND remaining.state NOT IN ('accepted', 'skipped')
  )`, tenantID, batchID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
WITH candidate_tasks AS (
  SELECT DISTINCT si.task_id
  FROM vaccination_completions vc
  JOIN sop_submission_items si
    ON si.tenant_id = vc.tenant_id
   AND si.item_id = vc.sop_submission_item_id
  WHERE vc.tenant_id = $1::uuid
    AND vc.batch_id = $2::uuid
    AND vc.status = 'accepted'
)
UPDATE sop_tasks st
SET state = 'accepted',
    verified_by = $3::uuid,
    verified_at = COALESCE(st.verified_at, now()),
    updated_at = now(),
    row_version = st.row_version + 1
FROM candidate_tasks ct
WHERE st.tenant_id = $1::uuid
  AND st.task_id = ct.task_id
  AND st.state IN ('submitted', 'needs_review')
  AND NOT EXISTS (
    SELECT 1
    FROM sop_submission_items remaining
    WHERE remaining.tenant_id = st.tenant_id
      AND remaining.task_id = st.task_id
      AND remaining.state NOT IN ('accepted', 'skipped')
  )`, tenantID, batchID, actorID); err != nil {
		return err
	}
	return nil
}

func (r *Repository) RecordVerdict(ctx context.Context, in domain.Verdict) (domain.Item, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.Item{}, err
	}
	defer rollback(ctx, tx)
	reservation, err := reserveIdempotency(ctx, tx, in.TenantID, "verification.verdict", in.IdempotencyKey,
		requestFingerprint(in.ItemID, in.Decision, in.Reason, in.VerifierID, fmt.Sprintf("%d", in.RowVersion)))
	if err != nil {
		return domain.Item{}, err
	}
	if !reservation.proceed {
		item, err := scanItemRow(tx.QueryRow(ctx, "SELECT "+itemColumns+" FROM verification_items WHERE tenant_id = $1::uuid AND item_id = $2::uuid", in.TenantID, reservation.resultID))
		if err != nil {
			return domain.Item{}, mapWriteErr(err)
		}
		if err := tx.Commit(ctx); err != nil {
			return domain.Item{}, err
		}
		return item, nil
	}

	status := domain.StatusApproved
	if in.Decision == domain.DecisionRejected {
		status = domain.StatusRejected
	}
	var reason any
	if in.Reason != "" {
		reason = in.Reason
	}
	tag, err := tx.Exec(ctx, `
UPDATE verification_items
SET status = $1, verdict_reason = $2, verified_by = $3::uuid, verified_at = now(), row_version = row_version + 1
WHERE tenant_id = $4::uuid
  AND item_id = $5::uuid
  AND row_version = $6
  AND status = 'pending'
  AND closed_at IS NULL
  AND (operator_id IS NULL OR operator_id <> $3::uuid)`,
		status, reason, in.VerifierID, in.TenantID, in.ItemID, in.RowVersion,
	)
	if err != nil {
		return domain.Item{}, mapWriteErr(err)
	}
	if tag.RowsAffected() == 0 {
		// Distinguish "does not exist" (404) from "row_version is stale" (409).
		var exists bool
		if scanErr := tx.QueryRow(ctx, "SELECT true FROM verification_items WHERE tenant_id = $1::uuid AND item_id = $2::uuid", in.TenantID, in.ItemID).Scan(&exists); scanErr != nil {
			if errors.Is(scanErr, pgx.ErrNoRows) {
				return domain.Item{}, ports.ErrNotFound
			}
			return domain.Item{}, scanErr
		}
		return domain.Item{}, ports.ErrConflict
	}

	item, err := scanItemRow(tx.QueryRow(ctx, "SELECT "+itemColumns+" FROM verification_items WHERE tenant_id = $1::uuid AND item_id = $2::uuid", in.TenantID, in.ItemID))
	if err != nil {
		return domain.Item{}, mapWriteErr(err)
	}

	eventType := EventVerdictApproved
	if in.Decision == domain.DecisionRejected {
		eventType = EventVerdictRework
	}
	idempotencyKey := fmt.Sprintf("%s:%s:%d", eventType, in.ItemID, item.RowVersion)
	if err := insertOutboxEvent(ctx, tx, in.TenantID, eventType, in.ItemID, idempotencyKey, verificationVerdictPayload(item)); err != nil {
		return domain.Item{}, err
	}
	if in.Decision == domain.DecisionApproved {
		if err := insertVaccinationDriveReadyOutboxIfReady(ctx, tx, item); err != nil {
			return domain.Item{}, err
		}
	}
	if err := completeIdempotency(ctx, tx, in.TenantID, "verification.verdict", in.IdempotencyKey, "verification_item", item.ItemID); err != nil {
		return domain.Item{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return domain.Item{}, err
	}
	return item, nil
}

// verificationItemPendingPayload is the verification.item.pending outbox payload. Field set is fixed
// by contract with the notification producer session (build-handover-20260713.md §1 P0 1a "the Max
// seam"): tenant/item identity + classification + WHO to route to (operator/shed/park) + WHEN
// captured, so the notifier can resolve the Verifier's assignment without a callback into this
// module.
func verificationItemPendingPayload(itemID string, in domain.CreateItem) map[string]any {
	return map[string]any{
		"tenant_id":     in.TenantID,
		"item_id":       itemID,
		"vertical":      in.Vertical,
		"module":        in.Module,
		"category":      in.Category,
		"subject_label": derefStr(in.SubjectLabel),
		"source": map[string]any{
			"module":        in.Source.Module,
			"task_id":       derefStr(in.Source.TaskID),
			"submission_id": derefStr(in.Source.SubmissionID),
			"ref_type":      in.Source.RefType,
			"ref_id":        in.Source.RefID,
		},
		"operator_id": derefStr(in.OperatorID),
		"shed_id":     derefStr(in.ShedID),
		"park_id":     derefStr(in.ParkID),
		"captured_at": in.CapturedAt.UTC().Format(time.RFC3339Nano),
	}
}

// verificationVerdictPayload is the verification.verdict.approved / verification.verdict.rework
// outbox payload. Carries the SAME who-to-route-to fields as the pending payload (operator_id,
// shed_id, park_id) plus the decision + reason, so the notifier can apply its own routing (rework ->
// operator + park head; approved -> digest/no-op) without a callback into this module.
func verificationVerdictPayload(item domain.Item) map[string]any {
	payload := map[string]any{
		"tenant_id":   item.TenantID,
		"item_id":     item.ItemID,
		"vertical":    item.Vertical,
		"module":      item.Module,
		"category":    item.Category,
		"status":      item.Status,
		"decision":    item.Status, // "approved" | "rejected" -- explicit alias, kept alongside status for notifier clarity.
		"operator_id": derefStr(item.OperatorID),
		"shed_id":     derefStr(item.ShedID),
		"park_id":     derefStr(item.ParkID),
		"source": map[string]any{
			"module":        item.Source.Module,
			"task_id":       derefStr(item.Source.TaskID),
			"submission_id": derefStr(item.Source.SubmissionID),
			"ref_type":      item.Source.RefType,
			"ref_id":        item.Source.RefID,
		},
	}
	if item.VerdictReason != nil {
		payload["reason"] = *item.VerdictReason
	}
	if item.VerifiedBy != nil {
		payload["verified_by"] = *item.VerifiedBy
	}
	if item.ClosedBy != nil {
		payload["closed_by"] = *item.ClosedBy
	}
	if item.ClosedAt != nil {
		payload["closed_at"] = item.ClosedAt.UTC().Format(time.RFC3339Nano)
	}
	return payload
}

// insertOutboxEvent inserts one outbox_messages row in the SAME transaction as the caller's state
// change, mirroring the vaccination.completed precedent
// (backend/internal/vaccination/adapters/postgres/repository.go:insertVaccinationCompletedOutbox).
// Idempotent: ON CONFLICT on the partial unique index for verification event types is a no-op.
func insertOutboxEvent(ctx context.Context, tx pgx.Tx, tenantID, eventType, aggregateID, idempotencyKey string, payload map[string]any) error {
	eventID := platformoutbox.DeterministicUUID(eventType + ":" + tenantID + ":" + idempotencyKey)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	envelope, err := json.Marshal(map[string]any{
		"event_id":       eventID,
		"event_type":     eventType,
		"schema_version": verificationSchemaVer,
		"schema_ref":     verificationSchemaRef + "#" + eventType,
		"aggregate_type": "verification_item",
		"aggregate_id":   aggregateID,
		"occurred_at":    now,
		"recorded_at":    now,
		"producer": map[string]any{
			"service": "goatos-api",
			"module":  "verification",
		},
		"idempotency_key": idempotencyKey,
		// A verdict/pending record is a system-rule transition (the module writes the outbox row in
		// the same transaction as the state change), not a direct human actor keystroke.
		"actor": map[string]any{
			"actor_type": "system_rule",
			"actor_ref":  "verification",
		},
		"subject_type": "verification_item",
		"subject_id":   aggregateID,
		"visibility_scope": map[string]any{
			"tenant_id": tenantID,
		},
		"evidence_refs": []any{},
		"payload":       payload,
		"trace_id":      idempotencyKey,
	})
	if err != nil {
		return fmt.Errorf("verification: outbox envelope: %w", err)
	}
	headers, err := json.Marshal(map[string]any{
		"producer":        "verification",
		"schema_version":  verificationSchemaVer,
		"idempotency_key": idempotencyKey,
	})
	if err != nil {
		return fmt.Errorf("verification: outbox headers: %w", err)
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO outbox_messages (
  tenant_id, event_id, event_type, schema_version, aggregate_type, aggregate_id,
  topic, payload, headers, idempotency_key, trace_id, status, next_attempt_at
) VALUES (
  $1::uuid, $2::uuid, $3, $4, 'verification_item', $5::uuid,
  $6, $7::jsonb, $8::jsonb, $9, $9, 'pending', now()
)
ON CONFLICT (tenant_id, idempotency_key) WHERE event_type IN (
  'verification.item.pending', 'verification.verdict.approved', 'verification.verdict.rework',
  'verification.item.closed'
) DO NOTHING`,
		tenantID, eventID, eventType, verificationSchemaVer, aggregateID,
		verificationTopic, envelope, headers, idempotencyKey); err != nil {
		return fmt.Errorf("verification: outbox insert: %w", err)
	}
	return nil
}

func insertVaccinationCompletedOutbox(ctx context.Context, tx pgx.Tx, tenantID, obligationID string) (bool, error) {
	eventID := platformoutbox.DeterministicUUID("vaccination.completed:" + tenantID + ":" + obligationID)
	idempotencyKey := "vaccination.completed:" + obligationID
	now := time.Now().UTC().Format(time.RFC3339Nano)
	payload := map[string]any{
		"tenant_id":     tenantID,
		"obligation_id": obligationID,
		"status":        "completed",
	}
	envelope, err := json.Marshal(map[string]any{
		"event_id":       eventID,
		"event_type":     vaccinationCompletedEventType,
		"schema_version": vaccinationCompletedSchemaVersion,
		"schema_ref":     vaccinationCompletedSchemaRef,
		"aggregate_type": "obligation_instance",
		"aggregate_id":   obligationID,
		"occurred_at":    now,
		"recorded_at":    now,
		"producer": map[string]any{
			"service": "goatos-api",
			"module":  "vaccination",
			"version": nil,
		},
		"idempotency_key": idempotencyKey,
		"actor": map[string]any{
			"actor_type": "system_rule",
			"actor_id":   nil,
			"actor_ref":  nil,
		},
		"subject_type": "obligation_instance",
		"subject_id":   obligationID,
		"visibility_scope": map[string]any{
			"tenant_id": tenantID,
		},
		"evidence_refs": []map[string]string{{
			"evidence_type": "obligation_status_event",
			"evidence_id":   obligationID + ":completed",
		}},
		"payload":  payload,
		"trace_id": idempotencyKey,
	})
	if err != nil {
		return false, fmt.Errorf("vaccination completed envelope: %w", err)
	}
	headers, err := json.Marshal(map[string]any{
		"producer":        "verification.CloseSubmission",
		"schema_version":  vaccinationCompletedSchemaVersion,
		"obligation_id":   obligationID,
		"idempotency_key": idempotencyKey,
	})
	if err != nil {
		return false, fmt.Errorf("vaccination completed headers: %w", err)
	}
	tag, err := tx.Exec(ctx, `
INSERT INTO outbox_messages (
  tenant_id, event_id, event_type, schema_version, aggregate_type, aggregate_id,
  topic, payload, headers, idempotency_key, trace_id, status, next_attempt_at
) VALUES (
  $1::uuid, $2::uuid, $3, $4, 'obligation_instance', $5::uuid,
  $6, $7::jsonb, $8::jsonb, $9, $9, 'pending', now()
)
ON CONFLICT (tenant_id, idempotency_key) WHERE event_type = 'vaccination.completed' DO NOTHING`,
		tenantID, eventID, vaccinationCompletedEventType, vaccinationCompletedSchemaVersion,
		obligationID, vaccinationCompletedTopic, envelope, headers, idempotencyKey)
	if err != nil {
		return false, fmt.Errorf("vaccination completed outbox: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

func insertVaccinationDriveReadyOutboxIfReady(ctx context.Context, tx pgx.Tx, item domain.Item) error {
	if item.Source.SubmissionID == nil || (item.Source.RefType != "sop_submission" && item.Source.RefType != "vaccination_goat") {
		return nil
	}
	var batchID string
	var expectedCount, proofCount, approvedCount, rejectedCount, pendingCount int
	if err := tx.QueryRow(ctx, `
WITH item_batch AS (
  SELECT vc.batch_id
  FROM vaccination_completions vc
  JOIN sop_submission_items si
    ON si.tenant_id = vc.tenant_id
   AND si.item_id = vc.sop_submission_item_id
  WHERE vc.tenant_id = $1::uuid
    AND si.submission_id = $2::uuid
    AND (
      ($4 = 'sop_submission' AND si.submission_id = $3::uuid)
      OR ($4 = 'vaccination_goat' AND si.goat_id = $3::uuid)
    )
  LIMIT 1
),
expected AS (
  SELECT COUNT(*)::int AS total_count
  FROM obligation_instances oi
  JOIN item_batch ib ON ib.batch_id = oi.batch_id
  WHERE oi.tenant_id = $1::uuid
),
proofs AS (
  SELECT vi.status
  FROM item_batch ib
  JOIN vaccination_completions vc
    ON vc.tenant_id = $1::uuid
   AND vc.batch_id = ib.batch_id
  JOIN sop_submission_items si
    ON si.tenant_id = vc.tenant_id
   AND si.item_id = vc.sop_submission_item_id
  JOIN verification_items vi
    ON vi.tenant_id = vc.tenant_id
   AND vi.source_submission_id = si.submission_id
   AND (
     (vi.source_ref_type = 'sop_submission' AND vi.source_ref_id = si.submission_id)
     OR (vi.source_ref_type = 'vaccination_goat' AND vi.source_ref_id = si.goat_id)
   )
   AND vi.closed_at IS NULL
)
SELECT ib.batch_id::text,
       e.total_count,
       COUNT(p.status)::int,
       COUNT(*) FILTER (WHERE p.status = 'approved')::int,
       COUNT(*) FILTER (WHERE p.status = 'rejected')::int,
       COUNT(*) FILTER (WHERE p.status = 'pending')::int
FROM item_batch ib
CROSS JOIN expected e
LEFT JOIN proofs p ON true
GROUP BY ib.batch_id, e.total_count`,
		item.TenantID, *item.Source.SubmissionID, item.Source.RefID, item.Source.RefType).Scan(
		&batchID, &expectedCount, &proofCount, &approvedCount, &rejectedCount, &pendingCount,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return err
	}
	if expectedCount == 0 || proofCount != expectedCount || approvedCount != expectedCount || rejectedCount != 0 || pendingCount != 0 {
		return nil
	}
	return insertVaccinationDriveLifecycleOutbox(ctx, tx, item.TenantID, batchID, derefStr(item.ParkID), "", EventVaccinationDriveReady, "ready")
}

func insertVaccinationDriveClosedOutbox(ctx context.Context, tx pgx.Tx, tenantID, batchID, actorID string) error {
	return insertVaccinationDriveLifecycleOutbox(ctx, tx, tenantID, batchID, "", actorID, EventVaccinationDriveClosed, "closed")
}

func insertVaccinationDriveLifecycleOutbox(ctx context.Context, tx pgx.Tx, tenantID, batchID, parkID, actorID, eventType, status string) error {
	idempotencyKey := eventType + ":" + batchID
	eventID := platformoutbox.DeterministicUUID(idempotencyKey + ":" + tenantID)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	payload := map[string]any{
		"tenant_id": tenantID,
		"batch_id":  batchID,
		"park_id":   parkID,
		"status":    status,
	}
	if actorID != "" {
		payload["closed_by"] = actorID
	}
	actorType := "system_rule"
	var actorValue any
	if actorID != "" {
		actorType = "human"
		actorValue = actorID
	}
	envelope, err := json.Marshal(map[string]any{
		"event_id":       eventID,
		"event_type":     eventType,
		"schema_version": verificationSchemaVer,
		"schema_ref":     verificationSchemaRef,
		"aggregate_type": "vaccination_batch",
		"aggregate_id":   batchID,
		"occurred_at":    now,
		"recorded_at":    now,
		"producer": map[string]any{
			"service": "goatos-api",
			"module":  "verification",
		},
		"idempotency_key": idempotencyKey,
		"actor": map[string]any{
			"actor_type": actorType,
			"actor_id":   actorValue,
		},
		"subject_type": "vaccination_batch",
		"subject_id":   batchID,
		"visibility_scope": map[string]any{
			"tenant_id": tenantID,
		},
		"evidence_refs": []any{},
		"payload":       payload,
		"trace_id":      idempotencyKey,
	})
	if err != nil {
		return fmt.Errorf("verification: drive lifecycle envelope: %w", err)
	}
	headers, err := json.Marshal(map[string]any{
		"producer":        "verification.vaccination_drive",
		"schema_version":  verificationSchemaVer,
		"idempotency_key": idempotencyKey,
	})
	if err != nil {
		return fmt.Errorf("verification: drive lifecycle headers: %w", err)
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO outbox_messages (
  tenant_id, event_id, event_type, schema_version, aggregate_type, aggregate_id,
  topic, payload, headers, idempotency_key, trace_id, status, next_attempt_at
) VALUES (
  $1::uuid, $2::uuid, $3, $4, 'vaccination_batch', $5::uuid,
  $6, $7::jsonb, $8::jsonb, $9, $9, 'pending', now()
)
ON CONFLICT (event_id) DO NOTHING`,
		tenantID, eventID, eventType, verificationSchemaVer, batchID,
		verificationTopic, envelope, headers, idempotencyKey); err != nil {
		return fmt.Errorf("verification: drive lifecycle outbox insert: %w", err)
	}
	return nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanItemRow(row rowScanner) (domain.Item, error) {
	return scanItem(row)
}

func scanItem(row rowScanner) (domain.Item, error) {
	var (
		item                                                            domain.Item
		sourceTaskID, sourceSubmissionID                                *string
		operatorID, shedID, parkID, verifiedBy, closedBy, verdictReason *string
		subjectLabel                                                    *string
		mediaJSON                                                       []byte
		verifiedAt, closedAt                                            *time.Time
	)
	if err := row.Scan(
		&item.ItemID, &item.TenantID, &item.Vertical, &item.Module, &item.Category,
		&item.Source.Module, &sourceTaskID, &sourceSubmissionID, &item.Source.RefType, &item.Source.RefID,
		&subjectLabel, &mediaJSON, &item.Status, &verdictReason, &operatorID, &shedID, &parkID,
		&item.CapturedAt, &verifiedBy, &verifiedAt, &closedBy, &closedAt,
		&item.RowVersion, &item.CreatedAt, &item.UpdatedAt,
	); err != nil {
		return domain.Item{}, err
	}
	item.Source.TaskID = sourceTaskID
	item.Source.SubmissionID = sourceSubmissionID
	item.VerdictReason = verdictReason
	item.SubjectLabel = subjectLabel
	item.OperatorID = operatorID
	item.ShedID = shedID
	item.ParkID = parkID
	item.VerifiedBy = verifiedBy
	item.VerifiedAt = verifiedAt
	item.ClosedBy = closedBy
	item.ClosedAt = closedAt
	item.CapturedAt = item.CapturedAt.UTC()
	item.CreatedAt = item.CreatedAt.UTC()
	item.UpdatedAt = item.UpdatedAt.UTC()
	if len(mediaJSON) > 0 {
		if err := json.Unmarshal(mediaJSON, &item.MediaRefs); err != nil {
			return domain.Item{}, fmt.Errorf("verification: unmarshal media_refs: %w", err)
		}
	}
	return item, nil
}

func scanItemWithLabels(row rowScanner) (domain.Item, error) {
	var (
		item                                                            domain.Item
		sourceTaskID, sourceSubmissionID                                *string
		operatorID, shedID, parkID, verifiedBy, closedBy, verdictReason *string
		subjectLabel                                                    *string
		operatorName, shedLabel, parkLabel                              *string
		mediaJSON                                                       []byte
		verifiedAt, closedAt                                            *time.Time
	)
	if err := row.Scan(
		&item.ItemID, &item.TenantID, &item.Vertical, &item.Module, &item.Category,
		&item.Source.Module, &sourceTaskID, &sourceSubmissionID, &item.Source.RefType, &item.Source.RefID,
		&subjectLabel, &mediaJSON, &item.Status, &verdictReason, &operatorID, &shedID, &parkID,
		&item.CapturedAt, &verifiedBy, &verifiedAt, &closedBy, &closedAt,
		&item.RowVersion, &item.CreatedAt, &item.UpdatedAt,
		&operatorName, &shedLabel, &parkLabel,
	); err != nil {
		return domain.Item{}, err
	}
	item.Source.TaskID = sourceTaskID
	item.Source.SubmissionID = sourceSubmissionID
	item.VerdictReason = verdictReason
	item.SubjectLabel = subjectLabel
	item.OperatorID = operatorID
	item.OperatorName = operatorName
	item.ShedID = shedID
	item.ShedLabel = shedLabel
	item.ParkID = parkID
	item.ParkLabel = parkLabel
	item.VerifiedBy = verifiedBy
	item.VerifiedAt = verifiedAt
	item.ClosedBy = closedBy
	item.ClosedAt = closedAt
	item.CapturedAt = item.CapturedAt.UTC()
	item.CreatedAt = item.CreatedAt.UTC()
	item.UpdatedAt = item.UpdatedAt.UTC()
	if len(mediaJSON) > 0 {
		if err := json.Unmarshal(mediaJSON, &item.MediaRefs); err != nil {
			return domain.Item{}, fmt.Errorf("verification: unmarshal media_refs: %w", err)
		}
	}
	return item, nil
}

func nonNilStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

func derefStr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func rollback(ctx context.Context, tx pgx.Tx) {
	_ = tx.Rollback(ctx)
}

func mapWriteErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return ports.ErrNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505":
			return ports.ErrConflict
		case "23503", "23514", "22P02":
			return ports.ErrConflict
		}
	}
	return err
}

// WithdrawItemsBySource retires the PENDING items raised for source records the
// producing module has superseded. It is deliberately status='pending'-only and
// verdict-free: an approved/rejected item is a decision that already happened and
// stays exactly as it was, and a withdrawn item records no verdict, no verifier and
// no verdict_reason because nobody decided anything.
//
// Every actionable path in this repository is already gated on status='pending'
// (the RecordVerdict UPDATE, the queue's status filter, the pending/approved/
// rejected roll-ups), so 'withdrawn' drops the item out of all of them at once.
func (r *Repository) WithdrawItemsBySource(ctx context.Context, tenantID, sourceModule, sourceRefType string, sourceRefIDs []string) (int, error) {
	if len(sourceRefIDs) == 0 {
		return 0, nil
	}
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tag, err := r.pool.Exec(ctx, `
UPDATE verification_items
SET status = 'withdrawn', row_version = row_version + 1, updated_at = now()
WHERE tenant_id = $1::uuid
  AND source_module = $2
  AND source_ref_type = $3
  AND source_ref_id = ANY($4::uuid[])
  AND status = 'pending'`, tenantID, sourceModule, sourceRefType, sourceRefIDs)
	if err != nil {
		return 0, mapWriteErr(err)
	}
	return int(tag.RowsAffected()), nil
}
