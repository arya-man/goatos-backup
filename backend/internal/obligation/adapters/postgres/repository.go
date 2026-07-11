// Package postgres implements the obligation Repository over generated sqlc queries.
package postgres

import (
	"context"
	"crypto/md5"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	obligationdb "github.com/vgoats/goatos/backend/internal/obligation/adapters/postgres/sqlc"
	"github.com/vgoats/goatos/backend/internal/obligation/domain"
	"github.com/vgoats/goatos/backend/internal/obligation/ports"
	"github.com/vgoats/goatos/backend/internal/platform/audit"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	platformoutbox "github.com/vgoats/goatos/backend/internal/platform/outbox"
	"github.com/vgoats/goatos/backend/internal/platform/pgconv"
)

const defaultQueryTimeout = 3 * time.Second

const (
	vaccinationCompletedEventType     = "vaccination.completed"
	vaccinationCompletedSchemaVersion = "1.0.0"
	vaccinationCompletedSchemaRef     = "domain-event-envelope.v1"
	vaccinationCompletedTopic         = "vaccination.events"
	obligationMissedEventType         = "obligation.missed"
	obligationMissedSchemaVersion     = "1.0.0"
	obligationMissedSchemaRef         = "domain-event-envelope.v1"
	obligationMissedTopic             = "obligation.events"
	obligationCanceledEventType       = "goat.obligations_canceled"
	obligationRescopedEventType       = "obligation.rescoped"
	obligationRegeneratedEventType    = "obligation.regenerated"
	obligationMissedBatchRepairAction = "obligation.missed_batch_repaired"
)

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
		id, reopened, reopenErr := r.reopenTerminalObligationForInsert(ctx, tenant, in)
		if reopenErr != nil {
			return "", false, reopenErr
		}
		if reopened {
			return id, true, nil
		}
		return "", false, nil // already generated for this idempotency key
	}
	if err != nil {
		return "", false, fmt.Errorf("obligation: insert instance: %w", err)
	}
	return id, true, nil
}

func (r *Repository) reopenTerminalObligationForInsert(ctx context.Context, tenant pgtype.UUID, in domain.NewObligation) (string, bool, error) {
	scope, err := pgconv.UUID(in.ScopeID)
	if err != nil {
		return "", false, fmt.Errorf("obligation: reopen terminal scope id: %w", err)
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return "", false, fmt.Errorf("obligation: begin reopen terminal tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := r.queries.WithTx(tx)

	var id string
	occurredAt := time.Now().UTC()
	err = tx.QueryRow(ctx, `
UPDATE obligation_instances oi
SET status = $3,
    batch_id = NULL,
    scope_type = $4,
    scope_id = $5,
    due_at = $6,
    window_start = $7,
    window_end = $8,
    row_version = row_version + 1,
    updated_at = now()
WHERE oi.tenant_id = $1
  AND oi.idempotency_key = $2
  AND oi.status IN ('canceled', 'missed')
  AND EXISTS (
    SELECT 1
    FROM goats g
    JOIN protocol_versions pv
      ON pv.tenant_id = oi.tenant_id
     AND pv.protocol_version_id = oi.protocol_version_id
    JOIN protocol_definitions pd
      ON pd.tenant_id = pv.tenant_id
     AND pd.protocol_id = pv.protocol_id
    WHERE oi.target_type = 'goat'
      AND g.tenant_id = oi.tenant_id
      AND g.goat_id = oi.target_id
      AND pd.category = 'vaccination'
      AND g.lifecycle_status NOT IN ('dead', 'sold', 'lost', 'culled', 'transferred', 'merged', 'inactive')
      AND g.merged_into_goat_id IS NULL
      AND NOT EXISTS (
        SELECT 1
        FROM vw_procurement_vaccination_excluded_goats ex
        WHERE ex.tenant_id = g.tenant_id
          AND ex.goat_id = g.goat_id
      )
  )
RETURNING oi.obligation_id::text`,
		tenant,
		in.IdempotencyKey,
		in.Status,
		in.ScopeType,
		scope,
		pgconv.Timestamptz(in.DueAt),
		pgconv.NullableTimestamptz(in.WindowStart),
		pgconv.NullableTimestamptz(in.WindowEnd),
	).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("obligation: reopen terminal insert target: %w", err)
	}

	eventKey := id + ":regenerated:" + in.DueAt.UTC().Format(time.RFC3339Nano)
	_, reserveErr := qtx.ReserveIdempotencyKey(ctx, obligationdb.ReserveIdempotencyKeyParams{
		IdempotencyKey: eventKey,
		TenantID:       tenant,
		Scope:          "obligation.status_event",
		RequestHash:    "regenerate:" + in.Status,
	})
	if reserveErr != nil && !errors.Is(reserveErr, pgx.ErrNoRows) {
		return "", false, fmt.Errorf("obligation: reserve regenerated event key: %w", reserveErr)
	}
	if reserveErr == nil {
		oblUUID, err := pgconv.UUID(id)
		if err != nil {
			return "", false, fmt.Errorf("obligation: regenerated obligation id: %w", err)
		}
		payload, _ := json.Marshal(map[string]string{
			"reason":             "generation_requalified_terminal_row",
			"rescheduled_due_at": in.DueAt.UTC().Format(time.RFC3339),
		})
		eventID, err := qtx.InsertObligationStatusEvent(ctx, obligationdb.InsertObligationStatusEventParams{
			TenantID:       tenant,
			ObligationID:   oblUUID,
			EventType:      "scheduled",
			OccurredAt:     pgconv.Timestamptz(occurredAt),
			Payload:        pgconv.JSONB(payload),
			IdempotencyKey: eventKey,
		})
		if err != nil {
			return "", false, fmt.Errorf("obligation: insert regenerated event: %w", err)
		}
		eventUUID, err := pgconv.UUID(eventID)
		if err != nil {
			return "", false, fmt.Errorf("obligation: regenerated event id: %w", err)
		}
		if err := qtx.CompleteIdempotencyKey(ctx, obligationdb.CompleteIdempotencyKeyParams{
			ResultType:     pgconv.Text("obligation_status_event"),
			ResultID:       eventUUID,
			IdempotencyKey: eventKey,
		}); err != nil {
			return "", false, fmt.Errorf("obligation: complete regenerated event key: %w", err)
		}
	}
	if err := insertObligationLifecycleOutbox(ctx, tx, in.TenantID, id, obligationRegeneratedEventType, "scheduled", occurredAt, map[string]any{
		"reason":             "generation_requalified_terminal_row",
		"rescheduled_due_at": in.DueAt.UTC().Format(time.RFC3339),
		"scope_type":         in.ScopeType,
		"scope_id":           in.ScopeID,
	}, "obligation.InsertObligation"); err != nil {
		return "", false, err
	}
	if err := audit.NewTxRecorder(tx).Record(ctx, audit.Event{
		TenantID:     in.TenantID,
		ActorType:    "system",
		Action:       obligationRegeneratedEventType,
		ResourceType: "obligation_instance",
		ResourceID:   id,
		ScopeType:    "obligation.status_event",
		ScopeID:      id,
		AfterState: map[string]any{
			"status":      "scheduled",
			"due_at":      in.DueAt.UTC().Format(time.RFC3339),
			"occurred_at": occurredAt.Format(time.RFC3339Nano),
		},
		Metadata: map[string]any{
			"source": "vaccination_generation",
		},
		TraceID: obligationRegeneratedEventType + ":" + id,
	}); err != nil {
		return "", false, fmt.Errorf("obligation: regenerated audit: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return "", false, fmt.Errorf("obligation: commit regenerated: %w", err)
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

// DeferOpenObligationByIdempotencyKey moves an existing scheduled/due obligation into the
// canonical deferred state during goat rechecks. If the row was still in a planned batch, it is
// detached so the held goat is not executed by an already-created drive.
func (r *Repository) DeferOpenObligationByIdempotencyKey(ctx context.Context, tenantID, idempotencyKey, reason string, occurredAt time.Time) (string, bool, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tenant, err := pgconv.UUID(tenantID)
	if err != nil {
		return "", false, fmt.Errorf("obligation: tenant id: %w", err)
	}
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return "", false, fmt.Errorf("obligation: begin defer tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var obligationID, oldBatchID, oldBatchStatus string
	err = tx.QueryRow(ctx, `
SELECT oi.obligation_id::text,
       COALESCE(oi.batch_id::text, '')::text AS batch_id,
       COALESCE(ob.status, '')::text AS batch_status
FROM obligation_instances oi
LEFT JOIN obligation_batches ob
  ON ob.tenant_id = oi.tenant_id
 AND ob.batch_id = oi.batch_id
WHERE oi.tenant_id = $1
  AND oi.idempotency_key = $2
  AND oi.status IN ('scheduled', 'due')
FOR UPDATE OF oi`, tenant, idempotencyKey).Scan(&obligationID, &oldBatchID, &oldBatchStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		row, lookupErr := r.queries.WithTx(tx).GetObligationByIdempotencyKey(ctx, obligationdb.GetObligationByIdempotencyKeyParams{
			TenantID:       tenant,
			IdempotencyKey: idempotencyKey,
		})
		if errors.Is(lookupErr, pgx.ErrNoRows) {
			return "", false, ports.ErrNotFound
		}
		if lookupErr != nil {
			return "", false, fmt.Errorf("obligation: lookup defer replay: %w", lookupErr)
		}
		if cerr := tx.Commit(ctx); cerr != nil {
			return "", false, fmt.Errorf("obligation: commit defer replay: %w", cerr)
		}
		return row.ObligationID, false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("obligation: lock defer target: %w", err)
	}

	detachPlannedBatch := oldBatchID != "" && oldBatchStatus == "planned"
	tag, err := tx.Exec(ctx, `
UPDATE obligation_instances
SET status = 'deferred',
    batch_id = CASE WHEN $3::boolean THEN NULL ELSE batch_id END,
    row_version = row_version + 1,
    updated_at = now()
WHERE tenant_id = $1
  AND obligation_id = $2::uuid
  AND status IN ('scheduled', 'due')`, tenant, obligationID, detachPlannedBatch)
	if err != nil {
		return "", false, fmt.Errorf("obligation: defer open obligation: %w", err)
	}
	if tag.RowsAffected() == 0 {
		if cerr := tx.Commit(ctx); cerr != nil {
			return "", false, fmt.Errorf("obligation: commit defer raced noop: %w", cerr)
		}
		return obligationID, false, nil
	}

	if detachPlannedBatch {
		if _, err := tx.Exec(ctx, `
WITH reserved AS (
  SELECT COALESCE(SUM(quantity), 0)::numeric AS qty
  FROM inventory_stock_movements
  WHERE tenant_id = $1
    AND batch_id = $2::uuid
    AND movement_type = 'reserve'
),
repair AS (
  SELECT (
    CASE WHEN context #>> '{defer_repair,state}' = 'stock_reconcile_required'
         THEN COALESCE(NULLIF(context #>> '{defer_repair,release_qty}', '')::numeric, 0)
         ELSE 0 END
    + CASE WHEN context #>> '{shift_repair,state}' = 'stock_reconcile_required'
         THEN COALESCE(NULLIF(context #>> '{shift_repair,release_qty}', '')::numeric, 0)
         ELSE 0 END
    + CASE WHEN context #>> '{cancel_repair,state}' = 'stock_reconcile_required'
         THEN COALESCE(NULLIF(context #>> '{cancel_repair,release_qty}', '')::numeric, 0)
         ELSE 0 END
    + CASE WHEN context #>> '{missed_repair,state}' = 'stock_reconcile_required'
         THEN COALESCE(NULLIF(context #>> '{missed_repair,release_qty}', '')::numeric, 0)
         ELSE 0 END
  )::numeric AS pending_release
  FROM obligation_batches
  WHERE tenant_id = $1
    AND batch_id = $2::uuid
)
UPDATE obligation_batches ob
SET estimated_targets = GREATEST(0, estimated_targets - 1),
    context = CASE
      WHEN reserved.qty > 0 THEN context || jsonb_build_object(
        'defer_repair', jsonb_build_object(
          'state', 'stock_reconcile_required',
          'held_obligation_id', $3::text,
          'reason', $4::text,
          'release_qty',
            (CASE
              WHEN context #>> '{defer_repair,state}' = 'stock_reconcile_required'
              THEN COALESCE(NULLIF(context #>> '{defer_repair,release_qty}', '')::numeric, 0)
              ELSE 0
            END) + LEAST(
              GREATEST(0, reserved.qty - repair.pending_release),
              GREATEST(0, reserved.qty - repair.pending_release) / GREATEST(ob.estimated_targets, 1)
            ),
          'recorded_at', now()
        )
      )
      ELSE context
    END,
    updated_at = now(),
    row_version = row_version + 1
FROM reserved, repair
WHERE tenant_id = $1
  AND batch_id = $2::uuid`, tenant, oldBatchID, obligationID, reason); err != nil {
			return "", false, fmt.Errorf("obligation: update deferred planned batch: %w", err)
		}
	}

	qtx := r.queries.WithTx(tx)
	// occurredAt suffix lets repeated defer cycles (defer -> recover -> defer) each record an event,
	// symmetric with the reopen event key.
	eventKey := obligationID + ":deferred:" + occurredAt.UTC().Format(time.RFC3339Nano)
	_, reserveErr := qtx.ReserveIdempotencyKey(ctx, obligationdb.ReserveIdempotencyKeyParams{
		IdempotencyKey: eventKey,
		TenantID:       tenant,
		Scope:          "obligation.status_event",
		RequestHash:    "defer:" + reason,
	})
	if reserveErr != nil && !errors.Is(reserveErr, pgx.ErrNoRows) {
		return "", false, fmt.Errorf("obligation: reserve deferred event key: %w", reserveErr)
	}
	if reserveErr == nil {
		oblUUID, err := pgconv.UUID(obligationID)
		if err != nil {
			return "", false, fmt.Errorf("obligation: deferred obligation id: %w", err)
		}
		payload, _ := json.Marshal(map[string]string{"reason": "defer_state", "defer_status": reason})
		eventID, err := qtx.InsertObligationStatusEvent(ctx, obligationdb.InsertObligationStatusEventParams{
			TenantID:       tenant,
			ObligationID:   oblUUID,
			EventType:      "deferred",
			OccurredAt:     pgconv.Timestamptz(occurredAt),
			Payload:        pgconv.JSONB(payload),
			IdempotencyKey: eventKey,
		})
		if err != nil {
			return "", false, fmt.Errorf("obligation: insert deferred event: %w", err)
		}
		eventUUID, err := pgconv.UUID(eventID)
		if err != nil {
			return "", false, fmt.Errorf("obligation: deferred event id: %w", err)
		}
		if err := qtx.CompleteIdempotencyKey(ctx, obligationdb.CompleteIdempotencyKeyParams{
			ResultType:     pgconv.Text("obligation_status_event"),
			ResultID:       eventUUID,
			IdempotencyKey: eventKey,
		}); err != nil {
			return "", false, fmt.Errorf("obligation: complete deferred event key: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return "", false, fmt.Errorf("obligation: commit defer: %w", err)
	}
	return obligationID, true, nil
}

// ReopenDeferredObligationByIdempotencyKey flips a still-'deferred' obligation back to 'scheduled'
// when a goat recovers from its defer state (sick/ICU/quarantine), and records a 'scheduled' status
// event in the same transaction. When reschedule is set, due_at is moved to align with a nearby
// planned drive or to recovery time for an immediate micro-drive. changed is false (a safe
// recovery-recheck replay) when no deferred row matches the key.
func (r *Repository) ReopenDeferredObligationByIdempotencyKey(ctx context.Context, tenantID, idempotencyKey string, occurredAt time.Time, reschedule *domain.RecoveryReschedule) (string, bool, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tenant, err := pgconv.UUID(tenantID)
	if err != nil {
		return "", false, fmt.Errorf("obligation: tenant id: %w", err)
	}
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return "", false, fmt.Errorf("obligation: begin reopen tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := r.queries.WithTx(tx)

	var obligationID string
	if reschedule != nil {
		obligationID, err = qtx.ReopenDeferredObligationForKeyWithDue(ctx, obligationdb.ReopenDeferredObligationForKeyWithDueParams{
			TenantID:               tenant,
			IdempotencyKey:         idempotencyKey,
			RescheduledDueAt:       pgconv.Timestamptz(reschedule.DueAt),
			RescheduledWindowStart: pgconv.Timestamptz(reschedule.WindowStart),
			RescheduledWindowEnd:   pgconv.NullableTimestamptz(reschedule.WindowEnd),
		})
	} else {
		obligationID, err = qtx.ReopenDeferredObligationForKey(ctx, obligationdb.ReopenDeferredObligationForKeyParams{
			TenantID:       tenant,
			IdempotencyKey: idempotencyKey,
		})
	}
	if errors.Is(err, pgx.ErrNoRows) {
		if cerr := tx.Commit(ctx); cerr != nil {
			return "", false, fmt.Errorf("obligation: commit reopen noop: %w", cerr)
		}
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("obligation: reopen deferred obligation: %w", err)
	}

	eventKey := obligationID + ":reopened:" + occurredAt.UTC().Format(time.RFC3339Nano)
	_, reserveErr := qtx.ReserveIdempotencyKey(ctx, obligationdb.ReserveIdempotencyKeyParams{
		IdempotencyKey: eventKey,
		TenantID:       tenant,
		Scope:          "obligation.status_event",
		RequestHash:    "reopen:recovered",
	})
	if reserveErr != nil && !errors.Is(reserveErr, pgx.ErrNoRows) {
		return "", false, fmt.Errorf("obligation: reserve reopen event key: %w", reserveErr)
	}
	if reserveErr == nil {
		oblUUID, err := pgconv.UUID(obligationID)
		if err != nil {
			return "", false, fmt.Errorf("obligation: reopened obligation id: %w", err)
		}
		payloadFields := map[string]string{"reason": "recovered_from_defer_state"}
		if reschedule != nil {
			payloadFields["recovery_align"] = reschedule.AlignReason
			payloadFields["rescheduled_due_at"] = reschedule.DueAt.UTC().Format(time.RFC3339)
		}
		payload, _ := json.Marshal(payloadFields)
		eventID, err := qtx.InsertObligationStatusEvent(ctx, obligationdb.InsertObligationStatusEventParams{
			TenantID:       tenant,
			ObligationID:   oblUUID,
			EventType:      "scheduled",
			OccurredAt:     pgconv.Timestamptz(occurredAt),
			Payload:        pgconv.JSONB(payload),
			IdempotencyKey: eventKey,
		})
		if err != nil {
			return "", false, fmt.Errorf("obligation: insert reopen event: %w", err)
		}
		eventUUID, err := pgconv.UUID(eventID)
		if err != nil {
			return "", false, fmt.Errorf("obligation: reopen event id: %w", err)
		}
		if err := qtx.CompleteIdempotencyKey(ctx, obligationdb.CompleteIdempotencyKeyParams{
			ResultType:     pgconv.Text("obligation_status_event"),
			ResultID:       eventUUID,
			IdempotencyKey: eventKey,
		}); err != nil {
			return "", false, fmt.Errorf("obligation: complete reopen event key: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return "", false, fmt.Errorf("obligation: commit reopen: %w", err)
	}
	return obligationID, true, nil
}

// RescheduleObligationByID reschedules a still-open (scheduled/due) obligation to a new due date in
// place, targeting the obligation directly by id rather than by idempotency_key. This is the mobile
// "reschedule an overdue vaccination obligation" write path; it is deliberately separate from
// ReopenDeferredObligationByIdempotencyKey above, which remains the ONLY path that can reopen a
// health-held 'deferred' obligation (SM-2 recovery) — a 'deferred' row can never match here (see
// RescheduleOpenObligationByID's status filter), so this endpoint can never widen that recovery-only
// path.
//
// A 'missed' target is handled differently, on purpose: state-machines.md's "Conventions" section is
// explicit that `missed` (alongside completed/waived/canceled/superseded) is immutable closed history,
// and "a later policy correction creates new work or a rework/correction record; it does not rewrite the
// closed row." So rescheduling a MISSED obligation never flips its status back to 'scheduled' in place —
// insertReworkObligationForMissed below inserts a brand-new obligation_instances row (fresh id, status
// 'scheduled', the requested new due date, unbatched) via the exact same InsertObligationInstance query
// the SM-1 generator uses, and records a 'scheduled' status event on that NEW row whose payload carries
// `superseded_missed_obligation_id` pointing back at the untouched missed row. (obligation_instances has
// no parent_obligation_id column, so this JSONB back-reference — not a schema FK — is the established
// traceability mechanism already used elsewhere in this file for "why did this row become scheduled";
// see the payload `reason` field on the plain-reschedule and reopenTerminalObligationForInsert paths.)
// The missed row's status/due_at/row_version/batch_id are left completely untouched and it receives no
// new obligation_status_events row of its own.
//
// Request-level idempotency is enforced via the shared idempotency_keys table (scope
// "obligation.reschedule", see idempotency.go): a first call performs the write; an exact replay (same
// key + same due_at/window payload) returns the original result without re-running the write; a
// same-key/different-payload replay returns ports.ErrIdempotencyConflict without mutating anything.
// If a still-open (scheduled/due) obligation was attached to a 'planned' batch, batch_id is cleared
// (mirrors DeferOpenObligationByIdempotencyKey's detachPlannedBatch pattern above) so the SM-4 sweeper
// re-attaches it to a drive that actually matches the new due date, and the old batch's
// estimated_targets is decremented. Unlike the defer/cancel flows, this intentionally does NOT also
// write the context->'*_repair' stock-reconciliation JSONB bookkeeping those flows use — see the inline
// comment at the detach site for why that was judged out of scope here.
func (r *Repository) RescheduleObligationByID(ctx context.Context, tenantID, obligationID, idempotencyKey string, dueAt, windowStart time.Time, windowEnd *time.Time, occurredAt time.Time) (string, bool, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tenant, err := pgconv.UUID(tenantID)
	if err != nil {
		return "", false, fmt.Errorf("obligation: tenant id: %w", err)
	}
	obligation, err := pgconv.UUID(obligationID)
	if err != nil {
		return "", false, fmt.Errorf("obligation: obligation id: %w", err)
	}
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return "", false, fmt.Errorf("obligation: begin reschedule tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := r.queries.WithTx(tx)

	const rescheduleIdemScope = "obligation.reschedule"
	fingerprint := requestFingerprint(obligationID, dueAt.UTC().Format(time.RFC3339Nano), windowStart.UTC().Format(time.RFC3339Nano), fpTime(windowEnd))
	reservation, err := reserveIdempotency(ctx, tx, tenantID, rescheduleIdemScope, idempotencyKey, fingerprint)
	if err != nil {
		return "", false, err
	}
	if !reservation.proceed {
		// Exact replay: no side effects, just hand back the original result.
		if cerr := tx.Commit(ctx); cerr != nil {
			return "", false, fmt.Errorf("obligation: commit reschedule replay: %w", cerr)
		}
		return reservation.resultID, true, nil
	}

	var lockedStatus, lockedBatchID, lockedBatchStatus string
	var srcProtocolVersionID, srcRuleID, srcTargetType, srcTargetID, srcScopeType, srcScopeID, srcTrigger string
	var srcSequence int32
	err = tx.QueryRow(ctx, `
SELECT oi.status,
       COALESCE(oi.batch_id::text, '')::text AS batch_id,
       COALESCE(ob.status, '')::text AS batch_status,
       oi.protocol_version_id::text,
       oi.rule_id::text,
       oi.target_type,
       oi.target_id::text,
       oi.scope_type,
       oi.scope_id::text,
       oi."sequence",
       COALESCE(oi.generated_by_trigger_id::text, '')::text AS generated_by_trigger_id
FROM obligation_instances oi
LEFT JOIN obligation_batches ob
  ON ob.tenant_id = oi.tenant_id
 AND ob.batch_id = oi.batch_id
WHERE oi.tenant_id = $1
  AND oi.obligation_id = $2
  AND oi.status IN ('scheduled', 'due', 'missed')
FOR UPDATE OF oi`, tenant, obligation).Scan(
		&lockedStatus, &lockedBatchID, &lockedBatchStatus,
		&srcProtocolVersionID, &srcRuleID, &srcTargetType, &srcTargetID,
		&srcScopeType, &srcScopeID, &srcSequence, &srcTrigger)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, ports.ErrNotFound
	}
	if err != nil {
		return "", false, fmt.Errorf("obligation: lock reschedule target: %w", err)
	}

	var newID string
	if lockedStatus == "missed" {
		// Immutable closed history: create new work instead of mutating the closed row. See this
		// function's doc comment and insertReworkObligationForMissed's doc comment.
		newID, err = r.insertReworkObligationForMissed(ctx, qtx, tenant, obligationID,
			srcProtocolVersionID, srcRuleID, srcTargetType, srcTargetID, srcScopeType, srcScopeID,
			srcTrigger, srcSequence, dueAt, windowStart, windowEnd, occurredAt)
		if err != nil {
			return "", false, err
		}
	} else {
		newID, err = qtx.RescheduleOpenObligationByID(ctx, obligationdb.RescheduleOpenObligationByIDParams{
			DueAt:        pgconv.Timestamptz(dueAt),
			WindowStart:  pgconv.Timestamptz(windowStart),
			WindowEnd:    pgconv.NullableTimestamptz(windowEnd),
			TenantID:     tenant,
			ObligationID: obligation,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			// Raced with a concurrent status transition between the lock read above and this update.
			return "", false, ports.ErrNotFound
		}
		if err != nil {
			return "", false, fmt.Errorf("obligation: reschedule obligation: %w", err)
		}

		if detachPlannedBatch := lockedBatchID != "" && lockedBatchStatus == "planned"; detachPlannedBatch {
			if _, err := tx.Exec(ctx, `
UPDATE obligation_instances
SET batch_id = NULL, updated_at = now()
WHERE tenant_id = $1 AND obligation_id = $2::uuid`, tenant, newID); err != nil {
				return "", false, fmt.Errorf("obligation: detach rescheduled obligation from planned batch: %w", err)
			}
			// Known incompleteness (documented, not accidental): the defer/cancel flows above also write a
			// context->'defer_repair'/'cancel_repair' JSONB note plus a matching stock-reservation release
			// amount, computed by a shared "pending_release" CTE that sums across defer_repair/shift_repair/
			// cancel_repair/missed_repair keys. Reusing one of those keys here would misrepresent the actual
			// reschedule reason in stock-reconciliation/audit views; adding a new 'reschedule_repair' key would
			// require updating that shared CTE (and its 3 existing call sites) to include it in the sum — a
			// materially larger, riskier change than this focused bug fix. So only estimated_targets is
			// decremented (still correct for "how many targets remain on this drive"); the reserved-inventory
			// release for the vacated slot is left for manual stock reconciliation, same as it would be if this
			// detach did not happen at all.
			if _, err := tx.Exec(ctx, `
UPDATE obligation_batches
SET estimated_targets = GREATEST(0, estimated_targets - 1),
    updated_at = now(),
    row_version = row_version + 1
WHERE tenant_id = $1 AND batch_id = $2::uuid`, tenant, lockedBatchID); err != nil {
				return "", false, fmt.Errorf("obligation: update rescheduled planned batch: %w", err)
			}
		}

		eventKey := newID + ":rescheduled:" + occurredAt.UTC().Format(time.RFC3339Nano)
		_, reserveErr := qtx.ReserveIdempotencyKey(ctx, obligationdb.ReserveIdempotencyKeyParams{
			IdempotencyKey: eventKey,
			TenantID:       tenant,
			Scope:          "obligation.status_event",
			RequestHash:    "rescheduled:" + dueAt.UTC().Format(time.RFC3339),
		})
		if reserveErr != nil && !errors.Is(reserveErr, pgx.ErrNoRows) {
			return "", false, fmt.Errorf("obligation: reserve rescheduled event key: %w", reserveErr)
		}
		if reserveErr == nil {
			oblUUID, err := pgconv.UUID(newID)
			if err != nil {
				return "", false, fmt.Errorf("obligation: rescheduled obligation id: %w", err)
			}
			payload, _ := json.Marshal(map[string]string{
				"reason":     "mobile_reschedule",
				"new_due_at": dueAt.UTC().Format(time.RFC3339),
			})
			// EventType "scheduled" (not a new "rescheduled" type) deliberately matches the established
			// convention used by ReopenDeferredObligationByIdempotencyKey and reopenTerminalObligationForInsert
			// above: any transition INTO 'scheduled' status records event_type='scheduled', with the "why"
			// captured in the payload's reason field (and mirrored into the idempotency key suffix below for
			// easy filtering) — not a new event_type per reason. This also avoids widening the
			// obligation_status_events_type_check CHECK constraint, which validate-hot-index-migrations.sh
			// (part of `make validate-migrations`) rejects for hot tables past the enforcement floor unless
			// done via a NOT VALID + VALIDATE CONSTRAINT + no-lock DROP CONSTRAINT rollout — unnecessary
			// ceremony when the existing 'scheduled' type already fits.
			eventID, err := qtx.InsertObligationStatusEvent(ctx, obligationdb.InsertObligationStatusEventParams{
				TenantID:       tenant,
				ObligationID:   oblUUID,
				EventType:      "scheduled",
				OccurredAt:     pgconv.Timestamptz(occurredAt),
				Payload:        pgconv.JSONB(payload),
				IdempotencyKey: eventKey,
			})
			if err != nil {
				return "", false, fmt.Errorf("obligation: insert rescheduled event: %w", err)
			}
			eventUUID, err := pgconv.UUID(eventID)
			if err != nil {
				return "", false, fmt.Errorf("obligation: rescheduled event id: %w", err)
			}
			if err := qtx.CompleteIdempotencyKey(ctx, obligationdb.CompleteIdempotencyKeyParams{
				ResultType:     pgconv.Text("obligation_status_event"),
				ResultID:       eventUUID,
				IdempotencyKey: eventKey,
			}); err != nil {
				return "", false, fmt.Errorf("obligation: complete rescheduled event key: %w", err)
			}
		}
	}

	if err := completeIdempotency(ctx, tx, tenantID, rescheduleIdemScope, idempotencyKey, "obligation_instance", newID); err != nil {
		return "", false, fmt.Errorf("obligation: complete reschedule idempotency: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return "", false, fmt.Errorf("obligation: commit reschedule: %w", err)
	}
	return newID, false, nil
}

// insertReworkObligationForMissed reschedules a 'missed' obligation by creating a brand-new
// obligation_instances row rather than mutating the closed one — see RescheduleObligationByID's doc
// comment for why. It reuses InsertObligationInstance, the exact same INSERT the SM-1 generator uses
// (via InsertObligation/reopenTerminalObligationForInsert above), so this is not a second, parallel
// "create an obligation" code path; it is called with qtx so the insert lands in the SAME transaction as
// the reschedule request's idempotency reservation, keeping the whole reschedule atomic.
//
// The new row copies the missed row's protocol_version/rule/target/scope/sequence (it is the same
// logical dose, just moved to a new date) and is left unbatched (batch_id NULL) so the SM-4 sweeper
// attaches it to whichever drive actually matches the new due date — mirroring the batch-detach behavior
// the plain scheduled/due reschedule path applies explicitly. Its idempotency_key is deterministically
// derived from the missed obligation's id and the new due date, distinct from (and independent of) the
// caller's own request-level idempotency key already reserved by RescheduleObligationByID above.
//
// A 'scheduled' status event is recorded on the NEW row with payload `superseded_missed_obligation_id`
// pointing at the old, untouched missed row — obligation_instances has no parent_obligation_id column,
// so this JSONB back-reference is the established traceability mechanism (see the payload `reason`
// field elsewhere in this file). The missed row itself is never written to: no column mutation, no new
// obligation_status_events row.
//
// Convergent on a same-logical-target replay under a DIFFERENT outer request idempotency key (P2 fix):
// RescheduleObligationByID's own idempotency reservation only dedups an EXACT key match, so a second
// reschedule request for the same missed obligation + same new due date (a different request key, for
// example a retried mobile request) still reaches this INSERT. InsertObligationInstance then affects 0
// rows because an equivalent open obligation already exists for this logical target -- that is duplicate
// logical work, not a failure, so this fetches the existing row via GetOpenObligationByLogicalKey and
// returns it (idempotent success, no duplicate status event) instead of surfacing an internal error.
func (r *Repository) insertReworkObligationForMissed(
	ctx context.Context,
	qtx *obligationdb.Queries,
	tenant pgtype.UUID,
	missedObligationID string,
	protocolVersionID, ruleID, targetType, targetID, scopeType, scopeID, generatedByTriggerID string,
	sequence int32,
	dueAt, windowStart time.Time, windowEnd *time.Time,
	occurredAt time.Time,
) (string, error) {
	protocolVersion, err := pgconv.UUID(protocolVersionID)
	if err != nil {
		return "", fmt.Errorf("obligation: rework protocol version id: %w", err)
	}
	rule, err := pgconv.UUID(ruleID)
	if err != nil {
		return "", fmt.Errorf("obligation: rework rule id: %w", err)
	}
	target, err := pgconv.UUID(targetID)
	if err != nil {
		return "", fmt.Errorf("obligation: rework target id: %w", err)
	}
	scope, err := pgconv.UUID(scopeID)
	if err != nil {
		return "", fmt.Errorf("obligation: rework scope id: %w", err)
	}
	trigger, err := pgconv.UUID(generatedByTriggerID)
	if err != nil {
		return "", fmt.Errorf("obligation: rework trigger id: %w", err)
	}

	newIdempotencyKey := "rescheduled_missed:" + missedObligationID + ":" + dueAt.UTC().Format(time.RFC3339Nano)
	newID, err := qtx.InsertObligationInstance(ctx, obligationdb.InsertObligationInstanceParams{
		TenantID:             tenant,
		ProtocolVersionID:    protocolVersion,
		RuleID:               rule,
		BatchID:              pgconv.NullableUUID(nil),
		TargetType:           targetType,
		TargetID:             target,
		ScopeType:            scopeType,
		ScopeID:              scope,
		DueAt:                pgconv.Timestamptz(dueAt),
		WindowStart:          pgconv.Timestamptz(windowStart),
		WindowEnd:            pgconv.NullableTimestamptz(windowEnd),
		Status:               "scheduled",
		IdempotencyKey:       newIdempotencyKey,
		GeneratedByTriggerID: trigger,
		Sequence:             sequence,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		// Convergent no-op (P2 fix): InsertObligationInstance's "WHERE NOT EXISTS" dedup guard (or,
		// equivalently, its ON CONFLICT(tenant_id, idempotency_key) branch, since newIdempotencyKey is
		// deterministic on (missedObligationID, dueAt)) skipped the insert because an equivalent open
		// obligation for this exact logical target already exists -- most commonly this same rework
		// replayed under a DIFFERENT outer request idempotency key (RescheduleObligationByID's own
		// idempotency reservation only dedups on an exact key match, so a new key for the same missed
		// obligation + due date reaches this far). That is duplicate logical work, not a failure: fetch
		// the existing row and return it as an idempotent success instead of surfacing an internal error.
		// No new status event is written here -- the original successful call already recorded one.
		existing, lookupErr := qtx.GetOpenObligationByLogicalKey(ctx, obligationdb.GetOpenObligationByLogicalKeyParams{
			TenantID:          tenant,
			ProtocolVersionID: protocolVersion,
			RuleID:            rule,
			TargetType:        targetType,
			TargetID:          target,
			Sequence:          sequence,
			DueAt:             pgconv.Timestamptz(dueAt),
		})
		if lookupErr != nil {
			return "", fmt.Errorf("obligation: insert rework obligation for missed %s: %w", missedObligationID, err)
		}
		return existing.ObligationID, nil
	}
	if err != nil {
		return "", fmt.Errorf("obligation: insert rework obligation for missed %s: %w", missedObligationID, err)
	}

	newObligation, err := pgconv.UUID(newID)
	if err != nil {
		return "", fmt.Errorf("obligation: rework obligation id: %w", err)
	}
	eventKey := newID + ":rescheduled_missed:" + occurredAt.UTC().Format(time.RFC3339Nano)
	_, reserveErr := qtx.ReserveIdempotencyKey(ctx, obligationdb.ReserveIdempotencyKeyParams{
		IdempotencyKey: eventKey,
		TenantID:       tenant,
		Scope:          "obligation.status_event",
		RequestHash:    "rescheduled_missed:" + dueAt.UTC().Format(time.RFC3339),
	})
	if reserveErr != nil && !errors.Is(reserveErr, pgx.ErrNoRows) {
		return "", fmt.Errorf("obligation: reserve rework event key: %w", reserveErr)
	}
	if reserveErr == nil {
		payload, _ := json.Marshal(map[string]string{
			"reason":                          "mobile_reschedule_of_missed",
			"new_due_at":                      dueAt.UTC().Format(time.RFC3339),
			"superseded_missed_obligation_id": missedObligationID,
		})
		eventID, err := qtx.InsertObligationStatusEvent(ctx, obligationdb.InsertObligationStatusEventParams{
			TenantID:       tenant,
			ObligationID:   newObligation,
			EventType:      "scheduled",
			OccurredAt:     pgconv.Timestamptz(occurredAt),
			Payload:        pgconv.JSONB(payload),
			IdempotencyKey: eventKey,
		})
		if err != nil {
			return "", fmt.Errorf("obligation: insert rework scheduled event: %w", err)
		}
		eventUUID, err := pgconv.UUID(eventID)
		if err != nil {
			return "", fmt.Errorf("obligation: rework event id: %w", err)
		}
		if err := qtx.CompleteIdempotencyKey(ctx, obligationdb.CompleteIdempotencyKeyParams{
			ResultType:     pgconv.Text("obligation_status_event"),
			ResultID:       eventUUID,
			IdempotencyKey: eventKey,
		}); err != nil {
			return "", fmt.Errorf("obligation: complete rework event key: %w", err)
		}
	}

	return newID, nil
}

// FindNearestPlannedBatchDate returns the earliest compatible planned drive date in the goat's shed or
// park within [from, to], used to align recovered sick goats to a nearby vaccination drive.
func (r *Repository) FindNearestPlannedBatchDate(ctx context.Context, tenantID, versionID, ruleID, vaccineCode, shedID, parkID string, from, to time.Time) (*time.Time, error) {
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
	var shedUUID, parkUUID pgtype.UUID
	if shedID != "" {
		shedUUID, err = pgconv.UUID(shedID)
		if err != nil {
			return nil, fmt.Errorf("obligation: shed id: %w", err)
		}
	}
	if parkID != "" {
		parkUUID, err = pgconv.UUID(parkID)
		if err != nil {
			return nil, fmt.Errorf("obligation: park id: %w", err)
		}
	}
	sessions := domain.CompatiblePlannedBatchSessions(ruleID, vaccineCode)
	if len(sessions) == 0 {
		return nil, nil
	}
	planned, err := r.queries.FindNearestPlannedBatchDate(ctx, obligationdb.FindNearestPlannedBatchDateParams{
		TenantID:          tenant,
		ProtocolVersionID: version,
		Sessions:          sessions,
		FromDate:          pgconv.Date(&from),
		ToDate:            pgconv.Date(&to),
		ShedID:            shedUUID,
		ParkID:            parkUUID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("obligation: find nearest planned batch: %w", err)
	}
	if !planned.Valid {
		return nil, nil
	}
	t := planned.Time
	return &t, nil
}

// CancelOpenObligationByIdempotencyKey closes a single generated/open row when a later source fact
// supersedes its deterministic key, for example replacing a missing-DOB placeholder with a real due
// date. It is intentionally narrow: terminal rows are left untouched and replays are no-ops.
func (r *Repository) CancelOpenObligationByIdempotencyKey(ctx context.Context, tenantID, idempotencyKey, reason string, occurredAt time.Time) (string, bool, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tenant, err := pgconv.UUID(tenantID)
	if err != nil {
		return "", false, fmt.Errorf("obligation: tenant id: %w", err)
	}
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}
	if strings.TrimSpace(reason) == "" {
		reason = "superseded"
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return "", false, fmt.Errorf("obligation: begin key cancel tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := r.queries.WithTx(tx)

	var obligationID string
	err = tx.QueryRow(ctx, `
UPDATE obligation_instances
SET status = 'canceled',
    batch_id = NULL,
    row_version = row_version + 1,
    updated_at = now()
WHERE tenant_id = $1
  AND idempotency_key = $2
  AND status IN ('scheduled', 'due', 'in_progress', 'deferred')
RETURNING obligation_id::text`, tenant, idempotencyKey).Scan(&obligationID)
	if errors.Is(err, pgx.ErrNoRows) {
		if cerr := tx.Commit(ctx); cerr != nil {
			return "", false, fmt.Errorf("obligation: commit key cancel noop: %w", cerr)
		}
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("obligation: cancel by idempotency key: %w", err)
	}

	eventKey := obligationID + ":canceled:" + reason
	_, reserveErr := qtx.ReserveIdempotencyKey(ctx, obligationdb.ReserveIdempotencyKeyParams{
		IdempotencyKey: eventKey,
		TenantID:       tenant,
		Scope:          "obligation.status_event",
		RequestHash:    "cancel:" + reason,
	})
	if reserveErr != nil && !errors.Is(reserveErr, pgx.ErrNoRows) {
		return "", false, fmt.Errorf("obligation: reserve key cancel event: %w", reserveErr)
	}
	if reserveErr == nil {
		oblUUID, err := pgconv.UUID(obligationID)
		if err != nil {
			return "", false, fmt.Errorf("obligation: key cancel obligation id: %w", err)
		}
		payload, _ := json.Marshal(map[string]string{"reason": reason})
		eventID, err := qtx.InsertObligationStatusEvent(ctx, obligationdb.InsertObligationStatusEventParams{
			TenantID:       tenant,
			ObligationID:   oblUUID,
			EventType:      "canceled",
			OccurredAt:     pgconv.Timestamptz(occurredAt),
			Payload:        pgconv.JSONB(payload),
			IdempotencyKey: eventKey,
		})
		if err != nil {
			return "", false, fmt.Errorf("obligation: insert key cancel event: %w", err)
		}
		eventUUID, err := pgconv.UUID(eventID)
		if err != nil {
			return "", false, fmt.Errorf("obligation: key cancel event id: %w", err)
		}
		if err := qtx.CompleteIdempotencyKey(ctx, obligationdb.CompleteIdempotencyKeyParams{
			ResultType:     pgconv.Text("obligation_status_event"),
			ResultID:       eventUUID,
			IdempotencyKey: eventKey,
		}); err != nil {
			return "", false, fmt.Errorf("obligation: complete key cancel event: %w", err)
		}
	}
	if err := insertObligationLifecycleOutbox(ctx, tx, tenantID, obligationID, obligationCanceledEventType, "canceled", occurredAt, map[string]any{
		"reason":          reason,
		"idempotency_key": idempotencyKey,
	}, "obligation.CancelOpenObligationByIdempotencyKey"); err != nil {
		return "", false, err
	}

	if err := tx.Commit(ctx); err != nil {
		return "", false, fmt.Errorf("obligation: commit key cancel: %w", err)
	}
	return obligationID, true, nil
}

// CancelOpenVaccinationObligationsForGoatExceptVersions cancels open vaccination work whose protocol
// version is no longer effective for the goat after a recheck. Terminal and in-progress work are left
// untouched; each changed row gets the same cancellation status event and outbox used by SM-3.
func (r *Repository) CancelOpenVaccinationObligationsForGoatExceptVersions(ctx context.Context, tenantID, goatID string, effectiveVersionIDs []string, reason string, occurredAt time.Time) (int, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	if effectiveVersionIDs == nil {
		effectiveVersionIDs = []string{}
	}
	tenant, err := pgconv.UUID(tenantID)
	if err != nil {
		return 0, fmt.Errorf("obligation: tenant id: %w", err)
	}
	if _, err := pgconv.UUID(goatID); err != nil {
		return 0, fmt.Errorf("obligation: goat id: %w", err)
	}
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}
	if strings.TrimSpace(reason) == "" {
		reason = "version_no_longer_effective"
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("obligation: begin version-except cancel tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := r.queries.WithTx(tx)

	rows, err := tx.Query(ctx, `
UPDATE obligation_instances oi
SET status = 'canceled',
    row_version = oi.row_version + 1,
    updated_at = now()
FROM protocol_versions pv
JOIN protocol_definitions pd
  ON pd.tenant_id = pv.tenant_id
 AND pd.protocol_id = pv.protocol_id
WHERE oi.tenant_id = $1::uuid
  AND oi.target_type = 'goat'
  AND oi.target_id = $2::uuid
  AND oi.status IN ('scheduled', 'due', 'deferred')
  AND pv.tenant_id = oi.tenant_id
  AND pv.protocol_version_id = oi.protocol_version_id
  AND pd.category = 'vaccination'
  AND NOT (oi.protocol_version_id::text = ANY($3::text[]))
RETURNING oi.obligation_id::text, COALESCE(oi.batch_id::text, '')`, tenantID, goatID, effectiveVersionIDs)
	if err != nil {
		return 0, fmt.Errorf("obligation: cancel non-effective vaccination obligations: %w", err)
	}
	count, err := recordCanceledObligationRows(ctx, tx, qtx, tenant, tenantID, goatID, reason, occurredAt, rows, map[string]any{
		"effective_version_ids": effectiveVersionIDs,
	}, "obligation.CancelOpenVaccinationObligationsForGoatExceptVersions")
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("obligation: commit version-except cancel: %w", err)
	}
	return count, nil
}

// CancelOpenVaccinationObligationsForGoatVersion cancels a goat's open work for one vaccination
// protocol version when the current goat state no longer matches that version's eligibility DSL.
func (r *Repository) CancelOpenVaccinationObligationsForGoatVersion(ctx context.Context, tenantID, goatID, protocolVersionID, reason string, occurredAt time.Time) (int, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tenant, err := pgconv.UUID(tenantID)
	if err != nil {
		return 0, fmt.Errorf("obligation: tenant id: %w", err)
	}
	if _, err := pgconv.UUID(goatID); err != nil {
		return 0, fmt.Errorf("obligation: goat id: %w", err)
	}
	if _, err := pgconv.UUID(protocolVersionID); err != nil {
		return 0, fmt.Errorf("obligation: protocol version id: %w", err)
	}
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}
	if strings.TrimSpace(reason) == "" {
		reason = "ineligible_after_recheck"
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("obligation: begin version cancel tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := r.queries.WithTx(tx)

	rows, err := tx.Query(ctx, `
UPDATE obligation_instances oi
SET status = 'canceled',
    row_version = oi.row_version + 1,
    updated_at = now()
FROM protocol_versions pv
JOIN protocol_definitions pd
  ON pd.tenant_id = pv.tenant_id
 AND pd.protocol_id = pv.protocol_id
WHERE oi.tenant_id = $1::uuid
  AND oi.target_type = 'goat'
  AND oi.target_id = $2::uuid
  AND oi.protocol_version_id = $3::uuid
  AND oi.status IN ('scheduled', 'due', 'deferred')
  AND pv.tenant_id = oi.tenant_id
  AND pv.protocol_version_id = oi.protocol_version_id
  AND pd.category = 'vaccination'
RETURNING oi.obligation_id::text, COALESCE(oi.batch_id::text, '')`, tenantID, goatID, protocolVersionID)
	if err != nil {
		return 0, fmt.Errorf("obligation: cancel ineligible vaccination obligations: %w", err)
	}
	count, err := recordCanceledObligationRows(ctx, tx, qtx, tenant, tenantID, goatID, reason, occurredAt, rows, map[string]any{
		"protocol_version_id": protocolVersionID,
	}, "obligation.CancelOpenVaccinationObligationsForGoatVersion")
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("obligation: commit version cancel: %w", err)
	}
	return count, nil
}

func recordCanceledObligationRows(ctx context.Context, tx pgx.Tx, qtx *obligationdb.Queries, tenant pgtype.UUID, tenantID, goatID, reason string, occurredAt time.Time, rows pgx.Rows, extra map[string]any, producer string) (int, error) {
	defer rows.Close()
	ids := make([]string, 0)
	oldBatches := make(map[string]int)
	for rows.Next() {
		var id, batchID string
		if err := rows.Scan(&id, &batchID); err != nil {
			return 0, fmt.Errorf("obligation: scan canceled obligation: %w", err)
		}
		ids = append(ids, id)
		if batchID != "" {
			oldBatches[batchID]++
		}
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("obligation: read canceled obligations: %w", err)
	}
	for batchID, count := range oldBatches {
		if _, err := tx.Exec(ctx, `
WITH reserved AS (
  SELECT COALESCE(SUM(quantity), 0)::numeric AS qty
  FROM inventory_stock_movements
  WHERE tenant_id = $1
    AND batch_id = $2::uuid
    AND movement_type = 'reserve'
),
repair AS (
  SELECT (
    CASE WHEN context #>> '{defer_repair,state}' = 'stock_reconcile_required'
         THEN COALESCE(NULLIF(context #>> '{defer_repair,release_qty}', '')::numeric, 0)
         ELSE 0 END
    + CASE WHEN context #>> '{shift_repair,state}' = 'stock_reconcile_required'
         THEN COALESCE(NULLIF(context #>> '{shift_repair,release_qty}', '')::numeric, 0)
         ELSE 0 END
    + CASE WHEN context #>> '{cancel_repair,state}' = 'stock_reconcile_required'
         THEN COALESCE(NULLIF(context #>> '{cancel_repair,release_qty}', '')::numeric, 0)
         ELSE 0 END
    + CASE WHEN context #>> '{missed_repair,state}' = 'stock_reconcile_required'
         THEN COALESCE(NULLIF(context #>> '{missed_repair,release_qty}', '')::numeric, 0)
         ELSE 0 END
  )::numeric AS pending_release
  FROM obligation_batches
  WHERE tenant_id = $1
    AND batch_id = $2::uuid
)
UPDATE obligation_batches ob
SET estimated_targets = GREATEST(0, estimated_targets - $3::int),
    context = CASE
      WHEN reserved.qty > 0 THEN context || jsonb_build_object(
        'cancel_repair', jsonb_build_object(
          'state', 'stock_reconcile_required',
          'target_id', $4::text,
          'reason', $5::text,
          'release_qty',
            (CASE
              WHEN context #>> '{cancel_repair,state}' = 'stock_reconcile_required'
              THEN COALESCE(NULLIF(context #>> '{cancel_repair,release_qty}', '')::numeric, 0)
              ELSE 0
            END) + LEAST(
              GREATEST(0, reserved.qty - repair.pending_release),
              ($3::numeric * GREATEST(0, reserved.qty - repair.pending_release)) / GREATEST(ob.estimated_targets, 1)
            ),
          'recorded_at', now()
        )
      )
      ELSE context
    END,
    updated_at = now(),
    row_version = row_version + 1
FROM reserved, repair
WHERE tenant_id = $1
  AND batch_id = $2::uuid`, tenant, batchID, count, goatID, reason); err != nil {
			return 0, fmt.Errorf("obligation: update ineligible cancel batch repair: %w", err)
		}
	}
	payload, _ := json.Marshal(map[string]string{"reason": reason})
	outboxExtra := map[string]any{
		"reason":  reason,
		"goat_id": goatID,
	}
	for key, value := range extra {
		outboxExtra[key] = value
	}
	for _, id := range ids {
		oid, err := pgconv.UUID(id)
		if err != nil {
			return 0, fmt.Errorf("obligation: obligation id: %w", err)
		}
		if _, err := qtx.InsertObligationStatusEvent(ctx, obligationdb.InsertObligationStatusEventParams{
			TenantID:       tenant,
			ObligationID:   oid,
			EventType:      "canceled",
			OccurredAt:     pgconv.Timestamptz(occurredAt),
			Payload:        payload,
			IdempotencyKey: id + ":canceled:" + reason,
		}); err != nil {
			return 0, fmt.Errorf("obligation: cancel event: %w", err)
		}
		if err := insertObligationLifecycleOutbox(ctx, tx, tenantID, id, obligationCanceledEventType, "canceled", occurredAt, outboxExtra, producer); err != nil {
			return 0, err
		}
	}
	return len(ids), nil
}

// ListDue returns obligations in a status whose due_at <= dueBefore.
func (r *Repository) ListDue(ctx context.Context, tenantID, status string, dueBefore time.Time, limit int32) ([]domain.DueObligation, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tenant, err := pgconv.UUID(tenantID)
	if err != nil {
		return nil, fmt.Errorf("obligation: tenant id: %w", err)
	}
	if status == "scheduled_or_due" || status == "scheduled_or_due_due" || status == "scheduled_or_due_overdue" {
		mode := strings.TrimPrefix(status, "scheduled_or_due")
		mode = strings.TrimPrefix(mode, "_")
		return r.listScheduledOrDue(ctx, tenant, dueBefore, limit, mode)
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

func (r *Repository) listScheduledOrDue(ctx context.Context, tenant pgtype.UUID, dueBefore time.Time, limit int32, mode string) ([]domain.DueObligation, error) {
	if limit <= 0 {
		limit = 1000
	}
	asOf := time.Now().In(biztime.DefaultLocation())
	rows, err := r.pool.Query(ctx, `
	SELECT obligation_id::text,
       protocol_version_id::text,
       rule_id::text,
       target_type,
       target_id::text,
       scope_type,
       scope_id::text,
       due_at,
       status
FROM obligation_instances
WHERE tenant_id = $1
  AND status IN ('scheduled', 'due')
  AND due_at <= $2
  AND (
    $4::text = ''
    OR ($4::text = 'overdue' AND due_at < $5::timestamptz)
    OR ($4::text = 'due' AND due_at >= $5::timestamptz)
  )
ORDER BY due_at ASC, obligation_id ASC
LIMIT $3`, tenant, pgconv.Timestamptz(dueBefore), limit, mode, pgconv.Timestamptz(asOf))
	if err != nil {
		return nil, fmt.Errorf("obligation: list scheduled/due: %w", err)
	}
	defer rows.Close()
	out := make([]domain.DueObligation, 0)
	for rows.Next() {
		var o domain.DueObligation
		if err := rows.Scan(
			&o.ObligationID,
			&o.ProtocolVersionID,
			&o.RuleID,
			&o.TargetType,
			&o.TargetID,
			&o.ScopeType,
			&o.ScopeID,
			&o.DueAt,
			&o.Status,
		); err != nil {
			return nil, fmt.Errorf("obligation: scan scheduled/due: %w", err)
		}
		out = append(out, o)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("obligation: read scheduled/due rows: %w", err)
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
		out = append(out, domain.UnbatchedDue{
			ObligationID:             row.ObligationID,
			RuleID:                   row.RuleID,
			ScopeType:                row.ScopeType,
			ScopeID:                  row.ScopeID,
			TargetID:                 row.TargetID,
			TargetSpecies:            row.TargetSpecies,
			TargetAnimalStage:        row.TargetAnimalStage,
			TargetReproductiveStatus: row.TargetReproductiveStatus,
			DueAt:                    row.DueAt.Time,
			WindowStart:              timestamptzValue(row.WindowStart),
			WindowEnd:                timestamptzValue(row.WindowEnd),
			BatchingHoldCount:        row.BatchingHoldCount,
			FirstBatchingHoldUntil:   timestamptzValue(row.FirstBatchingHoldUntil),
		})
	}
	return out, nil
}

// ListUnbatchedShedDueForParkConsolidation lists shed-scoped unbatched obligations with their park
// parent location for the second-pass park drive planner.
func (r *Repository) ListUnbatchedShedDueForParkConsolidation(ctx context.Context, tenantID, versionID string, dueBefore time.Time, limit int32, after *domain.ParkConsolidationCursor) ([]domain.ParkConsolidationCandidate, error) {
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
	cursorParkID := ""
	cursorRuleID := ""
	cursorSpecies := ""
	cursorStage := ""
	cursorDue := pgtype.Timestamptz{}
	cursorObligationID := ""
	if after != nil && after.ParkID != "" && after.ObligationID != "" && !after.DueAt.IsZero() {
		cursorParkID = after.ParkID
		cursorRuleID = after.RuleID
		cursorSpecies = after.TargetSpecies
		cursorStage = after.TargetAnimalStage
		cursorDue = pgconv.Timestamptz(after.DueAt)
		cursorObligationID = after.ObligationID
	}
	rows, err := r.pool.Query(ctx, `
WITH candidates AS (
SELECT o.obligation_id::text,
       o.rule_id::text,
       COALESCE(o.scope_id::text, '')::text AS shed_id,
       COALESCE(o.target_id::text, '')::text AS target_id,
       o.due_at,
       o.window_start,
       COALESCE(o.window_end, o.due_at + make_interval(days => GREATEST(pr.due_window_days, 0))) AS effective_window_end,
       park.location_id::text AS park_id,
       CASE
         WHEN o.target_type = 'goat' THEN COALESCE(g.species, 'goat')::text
         ELSE ''
       END AS target_species,
       CASE
         WHEN o.target_type = 'goat' THEN COALESCE(asl.stage_code, g.management_stage, '')::text
         ELSE ''
       END AS target_animal_stage,
       CASE
         WHEN o.target_type = 'goat' THEN COALESCE(g.reproductive_status, '')::text
         ELSE ''
       END AS target_reproductive_status,
       COALESCE(o.batching_hold_count, 0)::int AS batching_hold_count,
       o.first_batching_hold_until
FROM obligation_instances o
JOIN protocol_rules pr
  ON pr.tenant_id = o.tenant_id
 AND pr.rule_id = o.rule_id
JOIN locations shed
  ON shed.tenant_id = o.tenant_id
 AND shed.location_id = o.scope_id
 AND shed.location_type = 'shed'
JOIN locations park
  ON park.tenant_id = o.tenant_id
 AND park.location_id = shed.parent_location_id
 AND park.location_type = 'park'
LEFT JOIN goats g
  ON g.tenant_id = o.tenant_id
 AND g.goat_id = o.target_id
 AND o.target_type = 'goat'
LEFT JOIN location_operational_attributes loa
  ON loa.tenant_id = g.tenant_id
 AND loa.location_id = g.current_location_id
LEFT JOIN shed_profiles sp
  ON sp.tenant_id = g.tenant_id
 AND sp.location_id = COALESCE(g.shed_id, o.scope_id)
LEFT JOIN animal_stage_lookup asl
  ON asl.tenant_id = sp.tenant_id
 AND asl.animal_stage_id = sp.animal_stage_id
 AND asl.status = 'active'
WHERE o.tenant_id = $1
  AND o.protocol_version_id = $2
  AND o.status IN ('scheduled', 'due', 'missed')
  AND o.batch_id IS NULL
  AND o.scope_type = 'shed'
  AND o.due_at <= $3
  AND (
    o.target_type <> 'goat'
    OR (
      g.goat_id IS NOT NULL
      AND g.lifecycle_status = 'alive'
      AND COALESCE(g.health_status, '') NOT IN ('sick', 'under_treatment', 'quarantine', 'icu')
      AND COALESCE(loa.usable_for_vaccination, true)
      AND NOT COALESCE(loa.is_quarantine, false)
      AND NOT COALESCE(loa.is_icu, false)
    )
  )
)
SELECT
  obligation_id,
  rule_id,
  shed_id,
  target_id,
  due_at,
  window_start,
  effective_window_end,
  park_id,
  target_species,
  target_animal_stage,
  target_reproductive_status,
  batching_hold_count,
  first_batching_hold_until
FROM candidates
WHERE (
    $4::text = ''
    OR (park_id, rule_id, target_species, target_animal_stage, due_at, obligation_id)
      > ($4::text, $5::text, $6::text, $7::text, $8::timestamptz, $9::text)
  )
ORDER BY park_id, rule_id, target_species, target_animal_stage, due_at, obligation_id
LIMIT $10`, tenant, version, pgconv.Timestamptz(dueBefore), cursorParkID, cursorRuleID, cursorSpecies, cursorStage, cursorDue, cursorObligationID, limit)
	if err != nil {
		return nil, fmt.Errorf("obligation: list park consolidation candidates: %w", err)
	}
	defer rows.Close()
	out := make([]domain.ParkConsolidationCandidate, 0)
	for rows.Next() {
		var row domain.ParkConsolidationCandidate
		var windowStart, windowEnd, firstHoldUntil pgtype.Timestamptz
		if err := rows.Scan(
			&row.ObligationID,
			&row.RuleID,
			&row.ShedID,
			&row.TargetID,
			&row.DueAt,
			&windowStart,
			&windowEnd,
			&row.ParkID,
			&row.TargetSpecies,
			&row.TargetAnimalStage,
			&row.TargetReproductiveStatus,
			&row.BatchingHoldCount,
			&firstHoldUntil,
		); err != nil {
			return nil, fmt.Errorf("obligation: scan park consolidation candidate: %w", err)
		}
		row.WindowStart = timestamptzValue(windowStart)
		row.WindowEnd = timestamptzValue(windowEnd)
		row.FirstBatchingHoldUntil = timestamptzValue(firstHoldUntil)
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("obligation: list park consolidation candidates: %w", err)
	}
	return out, nil
}

// RecordBatchingHoldForObligations records the one-time due-group hold metadata for obligations
// that were deliberately planned after their due day to merge into a compatible drive.
func (r *Repository) RecordBatchingHoldForObligations(ctx context.Context, tenantID string, obligationIDs []string, holdUntil time.Time, occurredAt time.Time) (int64, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	if len(obligationIDs) == 0 {
		return 0, nil
	}
	tenant, err := pgconv.UUID(tenantID)
	if err != nil {
		return 0, fmt.Errorf("obligation: tenant id: %w", err)
	}
	ids, err := obligationUUIDs(obligationIDs)
	if err != nil {
		return 0, err
	}
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}
	tag, err := r.pool.Exec(ctx, `
UPDATE obligation_instances oi
SET batching_hold_count = COALESCE(oi.batching_hold_count, 0) + 1,
    first_batching_hold_until = COALESCE(oi.first_batching_hold_until, $3),
    updated_at = now(),
    row_version = row_version + 1
FROM unnest($2::uuid[]) AS selected(obligation_id)
WHERE oi.tenant_id = $1
  AND oi.obligation_id = selected.obligation_id`, tenant, ids, pgconv.Timestamptz(holdUntil))
	if err != nil {
		return 0, fmt.Errorf("obligation: record batching hold: %w", err)
	}
	return tag.RowsAffected(), nil
}

// DeferBlockedVaccinationSweepCandidates is the SM-4 pre-batch clinical recheck. It moves only
// unbatched scheduled/due rows into the visible deferred state when the goat is currently sick,
// under treatment, quarantine/ICU, or in a location unusable for vaccination. Missed/waived history
// and in-progress execution are never rewritten here.
func (r *Repository) DeferBlockedVaccinationSweepCandidates(ctx context.Context, tenantID, versionID string, dueBefore, occurredAt time.Time, limit int32) (int, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tenant, err := pgconv.UUID(tenantID)
	if err != nil {
		return 0, fmt.Errorf("obligation: tenant id: %w", err)
	}
	version, err := pgconv.UUID(versionID)
	if err != nil {
		return 0, fmt.Errorf("obligation: version id: %w", err)
	}
	if limit <= 0 {
		limit = 1000
	}
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("obligation: begin sweep defer tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	rows, err := tx.Query(ctx, `
WITH candidates AS (
  SELECT oi.obligation_id,
         CASE
           WHEN COALESCE(g.health_status, '') IN ('sick', 'under_treatment', 'quarantine', 'icu')
             THEN COALESCE(g.health_status, '')
           WHEN COALESCE(loa.is_quarantine, false) THEN 'quarantine_location'
           WHEN COALESCE(loa.is_icu, false) THEN 'icu_location'
           WHEN NOT COALESCE(loa.usable_for_vaccination, true) THEN 'location_vaccination_block'
           ELSE 'clinical_recheck_block'
         END AS defer_reason
  FROM obligation_instances oi
  JOIN goats g
    ON g.tenant_id = oi.tenant_id
   AND g.goat_id = oi.target_id
   AND oi.target_type = 'goat'
  LEFT JOIN location_operational_attributes loa
    ON loa.tenant_id = g.tenant_id
   AND loa.location_id = g.current_location_id
  WHERE oi.tenant_id = $1
    AND oi.protocol_version_id = $2
    AND oi.status IN ('scheduled', 'due')
    AND oi.batch_id IS NULL
    AND oi.due_at <= $3
    AND g.lifecycle_status = 'alive'
    AND (
      COALESCE(g.health_status, '') IN ('sick', 'under_treatment', 'quarantine', 'icu')
      OR COALESCE(loa.is_quarantine, false)
      OR COALESCE(loa.is_icu, false)
      OR NOT COALESCE(loa.usable_for_vaccination, true)
    )
  ORDER BY oi.due_at, oi.obligation_id
  LIMIT $4
  FOR UPDATE OF oi SKIP LOCKED
),
updated AS (
  UPDATE obligation_instances oi
  SET status = 'deferred',
      batch_id = NULL,
      row_version = row_version + 1,
      updated_at = now()
  FROM candidates c
  WHERE oi.tenant_id = $1
    AND oi.obligation_id = c.obligation_id
    AND oi.status IN ('scheduled', 'due')
  RETURNING oi.obligation_id::text, c.defer_reason
)
SELECT obligation_id, defer_reason
FROM updated`, tenant, version, pgconv.Timestamptz(dueBefore), limit)
	if err != nil {
		return 0, fmt.Errorf("obligation: defer blocked sweep candidates: %w", err)
	}
	defer rows.Close()
	type updatedRow struct {
		obligationID string
		reason       string
	}
	updated := make([]updatedRow, 0)
	for rows.Next() {
		var row updatedRow
		if err := rows.Scan(&row.obligationID, &row.reason); err != nil {
			return 0, fmt.Errorf("obligation: scan deferred sweep candidate: %w", err)
		}
		updated = append(updated, row)
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("obligation: defer blocked sweep candidates: %w", err)
	}

	qtx := r.queries.WithTx(tx)
	for _, row := range updated {
		eventKey := row.obligationID + ":deferred:sweep:" + occurredAt.UTC().Format(time.RFC3339Nano)
		_, reserveErr := qtx.ReserveIdempotencyKey(ctx, obligationdb.ReserveIdempotencyKeyParams{
			IdempotencyKey: eventKey,
			TenantID:       tenant,
			Scope:          "obligation.status_event",
			RequestHash:    "sweep_defer:" + row.reason,
		})
		if reserveErr != nil && !errors.Is(reserveErr, pgx.ErrNoRows) {
			return 0, fmt.Errorf("obligation: reserve sweep deferred event key: %w", reserveErr)
		}
		if reserveErr != nil {
			continue
		}
		oblUUID, err := pgconv.UUID(row.obligationID)
		if err != nil {
			return 0, fmt.Errorf("obligation: sweep deferred obligation id: %w", err)
		}
		payload, _ := json.Marshal(map[string]string{"reason": "sweep_clinical_recheck", "defer_status": row.reason})
		eventID, err := qtx.InsertObligationStatusEvent(ctx, obligationdb.InsertObligationStatusEventParams{
			TenantID:       tenant,
			ObligationID:   oblUUID,
			EventType:      "deferred",
			OccurredAt:     pgconv.Timestamptz(occurredAt),
			Payload:        pgconv.JSONB(payload),
			IdempotencyKey: eventKey,
		})
		if err != nil {
			return 0, fmt.Errorf("obligation: insert sweep deferred event: %w", err)
		}
		eventUUID, err := pgconv.UUID(eventID)
		if err != nil {
			return 0, fmt.Errorf("obligation: sweep deferred event id: %w", err)
		}
		if err := qtx.CompleteIdempotencyKey(ctx, obligationdb.CompleteIdempotencyKeyParams{
			ResultType:     pgconv.Text("obligation_status_event"),
			ResultID:       eventUUID,
			IdempotencyKey: eventKey,
		}); err != nil {
			return 0, fmt.Errorf("obligation: complete sweep deferred event key: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("obligation: commit sweep defer: %w", err)
	}
	return len(updated), nil
}

// CountAttachedObligationsByRule returns actionable attached obligation counts grouped by rule for one batch.
func (r *Repository) CountAttachedObligationsByRule(ctx context.Context, tenantID, batchID string) ([]domain.RuleAttachmentCount, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tenant, err := pgconv.UUID(tenantID)
	if err != nil {
		return nil, fmt.Errorf("obligation: tenant id: %w", err)
	}
	batch, err := pgconv.UUID(batchID)
	if err != nil {
		return nil, fmt.Errorf("obligation: batch id: %w", err)
	}
	rows, err := r.pool.Query(ctx, `
SELECT oi.rule_id::text, COUNT(*)::bigint
FROM obligation_instances oi
	WHERE oi.tenant_id = $1
	  AND oi.batch_id = $2
	  AND oi.status IN ('scheduled', 'due', 'in_progress')
	GROUP BY oi.rule_id
	ORDER BY oi.rule_id`, tenant, batch)
	if err != nil {
		return nil, fmt.Errorf("obligation: count attached by rule: %w", err)
	}
	defer rows.Close()
	out := make([]domain.RuleAttachmentCount, 0)
	for rows.Next() {
		var row domain.RuleAttachmentCount
		if err := rows.Scan(&row.RuleID, &row.Count); err != nil {
			return nil, fmt.Errorf("obligation: scan attached by rule: %w", err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("obligation: count attached by rule: %w", err)
	}
	return out, nil
}

// CountAttachedObligationsByRuleForBatches returns actionable attached obligation counts for a
// bounded page of planned batches in one grouped query.
func (r *Repository) CountAttachedObligationsByRuleForBatches(ctx context.Context, tenantID string, batchIDs []string) (map[string][]domain.RuleAttachmentCount, error) {
	out := make(map[string][]domain.RuleAttachmentCount, len(batchIDs))
	if len(batchIDs) == 0 {
		return out, nil
	}
	for _, batchID := range batchIDs {
		out[batchID] = nil
	}
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tenant, err := pgconv.UUID(tenantID)
	if err != nil {
		return nil, fmt.Errorf("obligation: tenant id: %w", err)
	}
	ids, err := pgconv.UUIDs(batchIDs)
	if err != nil {
		return nil, fmt.Errorf("obligation: batch ids: %w", err)
	}
	rows, err := r.pool.Query(ctx, `
SELECT oi.batch_id::text, oi.rule_id::text, COUNT(*)::bigint
FROM obligation_instances oi
WHERE oi.tenant_id = $1
  AND oi.batch_id = ANY($2::uuid[])
  AND oi.status IN ('scheduled', 'due', 'in_progress')
GROUP BY oi.batch_id, oi.rule_id
ORDER BY oi.batch_id, oi.rule_id`, tenant, ids)
	if err != nil {
		return nil, fmt.Errorf("obligation: count attached by rule for batches: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var batchID string
		var row domain.RuleAttachmentCount
		if err := rows.Scan(&batchID, &row.RuleID, &row.Count); err != nil {
			return nil, fmt.Errorf("obligation: scan attached by rule for batches: %w", err)
		}
		out[batchID] = append(out[batchID], row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("obligation: count attached by rule for batches: %w", err)
	}
	return out, nil
}

// ListPlannedComboBatches lists unfinalized planned batches that use combo sessions for cross-version alignment.
func (r *Repository) ListPlannedComboBatches(ctx context.Context, tenantID string, dueBefore time.Time, limit int32) ([]domain.ComboDriveBatch, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tenant, err := pgconv.UUID(tenantID)
	if err != nil {
		return nil, fmt.Errorf("obligation: tenant id: %w", err)
	}
	if limit <= 0 {
		limit = 1000
	}
	dueDay := dueBefore.UTC()
	rows, err := r.pool.Query(ctx, `
SELECT b.batch_id::text,
       b.protocol_version_id::text,
       b.scope_type,
       COALESCE(b.scope_id::text, '')::text AS scope_id,
       COALESCE(b.session, '')::text AS session,
       b.planned_date
FROM obligation_batches b
WHERE b.tenant_id = $1
  AND b.status = 'planned'
  AND b.session LIKE 'combo:%'
  AND b.sop_task_id IS NULL
  AND NOT (b.context ? 'stock_reservation')
  AND (b.planned_date IS NULL OR b.planned_date <= $2::date)
ORDER BY b.scope_type, b.scope_id, b.session, b.planned_date, b.batch_id
LIMIT $3`, tenant, pgconv.Date(&dueDay), limit)
	if err != nil {
		return nil, fmt.Errorf("obligation: list planned combo batches: %w", err)
	}
	defer rows.Close()
	out := make([]domain.ComboDriveBatch, 0)
	for rows.Next() {
		var row domain.ComboDriveBatch
		var planned pgtype.Date
		if err := rows.Scan(&row.BatchID, &row.ProtocolVersionID, &row.ScopeType, &row.ScopeID, &row.Session, &planned); err != nil {
			return nil, fmt.Errorf("obligation: scan planned combo batch: %w", err)
		}
		if planned.Valid {
			row.PlannedDate = pgconv.DateValue(planned)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("obligation: list planned combo batches: %w", err)
	}
	return out, nil
}

// UpdateBatchPlannedDate moves a planned batch to a harmonized combo drive date.
func (r *Repository) UpdateBatchPlannedDate(ctx context.Context, tenantID, batchID string, plannedDate time.Time) error {
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
	tag, err := r.pool.Exec(ctx, `
UPDATE obligation_batches
SET planned_date = $3::date,
    updated_at = now()
WHERE tenant_id = $1
  AND batch_id = $2
  AND status = 'planned'
  AND sop_task_id IS NULL`, tenant, batch, pgconv.Date(&plannedDate))
	if err != nil {
		return fmt.Errorf("obligation: update batch planned date: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ports.ErrNotFound
	}
	return nil
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

	lockKey := fmt.Sprintf("%s:obligation-batch:%s:%s:%s", in.TenantID, in.ProtocolVersionID, in.ScopeType, in.ScopeID)
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, lockKey); err != nil {
		return "", 0, fmt.Errorf("obligation: batch scope lock: %w", err)
	}
	var batchID string
	err = tx.QueryRow(ctx, `
SELECT batch_id::text
FROM obligation_batches
WHERE tenant_id = $1
  AND protocol_version_id = $2
  AND scope_type = $3
  AND scope_id = $4
  AND status = 'planned'
  AND COALESCE(session, '') = $5
  AND (($6::boolean = false AND planned_date IS NULL) OR ($6::boolean = true AND planned_date IS NOT DISTINCT FROM $7::date))
  AND (($8::boolean = false AND window_start IS NULL) OR ($8::boolean = true AND window_start IS NOT DISTINCT FROM $9::timestamptz))
  AND (($10::boolean = false AND window_end IS NULL) OR ($10::boolean = true AND window_end IS NOT DISTINCT FROM $11::timestamptz))
  AND sop_task_id IS NULL
  AND NOT (obligation_batches.context ? 'stock_reservation')
  AND NOT EXISTS (
    SELECT 1
    FROM inventory_stock_movements ism
    WHERE ism.tenant_id = obligation_batches.tenant_id
      AND ism.batch_id = obligation_batches.batch_id
      AND ism.movement_type = 'reserve'
  )
ORDER BY created_at ASC, batch_id ASC
LIMIT 1
FOR UPDATE`,
		tenant, version, in.ScopeType, scope, in.Session,
		in.PlannedDate != nil, in.PlannedDate,
		in.WindowStart != nil, in.WindowStart,
		in.WindowEnd != nil, in.WindowEnd,
	).Scan(&batchID)
	if errors.Is(err, pgx.ErrNoRows) {
		batchID, err = qtx.CreateObligationBatch(ctx, obligationdb.CreateObligationBatchParams{
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
	} else if err != nil {
		return "", 0, fmt.Errorf("obligation: find planned batch: %w", err)
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
	if _, err := tx.Exec(ctx, `
UPDATE obligation_batches ob
SET estimated_targets = live.attached::int,
    updated_at = now()
FROM (
  SELECT count(*) AS attached
  FROM obligation_instances oi
  WHERE oi.tenant_id = $1
    AND oi.batch_id = $2
) live
WHERE ob.tenant_id = $1
  AND ob.batch_id = $2`, tenant, batch); err != nil {
		return "", 0, fmt.Errorf("obligation: update batch target count: %w", err)
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

// SetBatchSOPTasks links a page of planned batches to their spawned SOP tasks in one update.
func (r *Repository) SetBatchSOPTasks(ctx context.Context, tenantID string, taskIDsByBatch map[string]string) error {
	if len(taskIDsByBatch) == 0 {
		return nil
	}
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tenant, err := pgconv.UUID(tenantID)
	if err != nil {
		return fmt.Errorf("obligation: tenant id: %w", err)
	}
	batchIDs := make([]string, 0, len(taskIDsByBatch))
	taskIDs := make([]string, 0, len(taskIDsByBatch))
	for batchID, taskID := range taskIDsByBatch {
		batchIDs = append(batchIDs, batchID)
		taskIDs = append(taskIDs, taskID)
	}
	batchUUIDs, err := pgconv.UUIDs(batchIDs)
	if err != nil {
		return fmt.Errorf("obligation: batch ids: %w", err)
	}
	taskUUIDs, err := pgconv.UUIDs(taskIDs)
	if err != nil {
		return fmt.Errorf("obligation: task ids: %w", err)
	}
	tag, err := r.pool.Exec(ctx, `
WITH input AS (
  SELECT *
  FROM unnest($2::uuid[], $3::uuid[]) AS input(batch_id, task_id)
)
UPDATE obligation_batches ob
SET sop_task_id = input.task_id,
    updated_at = now(),
    row_version = ob.row_version + 1
FROM input
WHERE ob.tenant_id = $1
  AND ob.batch_id = input.batch_id
  AND (ob.sop_task_id IS NULL OR ob.sop_task_id = input.task_id)`, tenant, batchUUIDs, taskUUIDs)
	if err != nil {
		return fmt.Errorf("obligation: set batch sop tasks: %w", err)
	}
	if tag.RowsAffected() != int64(len(taskIDsByBatch)) {
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
        'item_id', $3::text,
        'required_qty', $4::bigint,
        'reason', $5::text,
        'blocked_at', now(),
        'retry_after', now() + interval '15 minutes'
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

// MarkBatchStockBlocks records stock-block context for a page of failed reservations.
func (r *Repository) MarkBatchStockBlocks(ctx context.Context, tenantID string, blocks []domain.BatchStockBlock) error {
	if len(blocks) == 0 {
		return nil
	}
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	batchIDs := make([]string, 0, len(blocks))
	itemIDs := make([]string, 0, len(blocks))
	requiredQtys := make([]int64, 0, len(blocks))
	reasons := make([]string, 0, len(blocks))
	for _, block := range blocks {
		batchIDs = append(batchIDs, block.BatchID)
		itemIDs = append(itemIDs, block.ItemID)
		requiredQtys = append(requiredQtys, block.RequiredQty)
		reasons = append(reasons, block.Reason)
	}
	batchUUIDs, err := pgconv.UUIDs(batchIDs)
	if err != nil {
		return fmt.Errorf("obligation: batch ids: %w", err)
	}
	tag, err := r.pool.Exec(ctx, `
WITH input AS (
  SELECT *
  FROM unnest($2::uuid[], $3::text[], $4::bigint[], $5::text[]) AS input(batch_id, item_id, required_qty, reason)
)
UPDATE obligation_batches ob
SET context = ob.context || jsonb_build_object(
      'stock_block', jsonb_build_object(
        'state', 'blocked',
        'item_id', input.item_id,
        'required_qty', input.required_qty,
        'reason', input.reason,
        'blocked_at', now(),
        'retry_after', now() + interval '15 minutes'
      )
    ),
    updated_at = now(),
    row_version = ob.row_version + 1
FROM input
WHERE ob.tenant_id = $1::uuid
  AND ob.batch_id = input.batch_id`, tenantID, batchUUIDs, itemIDs, requiredQtys, reasons)
	if err != nil {
		return fmt.Errorf("obligation: mark batch stock blocks: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ports.ErrNotFound
	}
	return nil
}

// ClearBatchStockBlock removes a previous stock-block marker after a later reservation succeeds.
func (r *Repository) ClearBatchStockBlock(ctx context.Context, tenantID, batchID string) error {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tag, err := r.pool.Exec(ctx, `
UPDATE obligation_batches
SET context = context - 'stock_block',
    updated_at = now(),
    row_version = row_version + 1
WHERE tenant_id = $1::uuid
  AND batch_id = $2::uuid`, tenantID, batchID)
	if err != nil {
		return fmt.Errorf("obligation: clear batch stock block: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ports.ErrNotFound
	}
	return nil
}

// ClearBatchStockBlocks clears stock-block context for successfully reserved batches.
func (r *Repository) ClearBatchStockBlocks(ctx context.Context, tenantID string, batchIDs []string) error {
	if len(batchIDs) == 0 {
		return nil
	}
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	ids, err := pgconv.UUIDs(batchIDs)
	if err != nil {
		return fmt.Errorf("obligation: batch ids: %w", err)
	}
	_, err = r.pool.Exec(ctx, `
UPDATE obligation_batches
SET context = context - 'stock_block',
    updated_at = now(),
    row_version = row_version + 1
WHERE tenant_id = $1::uuid
  AND batch_id = ANY($2::uuid[])
  AND context ? 'stock_block'`, tenantID, ids)
	if err != nil {
		return fmt.Errorf("obligation: clear batch stock blocks: %w", err)
	}
	return nil
}

func (r *Repository) ListPlannedBatchesNeedingFinalization(ctx context.Context, tenantID, versionID string, needsTask, needsStock bool, after *domain.PlannedBatchFinalizationCursor, limit int32) ([]domain.PlannedBatchFinalization, error) {
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
	var afterCreatedAt *time.Time
	var afterBatchID pgtype.UUID
	if after != nil {
		afterCreatedAt = &after.CreatedAt
		afterBatchID, err = pgconv.UUID(after.BatchID)
		if err != nil {
			return nil, fmt.Errorf("obligation: planned finalization cursor batch id: %w", err)
		}
	}
	rows, err := r.pool.Query(ctx, `
SELECT ob.batch_id::text,
       COALESCE(MIN(oi.rule_id::text), '')::text AS rule_id,
       ob.scope_type,
       ob.scope_id::text,
       ob.created_at,
       ob.planned_date,
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
	 AND oi.status IN ('scheduled', 'due', 'in_progress')
WHERE ob.tenant_id = $1
  AND ob.protocol_version_id = $2
  AND ob.status = 'planned'
  AND (
    $5::timestamptz IS NULL
    OR ob.created_at > $5::timestamptz
    OR (ob.created_at = $5::timestamptz AND ob.batch_id > $6::uuid)
  )
GROUP BY ob.tenant_id, ob.batch_id, ob.scope_type, ob.scope_id, ob.planned_date, ob.estimated_targets, ob.sop_task_id, ob.context, ob.created_at
	HAVING COUNT(oi.obligation_id) > 0
   AND (
     ($3::boolean AND ob.sop_task_id IS NULL)
     OR (
       $4::boolean
       AND (
         NOT (ob.context ? 'stock_block')
         OR COALESCE(NULLIF(ob.context #>> '{stock_block,retry_after}', '')::timestamptz, '-infinity'::timestamptz) <= now()
       )
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
LIMIT $7`, tenant, version, needsTask, needsStock, afterCreatedAt, afterBatchID, limit)
	if err != nil {
		return nil, fmt.Errorf("obligation: list planned batch finalization: %w", err)
	}
	defer rows.Close()
	out := make([]domain.PlannedBatchFinalization, 0)
	for rows.Next() {
		var b domain.PlannedBatchFinalization
		var plannedDate pgtype.Date
		if err := rows.Scan(
			&b.BatchID,
			&b.RuleID,
			&b.ScopeType,
			&b.ScopeID,
			&b.CreatedAt,
			&plannedDate,
			&b.EstimatedTargets,
			&b.AttachedObligations,
			&b.HasSOPTask,
			&b.HasStockReservation,
			&b.StockBlocked,
		); err != nil {
			return nil, fmt.Errorf("obligation: scan planned batch finalization: %w", err)
		}
		b.PlannedDate = pgconv.DateValue(plannedDate)
		out = append(out, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("obligation: list planned batch finalization rows: %w", err)
	}
	return out, nil
}

func timestamptzValue(v pgtype.Timestamptz) *time.Time {
	if !v.Valid {
		return nil
	}
	t := v.Time
	return &t
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

// CancelOpenForGoat cancels a goat's open scheduled/due/in_progress/deferred/missed obligations
// (SM-3 death/sale) and writes a 'canceled' status event for each, in one transaction. Idempotent: a
// re-run finds no open rows and cancels nothing. Completed/accepted history is never touched.
func (r *Repository) CancelOpenForGoat(ctx context.Context, tenantID, goatID, reason string) (int, error) {
	return r.CancelOpenForGoatAt(ctx, tenantID, goatID, reason, time.Now().UTC(), "")
}

// CancelOpenForGoatAt cancels a goat's open scheduled/due/in_progress/deferred/missed obligations
// using the canonical event time for status events/outbox. eventID is accepted for symmetry with
// ordered shift handling; cancellation is idempotent by row status and per-obligation event key.
// writes a 'canceled' status event for each, in one transaction. Idempotent: a re-run finds no open
// rows and cancels nothing. Completed/accepted history is never touched.
func (r *Repository) CancelOpenForGoatAt(ctx context.Context, tenantID, goatID, reason string, occurredAt time.Time, eventID string) (int, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	} else {
		occurredAt = occurredAt.UTC()
	}
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

	rows, err := tx.Query(ctx, `
UPDATE obligation_instances
SET status = 'canceled',
    row_version = row_version + 1,
    updated_at = now()
WHERE tenant_id = $1
  AND target_type = 'goat'
  AND target_id = $2
  AND status IN ('scheduled', 'due', 'in_progress', 'deferred', 'missed')
RETURNING obligation_id::text, COALESCE(batch_id::text, '')::text`, tenant, goat)
	if err != nil {
		return 0, fmt.Errorf("obligation: cancel open for goat: %w", err)
	}
	defer rows.Close()
	ids := make([]string, 0)
	oldBatches := make(map[string]int)
	for rows.Next() {
		var id, batchID string
		if err := rows.Scan(&id, &batchID); err != nil {
			return 0, fmt.Errorf("obligation: scan canceled obligation: %w", err)
		}
		ids = append(ids, id)
		if batchID != "" {
			oldBatches[batchID]++
		}
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("obligation: read canceled obligations: %w", err)
	}
	for batchID, count := range oldBatches {
		if _, err := tx.Exec(ctx, `
WITH reserved AS (
  SELECT COALESCE(SUM(quantity), 0)::numeric AS qty
  FROM inventory_stock_movements
  WHERE tenant_id = $1
    AND batch_id = $2::uuid
    AND movement_type = 'reserve'
),
repair AS (
  SELECT (
    CASE WHEN context #>> '{defer_repair,state}' = 'stock_reconcile_required'
         THEN COALESCE(NULLIF(context #>> '{defer_repair,release_qty}', '')::numeric, 0)
         ELSE 0 END
    + CASE WHEN context #>> '{shift_repair,state}' = 'stock_reconcile_required'
         THEN COALESCE(NULLIF(context #>> '{shift_repair,release_qty}', '')::numeric, 0)
         ELSE 0 END
    + CASE WHEN context #>> '{cancel_repair,state}' = 'stock_reconcile_required'
         THEN COALESCE(NULLIF(context #>> '{cancel_repair,release_qty}', '')::numeric, 0)
         ELSE 0 END
    + CASE WHEN context #>> '{missed_repair,state}' = 'stock_reconcile_required'
         THEN COALESCE(NULLIF(context #>> '{missed_repair,release_qty}', '')::numeric, 0)
         ELSE 0 END
  )::numeric AS pending_release
  FROM obligation_batches
  WHERE tenant_id = $1
    AND batch_id = $2::uuid
)
UPDATE obligation_batches ob
SET estimated_targets = GREATEST(0, estimated_targets - $3::int),
    context = CASE
      WHEN reserved.qty > 0 THEN context || jsonb_build_object(
        'cancel_repair', jsonb_build_object(
          'state', 'stock_reconcile_required',
          'target_id', $4::text,
          'reason', $5::text,
          'release_qty',
            (CASE
              WHEN context #>> '{cancel_repair,state}' = 'stock_reconcile_required'
              THEN COALESCE(NULLIF(context #>> '{cancel_repair,release_qty}', '')::numeric, 0)
              ELSE 0
            END) + LEAST(
              GREATEST(0, reserved.qty - repair.pending_release),
              ($3::numeric * GREATEST(0, reserved.qty - repair.pending_release)) / GREATEST(ob.estimated_targets, 1)
            ),
          'recorded_at', now()
        )
      )
      ELSE context
    END,
    updated_at = now(),
    row_version = row_version + 1
FROM reserved, repair
WHERE tenant_id = $1
  AND batch_id = $2::uuid`, tenant, batchID, count, goatID, reason); err != nil {
			return 0, fmt.Errorf("obligation: update canceled batch repair: %w", err)
		}
	}
	payload, _ := json.Marshal(map[string]string{"reason": reason})
	for _, id := range ids {
		oid, err := pgconv.UUID(id)
		if err != nil {
			return 0, fmt.Errorf("obligation: obligation id: %w", err)
		}
		if _, err := qtx.InsertObligationStatusEvent(ctx, obligationdb.InsertObligationStatusEventParams{
			TenantID:       tenant,
			ObligationID:   oid,
			EventType:      "canceled",
			OccurredAt:     pgconv.Timestamptz(occurredAt),
			Payload:        payload,
			IdempotencyKey: id + ":canceled",
		}); err != nil {
			return 0, fmt.Errorf("obligation: cancel event: %w", err)
		}
		if err := insertObligationLifecycleOutbox(ctx, tx, tenantID, id, obligationCanceledEventType, "canceled", occurredAt, map[string]any{
			"reason":   reason,
			"goat_id":  goatID,
			"event_id": eventID,
		}, "obligation.CancelOpenForGoat"); err != nil {
			return 0, err
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

	count, err := reScopeOpenForGoatInTx(ctx, tx, qtx, tenant, tenantID, goatID, scopeType, scopeID, scopeID, time.Now().UTC())
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("obligation: commit re-scope: %w", err)
	}
	return count, nil
}

// ReScopeOpenForGoatShift applies an event-driven goat shift only when the event is newer than the
// goat's last accepted shift event. Older or same-timestamp deliveries are durable no-ops, preventing
// out-of-order Pub/Sub redelivery from rewinding open obligations to a stale scope.
func (r *Repository) ReScopeOpenForGoatShift(ctx context.Context, tenantID, goatID, scopeType, scopeID string, occurredAt time.Time, eventID string) (int, bool, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tenant, err := pgconv.UUID(tenantID)
	if err != nil {
		return 0, false, fmt.Errorf("obligation: tenant id: %w", err)
	}
	if _, err := pgconv.UUID(goatID); err != nil {
		return 0, false, fmt.Errorf("obligation: goat id: %w", err)
	}
	if _, err := pgconv.UUID(scopeID); err != nil {
		return 0, false, fmt.Errorf("obligation: scope id: %w", err)
	}
	occurredAt = occurredAt.UTC()
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}
	eventID = strings.TrimSpace(eventID)
	if eventID == "" {
		eventID = syntheticShiftEventID(tenantID, goatID, scopeType, scopeID, occurredAt)
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, false, fmt.Errorf("obligation: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := r.queries.WithTx(tx)

	applied, err := claimGoatShiftWatermark(ctx, tx, tenantID, goatID, scopeType, scopeID, occurredAt, eventID)
	if err != nil {
		return 0, false, err
	}
	if !applied {
		if err := tx.Commit(ctx); err != nil {
			return 0, false, fmt.Errorf("obligation: commit stale re-scope: %w", err)
		}
		return 0, false, nil
	}

	count, err := reScopeOpenForGoatInTx(ctx, tx, qtx, tenant, tenantID, goatID, scopeType, scopeID, eventID, occurredAt)
	if err != nil {
		return 0, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, false, fmt.Errorf("obligation: commit re-scope: %w", err)
	}
	return count, true, nil
}

func reScopeOpenForGoatInTx(ctx context.Context, tx pgx.Tx, qtx *obligationdb.Queries, tenant pgtype.UUID, tenantID, goatID, scopeType, scopeID, idempotencySuffix string, occurredAt time.Time) (int, error) {
	ids, oldBatches, err := reScopeOpenObligationsForGoat(ctx, tx, tenantID, goatID, scopeType, scopeID)
	if err != nil {
		return 0, fmt.Errorf("obligation: re-scope open for goat: %w", err)
	}
	for batchID, count := range oldBatches {
		if _, err := tx.Exec(ctx, `
WITH reserved AS (
  SELECT COALESCE(SUM(quantity), 0)::numeric AS qty
  FROM inventory_stock_movements
  WHERE tenant_id = $1::uuid
    AND batch_id = $2::uuid
    AND movement_type = 'reserve'
),
repair AS (
  SELECT (
    CASE WHEN context #>> '{defer_repair,state}' = 'stock_reconcile_required'
         THEN COALESCE(NULLIF(context #>> '{defer_repair,release_qty}', '')::numeric, 0)
         ELSE 0 END
    + CASE WHEN context #>> '{shift_repair,state}' = 'stock_reconcile_required'
         THEN COALESCE(NULLIF(context #>> '{shift_repair,release_qty}', '')::numeric, 0)
         ELSE 0 END
    + CASE WHEN context #>> '{cancel_repair,state}' = 'stock_reconcile_required'
         THEN COALESCE(NULLIF(context #>> '{cancel_repair,release_qty}', '')::numeric, 0)
         ELSE 0 END
    + CASE WHEN context #>> '{missed_repair,state}' = 'stock_reconcile_required'
         THEN COALESCE(NULLIF(context #>> '{missed_repair,release_qty}', '')::numeric, 0)
         ELSE 0 END
  )::numeric AS pending_release
  FROM obligation_batches
  WHERE tenant_id = $1::uuid
    AND batch_id = $2::uuid
)
UPDATE obligation_batches ob
SET estimated_targets = GREATEST(0, estimated_targets - $3::int),
    context = CASE
      WHEN reserved.qty > 0 THEN context || jsonb_build_object(
        'shift_repair', jsonb_build_object(
          'state', 'stock_reconcile_required',
          'moved_target_id', $4,
          'reason', 'goat_shifted_after_batch_planned',
          'release_qty',
            (CASE
              WHEN context #>> '{shift_repair,state}' = 'stock_reconcile_required'
              THEN COALESCE(NULLIF(context #>> '{shift_repair,release_qty}', '')::numeric, 0)
              ELSE 0
            END) + LEAST(
              GREATEST(0, reserved.qty - repair.pending_release),
              ($3::numeric * GREATEST(0, reserved.qty - repair.pending_release)) / GREATEST(ob.estimated_targets, 1)
            ),
          'recorded_at', now()
        )
      )
      ELSE context
    END,
    updated_at = now(),
    row_version = row_version + 1
FROM reserved, repair
WHERE tenant_id = $1::uuid
  AND batch_id = $2::uuid`, tenantID, batchID, count, goatID); err != nil {
			return 0, fmt.Errorf("obligation: update old shift batch: %w", err)
		}
	}
	payloadFields := map[string]string{"scope_type": scopeType, "scope_id": scopeID}
	if idempotencySuffix != "" && idempotencySuffix != scopeID {
		payloadFields["source_event_id"] = idempotencySuffix
		payloadFields["source_occurred_at"] = occurredAt.UTC().Format(time.RFC3339Nano)
	}
	payload, _ := json.Marshal(payloadFields)
	statusOccurredAt := occurredAt
	if statusOccurredAt.IsZero() {
		statusOccurredAt = time.Now().UTC()
	}
	for _, id := range ids {
		oid, err := pgconv.UUID(id)
		if err != nil {
			return 0, fmt.Errorf("obligation: obligation id: %w", err)
		}
		if _, err := qtx.InsertObligationStatusEvent(ctx, obligationdb.InsertObligationStatusEventParams{
			TenantID:       tenant,
			ObligationID:   oid,
			EventType:      "rescoped",
			OccurredAt:     pgconv.Timestamptz(statusOccurredAt),
			Payload:        payload,
			IdempotencyKey: id + ":rescoped:" + idempotencySuffix,
		}); err != nil {
			return 0, fmt.Errorf("obligation: rescoped event: %w", err)
		}
		if err := insertObligationLifecycleOutbox(ctx, tx, tenantID, id, obligationRescopedEventType, "rescoped", statusOccurredAt, map[string]any{
			"scope_type":      scopeType,
			"scope_id":        scopeID,
			"source_event_id": idempotencySuffix,
		}, "obligation.ReScopeOpenForGoat"); err != nil {
			return 0, err
		}
	}
	return len(ids), nil
}

func claimGoatShiftWatermark(ctx context.Context, tx pgx.Tx, tenantID, goatID, scopeType, scopeID string, occurredAt time.Time, eventID string) (bool, error) {
	tag, err := tx.Exec(ctx, `
INSERT INTO obligation_goat_shift_watermarks (
  tenant_id, goat_id, last_occurred_at, last_event_id, last_scope_type, last_scope_id
) VALUES (
  $1::uuid, $2::uuid, $3, $4, $5, $6::uuid
)
ON CONFLICT (tenant_id, goat_id) DO NOTHING`, tenantID, goatID, occurredAt, eventID, scopeType, scopeID)
	if err != nil {
		return false, fmt.Errorf("obligation: insert shift watermark: %w", err)
	}
	if tag.RowsAffected() == 1 {
		return true, nil
	}

	var lastOccurredAt time.Time
	var lastEventID string
	if err := tx.QueryRow(ctx, `
SELECT last_occurred_at, last_event_id
FROM obligation_goat_shift_watermarks
WHERE tenant_id = $1::uuid
  AND goat_id = $2::uuid
FOR UPDATE`, tenantID, goatID).Scan(&lastOccurredAt, &lastEventID); err != nil {
		return false, fmt.Errorf("obligation: lock shift watermark: %w", err)
	}
	if occurredAt.Before(lastOccurredAt) || (occurredAt.Equal(lastOccurredAt) && eventID <= lastEventID) {
		return false, nil
	}
	if _, err := tx.Exec(ctx, `
UPDATE obligation_goat_shift_watermarks
SET last_occurred_at = $3,
    last_event_id = $4,
    last_scope_type = $5,
    last_scope_id = $6::uuid,
    updated_at = now()
WHERE tenant_id = $1::uuid
  AND goat_id = $2::uuid`, tenantID, goatID, occurredAt, eventID, scopeType, scopeID); err != nil {
		return false, fmt.Errorf("obligation: update shift watermark: %w", err)
	}
	return true, nil
}

func syntheticShiftEventID(tenantID, goatID, scopeType, scopeID string, occurredAt time.Time) string {
	sum := md5.Sum([]byte(strings.Join([]string{tenantID, goatID, scopeType, scopeID, occurredAt.Format(time.RFC3339Nano)}, "\x00")))
	return fmt.Sprintf("synthetic-shift:%x", sum)
}

// reScopeOpenObligationsForGoat moves a goat's open obligations to a new scope on SM-2 shift. 'deferred'
// (held sick/ICU/quarantine) work is re-scoped alongside scheduled/due — symmetric with SM-3
// CancelOpenObligationsForGoat — so a goat that shifts while held later reopens (on recovery) at its
// CURRENT shed, not the stale pre-move one (otherwise SM-4 would batch the drive under the wrong shed).
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
  AND status IN ('scheduled', 'due', 'deferred')
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
  AND oi.status IN ('scheduled', 'due', 'deferred')
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

	idempotencyKey := obligationID + ":completed"
	if _, err := qtx.ReserveIdempotencyKey(ctx, obligationdb.ReserveIdempotencyKeyParams{
		IdempotencyKey: idempotencyKey,
		TenantID:       tenant,
		Scope:          "obligation.mark_completed",
		RequestHash:    "mark-completed:" + obligationID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, fmt.Errorf("obligation: completed idempotency key already reserved")
		}
		return false, fmt.Errorf("obligation: reserve completed idempotency key: %w", err)
	}

	now := time.Now().UTC()
	eventID, err := qtx.InsertObligationStatusEvent(ctx, obligationdb.InsertObligationStatusEventParams{
		TenantID:       tenant,
		ObligationID:   obl,
		EventType:      "completed",
		OccurredAt:     pgconv.Timestamptz(now),
		Payload:        []byte(`{"event":"completed"}`),
		IdempotencyKey: idempotencyKey,
	})
	if err != nil {
		return false, fmt.Errorf("obligation: completed event: %w", err)
	}
	eventUUID, err := pgconv.UUID(eventID)
	if err != nil {
		return false, fmt.Errorf("obligation: completed event id: %w", err)
	}
	if err := qtx.CompleteIdempotencyKey(ctx, obligationdb.CompleteIdempotencyKeyParams{
		ResultType:     pgconv.Text("obligation_status_event"),
		ResultID:       eventUUID,
		IdempotencyKey: idempotencyKey,
	}); err != nil {
		return false, fmt.Errorf("obligation: complete completed idempotency key: %w", err)
	}
	if err := insertVaccinationCompletedOutbox(ctx, tx, tenantID, obligationID); err != nil {
		return false, err
	}
	if err := audit.NewTxRecorder(tx).Record(ctx, audit.Event{
		TenantID:     tenantID,
		ActorType:    "system",
		Action:       vaccinationCompletedEventType,
		ResourceType: "obligation_instance",
		ResourceID:   obligationID,
		ScopeType:    "obligation.status_event",
		ScopeID:      obligationID,
		AfterState: map[string]any{
			"status":      "completed",
			"occurred_at": now.Format(time.RFC3339Nano),
		},
		Metadata: map[string]any{
			"source": "obligation_mark_completed",
		},
		TraceID: "vaccination.completed:" + obligationID,
	}); err != nil {
		return false, fmt.Errorf("obligation: completed audit: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("obligation: commit complete: %w", err)
	}
	return true, nil
}

func (r *Repository) IsCompleted(ctx context.Context, tenantID, obligationID string) (bool, error) {
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
	var completed bool
	if err := r.pool.QueryRow(ctx, `
	SELECT status = 'completed'
	FROM obligation_instances
	WHERE tenant_id = $1::uuid
	  AND obligation_id = $2::uuid`, tenant, obl).Scan(&completed); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("obligation: is completed: %w", err)
	}
	return completed, nil
}

func insertObligationLifecycleOutbox(ctx context.Context, tx pgx.Tx, tenantID, obligationID, eventType, status string, occurredAt time.Time, extra map[string]any, producer string) error {
	suffix := ""
	if eventType == obligationRescopedEventType {
		if source, ok := extra["source_event_id"].(string); ok && strings.TrimSpace(source) != "" {
			suffix = ":" + strings.TrimSpace(source)
		} else if scopeID, ok := extra["scope_id"].(string); ok && strings.TrimSpace(scopeID) != "" {
			suffix = ":" + strings.TrimSpace(scopeID)
		}
	} else if eventType == obligationRegeneratedEventType {
		if dueAt, ok := extra["rescheduled_due_at"].(string); ok && strings.TrimSpace(dueAt) != "" {
			suffix = ":" + strings.TrimSpace(dueAt)
		}
	}
	idempotencyKey := eventType + ":" + obligationID + suffix
	eventID := platformoutbox.DeterministicUUID(eventType + ":" + tenantID + ":" + obligationID + suffix)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	occurred := occurredAt.UTC().Format(time.RFC3339Nano)
	payload := map[string]any{
		"tenant_id":     tenantID,
		"obligation_id": obligationID,
		"status":        status,
	}
	for k, v := range extra {
		if v != nil {
			payload[k] = v
		}
	}
	envelope, err := json.Marshal(map[string]any{
		"event_id":       eventID,
		"event_type":     eventType,
		"schema_version": obligationMissedSchemaVersion,
		"schema_ref":     obligationMissedSchemaRef,
		"aggregate_type": "obligation_instance",
		"aggregate_id":   obligationID,
		"occurred_at":    occurred,
		"recorded_at":    now,
		"producer": map[string]any{
			"service": "goatos-api",
			"module":  "obligation",
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
			"evidence_id":   obligationID + ":" + status,
		}},
		"payload":  payload,
		"trace_id": idempotencyKey,
	})
	if err != nil {
		return fmt.Errorf("obligation: %s envelope: %w", eventType, err)
	}
	headers, err := json.Marshal(map[string]any{
		"producer":        producer,
		"schema_version":  obligationMissedSchemaVersion,
		"obligation_id":   obligationID,
		"idempotency_key": idempotencyKey,
	})
	if err != nil {
		return fmt.Errorf("obligation: %s headers: %w", eventType, err)
	}
	_, err = tx.Exec(ctx, `
INSERT INTO outbox_messages (
  tenant_id, event_id, event_type, schema_version, aggregate_type, aggregate_id,
  topic, payload, headers, idempotency_key, trace_id, status, next_attempt_at
) VALUES (
  $1::uuid, $2::uuid, $3, $4, 'obligation_instance', $5::uuid,
  $6, $7::jsonb, $8::jsonb, $9, $9, 'pending', now()
)
ON CONFLICT DO NOTHING`,
		tenantID, eventID, eventType, obligationMissedSchemaVersion,
		obligationID, obligationMissedTopic, envelope, headers, idempotencyKey)
	if err != nil {
		return fmt.Errorf("obligation: %s outbox: %w", eventType, err)
	}
	return nil
}

func insertVaccinationCompletedOutbox(ctx context.Context, tx pgx.Tx, tenantID, obligationID string) error {
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
			"module":  "obligation",
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
		return fmt.Errorf("obligation: vaccination completed envelope: %w", err)
	}
	headers, err := json.Marshal(map[string]any{
		"producer":        "obligation.MarkCompleted",
		"schema_version":  vaccinationCompletedSchemaVersion,
		"obligation_id":   obligationID,
		"idempotency_key": idempotencyKey,
	})
	if err != nil {
		return fmt.Errorf("obligation: vaccination completed headers: %w", err)
	}
	_, err = tx.Exec(ctx, `
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
		return fmt.Errorf("obligation: vaccination completed outbox: %w", err)
	}
	return nil
}

func insertObligationMissedOutbox(ctx context.Context, tx pgx.Tx, tenantID, obligationID string, occurredAt time.Time) error {
	eventID := platformoutbox.DeterministicUUID("obligation.missed:" + tenantID + ":" + obligationID)
	idempotencyKey := "obligation.missed:" + obligationID
	now := time.Now().UTC().Format(time.RFC3339Nano)
	occurred := occurredAt.UTC().Format(time.RFC3339Nano)
	payload := map[string]any{
		"tenant_id":     tenantID,
		"obligation_id": obligationID,
		"status":        "missed",
	}
	envelope, err := json.Marshal(map[string]any{
		"event_id":       eventID,
		"event_type":     obligationMissedEventType,
		"schema_version": obligationMissedSchemaVersion,
		"schema_ref":     obligationMissedSchemaRef,
		"aggregate_type": "obligation_instance",
		"aggregate_id":   obligationID,
		"occurred_at":    occurred,
		"recorded_at":    now,
		"producer": map[string]any{
			"service": "goatos-api",
			"module":  "obligation",
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
			"evidence_id":   obligationID + ":missed",
		}},
		"payload":  payload,
		"trace_id": idempotencyKey,
	})
	if err != nil {
		return fmt.Errorf("obligation: missed envelope: %w", err)
	}
	headers, err := json.Marshal(map[string]any{
		"producer":        "obligation.MarkMissedBefore",
		"schema_version":  obligationMissedSchemaVersion,
		"obligation_id":   obligationID,
		"idempotency_key": idempotencyKey,
	})
	if err != nil {
		return fmt.Errorf("obligation: missed headers: %w", err)
	}
	_, err = tx.Exec(ctx, `
	INSERT INTO outbox_messages (
	  tenant_id, event_id, event_type, schema_version, aggregate_type, aggregate_id,
	  topic, payload, headers, idempotency_key, trace_id, status, next_attempt_at
	) VALUES (
	  $1::uuid, $2::uuid, $3, $4, 'obligation_instance', $5::uuid,
	  $6, $7::jsonb, $8::jsonb, $9, $9, 'pending', now()
	)
	ON CONFLICT (tenant_id, idempotency_key) WHERE event_type = 'obligation.missed' DO NOTHING`,
		tenantID, eventID, obligationMissedEventType, obligationMissedSchemaVersion,
		obligationID, obligationMissedTopic, envelope, headers, idempotencyKey)
	if err != nil {
		return fmt.Errorf("obligation: missed outbox: %w", err)
	}
	return nil
}

// MarkMissedBefore marks open obligations whose due window has crossed as missed and writes a
// durable 'missed' status event per transition. It is safe for repeated/parallel sweepers: candidates
// are locked with SKIP LOCKED and only scheduled/due rows, plus in_progress rows outside an active
// in-progress batch, can transition.
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
	  SELECT oi.obligation_id,
	         oi.batch_id AS old_batch_id
	  FROM obligation_instances oi
	  LEFT JOIN obligation_batches ob
	    ON ob.tenant_id = oi.tenant_id
	   AND ob.batch_id = oi.batch_id
	  WHERE oi.tenant_id = $1
	    AND oi.status IN ('scheduled', 'due', 'in_progress')
	    AND COALESCE(oi.window_end, oi.due_at) < $2
	    AND NOT (oi.status = 'in_progress' AND COALESCE(ob.status, '') = 'in_progress')
    AND NOT EXISTS (
      SELECT 1
      FROM protocol_versions pv
      JOIN protocol_definitions pd
        ON pd.tenant_id = pv.tenant_id
       AND pd.protocol_id = pv.protocol_id
      JOIN vw_procurement_vaccination_excluded_goats ex
        ON ex.tenant_id = oi.tenant_id
       AND ex.goat_id = oi.target_id
      WHERE oi.target_type = 'goat'
        AND pv.tenant_id = oi.tenant_id
        AND pv.protocol_version_id = oi.protocol_version_id
        AND pd.category = 'vaccination'
  )
  ORDER BY COALESCE(oi.window_end, oi.due_at) ASC, oi.obligation_id ASC
  LIMIT $3
  FOR UPDATE OF oi SKIP LOCKED
)
UPDATE obligation_instances oi
SET status = 'missed',
    batch_id = NULL,
    row_version = oi.row_version + 1,
    updated_at = now()
FROM candidate c
WHERE oi.tenant_id = $1
  AND oi.obligation_id = c.obligation_id
RETURNING oi.obligation_id::text, COALESCE(c.old_batch_id::text, '')::text`, tenant, pgconv.Timestamptz(missedBefore), limit)
	if err != nil {
		return 0, fmt.Errorf("obligation: mark missed: %w", err)
	}
	defer rows.Close()

	ids := make([]string, 0)
	oldBatches := make(map[string]int)
	for rows.Next() {
		var id, batchID string
		if err := rows.Scan(&id, &batchID); err != nil {
			return 0, fmt.Errorf("obligation: scan missed id: %w", err)
		}
		ids = append(ids, id)
		if batchID != "" {
			oldBatches[batchID]++
		}
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("obligation: mark missed rows: %w", err)
	}
	rows.Close()

	if len(oldBatches) > 0 {
		batchIDs := make([]string, 0, len(oldBatches))
		missedCounts := make([]int32, 0, len(oldBatches))
		for batchID, count := range oldBatches {
			batchIDs = append(batchIDs, batchID)
			missedCounts = append(missedCounts, int32(count))
		}
		if _, err := tx.Exec(ctx, `
WITH affected(batch_id, missed_count) AS (
  SELECT batch_id::uuid, missed_count::int
  FROM unnest($2::text[], $3::int[]) AS u(batch_id, missed_count)
),
reserved AS (
  SELECT a.batch_id,
         COALESCE(SUM(ism.quantity), 0)::numeric AS qty
  FROM affected a
  LEFT JOIN inventory_stock_movements ism
    ON ism.tenant_id = $1
   AND ism.batch_id = a.batch_id
   AND ism.movement_type = 'reserve'
  GROUP BY a.batch_id
),
repair AS (
  SELECT ob.batch_id,
         (
    CASE WHEN ob.context #>> '{defer_repair,state}' = 'stock_reconcile_required'
         THEN COALESCE(NULLIF(ob.context #>> '{defer_repair,release_qty}', '')::numeric, 0)
         ELSE 0 END
    + CASE WHEN ob.context #>> '{shift_repair,state}' = 'stock_reconcile_required'
         THEN COALESCE(NULLIF(ob.context #>> '{shift_repair,release_qty}', '')::numeric, 0)
         ELSE 0 END
    + CASE WHEN ob.context #>> '{cancel_repair,state}' = 'stock_reconcile_required'
         THEN COALESCE(NULLIF(ob.context #>> '{cancel_repair,release_qty}', '')::numeric, 0)
         ELSE 0 END
    + CASE WHEN ob.context #>> '{missed_repair,state}' = 'stock_reconcile_required'
         THEN COALESCE(NULLIF(ob.context #>> '{missed_repair,release_qty}', '')::numeric, 0)
         ELSE 0 END
  )::numeric AS pending_release
  FROM obligation_batches ob
  JOIN affected a
    ON a.batch_id = ob.batch_id
  WHERE ob.tenant_id = $1
)
UPDATE obligation_batches ob
SET estimated_targets = GREATEST(0, ob.estimated_targets - a.missed_count),
    context = CASE
      WHEN r.qty > 0 THEN ob.context || jsonb_build_object(
        'missed_repair', jsonb_build_object(
          'state', 'stock_reconcile_required',
          'reason', 'obligation_missed',
          'missed_count', a.missed_count,
          'release_qty',
            (CASE
              WHEN ob.context #>> '{missed_repair,state}' = 'stock_reconcile_required'
              THEN COALESCE(NULLIF(ob.context #>> '{missed_repair,release_qty}', '')::numeric, 0)
              ELSE 0
            END) + LEAST(
              GREATEST(0, r.qty - repair.pending_release),
              (a.missed_count::numeric * GREATEST(0, r.qty - repair.pending_release)) / GREATEST(ob.estimated_targets, 1)
            ),
          'recorded_at', now()
        )
      )
      ELSE ob.context
    END,
    updated_at = now(),
    row_version = ob.row_version + 1
FROM affected a
JOIN reserved r
  ON r.batch_id = a.batch_id
JOIN repair
  ON repair.batch_id = a.batch_id
WHERE ob.tenant_id = $1
  AND ob.batch_id = a.batch_id`, tenant, batchIDs, missedCounts); err != nil {
			return 0, fmt.Errorf("obligation: update missed batch repair: %w", err)
		}
	}

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
		if err := insertObligationMissedOutbox(ctx, tx, tenantID, id, now); err != nil {
			return 0, err
		}
		if err := audit.NewTxRecorder(tx).Record(ctx, audit.Event{
			TenantID:     tenantID,
			ActorType:    "system",
			Action:       obligationMissedEventType,
			ResourceType: "obligation_instance",
			ResourceID:   id,
			ScopeType:    "obligation.status_event",
			ScopeID:      id,
			AfterState: map[string]any{
				"status":      "missed",
				"occurred_at": now.Format(time.RFC3339Nano),
			},
			Metadata: map[string]any{
				"source": "obligation_missed_sweeper",
			},
			TraceID: "obligation.missed:" + id,
		}); err != nil {
			return 0, fmt.Errorf("obligation: missed audit: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("obligation: commit missed: %w", err)
	}
	return len(ids), nil
}

// RepairStaleMissedVaccinationBatchLinks detaches legacy missed vaccination rows that still carry a
// batch_id. New MarkMissedBefore transitions do this inline, but this bounded repair lets the recovery
// generator heal old rows before the normal sweeper looks for missed + unbatched obligations.
func (r *Repository) RepairStaleMissedVaccinationBatchLinks(ctx context.Context, tenantID string, olderThan time.Time, limit int32) (int, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tenant, err := pgconv.UUID(tenantID)
	if err != nil {
		return 0, fmt.Errorf("obligation: tenant id: %w", err)
	}
	if olderThan.IsZero() {
		olderThan = time.Now().UTC()
	}
	if limit <= 0 {
		limit = 1000
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("obligation: begin missed batch repair tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	rows, err := tx.Query(ctx, `
WITH candidate AS (
  SELECT oi.obligation_id,
         oi.batch_id AS old_batch_id
  FROM obligation_instances oi
  JOIN protocol_versions pv
    ON pv.tenant_id = oi.tenant_id
   AND pv.protocol_version_id = oi.protocol_version_id
  JOIN protocol_definitions pd
    ON pd.tenant_id = pv.tenant_id
   AND pd.protocol_id = pv.protocol_id
  WHERE oi.tenant_id = $1
    AND oi.target_type = 'goat'
    AND oi.status = 'missed'
    AND oi.batch_id IS NOT NULL
    AND oi.due_at <= $2
    AND pd.category = 'vaccination'
  ORDER BY oi.due_at ASC, oi.obligation_id ASC
  LIMIT $3
  FOR UPDATE OF oi SKIP LOCKED
)
UPDATE obligation_instances oi
SET batch_id = NULL,
    row_version = oi.row_version + 1,
    updated_at = now()
FROM candidate c
WHERE oi.tenant_id = $1
  AND oi.obligation_id = c.obligation_id
RETURNING oi.obligation_id::text, c.old_batch_id::text`, tenant, pgconv.Timestamptz(olderThan), limit)
	if err != nil {
		return 0, fmt.Errorf("obligation: detach stale missed batches: %w", err)
	}
	defer rows.Close()

	ids := make([]string, 0)
	oldBatches := make(map[string]int)
	for rows.Next() {
		var id, batchID string
		if err := rows.Scan(&id, &batchID); err != nil {
			return 0, fmt.Errorf("obligation: scan missed batch repair: %w", err)
		}
		ids = append(ids, id)
		if batchID != "" {
			oldBatches[batchID]++
		}
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("obligation: missed batch repair rows: %w", err)
	}
	rows.Close()

	if len(oldBatches) > 0 {
		batchIDs := make([]string, 0, len(oldBatches))
		missedCounts := make([]int32, 0, len(oldBatches))
		for batchID, count := range oldBatches {
			batchIDs = append(batchIDs, batchID)
			missedCounts = append(missedCounts, int32(count))
		}
		if _, err := tx.Exec(ctx, `
WITH affected(batch_id, missed_count) AS (
  SELECT batch_id::uuid, missed_count::int
  FROM unnest($2::text[], $3::int[]) AS u(batch_id, missed_count)
),
reserved AS (
  SELECT a.batch_id,
         COALESCE(SUM(ism.quantity), 0)::numeric AS qty
  FROM affected a
  LEFT JOIN inventory_stock_movements ism
    ON ism.tenant_id = $1
   AND ism.batch_id = a.batch_id
   AND ism.movement_type = 'reserve'
  GROUP BY a.batch_id
),
repair AS (
  SELECT ob.batch_id,
         (
    CASE WHEN ob.context #>> '{defer_repair,state}' = 'stock_reconcile_required'
         THEN COALESCE(NULLIF(ob.context #>> '{defer_repair,release_qty}', '')::numeric, 0)
         ELSE 0 END
    + CASE WHEN ob.context #>> '{shift_repair,state}' = 'stock_reconcile_required'
         THEN COALESCE(NULLIF(ob.context #>> '{shift_repair,release_qty}', '')::numeric, 0)
         ELSE 0 END
    + CASE WHEN ob.context #>> '{cancel_repair,state}' = 'stock_reconcile_required'
         THEN COALESCE(NULLIF(ob.context #>> '{cancel_repair,release_qty}', '')::numeric, 0)
         ELSE 0 END
    + CASE WHEN ob.context #>> '{missed_repair,state}' = 'stock_reconcile_required'
         THEN COALESCE(NULLIF(ob.context #>> '{missed_repair,release_qty}', '')::numeric, 0)
         ELSE 0 END
  )::numeric AS pending_release
  FROM obligation_batches ob
  JOIN affected a
    ON a.batch_id = ob.batch_id
  WHERE ob.tenant_id = $1
)
UPDATE obligation_batches ob
SET estimated_targets = GREATEST(0, ob.estimated_targets - a.missed_count),
    context = CASE
      WHEN r.qty > 0 THEN ob.context || jsonb_build_object(
        'missed_repair', jsonb_build_object(
          'state', 'stock_reconcile_required',
          'reason', 'stale_missed_batch_link',
          'missed_count', a.missed_count,
          'release_qty',
            (CASE
              WHEN ob.context #>> '{missed_repair,state}' = 'stock_reconcile_required'
              THEN COALESCE(NULLIF(ob.context #>> '{missed_repair,release_qty}', '')::numeric, 0)
              ELSE 0
            END) + LEAST(
              GREATEST(0, r.qty - repair.pending_release),
              (a.missed_count::numeric * GREATEST(0, r.qty - repair.pending_release)) / GREATEST(ob.estimated_targets, 1)
            ),
          'recorded_at', now()
        )
      )
      ELSE ob.context
    END,
    updated_at = now(),
    row_version = ob.row_version + 1
FROM affected a
JOIN reserved r
  ON r.batch_id = a.batch_id
JOIN repair
  ON repair.batch_id = a.batch_id
WHERE ob.tenant_id = $1
  AND ob.batch_id = a.batch_id`, tenant, batchIDs, missedCounts); err != nil {
			return 0, fmt.Errorf("obligation: bulk update stale missed batch repair: %w", err)
		}
	}

	now := time.Now().UTC()
	if len(ids) > 0 {
		if _, err := tx.Exec(ctx, `
INSERT INTO audit_log (
  tenant_id,
  actor_id,
  actor_type,
  action,
  resource_type,
  resource_id,
  scope_type,
  scope_id,
  decision_id,
  before_state,
  after_state,
  metadata,
  trace_id
)
SELECT
  $1::uuid,
  NULL::uuid,
  'system',
  $2::text,
  'obligation_instance',
  id::uuid,
  'obligation.repair',
  id::uuid,
  NULL::uuid,
  NULL::jsonb,
  jsonb_build_object('batch_id', NULL, 'occurred_at', $4::text),
  jsonb_build_object('source', 'vaccination_recovery_repair'),
  $2::text || ':' || id
FROM unnest($3::text[]) AS ids(id)`, tenant, obligationMissedBatchRepairAction, ids, now.Format(time.RFC3339Nano)); err != nil {
			return 0, fmt.Errorf("obligation: bulk missed batch repair audit: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("obligation: commit missed batch repair: %w", err)
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
			BatchID:           row.BatchID,
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
