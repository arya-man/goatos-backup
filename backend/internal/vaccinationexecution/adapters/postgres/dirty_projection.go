package postgres

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/vaccinationexecution/domain"
)

const dirtyProjectionShardRowBudget = 1_000_000

type dirtyProjectionScope struct {
	ID           int64
	TenantID     string
	ShedID       string
	BusinessDate time.Time
	DirtyThrough time.Time
	AttemptCount int
	MaxAttempts  int
	LeaseToken   string
}

// ProcessDirtyProjectionScopes is the normal vaccination read-model writer. Source-table triggers put
// shed scopes in the durable queue in the same transaction as the source mutation. This worker claims a
// bounded SKIP LOCKED window, coalesces it per tenant, and replaces only those shed shards in all three
// version-swapped projections. The Recompute*Projection methods remain bootstrap/repair operations.
func (r *Repository) ProcessDirtyProjectionScopes(ctx context.Context, req domain.DirtyProjectionWorkerRequest) (domain.DirtyProjectionWorkerResult, error) {
	if strings.TrimSpace(req.WorkerID) == "" {
		return domain.DirtyProjectionWorkerResult{}, fmt.Errorf("vaccination projection worker: worker id is required")
	}
	if req.Limit <= 0 {
		req.Limit = 50
	}
	if req.Limit > 500 {
		req.Limit = 500
	}
	if req.LeaseFor <= 0 {
		req.LeaseFor = 2 * time.Minute
	}
	if req.AsOf.IsZero() {
		req.AsOf = time.Now().In(biztime.DefaultLocation())
	}
	if req.DueBefore.IsZero() {
		req.DueBefore = req.AsOf.Add(defaultExecutionHorizon)
	}

	claimed, err := r.claimDirtyProjectionScopes(ctx, req.WorkerID, req.Limit, req.LeaseFor)
	if err != nil {
		return domain.DirtyProjectionWorkerResult{}, err
	}
	result := domain.DirtyProjectionWorkerResult{Claimed: len(claimed)}
	if len(claimed) == 0 {
		return result, nil
	}
	byTenant := make(map[string][]dirtyProjectionScope)
	for _, scope := range claimed {
		byTenant[scope.TenantID] = append(byTenant[scope.TenantID], scope)
	}
	result.Tenants = len(byTenant)

	tenants := make([]string, 0, len(byTenant))
	for tenantID := range byTenant {
		tenants = append(tenants, tenantID)
	}
	sort.Strings(tenants)
	var workerErr error
	for _, tenantID := range tenants {
		scopes := byTenant[tenantID]
		err := r.refreshDirtyProjectionTenant(ctx, tenantID, scopes, req.AsOf, req.DueBefore)
		if errors.Is(err, domain.ErrProjectionUnavailable) {
			err = r.bootstrapVaccinationProjections(ctx, tenantID, req.AsOf, req.DueBefore)
			if err == nil {
				err = r.refreshDirtyProjectionTenant(ctx, tenantID, scopes, req.AsOf, req.DueBefore)
			}
		}
		if err != nil {
			retried, dead, markErr := r.failDirtyProjectionScopes(ctx, scopes, err)
			result.Retried += retried
			result.DeadLettered += dead
			workerErr = errors.Join(workerErr, fmt.Errorf("tenant %s: %w", tenantID, err), markErr)
			continue
		}
		result.Completed += len(scopes)
	}
	return result, workerErr
}

// bootstrapVaccinationProjections is reached only when one of the three serving pointers does not
// exist (new migration/new tenant). It is the automatic repair/bootstrap path, never the steady-state
// source-change path.
func (r *Repository) bootstrapVaccinationProjections(ctx context.Context, tenantID string, asOf, dueBefore time.Time) error {
	if _, err := r.RecomputeShedProjection(ctx, domain.ShedProjectionRecomputeRequest{TenantID: tenantID, AsOf: asOf, DueBefore: dueBefore}); err != nil {
		return fmt.Errorf("vaccination projection worker: bootstrap shed: %w", err)
	}
	if _, err := r.RecomputeExecutionProjection(ctx, domain.ExecutionProjectionRecomputeRequest{TenantID: tenantID, AsOf: asOf, DueBefore: dueBefore}); err != nil {
		return fmt.Errorf("vaccination projection worker: bootstrap execution: %w", err)
	}
	if _, err := r.RecomputeOperationsProjection(ctx, domain.OperationsProjectionRecomputeRequest{TenantID: tenantID, AsOf: asOf, DueBefore: dueBefore}); err != nil {
		return fmt.Errorf("vaccination projection worker: bootstrap operations: %w", err)
	}
	return nil
}

func (r *Repository) claimDirtyProjectionScopes(ctx context.Context, workerID string, limit int, leaseFor time.Duration) ([]dirtyProjectionScope, error) {
	if _, err := r.pool.Exec(ctx, `
UPDATE projection_dirty_scopes
SET status=CASE WHEN attempt_count>=max_attempts THEN 'dead_letter' ELSE 'retry' END,
    available_at=CASE WHEN attempt_count>=max_attempts THEN available_at ELSE now() END,
    lease_owner=NULL,lease_token=NULL,lease_until=NULL,
    last_error=COALESCE(last_error,'worker lease expired'),updated_at=now()
WHERE family='vaccination' AND status='processing' AND lease_until<now()`); err != nil {
		return nil, fmt.Errorf("vaccination projection worker: recover leases: %w", err)
	}
	rows, err := r.pool.Query(ctx, `
WITH candidates AS (
  SELECT dirty_scope_id
  FROM projection_dirty_scopes
  WHERE family='vaccination' AND status IN ('pending','retry') AND available_at<=now()
  ORDER BY available_at,dirty_scope_id
  FOR UPDATE SKIP LOCKED
  LIMIT $1
)
UPDATE projection_dirty_scopes q
SET status='processing',attempt_count=q.attempt_count+1,lease_owner=$2,
    lease_token=gen_random_uuid(),lease_until=now()+$3::interval,updated_at=now()
FROM candidates
WHERE q.dirty_scope_id=candidates.dirty_scope_id
RETURNING q.dirty_scope_id,q.tenant_id::text,q.shed_id::text,q.business_date,
          q.dirty_through,q.attempt_count,q.max_attempts,q.lease_token::text`, limit, workerID, leaseFor.String())
	if err != nil {
		return nil, fmt.Errorf("vaccination projection worker: claim: %w", err)
	}
	defer rows.Close()
	out := make([]dirtyProjectionScope, 0, limit)
	for rows.Next() {
		var scope dirtyProjectionScope
		if err := rows.Scan(&scope.ID, &scope.TenantID, &scope.ShedID, &scope.BusinessDate,
			&scope.DirtyThrough, &scope.AttemptCount, &scope.MaxAttempts, &scope.LeaseToken); err != nil {
			return nil, fmt.Errorf("vaccination projection worker: scan claim: %w", err)
		}
		out = append(out, scope)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("vaccination projection worker: iterate claims: %w", err)
	}
	return out, nil
}

func (r *Repository) failDirtyProjectionScopes(ctx context.Context, scopes []dirtyProjectionScope, cause error) (retried, dead int, retErr error) {
	message := cause.Error()
	if len(message) > 2000 {
		message = message[:2000]
	}
	for _, scope := range scopes {
		status := "retry"
		if scope.AttemptCount >= scope.MaxAttempts {
			status = "dead_letter"
			dead++
		} else {
			retried++
		}
		_, err := r.pool.Exec(ctx, `
UPDATE projection_dirty_scopes
SET status=$3,available_at=CASE WHEN $3='retry' THEN now()+make_interval(secs=>LEAST(900,1<<LEAST(attempt_count,9))) ELSE available_at END,
    lease_owner=NULL,lease_token=NULL,lease_until=NULL,last_error=$4,updated_at=now()
WHERE dirty_scope_id=$1 AND lease_token=$2::uuid`, scope.ID, scope.LeaseToken, status, message)
		if err != nil {
			retErr = errors.Join(retErr, fmt.Errorf("mark scope %d %s: %w", scope.ID, status, err))
		}
	}
	return retried, dead, retErr
}

func (r *Repository) refreshDirtyProjectionTenant(ctx context.Context, tenantID string, scopes []dirtyProjectionScope, asOf, dueBefore time.Time) error {
	shedIDs := make([]string, 0, len(scopes))
	seen := make(map[string]struct{}, len(scopes))
	for _, scope := range scopes {
		if _, ok := seen[scope.ShedID]; !ok {
			seen[scope.ShedID] = struct{}{}
			shedIDs = append(shedIDs, scope.ShedID)
		}
	}
	sort.Strings(shedIDs)

	cfg, err := r.CapacityConfig(ctx, tenantID)
	if err != nil {
		return fmt.Errorf("vaccination projection worker: capacity config: %w", err)
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return fmt.Errorf("vaccination projection worker: begin: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1::text,86172))`, tenantID); err != nil {
		return fmt.Errorf("vaccination projection worker: lock: %w", err)
	}
	var shedVersion, executionVersion, operationsVersion int64
	var shedTotal, executionTotal, operationsTotal int64
	if err := tx.QueryRow(ctx, `
SELECT s.serving_projection_version,e.serving_projection_version,o.serving_projection_version,
       s.row_count,e.row_count,o.row_count
FROM vaccination_shed_projection_state s
JOIN vaccination_execution_projection_state e USING(tenant_id)
JOIN vaccination_operations_projection_state o USING(tenant_id)
WHERE s.tenant_id=$1::uuid
  AND s.serving_projection_version IS NOT NULL
  AND e.serving_projection_version IS NOT NULL
  AND o.serving_projection_version IS NOT NULL`, tenantID).Scan(
		&shedVersion, &executionVersion, &operationsVersion,
		&shedTotal, &executionTotal, &operationsTotal,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("vaccination projection worker: bootstrap repair required: %w", domain.ErrProjectionUnavailable)
		}
		return fmt.Errorf("vaccination projection worker: load serving versions: %w", err)
	}
	var projectedAt time.Time
	if err := tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&projectedAt); err != nil {
		return fmt.Errorf("vaccination projection worker: stamp: %w", err)
	}
	var oldShedRows, oldExecutionRows, oldOperationsRows int64
	if err := tx.QueryRow(ctx, `SELECT
  (SELECT count(*) FROM vaccination_shed_projection_rows WHERE tenant_id=$1::uuid AND projection_version=$2 AND shed_id=ANY($5::text[])),
  (SELECT count(*) FROM vaccination_execution_projection_rows WHERE tenant_id=$1::uuid AND projection_version=$3 AND shed_id=ANY($5::text[])),
  (SELECT count(*) FROM vaccination_operations_projection_rows WHERE tenant_id=$1::uuid AND projection_version=$4 AND shed_id=ANY($5::text[]))`,
		tenantID, shedVersion, executionVersion, operationsVersion, shedIDs).Scan(
		&oldShedRows, &oldExecutionRows, &oldOperationsRows,
	); err != nil {
		return fmt.Errorf("vaccination projection worker: count dirty shards: %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM vaccination_shed_projection_rows
WHERE tenant_id=$1::uuid AND projection_version=$2 AND shed_id=ANY($3::text[])`, tenantID, shedVersion, shedIDs); err != nil {
		return fmt.Errorf("vaccination projection worker: delete shed shards: %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM vaccination_execution_projection_rows
WHERE tenant_id=$1::uuid AND projection_version=$2 AND shed_id=ANY($3::text[])`, tenantID, executionVersion, shedIDs); err != nil {
		return fmt.Errorf("vaccination projection worker: delete execution shards: %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM vaccination_operations_projection_rows
WHERE tenant_id=$1::uuid AND projection_version=$2 AND shed_id=ANY($3::text[])`, tenantID, operationsVersion, shedIDs); err != nil {
		return fmt.Errorf("vaccination projection worker: delete operations shards: %w", err)
	}
	if _, err := tx.Exec(ctx, vaccinationShedProjectionInsertSQL, tenantID, asOf, dueBefore,
		cfg.MaxPerDay, cfg.MaxBufferDays, shedVersion, projectedAt, shedIDs); err != nil {
		return fmt.Errorf("vaccination projection worker: rebuild shed shards: %w", err)
	}
	if err := rebuildExecutionProjectionShards(ctx, tx, tenantID, shedIDs, executionVersion, projectedAt, asOf, dueBefore); err != nil {
		return err
	}
	if err := rebuildOperationsProjectionShards(ctx, tx, tenantID, shedIDs, operationsVersion, projectedAt, asOf, dueBefore); err != nil {
		return err
	}

	var newShedRows, newExecutionRows, newOperationsRows int64
	if err := tx.QueryRow(ctx, `SELECT
  (SELECT count(*) FROM vaccination_shed_projection_rows WHERE tenant_id=$1::uuid AND projection_version=$2 AND shed_id=ANY($5::text[])),
  (SELECT count(*) FROM vaccination_execution_projection_rows WHERE tenant_id=$1::uuid AND projection_version=$3 AND shed_id=ANY($5::text[])),
  (SELECT count(*) FROM vaccination_operations_projection_rows WHERE tenant_id=$1::uuid AND projection_version=$4 AND shed_id=ANY($5::text[]))`,
		tenantID, shedVersion, executionVersion, operationsVersion, shedIDs).Scan(
		&newShedRows, &newExecutionRows, &newOperationsRows,
	); err != nil {
		return fmt.Errorf("vaccination projection worker: count rebuilt shards: %w", err)
	}
	shedTotal = shedTotal - oldShedRows + newShedRows
	executionTotal = executionTotal - oldExecutionRows + newExecutionRows
	operationsTotal = operationsTotal - oldOperationsRows + newOperationsRows
	closedAfter := asOf.Add(-defaultClosedHistoryAge)
	if _, err := tx.Exec(ctx, `
UPDATE vaccination_shed_projection_state SET projected_at=$2,as_of=$3,due_before=$4,row_count=$5,
  freshness_status='green',serving_state='fresh',last_error=NULL,updated_at=now()
WHERE tenant_id=$1::uuid`, tenantID, projectedAt, asOf, dueBefore, shedTotal); err != nil {
		return fmt.Errorf("vaccination projection worker: publish shed version: %w", err)
	}
	if _, err := tx.Exec(ctx, `
UPDATE vaccination_execution_projection_state SET projected_at=$2,as_of=$3,due_before=$4,closed_after=$5,row_count=$6,
  freshness_status='green',serving_state='fresh',last_error=NULL,updated_at=now()
WHERE tenant_id=$1::uuid`, tenantID, projectedAt, asOf, dueBefore, closedAfter, executionTotal); err != nil {
		return fmt.Errorf("vaccination projection worker: publish execution version: %w", err)
	}
	if _, err := tx.Exec(ctx, `
UPDATE vaccination_operations_projection_state SET projected_at=$2,as_of=$3,due_before=$4,row_count=$5,
  freshness_status='green',serving_state='fresh',last_error=NULL,updated_at=now()
WHERE tenant_id=$1::uuid`, tenantID, projectedAt, asOf, dueBefore, operationsTotal); err != nil {
		return fmt.Errorf("vaccination projection worker: publish operations version: %w", err)
	}
	for _, scope := range scopes {
		if _, err := tx.Exec(ctx, `
UPDATE projection_dirty_scopes
SET status='completed',lease_owner=NULL,lease_token=NULL,lease_until=NULL,last_error=NULL,
    checkpoint=jsonb_build_object('projection_version',$3::bigint,'projected_at',$4::timestamptz),updated_at=now()
WHERE dirty_scope_id=$1 AND lease_token=$2::uuid`, scope.ID, scope.LeaseToken, shedVersion, projectedAt); err != nil {
			return fmt.Errorf("vaccination projection worker: checkpoint scope %d: %w", scope.ID, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("vaccination projection worker: commit: %w", err)
	}
	committed = true

	return nil
}

func rebuildExecutionProjectionShards(ctx context.Context, tx pgx.Tx, tenantID string, shedIDs []string, version int64, projectedAt, asOf, dueBefore time.Time) error {
	columns := []string{
		"tenant_id", "projection_version", "park_id", "park_name", "shed_id", "shed_name", "animal_stage",
		"batch_id", "protocol_name", "dose_code", "due_at", "obligation_count", "scheduled_count", "due_count",
		"in_progress_count", "completed_count", "missed_count", "deferred_count", "canceled_count", "completion_recorded",
		"completion_accepted", "completion_rejected", "completion_reversed", "batch_status", "task_state", "operator_name",
		"park_head_name", "verifier_name", "usable_for_vaccination", "is_quarantine", "is_icu", "health_deferred_count",
		"obligation_id", "sop_task_id", "sop_version_id", "sop_task_row_version", "completion_id", "work_state", "severity",
		"sort_rank", "sort_due_micros", "sort_row_key", "projected_at",
	}
	closedAfter := asOf.Add(-defaultClosedHistoryAge)
	for _, shedID := range shedIDs {
		raw, err := tx.Query(ctx, vaccinationExecutionSQL, tenantID, "", shedID, dueBefore,
			dirtyProjectionShardRowBudget, "", asOf, closedAfter, "", false, 0, int64(0), "")
		if err != nil {
			return fmt.Errorf("vaccination projection worker: replay execution shed %s: %w", shedID, err)
		}
		page, err := scanExecutionProjectionPage(raw, dirtyProjectionShardRowBudget)
		raw.Close()
		if err != nil {
			return err
		}
		if page.NextCursor != nil {
			return fmt.Errorf("vaccination projection worker: execution shed %s row budget exhausted", shedID)
		}
		copyRows := make([][]any, 0, len(page.Rows))
		for _, p := range page.Rows {
			copyRows = append(copyRows, []any{tenantID, version, p.ParkID, p.ParkName, p.ShedID, p.ShedName,
				p.AnimalStage, p.BatchID, p.ProtocolName, p.DoseCode, p.DueAt, p.ObligationCount, p.ScheduledCount,
				p.DueCount, p.InProgressCount, p.CompletedCount, p.MissedCount, p.DeferredCount, p.CanceledCount,
				p.CompletionRecorded, p.CompletionAccepted, p.CompletionRejected, p.CompletionReversed, p.BatchStatus,
				p.TaskState, p.OperatorName, p.ParkHeadName, p.VerifierName, p.UsableForVaccination, p.IsQuarantine,
				p.IsICU, p.HealthDeferredCount, p.ObligationID, p.SOPTaskID, p.SOPVersionID, p.SOPTaskRowVersion,
				p.CompletionID, string(p.WorkState), executionSeverity(p.WorkState), p.SortRank, p.SortDueMicros,
				p.SortRowKey, projectedAt})
		}
		if len(copyRows) > 0 {
			if _, err := tx.CopyFrom(ctx, pgx.Identifier{"vaccination_execution_projection_rows"}, columns, pgx.CopyFromRows(copyRows)); err != nil {
				return fmt.Errorf("vaccination projection worker: copy execution shed %s: %w", shedID, err)
			}
		}
	}
	return nil
}

func rebuildOperationsProjectionShards(ctx context.Context, tx pgx.Tx, tenantID string, shedIDs []string, version int64, projectedAt, asOf, dueBefore time.Time) error {
	columns := []string{"tenant_id", "projection_version", "park_id", "park_name", "shed_id", "shed_name", "stage", "age_band",
		"protocol_id", "protocol_name", "animals", "next_due", "last_dose", "overdue_count", "due_count", "in_progress_count",
		"scheduled_count", "missed_count", "deferred_count", "accepted_count", "proof_pending_count", "rejected_count", "total_count", "projected_at"}
	for _, shedID := range shedIDs {
		raw, err := tx.Query(ctx, vaccinationOperationsSQL, tenantID, asOf, dueBefore, "", shedID, "", "", "",
			dirtyProjectionShardRowBudget, "", "")
		if err != nil {
			return fmt.Errorf("vaccination projection worker: replay operations shed %s: %w", shedID, err)
		}
		projected, err := scanOperationsRows(raw, nil)
		raw.Close()
		if err != nil {
			return err
		}
		copyRows := make([][]any, 0, len(projected))
		for _, p := range projected {
			copyRows = append(copyRows, []any{tenantID, version, p.ParkID, p.ParkName, p.ShedID, p.ShedName, p.Stage,
				p.AgeBand, p.ProtocolID, p.ProtocolName, p.Animals, p.NextDue, p.LastDose, p.OverdueCount, p.DueCount,
				p.InProgressCount, p.ScheduledCount, p.MissedCount, p.DeferredCount, p.AcceptedCount, p.ProofPendingCount,
				p.RejectedCount, p.TotalCount, projectedAt})
		}
		if len(copyRows) > 0 {
			if _, err := tx.CopyFrom(ctx, pgx.Identifier{"vaccination_operations_projection_rows"}, columns, pgx.CopyFromRows(copyRows)); err != nil {
				return fmt.Errorf("vaccination projection worker: copy operations shed %s: %w", shedID, err)
			}
		}
	}
	return nil
}
