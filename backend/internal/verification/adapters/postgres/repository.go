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
	EventItemPending      = "verification.item.pending"
	EventVerdictApproved  = "verification.verdict.approved"
	EventVerdictRework    = "verification.verdict.rework"
	verificationTopic     = "verification"
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
  source_task_id::text, source_submission_id::text, source_ref_type, source_ref_id::text, media_refs,
  status, verdict_reason, operator_id::text, shed_id::text, park_id::text, captured_at, verified_by::text,
  verified_at, row_version, created_at, updated_at`

const itemColumnsWithLabels = `vi.item_id::text, vi.tenant_id::text, vi.vertical, vi.module, vi.category, vi.source_module,
  vi.source_task_id::text, vi.source_submission_id::text, vi.source_ref_type, vi.source_ref_id::text, vi.media_refs,
  vi.status, vi.verdict_reason, vi.operator_id::text, vi.shed_id::text, vi.park_id::text, vi.captured_at, vi.verified_by::text,
  vi.verified_at, vi.row_version, vi.created_at, vi.updated_at,
  wm.display_name::text, shed_loc.name::text, park_loc.name::text`

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
  source_ref_type, source_ref_id, media_refs, status, operator_id, shed_id, park_id, captured_at,
  idempotency_key
) VALUES (
  $1::uuid, $2, $3, $4, $5, nullif($6, '')::uuid, nullif($7, '')::uuid, $8, $9::uuid, $10::jsonb,
  'pending', nullif($11, '')::uuid, nullif($12, '')::uuid, nullif($13, '')::uuid, $14, $15
)
ON CONFLICT (tenant_id, idempotency_key) DO NOTHING
RETURNING item_id::text`,
		in.TenantID, in.Vertical, in.Module, in.Category, in.Source.Module,
		derefStr(in.Source.TaskID), derefStr(in.Source.SubmissionID), in.Source.RefType, in.Source.RefID,
		string(mediaJSON), derefStr(in.OperatorID), derefStr(in.ShedID), derefStr(in.ParkID),
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
LEFT JOIN workforce_members wm ON vi.tenant_id = wm.tenant_id AND vi.operator_id = wm.workforce_member_id
LEFT JOIN locations shed_loc ON vi.tenant_id = shed_loc.tenant_id AND vi.shed_id = shed_loc.location_id
LEFT JOIN locations park_loc ON vi.tenant_id = park_loc.tenant_id AND vi.park_id = park_loc.location_id
WHERE vi.tenant_id = $1::uuid
  AND vi.status = $2
  AND ($3 = '' OR vi.category = $3)
  AND ($4 = '' OR vi.vertical = $4)
  AND ($5 = '' OR vi.module = $5)
  AND ($6::timestamptz IS NULL OR (vi.captured_at, vi.item_id) > ($6::timestamptz, $7::uuid))
ORDER BY vi.captured_at ASC, vi.item_id ASC
LIMIT $8`,
		params.TenantID, params.Status, params.Category, params.Vertical, params.Module,
		cursorCapturedAt, cursorItemID, params.Limit,
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

func (r *Repository) RecordVerdict(ctx context.Context, in domain.Verdict) (domain.Item, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.Item{}, err
	}
	defer rollback(ctx, tx)

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
WHERE tenant_id = $4::uuid AND item_id = $5::uuid AND row_version = $6`,
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
		"tenant_id": in.TenantID,
		"item_id":   itemID,
		"vertical":  in.Vertical,
		"module":    in.Module,
		"category":  in.Category,
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
  'verification.item.pending', 'verification.verdict.approved', 'verification.verdict.rework'
) DO NOTHING`,
		tenantID, eventID, eventType, verificationSchemaVer, aggregateID,
		verificationTopic, envelope, headers, idempotencyKey); err != nil {
		return fmt.Errorf("verification: outbox insert: %w", err)
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
		item                                                  domain.Item
		sourceTaskID, sourceSubmissionID                      *string
		operatorID, shedID, parkID, verifiedBy, verdictReason *string
		mediaJSON                                             []byte
		verifiedAt                                            *time.Time
	)
	if err := row.Scan(
		&item.ItemID, &item.TenantID, &item.Vertical, &item.Module, &item.Category,
		&item.Source.Module, &sourceTaskID, &sourceSubmissionID, &item.Source.RefType, &item.Source.RefID,
		&mediaJSON, &item.Status, &verdictReason, &operatorID, &shedID, &parkID,
		&item.CapturedAt, &verifiedBy, &verifiedAt, &item.RowVersion, &item.CreatedAt, &item.UpdatedAt,
	); err != nil {
		return domain.Item{}, err
	}
	item.Source.TaskID = sourceTaskID
	item.Source.SubmissionID = sourceSubmissionID
	item.VerdictReason = verdictReason
	item.OperatorID = operatorID
	item.ShedID = shedID
	item.ParkID = parkID
	item.VerifiedBy = verifiedBy
	item.VerifiedAt = verifiedAt
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
		item                                                  domain.Item
		sourceTaskID, sourceSubmissionID                      *string
		operatorID, shedID, parkID, verifiedBy, verdictReason *string
		operatorName, shedLabel, parkLabel                    *string
		mediaJSON                                             []byte
		verifiedAt                                            *time.Time
	)
	if err := row.Scan(
		&item.ItemID, &item.TenantID, &item.Vertical, &item.Module, &item.Category,
		&item.Source.Module, &sourceTaskID, &sourceSubmissionID, &item.Source.RefType, &item.Source.RefID,
		&mediaJSON, &item.Status, &verdictReason, &operatorID, &shedID, &parkID,
		&item.CapturedAt, &verifiedBy, &verifiedAt, &item.RowVersion, &item.CreatedAt, &item.UpdatedAt,
		&operatorName, &shedLabel, &parkLabel,
	); err != nil {
		return domain.Item{}, err
	}
	item.Source.TaskID = sourceTaskID
	item.Source.SubmissionID = sourceSubmissionID
	item.VerdictReason = verdictReason
	item.OperatorID = operatorID
	item.OperatorName = operatorName
	item.ShedID = shedID
	item.ShedLabel = shedLabel
	item.ParkID = parkID
	item.ParkLabel = parkLabel
	item.VerifiedBy = verifiedBy
	item.VerifiedAt = verifiedAt
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
