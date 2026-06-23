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
	ids := make([]pgtype.UUID, 0, len(obligationIDs))
	for _, id := range obligationIDs {
		u, err := pgconv.UUID(id)
		if err != nil {
			return 0, fmt.Errorf("obligation: obligation id: %w", err)
		}
		ids = append(ids, u)
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

// CancelOpenForGoat cancels a goat's scheduled/due obligations (SM-3 death/sale) and writes a
// 'canceled' status event for each, in one transaction. Idempotent: a re-run finds no open rows
// and cancels nothing. Completed/accepted/missed history is never touched (WHERE status filter).
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

// ReScopeOpenForGoat moves a shifted goat's still-open, unbatched obligations to a new scope (SM-2)
// and writes a 'rescoped' status event per moved obligation, in one txn. Completed/in-progress/
// batched obligations are untouched. Idempotent: a shift to the same scope moves nothing and writes
// no events. Returns the count re-scoped.
func (r *Repository) ReScopeOpenForGoat(ctx context.Context, tenantID, goatID, scopeType, scopeID string) (int, error) {
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
	scope, err := pgconv.UUID(scopeID)
	if err != nil {
		return 0, fmt.Errorf("obligation: scope id: %w", err)
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("obligation: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := r.queries.WithTx(tx)

	ids, err := qtx.ReScopeOpenObligationsForGoat(ctx, obligationdb.ReScopeOpenObligationsForGoatParams{
		TenantID:  tenant,
		TargetID:  goat,
		ScopeType: scopeType,
		ScopeID:   scope,
	})
	if err != nil {
		return 0, fmt.Errorf("obligation: re-scope open for goat: %w", err)
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

// MarkCompleted marks an obligation completed (SM-5) and writes a 'completed' status event, in one
// txn. Returns completed=false (no-op) when the obligation is already terminal. Idempotent.
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
