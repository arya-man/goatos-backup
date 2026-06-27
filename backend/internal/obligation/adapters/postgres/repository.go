// Package postgres implements the obligation Repository over generated sqlc queries.
package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	obligationdb "github.com/vgoats/goatos/backend/internal/obligation/adapters/postgres/sqlc"
	"github.com/vgoats/goatos/backend/internal/obligation/domain"
	"github.com/vgoats/goatos/backend/internal/obligation/ports"
	"github.com/vgoats/goatos/backend/internal/platform/pgconv"
)

const defaultQueryTimeout = 3 * time.Second

// Repository is the Postgres-backed obligation repository.
type Repository struct {
	pool         *pgxpool.Pool
	queries      *obligationdb.Queries
	queryTimeout time.Duration
}

// NewRepository builds a Repository bound to a pgx pool.
func NewRepository(pool *pgxpool.Pool, queryTimeout time.Duration) *Repository {
	if queryTimeout <= 0 {
		queryTimeout = defaultQueryTimeout
	}
	return &Repository{pool: pool, queries: obligationdb.New(pool), queryTimeout: queryTimeout}
}

var _ ports.Repository = (*Repository)(nil)

func (r *Repository) withTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, r.queryTimeout)
}

// Ping checks pool connectivity.
func (r *Repository) Ping(ctx context.Context) error {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	return r.pool.Ping(ctx)
}

// InsertObligation generates one obligation; idempotent on (tenant_id, idempotency_key).
func (r *Repository) InsertObligation(ctx context.Context, in domain.NewObligation) (string, bool, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tenant, err := pgconv.UUID(in.TenantID)
	if err != nil {
		return "", false, fmt.Errorf("obligation: tenant id: %w", err)
	}
	version, err := pgconv.UUID(in.ProtocolVersionID)
	if err != nil {
		return "", false, fmt.Errorf("obligation: version id: %w", err)
	}
	rule, err := pgconv.UUID(in.RuleID)
	if err != nil {
		return "", false, fmt.Errorf("obligation: rule id: %w", err)
	}
	target, err := pgconv.UUID(in.TargetID)
	if err != nil {
		return "", false, fmt.Errorf("obligation: target id: %w", err)
	}
	scope, err := pgconv.UUID(in.ScopeID)
	if err != nil {
		return "", false, fmt.Errorf("obligation: scope id: %w", err)
	}
	id, err := r.queries.InsertObligationInstance(ctx, obligationdb.InsertObligationInstanceParams{
		TenantID:             tenant,
		ProtocolVersionID:    version,
		RuleID:               rule,
		BatchID:              pgconv.NullableUUID(in.BatchID),
		TargetType:           in.TargetType,
		TargetID:             target,
		ScopeType:            in.ScopeType,
		ScopeID:              scope,
		DueAt:                pgconv.Timestamptz(in.DueAt),
		WindowStart:          pgconv.NullableTimestamptz(in.WindowStart),
		WindowEnd:            pgconv.NullableTimestamptz(in.WindowEnd),
		Status:               in.Status,
		IdempotencyKey:       in.IdempotencyKey,
		GeneratedByTriggerID: pgconv.NullableUUID(in.GeneratedByTriggerID),
		Sequence:             in.Sequence,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil // already generated for this idempotency key
	}
	if err != nil {
		return "", false, fmt.Errorf("obligation: insert instance: %w", err)
	}
	return id, true, nil
}

// GetByIdempotencyKey looks up an obligation by its deterministic key.
func (r *Repository) GetByIdempotencyKey(ctx context.Context, tenantID, idempotencyKey string) (domain.ObligationRef, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tenant, err := pgconv.UUID(tenantID)
	if err != nil {
		return domain.ObligationRef{}, fmt.Errorf("obligation: tenant id: %w", err)
	}
	row, err := r.queries.GetObligationByIdempotencyKey(ctx, obligationdb.GetObligationByIdempotencyKeyParams{TenantID: tenant, IdempotencyKey: idempotencyKey})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ObligationRef{}, ports.ErrNotFound
	}
	if err != nil {
		return domain.ObligationRef{}, fmt.Errorf("obligation: get by idempotency key: %w", err)
	}
	return domain.ObligationRef{
		ObligationID: row.ObligationID,
		Status:       row.Status,
		DueAt:        row.DueAt.Time,
		RowVersion:   row.RowVersion,
	}, nil
}

// ListDue returns obligations in a status whose due_at <= dueBefore.
func (r *Repository) ListDue(ctx context.Context, tenantID, status string, dueBefore time.Time, limit int32) ([]domain.DueObligation, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tenant, err := pgconv.UUID(tenantID)
	if err != nil {
		return nil, fmt.Errorf("obligation: tenant id: %w", err)
	}
	rows, err := r.queries.ListDueObligations(ctx, obligationdb.ListDueObligationsParams{
		TenantID:  tenant,
		Status:    status,
		DueBefore: pgconv.Timestamptz(dueBefore),
		RowLimit:  limit,
	})
	if err != nil {
		return nil, fmt.Errorf("obligation: list due: %w", err)
	}
	out := make([]domain.DueObligation, 0, len(rows))
	for _, row := range rows {
		out = append(out, domain.DueObligation{
			ObligationID:      row.ObligationID,
			ProtocolVersionID: row.ProtocolVersionID,
			RuleID:            row.RuleID,
			TargetType:        row.TargetType,
			TargetID:          row.TargetID,
			ScopeType:         row.ScopeType,
			ScopeID:           row.ScopeID,
			DueAt:             row.DueAt.Time,
			Status:            row.Status,
		})
	}
	return out, nil
}

// CountByScope returns the count of obligations in a status for an (scope_type, scope_id).
func (r *Repository) CountByScope(ctx context.Context, tenantID, scopeType, scopeID, status string) (int64, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tenant, err := pgconv.UUID(tenantID)
	if err != nil {
		return 0, fmt.Errorf("obligation: tenant id: %w", err)
	}
	scope, err := pgconv.UUID(scopeID)
	if err != nil {
		return 0, fmt.Errorf("obligation: scope id: %w", err)
	}
	total, err := r.queries.CountObligationsByScope(ctx, obligationdb.CountObligationsByScopeParams{
		TenantID:  tenant,
		ScopeType: scopeType,
		ScopeID:   scope,
		Status:    status,
	})
	if err != nil {
		return 0, fmt.Errorf("obligation: count by scope: %w", err)
	}
	return total, nil
}

// ListUnbatchedDueForVersion lists unbatched scheduled/due obligations for a version in the window.
func (r *Repository) ListUnbatchedDueForVersion(ctx context.Context, tenantID, versionID string, dueBefore time.Time, limit int32) ([]domain.UnbatchedDue, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tenant, err := pgconv.UUID(tenantID)
	if err != nil {
		return nil, fmt.Errorf("obligation: tenant id: %w", err)
	}
	version, err := pgconv.UUID(versionID)
	if err != nil {
		return nil, fmt.Errorf("obligation: version id: %w", err)
	}
	if limit <= 0 {
		limit = 1000
	}
	rows, err := r.queries.ListUnbatchedDueForVersion(ctx, obligationdb.ListUnbatchedDueForVersionParams{
		TenantID:          tenant,
		ProtocolVersionID: version,
		DueBefore:         pgconv.Timestamptz(dueBefore),
		RowLimit:          limit,
	})
	if err != nil {
		return nil, fmt.Errorf("obligation: list unbatched due: %w", err)
	}
	out := make([]domain.UnbatchedDue, 0, len(rows))
	for _, row := range rows {
		out = append(out, domain.UnbatchedDue{ObligationID: row.ObligationID, ScopeType: row.ScopeType, ScopeID: row.ScopeID})
	}
	return out, nil
}

// AttachObligationsToBatch attaches still-unbatched obligations to a batch (returns count attached).
func (r *Repository) AttachObligationsToBatch(ctx context.Context, tenantID, batchID string, obligationIDs []string) (int64, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tenant, err := pgconv.UUID(tenantID)
	if err != nil {
		return 0, fmt.Errorf("obligation: tenant id: %w", err)
	}
	batch, err := pgconv.UUID(batchID)
	if err != nil {
		return 0, fmt.Errorf("obligation: batch id: %w", err)
	}
	ids, err := obligationUUIDs(obligationIDs)
	if err != nil {
		return 0, err
	}
	n, err := r.queries.AttachObligationsToBatch(ctx, obligationdb.AttachObligationsToBatchParams{
		BatchID:       batch,
		TenantID:      tenant,
		ObligationIds: ids,
	})
	if err != nil {
		return 0, fmt.Errorf("obligation: attach to batch: %w", err)
	}
	return n, nil
}

// CreateBatch inserts a work-unit batch.
func (r *Repository) CreateBatch(ctx context.Context, in domain.NewBatch) (string, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tenant, err := pgconv.UUID(in.TenantID)
	if err != nil {
		return "", fmt.Errorf("obligation: tenant id: %w", err)
	}
	version, err := pgconv.UUID(in.ProtocolVersionID)
	if err != nil {
		return "", fmt.Errorf("obligation: version id: %w", err)
	}
	scope, err := pgconv.UUID(in.ScopeID)
	if err != nil {
		return "", fmt.Errorf("obligation: scope id: %w", err)
	}
	plannedQty, err := pgconv.Numeric(in.PlannedQuantity)
	if err != nil {
		return "", fmt.Errorf("obligation: planned_quantity: %w", err)
	}
	id, err := r.queries.CreateObligationBatch(ctx, obligationdb.CreateObligationBatchParams{
		TenantID:              tenant,
		ProtocolVersionID:     version,
		ScopeType:             in.ScopeType,
		ScopeID:               scope,
		Session:               pgconv.Text(in.Session),
		PlannedDate:           pgconv.Date(in.PlannedDate),
		WindowStart:           pgconv.NullableTimestamptz(in.WindowStart),
		WindowEnd:             pgconv.NullableTimestamptz(in.WindowEnd),
		Status:                in.Status,
		EstimatedTargets:      in.EstimatedTargets,
		PlannedQuantity:       plannedQty,
		QuantityUnit:          pgconv.Text(in.QuantityUnit),
		PrimaryInventoryLotID: pgconv.NullableUUID(in.PrimaryInventoryLotID),
		SopTaskID:             pgconv.NullableUUID(in.SopTaskID),
		ConductedBy:           pgconv.NullableUUID(in.ConductedBy),
	})
	if err != nil {
		return "", fmt.Errorf("obligation: create batch: %w", err)
	}
	return id, nil
}

func (r *Repository) CreateBatchWithObligations(ctx context.Context, in domain.NewBatch, obligationIDs []string) (string, int64, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	if len(obligationIDs) == 0 {
		return "", 0, nil
	}
	tenant, err := pgconv.UUID(in.TenantID)
	if err != nil {
		return "", 0, fmt.Errorf("obligation: tenant id: %w", err)
	}
	version, err := pgconv.UUID(in.ProtocolVersionID)
	if err != nil {
		return "", 0, fmt.Errorf("obligation: version id: %w", err)
	}
	scope, err := pgconv.UUID(in.ScopeID)
	if err != nil {
		return "", 0, fmt.Errorf("obligation: scope id: %w", err)
	}
	plannedQty, err := pgconv.Numeric(in.PlannedQuantity)
	if err != nil {
		return "", 0, fmt.Errorf("obligation: planned_quantity: %w", err)
	}
	ids, err := obligationUUIDs(obligationIDs)
	if err != nil {
		return "", 0, err
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return "", 0, fmt.Errorf("obligation: begin batch attach tx: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()
	qtx := r.queries.WithTx(tx)

	batchID, err := qtx.CreateObligationBatch(ctx, obligationdb.CreateObligationBatchParams{
		TenantID:              tenant,
		ProtocolVersionID:     version,
		ScopeType:             in.ScopeType,
		ScopeID:               scope,
		Session:               pgconv.Text(in.Session),
		PlannedDate:           pgconv.Date(in.PlannedDate),
		WindowStart:           pgconv.NullableTimestamptz(in.WindowStart),
		WindowEnd:             pgconv.NullableTimestamptz(in.WindowEnd),
		Status:                in.Status,
		EstimatedTargets:      in.EstimatedTargets,
		PlannedQuantity:       plannedQty,
		QuantityUnit:          pgconv.Text(in.QuantityUnit),
		PrimaryInventoryLotID: pgconv.NullableUUID(in.PrimaryInventoryLotID),
		SopTaskID:             pgconv.NullableUUID(in.SopTaskID),
		ConductedBy:           pgconv.NullableUUID(in.ConductedBy),
	})
	if err != nil {
		return "", 0, fmt.Errorf("obligation: create batch: %w", err)
	}
	batch, err := pgconv.UUID(batchID)
	if err != nil {
		return "", 0, fmt.Errorf("obligation: batch id: %w", err)
	}
	attached, err := qtx.AttachObligationsToBatch(ctx, obligationdb.AttachObligationsToBatchParams{
		BatchID:       batch,
		TenantID:      tenant,
		ObligationIds: ids,
	})
	if err != nil {
		return "", 0, fmt.Errorf("obligation: attach to batch: %w", err)
	}
	if attached == 0 {
		return "", 0, nil
	}
	if attached != int64(in.EstimatedTargets) {
		if _, err := tx.Exec(ctx, `
UPDATE obligation_batches
SET estimated_targets = $1, updated_at = now()
WHERE tenant_id = $2 AND batch_id = $3`, int32(attached), tenant, batch); err != nil {
			return "", 0, fmt.Errorf("obligation: update batch target count: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return "", 0, fmt.Errorf("obligation: commit batch attach: %w", err)
	}
	committed = true
	return batchID, attached, nil
}

func (r *Repository) SetBatchSOPTask(ctx context.Context, tenantID, batchID, taskID string) error {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tenant, err := pgconv.UUID(tenantID)
	if err != nil {
		return fmt.Errorf("obligation: tenant id: %w", err)
	}
	batch, err := pgconv.UUID(batchID)
	if err != nil {
		return fmt.Errorf("obligation: batch id: %w", err)
	}
	task, err := pgconv.UUID(taskID)
	if err != nil {
		return fmt.Errorf("obligation: sop task id: %w", err)
	}
	tag, err := r.pool.Exec(ctx, `
UPDATE obligation_batches
SET sop_task_id = $1, updated_at = now(), row_version = row_version + 1
WHERE tenant_id = $2
  AND batch_id = $3
  AND (sop_task_id IS NULL OR sop_task_id = $1)`, task, tenant, batch)
	if err != nil {
		return fmt.Errorf("obligation: set batch sop task: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ports.ErrNotFound
	}
	return nil
}

// MarkBatchStockBlocked records an explicit stock-block context on a batch after hard reservation
// failure. The batch remains planned and visible; execution surfaces can show the reason/action.
func (r *Repository) MarkBatchStockBlocked(ctx context.Context, tenantID, batchID, itemID string, requiredQty int64, reason string) error {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tag, err := r.pool.Exec(ctx, `
UPDATE obligation_batches
SET context = context || jsonb_build_object(
      'stock_block', jsonb_build_object(
        'state', 'blocked',
        'item_id', $3,
        'required_qty', $4,
        'reason', $5,
        'blocked_at', now()
      )
    ),
    updated_at = now(),
    row_version = row_version + 1
WHERE tenant_id = $1::uuid
  AND batch_id = $2::uuid`, tenantID, batchID, itemID, requiredQty, reason)
	if err != nil {
		return fmt.Errorf("obligation: mark batch stock blocked: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ports.ErrNotFound
	}
	return nil
}

func (r *Repository) ListPlannedBatchesNeedingFinalization(ctx context.Context, tenantID, versionID string, needsTask, needsStock bool, limit int32) ([]domain.PlannedBatchFinalization, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	if !needsTask && !needsStock {
		return nil, nil
	}
	tenant, err := pgconv.UUID(tenantID)
	if err != nil {
		return nil, fmt.Errorf("obligation: tenant id: %w", err)
	}
	version, err := pgconv.UUID(versionID)
	if err != nil {
		return nil, fmt.Errorf("obligation: version id: %w", err)
	}
	if limit <= 0 {
		limit = 1000
	}
	rows, err := r.pool.Query(ctx, `
SELECT ob.batch_id::text,
       ob.scope_type,
       ob.scope_id::text,
       ob.estimated_targets,
       COUNT(oi.obligation_id)::bigint AS attached_obligations,
       (ob.sop_task_id IS NOT NULL) AS has_sop_task,
       EXISTS (
         SELECT 1
         FROM inventory_stock_movements ism
         WHERE ism.tenant_id = ob.tenant_id
           AND ism.batch_id = ob.batch_id
           AND ism.movement_type = 'reserve'
       ) AS has_stock_reservation,
       (ob.context ? 'stock_block') AS stock_blocked
FROM obligation_batches ob
JOIN obligation_instances oi
  ON oi.tenant_id = ob.tenant_id
 AND oi.batch_id = ob.batch_id
 AND oi.status IN ('scheduled', 'due', 'in_progress', 'missed')
WHERE ob.tenant_id = $1
  AND ob.protocol_version_id = $2
  AND ob.status = 'planned'
GROUP BY ob.tenant_id, ob.batch_id, ob.scope_type, ob.scope_id, ob.estimated_targets, ob.sop_task_id, ob.context, ob.created_at
HAVING COUNT(oi.obligation_id) > 0
   AND (
     ($3::boolean AND ob.sop_task_id IS NULL)
     OR (
       $4::boolean
       AND NOT (ob.context ? 'stock_block')
       AND NOT EXISTS (
         SELECT 1
         FROM inventory_stock_movements ism
         WHERE ism.tenant_id = ob.tenant_id
           AND ism.batch_id = ob.batch_id
           AND ism.movement_type = 'reserve'
       )
     )
   )
ORDER BY ob.created_at ASC, ob.batch_id ASC
LIMIT $5`, tenant, version, needsTask, needsStock, limit)
	if err != nil {
		return nil, fmt.Errorf("obligation: list planned batch finalization: %w", err)
	}
	defer rows.Close()
	out := make([]domain.PlannedBatchFinalization, 0)
	for rows.Next() {
		var b domain.PlannedBatchFinalization
		if err := rows.Scan(
			&b.BatchID,
			&b.ScopeType,
			&b.ScopeID,
			&b.EstimatedTargets,
			&b.AttachedObligations,
			&b.HasSOPTask,
			&b.HasStockReservation,
			&b.StockBlocked,
		); err != nil {
			return nil, fmt.Errorf("obligation: scan planned batch finalization: %w", err)
		}
		out = append(out, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("obligation: list planned batch finalization rows: %w", err)
	}
	return out, nil
}

func obligationUUIDs(values []string) ([]pgtype.UUID, error) {
	ids := make([]pgtype.UUID, 0, len(values))
	for _, id := range values {
		u, err := pgconv.UUID(id)
		if err != nil {
			return nil, fmt.Errorf("obligation: obligation id: %w", err)
		}
		ids = append(ids, u)
	}
	return ids, nil
}

// CancelOpenForGoat cancels a goat's scheduled/due/in_progress obligations (SM-3 death/sale) and
// writes a 'canceled' status event for each, in one transaction. Idempotent: a re-run finds no open
// rows and cancels nothing. Completed/accepted/missed history is never touched (WHERE status filter).
func (r *Repository) CancelOpenForGoat(ctx context.Context, tenantID, goatID, reason string) (int, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tenant, err := pgconv.UUID(tenantID)
	if err != nil {
		return 0, fmt.Errorf("obligation: tenant id: %w", err)
	}
	goat, err := pgconv.UUID(goatID)
	if err != nil {
		return 0, fmt.Errorf("obligation: goat id: %w", err)
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("obligation: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := r.queries.WithTx(tx)

	ids, err := qtx.CancelOpenObligationsForGoat(ctx, obligationdb.CancelOpenObligationsForGoatParams{
		TenantID: tenant,
		TargetID: goat,
	})
	if err != nil {
		return 0, fmt.Errorf("obligation: cancel open for goat: %w", err)
	}
	payload, _ := json.Marshal(map[string]string{"reason": reason})
	now := time.Now()
	for _, id := range ids {
		oid, err := pgconv.UUID(id)
		if err != nil {
			return 0, fmt.Errorf("obligation: obligation id: %w", err)
		}
		if _, err := qtx.InsertObligationStatusEvent(ctx, obligationdb.InsertObligationStatusEventParams{
			TenantID:       tenant,
			ObligationID:   oid,
			EventType:      "canceled",
			OccurredAt:     pgconv.Timestamptz(now),
			Payload:        payload,
			IdempotencyKey: id + ":canceled",
		}); err != nil {
			return 0, fmt.Errorf("obligation: cancel event: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("obligation: commit cancel: %w", err)
	}
	return len(ids), nil
}

// ReScopeOpenForGoat moves a shifted goat's still-open obligations to a new scope (SM-2) and writes
// a 'rescoped' status event per moved obligation, in one txn. Unbatched rows are re-scoped in place.
// Rows already attached to a still-planned batch are detached from the old batch, the old batch count
// is reduced, and the obligation is left unbatched for the destination-shed sweeper to merge/create
// the target drive. In-progress/completed batches are not touched; those require an execution repair
// exception because field work may already have started.
func (r *Repository) ReScopeOpenForGoat(ctx context.Context, tenantID, goatID, scopeType, scopeID string) (int, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tenant, err := pgconv.UUID(tenantID)
	if err != nil {
		return 0, fmt.Errorf("obligation: tenant id: %w", err)
	}
	if _, err := pgconv.UUID(goatID); err != nil {
		return 0, fmt.Errorf("obligation: goat id: %w", err)
	}
	if _, err := pgconv.UUID(scopeID); err != nil {
		return 0, fmt.Errorf("obligation: scope id: %w", err)
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("obligation: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := r.queries.WithTx(tx)

	ids, oldBatches, err := reScopeOpenObligationsForGoat(ctx, tx, tenantID, goatID, scopeType, scopeID)
	if err != nil {
		return 0, fmt.Errorf("obligation: re-scope open for goat: %w", err)
	}
	for batchID, count := range oldBatches {
		if _, err := tx.Exec(ctx, `
UPDATE obligation_batches
SET estimated_targets = GREATEST(0, estimated_targets - $3::int),
    context = CASE
      WHEN reserved_quantity > 0 THEN context || jsonb_build_object(
        'shift_repair', jsonb_build_object(
          'state', 'stock_reconcile_required',
          'moved_target_id', $4,
          'reason', 'goat_shifted_after_batch_planned',
          'recorded_at', now()
        )
      )
      ELSE context
    END,
    updated_at = now(),
    row_version = row_version + 1
WHERE tenant_id = $1::uuid
  AND batch_id = $2::uuid`, tenantID, batchID, count, goatID); err != nil {
			return 0, fmt.Errorf("obligation: update old shift batch: %w", err)
		}
	}
	payload, _ := json.Marshal(map[string]string{"scope_type": scopeType, "scope_id": scopeID})
	now := time.Now()
	for _, id := range ids {
		oid, err := pgconv.UUID(id)
		if err != nil {
			return 0, fmt.Errorf("obligation: obligation id: %w", err)
		}
		if _, err := qtx.InsertObligationStatusEvent(ctx, obligationdb.InsertObligationStatusEventParams{
			TenantID:       tenant,
			ObligationID:   oid,
			EventType:      "rescoped",
			OccurredAt:     pgconv.Timestamptz(now),
			Payload:        payload,
			IdempotencyKey: id + ":rescoped:" + scopeID,
		}); err != nil {
			return 0, fmt.Errorf("obligation: rescoped event: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("obligation: commit re-scope: %w", err)
	}
	return len(ids), nil
}

func reScopeOpenObligationsForGoat(ctx context.Context, tx pgx.Tx, tenantID, goatID, scopeType, scopeID string) ([]string, map[string]int, error) {
	rows, err := tx.Query(ctx, `
UPDATE obligation_instances
SET scope_type = $3,
    scope_id = $4::uuid,
    row_version = row_version + 1,
    updated_at = now()
WHERE tenant_id = $1::uuid
  AND target_type = 'goat'
  AND target_id = $2::uuid
  AND status IN ('scheduled', 'due')
  AND batch_id IS NULL
  AND (scope_type IS DISTINCT FROM $3 OR scope_id IS DISTINCT FROM $4::uuid)
RETURNING obligation_id::text`, tenantID, goatID, scopeType, scopeID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}

	rows, err = tx.Query(ctx, `
UPDATE obligation_instances oi
SET scope_type = $3,
    scope_id = $4::uuid,
    batch_id = NULL,
    row_version = oi.row_version + 1,
    updated_at = now()
FROM obligation_batches ob
WHERE oi.tenant_id = $1::uuid
  AND oi.target_type = 'goat'
  AND oi.target_id = $2::uuid
  AND oi.status IN ('scheduled', 'due')
  AND oi.batch_id = ob.batch_id
  AND ob.tenant_id = oi.tenant_id
  AND ob.status = 'planned'
  AND (oi.scope_type IS DISTINCT FROM $3 OR oi.scope_id IS DISTINCT FROM $4::uuid)
RETURNING oi.obligation_id::text, ob.batch_id::text`, tenantID, goatID, scopeType, scopeID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	oldBatches := make(map[string]int)
	for rows.Next() {
		var id, batchID string
		if err := rows.Scan(&id, &batchID); err != nil {
			return nil, nil, err
		}
		ids = append(ids, id)
		oldBatches[batchID]++
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	return ids, oldBatches, nil
}

// MarkCompleted marks an obligation completed (SM-5) and writes a 'completed' status event, in one
// txn. Missed obligations can complete late; the missed event remains as audit history. Returns
// completed=false (no-op) when the obligation is already closed. Idempotent.
func (r *Repository) MarkCompleted(ctx context.Context, tenantID, obligationID string) (bool, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tenant, err := pgconv.UUID(tenantID)
	if err != nil {
		return false, fmt.Errorf("obligation: tenant id: %w", err)
	}
	obl, err := pgconv.UUID(obligationID)
	if err != nil {
		return false, fmt.Errorf("obligation: obligation id: %w", err)
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("obligation: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := r.queries.WithTx(tx)

	n, err := qtx.MarkObligationCompleted(ctx, obligationdb.MarkObligationCompletedParams{TenantID: tenant, ObligationID: obl})
	if err != nil {
		return false, fmt.Errorf("obligation: mark completed: %w", err)
	}
	if n == 0 {
		if cerr := tx.Commit(ctx); cerr != nil {
			return false, fmt.Errorf("obligation: commit noop complete: %w", cerr)
		}
		return false, nil
	}
	if _, err := qtx.InsertObligationStatusEvent(ctx, obligationdb.InsertObligationStatusEventParams{
		TenantID:       tenant,
		ObligationID:   obl,
		EventType:      "completed",
		OccurredAt:     pgconv.Timestamptz(time.Now()),
		Payload:        []byte(`{"event":"completed"}`),
		IdempotencyKey: obligationID + ":completed",
	}); err != nil {
		return false, fmt.Errorf("obligation: completed event: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("obligation: commit complete: %w", err)
	}
	return true, nil
}

// MarkMissedBefore marks open obligations whose due window has crossed as missed and writes a
// durable 'missed' status event per transition. It is safe for repeated/parallel sweepers: candidates
// are locked with SKIP LOCKED and only scheduled/due rows can transition.
func (r *Repository) MarkMissedBefore(ctx context.Context, tenantID string, missedBefore time.Time, limit int32) (int, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tenant, err := pgconv.UUID(tenantID)
	if err != nil {
		return 0, fmt.Errorf("obligation: tenant id: %w", err)
	}
	if missedBefore.IsZero() {
		missedBefore = time.Now().UTC()
	}
	if limit <= 0 {
		limit = 1000
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("obligation: begin missed tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	rows, err := tx.Query(ctx, `
WITH candidate AS (
  SELECT obligation_id
  FROM obligation_instances
  WHERE tenant_id = $1
    AND status IN ('scheduled', 'due')
    AND COALESCE(window_end, due_at) < $2
  ORDER BY COALESCE(window_end, due_at) ASC, obligation_id ASC
  LIMIT $3
  FOR UPDATE SKIP LOCKED
)
UPDATE obligation_instances oi
SET status = 'missed',
    row_version = oi.row_version + 1,
    updated_at = now()
FROM candidate c
WHERE oi.tenant_id = $1
  AND oi.obligation_id = c.obligation_id
RETURNING oi.obligation_id::text`, tenant, pgconv.Timestamptz(missedBefore), limit)
	if err != nil {
		return 0, fmt.Errorf("obligation: mark missed: %w", err)
	}
	defer rows.Close()

	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return 0, fmt.Errorf("obligation: scan missed id: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("obligation: mark missed rows: %w", err)
	}
	rows.Close()

	qtx := r.queries.WithTx(tx)
	payload, _ := json.Marshal(map[string]string{"event": "missed"})
	now := time.Now().UTC()
	for _, id := range ids {
		oid, err := pgconv.UUID(id)
		if err != nil {
			return 0, fmt.Errorf("obligation: obligation id: %w", err)
		}
		if _, err := qtx.InsertObligationStatusEvent(ctx, obligationdb.InsertObligationStatusEventParams{
			TenantID:       tenant,
			ObligationID:   oid,
			EventType:      "missed",
			OccurredAt:     pgconv.Timestamptz(now),
			Payload:        payload,
			IdempotencyKey: id + ":missed",
		}); err != nil {
			return 0, fmt.Errorf("obligation: missed event: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("obligation: commit missed: %w", err)
	}
	return len(ids), nil
}

// ListOpenByGoat returns a goat's still-open obligations, earliest due first (Goat Passport next-due).
func (r *Repository) ListOpenByGoat(ctx context.Context, tenantID, goatID string, limit int32) ([]domain.OpenObligation, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tenant, err := pgconv.UUID(tenantID)
	if err != nil {
		return nil, fmt.Errorf("obligation: tenant id: %w", err)
	}
	goat, err := pgconv.UUID(goatID)
	if err != nil {
		return nil, fmt.Errorf("obligation: goat id: %w", err)
	}
	if limit <= 0 {
		limit = 100
	}
	rows, err := r.queries.ListOpenObligationsByGoat(ctx, obligationdb.ListOpenObligationsByGoatParams{
		TenantID: tenant, TargetID: goat, RowLimit: limit,
	})
	if err != nil {
		return nil, fmt.Errorf("obligation: list open by goat: %w", err)
	}
	out := make([]domain.OpenObligation, 0, len(rows))
	for _, row := range rows {
		out = append(out, domain.OpenObligation{
			ObligationID:      row.ObligationID,
			ProtocolVersionID: row.ProtocolVersionID,
			RuleID:            row.RuleID,
			ScopeType:         row.ScopeType,
			ScopeID:           row.ScopeID,
			DueAt:             row.DueAt.Time,
			Status:            row.Status,
			Sequence:          row.Sequence,
		})
	}
	return out, nil
}

// GetBoosterContext returns an obligation's protocol version, scope, and sequence (SM-7 basis on the
// verify path). Returns ports.ErrNotFound when the obligation does not exist.
func (r *Repository) GetBoosterContext(ctx context.Context, tenantID, obligationID string) (versionID, scopeType, scopeID string, sequence int32, err error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tenant, err := pgconv.UUID(tenantID)
	if err != nil {
		return "", "", "", 0, fmt.Errorf("obligation: tenant id: %w", err)
	}
	obl, err := pgconv.UUID(obligationID)
	if err != nil {
		return "", "", "", 0, fmt.Errorf("obligation: obligation id: %w", err)
	}
	row, err := r.queries.GetObligationBoosterContext(ctx, obligationdb.GetObligationBoosterContextParams{
		TenantID:     tenant,
		ObligationID: obl,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", "", 0, ports.ErrNotFound
	}
	if err != nil {
		return "", "", "", 0, fmt.Errorf("obligation: get booster context: %w", err)
	}
	return row.ProtocolVersionID, row.ScopeType, row.ScopeID, row.Sequence, nil
}

// RecordStatusEvent appends a status event with a reserve-before-insert idempotency guard, in
// one transaction. Returns applied=false on retry (key already reserved) — no duplicate event.
func (r *Repository) RecordStatusEvent(ctx context.Context, ev domain.NewStatusEvent) (string, bool, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tenant, err := pgconv.UUID(ev.TenantID)
	if err != nil {
		return "", false, fmt.Errorf("obligation: tenant id: %w", err)
	}
	obligationID, err := pgconv.UUID(ev.ObligationID)
	if err != nil {
		return "", false, fmt.Errorf("obligation: obligation id: %w", err)
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return "", false, fmt.Errorf("obligation: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := r.queries.WithTx(tx)

	// Reserve the idempotency key. ON CONFLICT DO NOTHING -> no row on retry.
	if _, err := qtx.ReserveIdempotencyKey(ctx, obligationdb.ReserveIdempotencyKeyParams{
		IdempotencyKey: ev.IdempotencyKey,
		TenantID:       tenant,
		Scope:          ev.Scope,
		RequestHash:    ev.RequestHash,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Already reserved: this is a retry. Do not insert a second event.
			if cerr := tx.Commit(ctx); cerr != nil {
				return "", false, fmt.Errorf("obligation: commit replay: %w", cerr)
			}
			return "", false, nil
		}
		return "", false, fmt.Errorf("obligation: reserve idempotency key: %w", err)
	}

	eventID, err := qtx.InsertObligationStatusEvent(ctx, obligationdb.InsertObligationStatusEventParams{
		TenantID:       tenant,
		ObligationID:   obligationID,
		EventType:      ev.EventType,
		OccurredAt:     pgconv.Timestamptz(ev.OccurredAt),
		ActorID:        pgconv.NullableUUID(ev.ActorID),
		Payload:        pgconv.JSONB(ev.Payload),
		IdempotencyKey: ev.IdempotencyKey,
	})
	if err != nil {
		return "", false, fmt.Errorf("obligation: insert status event: %w", err)
	}

	eventUUID, err := pgconv.UUID(eventID)
	if err != nil {
		return "", false, fmt.Errorf("obligation: event id: %w", err)
	}
	if err := qtx.CompleteIdempotencyKey(ctx, obligationdb.CompleteIdempotencyKeyParams{
		ResultType:     pgconv.Text("obligation_status_event"),
		ResultID:       eventUUID,
		IdempotencyKey: ev.IdempotencyKey,
	}); err != nil {
		return "", false, fmt.Errorf("obligation: complete idempotency key: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return "", false, fmt.Errorf("obligation: commit: %w", err)
	}
	return eventID, true, nil
}
