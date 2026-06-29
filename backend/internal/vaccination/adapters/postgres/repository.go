// Package postgres implements the vaccination Repository over generated sqlc queries.
package postgres

import (
	"context"
	"crypto/md5"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/audit"
	"github.com/vgoats/goatos/backend/internal/platform/pgconv"
	vaccinationdb "github.com/vgoats/goatos/backend/internal/vaccination/adapters/postgres/sqlc"
	"github.com/vgoats/goatos/backend/internal/vaccination/domain"
	"github.com/vgoats/goatos/backend/internal/vaccination/ports"
)

const defaultQueryTimeout = 3 * time.Second

const (
	vaccinationCompletedEventType     = "vaccination.completed"
	vaccinationCompletedSchemaVersion = "1.0.0"
	vaccinationCompletedSchemaRef     = "domain-event-envelope.v1"
	vaccinationCompletedTopic         = "vaccination.events"
)

// Repository is the Postgres-backed vaccination repository.
type Repository struct {
	pool         *pgxpool.Pool
	queries      *vaccinationdb.Queries
	queryTimeout time.Duration
}

// NewRepository builds a Repository bound to a pgx pool.
func NewRepository(pool *pgxpool.Pool, queryTimeout time.Duration) *Repository {
	if queryTimeout <= 0 {
		queryTimeout = defaultQueryTimeout
	}
	return &Repository{pool: pool, queries: vaccinationdb.New(pool), queryTimeout: queryTimeout}
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

// StartGenerationRun creates or returns the durable status row for an existing-cohort generation
// pass. The idempotency key prevents replayed publish hooks from launching duplicate herd scans.
func (r *Repository) StartGenerationRun(ctx context.Context, in domain.GenerationRunInput) (domain.GenerationRun, bool, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	if in.StartedAt.IsZero() {
		in.StartedAt = time.Now().UTC()
	}
	runContext, _ := json.Marshal(map[string]string{"request_hash": in.RequestHash})
	rows, err := r.pool.Query(ctx, `
INSERT INTO vaccination_generation_runs (
  tenant_id, protocol_version_id, trigger_type, trigger_ref, status,
  started_at, idempotency_key, context
) VALUES (
  $1::uuid, $2::uuid, $3, $4, 'running', $5::timestamptz, $6, $7::jsonb
)
ON CONFLICT (tenant_id, idempotency_key) DO UPDATE
SET status = 'running',
    started_at = CASE
      WHEN vaccination_generation_runs.status = 'running' THEN EXCLUDED.started_at
      ELSE vaccination_generation_runs.started_at
    END,
    context = CASE
      WHEN COALESCE(vaccination_generation_runs.context->>'request_hash', '') = ''
        THEN EXCLUDED.context
      ELSE vaccination_generation_runs.context
    END,
    completed_at = NULL,
    generated_count = 0,
    deferred_count = 0,
    reopened_count = 0,
    skipped_no_due_date_count = 0,
    suppressed_trusted_history_count = 0,
    cursor_goat_id = NULL,
    last_error = NULL,
    updated_at = now(),
    row_version = vaccination_generation_runs.row_version + 1
WHERE (
    vaccination_generation_runs.status = 'failed'
    OR (
      vaccination_generation_runs.status = 'running'
      -- Staleness is measured against WALL-CLOCK now(), never the caller's business asOf ($5): on
      -- at-least-once redelivery of a crashed publish event asOf == started_at, so an asOf-based
      -- window could never elapse and a dead 1M-goat run would wedge forever. A live long run bumps
      -- updated_at via HeartbeatGenerationRun every page, so GREATEST(started_at, updated_at) stays
      -- fresh (not reclaimed); a crashed run with no heartbeat is reclaimable after 15 real minutes.
      AND GREATEST(vaccination_generation_runs.started_at, vaccination_generation_runs.updated_at) < now() - interval '15 minutes'
    )
  )
  AND (
    COALESCE(vaccination_generation_runs.context->>'request_hash', '') = ''
    OR COALESCE(vaccination_generation_runs.context->>'request_hash', '') = $8
  )
RETURNING run_id::text, tenant_id::text, protocol_version_id::text, trigger_type,
          COALESCE(trigger_ref, ''), status, started_at, completed_at,
          generated_count, deferred_count, reopened_count, skipped_no_due_date_count,
          suppressed_trusted_history_count, COALESCE(cursor_goat_id::text, ''),
          COALESCE(last_error, ''), idempotency_key, COALESCE(context->>'request_hash', '')`,
		in.TenantID, in.ProtocolVersionID, in.TriggerType, in.TriggerRef, in.StartedAt, in.IdempotencyKey, string(runContext), in.RequestHash)
	if err != nil {
		return domain.GenerationRun{}, false, fmt.Errorf("vaccination: start generation run: %w", err)
	}
	defer rows.Close()
	if rows.Next() {
		run, scanErr := scanGenerationRun(rows)
		return run, true, scanErr
	}
	if err := rows.Err(); err != nil {
		return domain.GenerationRun{}, false, fmt.Errorf("vaccination: start generation run rows: %w", err)
	}
	run, err := r.getGenerationRunByKey(ctx, in.TenantID, in.IdempotencyKey)
	if err == nil && in.RequestHash != "" && run.RequestHash != "" && run.RequestHash != in.RequestHash {
		return domain.GenerationRun{}, false, ports.ErrIdempotencyConflict
	}
	return run, false, err
}

// FinishGenerationRun records the terminal counts and error, if any. Empty lastError means success.
func (r *Repository) FinishGenerationRun(ctx context.Context, tenantID, runID string, result domain.GenerateResult, cursorGoatID, lastError string, completedAt time.Time) error {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	if completedAt.IsZero() {
		completedAt = time.Now().UTC()
	}
	status := "completed"
	if lastError != "" {
		status = "failed"
	}
	_, err := r.pool.Exec(ctx, `
UPDATE vaccination_generation_runs
SET status = $3,
    completed_at = $4::timestamptz,
    generated_count = $5,
    deferred_count = $6,
    reopened_count = $7,
    skipped_no_due_date_count = $8,
    suppressed_trusted_history_count = $9,
    cursor_goat_id = nullif($10::text, '')::uuid,
    last_error = nullif($11, ''),
    updated_at = now(),
    row_version = row_version + 1
WHERE tenant_id = $1::uuid
  AND run_id = $2::uuid`,
		tenantID, runID, status, completedAt, result.Generated, result.Deferred,
		result.Reopened, result.SkippedNoDueDate, result.SuppressedByTrustedHistory, cursorGoatID, lastError)
	if err != nil {
		return fmt.Errorf("vaccination: finish generation run: %w", err)
	}
	return nil
}

// HeartbeatGenerationRun bumps updated_at on a running generation run so a long (1M-goat) pass is not
// reclaimed/duplicated by the stale-run detector in StartGenerationRun. Best-effort liveness: callers
// ignore the error rather than abort a multi-minute run on a transient blip.
func (r *Repository) HeartbeatGenerationRun(ctx context.Context, tenantID, runID string) error {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	_, err := r.pool.Exec(ctx, `
UPDATE vaccination_generation_runs
SET updated_at = now()
WHERE tenant_id = $1::uuid AND run_id = $2::uuid AND status = 'running'`, tenantID, runID)
	if err != nil {
		return fmt.Errorf("vaccination: heartbeat generation run: %w", err)
	}
	return nil
}

func (r *Repository) getGenerationRunByKey(ctx context.Context, tenantID, key string) (domain.GenerationRun, error) {
	var run domain.GenerationRun
	err := r.pool.QueryRow(ctx, `
SELECT run_id::text, tenant_id::text, protocol_version_id::text, trigger_type,
       COALESCE(trigger_ref, ''), status, started_at, completed_at,
       generated_count, deferred_count, reopened_count, skipped_no_due_date_count,
       suppressed_trusted_history_count, COALESCE(cursor_goat_id::text, ''),
       COALESCE(last_error, ''), idempotency_key, COALESCE(context->>'request_hash', '')
FROM vaccination_generation_runs
WHERE tenant_id = $1::uuid AND idempotency_key = $2`, tenantID, key).Scan(
		&run.RunID,
		&run.TenantID,
		&run.ProtocolVersionID,
		&run.TriggerType,
		&run.TriggerRef,
		&run.Status,
		&run.StartedAt,
		&run.CompletedAt,
		&run.Generated,
		&run.Deferred,
		&run.Reopened,
		&run.SkippedNoDueDate,
		&run.SuppressedByTrustedHistory,
		&run.CursorGoatID,
		&run.LastError,
		&run.IdempotencyKey,
		&run.RequestHash,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.GenerationRun{}, ports.ErrNotFound
	}
	if err != nil {
		return domain.GenerationRun{}, fmt.Errorf("vaccination: get generation run: %w", err)
	}
	return run, nil
}

type generationRunScanner interface {
	Scan(dest ...any) error
}

func scanGenerationRun(row generationRunScanner) (domain.GenerationRun, error) {
	var run domain.GenerationRun
	if err := row.Scan(
		&run.RunID,
		&run.TenantID,
		&run.ProtocolVersionID,
		&run.TriggerType,
		&run.TriggerRef,
		&run.Status,
		&run.StartedAt,
		&run.CompletedAt,
		&run.Generated,
		&run.Deferred,
		&run.Reopened,
		&run.SkippedNoDueDate,
		&run.SuppressedByTrustedHistory,
		&run.CursorGoatID,
		&run.LastError,
		&run.IdempotencyKey,
		&run.RequestHash,
	); err != nil {
		return domain.GenerationRun{}, fmt.Errorf("vaccination: scan generation run: %w", err)
	}
	return run, nil
}

// RecordCompletion appends an idempotent dose-administered record.
func (r *Repository) RecordCompletion(ctx context.Context, in domain.NewCompletion) (string, bool, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tenant, err := pgconv.UUID(in.TenantID)
	if err != nil {
		return "", false, fmt.Errorf("vaccination: tenant id: %w", err)
	}
	obligation, err := pgconv.UUID(in.ObligationID)
	if err != nil {
		return "", false, fmt.Errorf("vaccination: obligation id: %w", err)
	}
	goat, err := pgconv.UUID(in.GoatID)
	if err != nil {
		return "", false, fmt.Errorf("vaccination: goat id: %w", err)
	}
	doseML, err := pgconv.Numeric(in.DoseMlGiven)
	if err != nil {
		return "", false, fmt.Errorf("vaccination: dose_ml_given: %w", err)
	}
	status := in.Status
	if status == "" {
		status = "recorded"
	}
	id, err := r.queries.RecordVaccinationCompletion(ctx, vaccinationdb.RecordVaccinationCompletionParams{
		TenantID:                 tenant,
		ObligationID:             obligation,
		BatchID:                  pgconv.NullableUUID(in.BatchID),
		GoatID:                   goat,
		SopSubmissionItemID:      pgconv.NullableUUID(in.SopSubmissionItemID),
		VaccineInventoryLotID:    pgconv.NullableUUID(in.VaccineInventoryLotID),
		Doses:                    pgconv.Int4(in.Doses),
		DoseMlGiven:              doseML,
		RouteSite:                pgconv.Text(in.RouteSite),
		AdverseReaction:          in.AdverseReaction,
		AdverseReactionProblemID: pgconv.NullableUUID(in.AdverseReactionProblemID),
		ColdChainVerified:        in.ColdChainVerified,
		AdministeredAt:           pgconv.Timestamptz(in.AdministeredAt),
		Status:                   status,
		WithdrawalUntilDate:      pgconv.Date(in.WithdrawalUntilDate),
		RecordedBy:               pgconv.NullableUUID(in.RecordedBy),
		IdempotencyKey:           in.IdempotencyKey,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		if ferr := checkCompletionIdempotencyFingerprint(ctx, r.pool, in, doseML); ferr != nil {
			return "", false, ferr
		}
		return "", false, nil // already recorded for this idempotency key
	}
	if err != nil {
		return "", false, fmt.Errorf("vaccination: record completion: %w", err)
	}
	return id, true, nil
}

type queryRower interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func checkCompletionIdempotencyFingerprint(ctx context.Context, db queryRower, in domain.NewCompletion, doseML pgtype.Numeric) error {
	var conflict bool
	err := db.QueryRow(ctx, `
SELECT EXISTS (
  SELECT 1
  FROM vaccination_completions
  WHERE tenant_id = $1::uuid
    AND idempotency_key = $2
    AND (
      obligation_id IS DISTINCT FROM $3::uuid OR
      COALESCE(batch_id::text, '') IS DISTINCT FROM $4 OR
      goat_id IS DISTINCT FROM $5::uuid OR
      COALESCE(sop_submission_item_id::text, '') IS DISTINCT FROM $6 OR
      COALESCE(vaccine_inventory_lot_id::text, '') IS DISTINCT FROM $7 OR
      COALESCE(doses, 0) IS DISTINCT FROM $8::int OR
      dose_ml_given IS DISTINCT FROM $9::numeric OR
      COALESCE(route_site, '') IS DISTINCT FROM $10 OR
      adverse_reaction IS DISTINCT FROM $11 OR
      COALESCE(adverse_reaction_problem_id::text, '') IS DISTINCT FROM $12 OR
      cold_chain_verified IS DISTINCT FROM $13 OR
      administered_at IS DISTINCT FROM $14::timestamptz OR
      COALESCE(recorded_by::text, '') IS DISTINCT FROM $15
    )
)`,
		in.TenantID,
		in.IdempotencyKey,
		in.ObligationID,
		optionalString(in.BatchID),
		in.GoatID,
		optionalString(in.SopSubmissionItemID),
		optionalString(in.VaccineInventoryLotID),
		completionDoseValue(in.Doses),
		doseML,
		in.RouteSite,
		in.AdverseReaction,
		optionalString(in.AdverseReactionProblemID),
		in.ColdChainVerified,
		in.AdministeredAt,
		optionalString(in.RecordedBy),
	).Scan(&conflict)
	if err != nil {
		return fmt.Errorf("vaccination: check completion idempotency fingerprint: %w", err)
	}
	if conflict {
		return ports.ErrIdempotencyConflict
	}
	return nil
}

func completionDoseValue(v *int32) int32 {
	if v == nil {
		return 0
	}
	return *v
}

// AcceptCompletion marks a recorded completion accepted (SM-5).
func (r *Repository) AcceptCompletion(ctx context.Context, tenantID, completionID string, verifiedBy *string, withdrawalUntil *time.Time) (domain.AcceptedCompletion, bool, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tenant, err := pgconv.UUID(tenantID)
	if err != nil {
		return domain.AcceptedCompletion{}, false, fmt.Errorf("vaccination: tenant id: %w", err)
	}
	cid, err := pgconv.UUID(completionID)
	if err != nil {
		return domain.AcceptedCompletion{}, false, fmt.Errorf("vaccination: completion id: %w", err)
	}
	rows, err := r.queries.AcceptVaccinationCompletion(ctx, vaccinationdb.AcceptVaccinationCompletionParams{
		VerifiedBy:          pgconv.NullableUUID(verifiedBy),
		WithdrawalUntilDate: pgconv.Date(withdrawalUntil),
		TenantID:            tenant,
		CompletionID:        cid,
	})
	if err != nil {
		return domain.AcceptedCompletion{}, false, fmt.Errorf("vaccination: accept completion: %w", err)
	}
	if len(rows) == 0 {
		return domain.AcceptedCompletion{}, false, nil // already accepted/rejected → no-op
	}
	row := rows[0]
	return domain.AcceptedCompletion{
		CompletionID:   completionID,
		Status:         "accepted",
		ObligationID:   row.ObligationID,
		GoatID:         row.GoatID,
		BatchID:        row.BatchID,
		LotID:          row.VaccineInventoryLotID,
		Doses:          row.Doses,
		AdministeredAt: row.AdministeredAt.Time,
	}, true, nil
}

// RecordAndAcceptCompletionAtomic records a direct completion and applies SM-5 accept side effects in
// one Postgres transaction. Replays resume incomplete legacy side effects and otherwise no-op.
func (r *Repository) RecordAndAcceptCompletionAtomic(ctx context.Context, completion domain.NewCompletion, verifiedBy *string, withdrawalUntil *time.Time) (domain.AcceptCompletionAtomicResult, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	if _, err := pgconv.UUID(completion.TenantID); err != nil {
		return domain.AcceptCompletionAtomicResult{}, fmt.Errorf("vaccination: tenant id: %w", err)
	}
	if _, err := pgconv.UUID(completion.ObligationID); err != nil {
		return domain.AcceptCompletionAtomicResult{}, fmt.Errorf("vaccination: obligation id: %w", err)
	}
	if _, err := pgconv.UUID(completion.GoatID); err != nil {
		return domain.AcceptCompletionAtomicResult{}, fmt.Errorf("vaccination: goat id: %w", err)
	}
	doseML, err := pgconv.Numeric(completion.DoseMlGiven)
	if err != nil {
		return domain.AcceptCompletionAtomicResult{}, fmt.Errorf("vaccination: dose_ml_given: %w", err)
	}

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return domain.AcceptCompletionAtomicResult{}, fmt.Errorf("vaccination: begin atomic record accept: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()

	var completionID string
	err = tx.QueryRow(ctx, `
	INSERT INTO vaccination_completions (
	  tenant_id,
	  obligation_id,
	  batch_id,
	  goat_id,
	  sop_submission_item_id,
	  vaccine_inventory_lot_id,
	  doses,
	  dose_ml_given,
	  route_site,
	  adverse_reaction,
	  adverse_reaction_problem_id,
	  cold_chain_verified,
	  administered_at,
	  status,
	  withdrawal_until_date,
	  recorded_by,
	  idempotency_key
	) VALUES (
	  $1::uuid,
	  $2::uuid,
	  nullif($3, '')::uuid,
	  $4::uuid,
	  nullif($5, '')::uuid,
	  nullif($6, '')::uuid,
	  $7,
	  $8,
	  nullif($9, ''),
	  $10,
	  nullif($11, '')::uuid,
	  $12,
	  $13,
	  'recorded',
	  $14,
	  nullif($15, '')::uuid,
	  $16
	)
	ON CONFLICT (tenant_id, idempotency_key) DO NOTHING
	RETURNING completion_id::text`,
		completion.TenantID,
		completion.ObligationID,
		optionalString(completion.BatchID),
		completion.GoatID,
		optionalString(completion.SopSubmissionItemID),
		optionalString(completion.VaccineInventoryLotID),
		pgconv.Int4(completion.Doses),
		doseML,
		completion.RouteSite,
		completion.AdverseReaction,
		optionalString(completion.AdverseReactionProblemID),
		completion.ColdChainVerified,
		completion.AdministeredAt,
		pgconv.Date(completion.WithdrawalUntilDate),
		optionalString(completion.RecordedBy),
		completion.IdempotencyKey,
	).Scan(&completionID)
	result := domain.AcceptCompletionAtomicResult{}
	if errors.Is(err, pgx.ErrNoRows) {
		if ferr := checkCompletionIdempotencyFingerprint(ctx, tx, completion, doseML); ferr != nil {
			return domain.AcceptCompletionAtomicResult{}, ferr
		}
		err = tx.QueryRow(ctx, `
		SELECT completion_id::text
	FROM vaccination_completions
	WHERE tenant_id = $1::uuid
	  AND idempotency_key = $2`, completion.TenantID, completion.IdempotencyKey).Scan(&completionID)
		if errors.Is(err, pgx.ErrNoRows) {
			if cerr := tx.Commit(ctx); cerr != nil {
				return domain.AcceptCompletionAtomicResult{}, fmt.Errorf("vaccination: commit atomic record accept noop: %w", cerr)
			}
			committed = true
			return result, nil
		}
		if err != nil {
			return domain.AcceptCompletionAtomicResult{}, fmt.Errorf("vaccination: get replay completion: %w", err)
		}
	} else if err != nil {
		return domain.AcceptCompletionAtomicResult{}, fmt.Errorf("vaccination: record atomic completion: %w", err)
	} else {
		result.Applied = true
	}

	result, err = r.acceptCompletionInTx(ctx, tx, domain.AcceptCompletionAtomicInput{
		TenantID:        completion.TenantID,
		CompletionID:    completionID,
		VerifiedBy:      verifiedBy,
		WithdrawalUntil: withdrawalUntil,
	}, result)
	if err != nil {
		return domain.AcceptCompletionAtomicResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.AcceptCompletionAtomicResult{}, fmt.Errorf("vaccination: commit atomic record accept: %w", err)
	}
	committed = true
	return result, nil
}

// AcceptCompletionAtomic applies the existing-completion SM-5 accept side effects in one Postgres
// transaction: completion accept, stock consume, obligation completed event, and vaccination.completed
// outbox row commit or roll back together.
func (r *Repository) AcceptCompletionAtomic(ctx context.Context, in domain.AcceptCompletionAtomicInput) (domain.AcceptCompletionAtomicResult, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	if _, err := pgconv.UUID(in.TenantID); err != nil {
		return domain.AcceptCompletionAtomicResult{}, fmt.Errorf("vaccination: tenant id: %w", err)
	}
	if _, err := pgconv.UUID(in.CompletionID); err != nil {
		return domain.AcceptCompletionAtomicResult{}, fmt.Errorf("vaccination: completion id: %w", err)
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return domain.AcceptCompletionAtomicResult{}, fmt.Errorf("vaccination: begin atomic accept: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()
	result, err := r.acceptCompletionInTx(ctx, tx, in, domain.AcceptCompletionAtomicResult{})
	if err != nil {
		return domain.AcceptCompletionAtomicResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.AcceptCompletionAtomicResult{}, fmt.Errorf("vaccination: commit atomic accept: %w", err)
	}
	committed = true
	return result, nil
}

type acceptCompletionTxRow struct {
	completionID   string
	status         string
	obligationID   string
	goatID         string
	batchID        string
	lotID          string
	doses          int32
	administeredAt time.Time
	coldChain      bool
}

func (r *Repository) acceptCompletionInTx(ctx context.Context, tx pgx.Tx, in domain.AcceptCompletionAtomicInput, result domain.AcceptCompletionAtomicResult) (domain.AcceptCompletionAtomicResult, error) {
	row, found, err := r.lockAcceptableCompletion(ctx, tx, in.TenantID, in.CompletionID)
	if err != nil {
		return domain.AcceptCompletionAtomicResult{}, err
	}
	if !found {
		return result, nil
	}
	result.Completion = domain.AcceptedCompletion{
		CompletionID:   row.completionID,
		Status:         row.status,
		ObligationID:   row.obligationID,
		GoatID:         row.goatID,
		BatchID:        row.batchID,
		LotID:          row.lotID,
		Doses:          row.doses,
		AdministeredAt: row.administeredAt,
	}
	obligationStatus, err := r.lockObligationStatus(ctx, tx, in.TenantID, row.obligationID)
	if err != nil {
		return domain.AcceptCompletionAtomicResult{}, err
	}
	if obligationStatus == "completed" {
		if row.status == "accepted" {
			return result, nil
		}
		return domain.AcceptCompletionAtomicResult{}, domain.ErrCompletionNotOpen
	}
	if obligationStatus != "scheduled" && obligationStatus != "due" && obligationStatus != "in_progress" && obligationStatus != "missed" {
		return domain.AcceptCompletionAtomicResult{}, domain.ErrCompletionNotOpen
	}
	consumed, err := r.consumeAcceptedCompletionStock(ctx, tx, in.TenantID, row)
	if err != nil {
		return domain.AcceptCompletionAtomicResult{}, err
	}
	if consumed {
		result.Consumed = true
		result.Applied = true
	}
	if row.status == "recorded" {
		tag, err := tx.Exec(ctx, `
	UPDATE vaccination_completions
	SET status = 'accepted',
	    verified_by = nullif($3, '')::uuid,
	    verified_at = now(),
	    withdrawal_until_date = $4,
	    updated_at = now(),
	    row_version = row_version + 1
	WHERE tenant_id = $1::uuid
	  AND completion_id = $2::uuid
	  AND status = 'recorded'`, in.TenantID, row.completionID, optionalString(in.VerifiedBy), in.WithdrawalUntil)
		if err != nil {
			return domain.AcceptCompletionAtomicResult{}, fmt.Errorf("vaccination: atomic accept completion: %w", err)
		}
		if tag.RowsAffected() > 0 {
			row.status = "accepted"
			result.Accepted = true
			result.Applied = true
			result.Completion.Status = "accepted"
		}
	}
	if obligationStatus != "completed" {
		tag, err := tx.Exec(ctx, `
	UPDATE obligation_instances
	SET status = 'completed',
	    completed_at = now(),
	    row_version = row_version + 1,
	    updated_at = now()
	WHERE tenant_id = $1::uuid
	  AND obligation_id = $2::uuid
	  AND status IN ('scheduled', 'due', 'in_progress', 'missed')`, in.TenantID, row.obligationID)
		if err != nil {
			return domain.AcceptCompletionAtomicResult{}, fmt.Errorf("vaccination: atomic complete obligation: %w", err)
		}
		if tag.RowsAffected() > 0 {
			result.Completed = true
			result.Applied = true
		}
	}
	if row.status == "accepted" || result.Accepted {
		eventID, inserted, err := r.ensureCompletedStatusEvent(ctx, tx, in.TenantID, row.obligationID)
		if err != nil {
			return domain.AcceptCompletionAtomicResult{}, err
		}
		result.StatusEventID = eventID
		if inserted {
			result.Applied = true
		}
		outboxInserted, err := insertVaccinationCompletedOutbox(ctx, tx, in.TenantID, row.obligationID)
		if err != nil {
			return domain.AcceptCompletionAtomicResult{}, err
		}
		result.OutboxInserted = outboxInserted
		if outboxInserted {
			result.Applied = true
		}
	}
	if result.Completed {
		now := time.Now().UTC()
		if err := audit.NewTxRecorder(tx).Record(ctx, audit.Event{
			TenantID:     in.TenantID,
			ActorType:    "system",
			Action:       vaccinationCompletedEventType,
			ResourceType: "obligation_instance",
			ResourceID:   row.obligationID,
			ScopeType:    "obligation.status_event",
			ScopeID:      row.obligationID,
			AfterState: map[string]any{
				"status":        "completed",
				"completion_id": row.completionID,
				"goat_id":       row.goatID,
				"occurred_at":   now.Format(time.RFC3339Nano),
			},
			Metadata: map[string]any{
				"source": "vaccination_accept_completion_atomic",
			},
			TraceID: "vaccination.completed:" + row.obligationID,
		}); err != nil {
			return domain.AcceptCompletionAtomicResult{}, fmt.Errorf("vaccination: completed audit: %w", err)
		}
	}
	return result, nil
}

func (r *Repository) lockAcceptableCompletion(ctx context.Context, tx pgx.Tx, tenantID, completionID string) (acceptCompletionTxRow, bool, error) {
	var row acceptCompletionTxRow
	err := tx.QueryRow(ctx, `
	SELECT completion_id::text,
	       status,
	       obligation_id::text,
	       goat_id::text,
	       COALESCE(batch_id::text, ''),
	       COALESCE(vaccine_inventory_lot_id::text, ''),
	       COALESCE(doses, 1)::int,
	       administered_at,
	       cold_chain_verified
	FROM vaccination_completions
	WHERE tenant_id = $1::uuid
	  AND completion_id = $2::uuid
	  AND status IN ('recorded', 'accepted')
	FOR UPDATE`, tenantID, completionID).Scan(
		&row.completionID,
		&row.status,
		&row.obligationID,
		&row.goatID,
		&row.batchID,
		&row.lotID,
		&row.doses,
		&row.administeredAt,
		&row.coldChain,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return acceptCompletionTxRow{}, false, nil
	}
	if err != nil {
		return acceptCompletionTxRow{}, false, fmt.Errorf("vaccination: lock acceptable completion: %w", err)
	}
	if row.doses <= 0 {
		row.doses = 1
	}
	return row, true, nil
}

func (r *Repository) lockObligationStatus(ctx context.Context, tx pgx.Tx, tenantID, obligationID string) (string, error) {
	var status string
	err := tx.QueryRow(ctx, `
	SELECT status
	FROM obligation_instances
	WHERE tenant_id = $1::uuid
	  AND obligation_id = $2::uuid
	FOR UPDATE`, tenantID, obligationID).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", domain.ErrCompletionNotOpen
	}
	if err != nil {
		return "", fmt.Errorf("vaccination: lock obligation status: %w", err)
	}
	return status, nil
}

func (r *Repository) consumeAcceptedCompletionStock(ctx context.Context, tx pgx.Tx, tenantID string, row acceptCompletionTxRow) (bool, error) {
	if row.batchID == "" {
		return false, nil
	}
	if row.lotID == "" || !row.coldChain {
		return false, domain.ErrStockGateBlocked
	}
	var itemID, locationID, unit string
	if err := tx.QueryRow(ctx, `
	SELECT item_id::text, location_id::text, quantity_unit
	FROM inventory_stock
	WHERE tenant_id = $1::uuid
	  AND stock_id = $2::uuid
	  AND status = 'active'
	  AND (expiry_date IS NULL OR expiry_date >= $3::date)
	FOR UPDATE`, tenantID, row.lotID, row.administeredAt).Scan(&itemID, &locationID, &unit); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, domain.ErrStockGateBlocked
		}
		return false, fmt.Errorf("vaccination: lock completion stock: %w", err)
	}
	key := row.batchID + ":consume:" + row.obligationID + ":" + row.goatID
	qty := int64(row.doses)
	var conflict bool
	if err := tx.QueryRow(ctx, `
	SELECT EXISTS (
	  SELECT 1
	  FROM inventory_stock_movements
	  WHERE tenant_id = $1::uuid
	    AND idempotency_key = $2
	    AND (
	      lot_id IS DISTINCT FROM $3::uuid OR
	      item_id IS DISTINCT FROM $4::uuid OR
	      location_id IS DISTINCT FROM $5::uuid OR
	      movement_type IS DISTINCT FROM 'consume' OR
	      quantity IS DISTINCT FROM $6::numeric OR
	      quantity_unit IS DISTINCT FROM $7 OR
	      batch_id IS DISTINCT FROM $8::uuid
	    )
	)`, tenantID, key, row.lotID, itemID, locationID, qty, unit, row.batchID).Scan(&conflict); err != nil {
		return false, fmt.Errorf("vaccination: check consume idempotency fingerprint: %w", err)
	}
	if conflict {
		return false, domain.ErrStockMovementConflict
	}
	var replay bool
	if err := tx.QueryRow(ctx, `
	SELECT EXISTS (
	  SELECT 1
	  FROM inventory_stock_movements
	  WHERE tenant_id = $1::uuid
	    AND idempotency_key = $2
	)`, tenantID, key).Scan(&replay); err != nil {
		return false, fmt.Errorf("vaccination: check consume idempotency replay: %w", err)
	}
	if replay {
		return false, nil
	}
	var batchReserved int64
	if err := tx.QueryRow(ctx, `
	SELECT COALESCE(SUM(CASE
	  WHEN movement_type = 'reserve' THEN quantity
	  WHEN movement_type IN ('consume', 'release') THEN -quantity
	  ELSE 0
	END), 0)::bigint
	FROM inventory_stock_movements
	WHERE tenant_id = $1::uuid
	  AND batch_id = $2::uuid
	  AND lot_id = $3::uuid
	  AND movement_type IN ('reserve', 'consume', 'release')`, tenantID, row.batchID, row.lotID).Scan(&batchReserved); err != nil {
		return false, fmt.Errorf("vaccination: check batch reservation ledger: %w", err)
	}
	if batchReserved < qty {
		return false, domain.ErrStockGateBlocked
	}
	var movementID string
	err := tx.QueryRow(ctx, `
	INSERT INTO inventory_stock_movements (
	  tenant_id, lot_id, item_id, location_id, movement_type,
	  quantity, quantity_unit, batch_id, reason, idempotency_key,
	  context
	) VALUES (
	  $1::uuid, $2::uuid, $3::uuid, $4::uuid, 'consume',
	  $5::numeric, $6, $7::uuid, 'vaccination completion accepted', $8,
	  jsonb_build_object(
	    'obligation_id', $9::text,
	    'goat_id', $10::text,
	    'semantic_fingerprint',
	    md5($2::text || ':' || $3::text || ':' || $4::text || ':consume:' || $5::numeric::text || ':' || $6 || ':' || $7::text)
	  )
	)
	ON CONFLICT (tenant_id, idempotency_key) DO NOTHING
	RETURNING movement_id::text`, tenantID, row.lotID, itemID, locationID, qty, unit, row.batchID, key, row.obligationID, row.goatID).Scan(&movementID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("vaccination: record consume movement: %w", err)
	}
	tag, err := tx.Exec(ctx, `
	UPDATE inventory_stock
	SET quantity_in_stock = quantity_in_stock - $3::numeric,
	    quantity_reserved = quantity_reserved - $3::numeric,
	    row_version = row_version + 1,
	    updated_at = now()
	WHERE tenant_id = $1::uuid
	  AND stock_id = $2::uuid
	  AND quantity_reserved >= $3::numeric`, tenantID, row.lotID, qty)
	if err != nil {
		return false, fmt.Errorf("vaccination: adjust consume balances: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return false, domain.ErrStockGateBlocked
	}
	return true, nil
}

func (r *Repository) ensureCompletedStatusEvent(ctx context.Context, tx pgx.Tx, tenantID, obligationID string) (string, bool, error) {
	key := tenantID + ":" + obligationID + ":completed"
	var eventID string
	err := tx.QueryRow(ctx, `
	SELECT obligation_event_id::text
	FROM obligation_status_events
	WHERE tenant_id = $1::uuid
	  AND idempotency_key = $2
	LIMIT 1`, tenantID, key).Scan(&eventID)
	if err == nil {
		if err := r.ensureCompletedStatusIdempotencyKey(ctx, tx, tenantID, key, eventID); err != nil {
			return "", false, err
		}
		return eventID, false, nil
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return "", false, fmt.Errorf("vaccination: find completed status event: %w", err)
	}
	var reservedKey string
	err = tx.QueryRow(ctx, `
	INSERT INTO idempotency_keys (idempotency_key, tenant_id, scope, request_hash, status)
	VALUES ($1, $2::uuid, 'obligation.status_event', 'completed', 'started')
	ON CONFLICT (idempotency_key) DO NOTHING
	RETURNING idempotency_key`, key, tenantID).Scan(&reservedKey)
	if errors.Is(err, pgx.ErrNoRows) {
		err = tx.QueryRow(ctx, `
	SELECT obligation_event_id::text
	FROM obligation_status_events
	WHERE tenant_id = $1::uuid
	  AND idempotency_key = $2
	LIMIT 1`, tenantID, key).Scan(&eventID)
		if errors.Is(err, pgx.ErrNoRows) {
			return "", false, fmt.Errorf("vaccination: completed status event idempotency key is reserved without an event")
		}
		if err != nil {
			return "", false, fmt.Errorf("vaccination: load reserved completed status event: %w", err)
		}
		return eventID, false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("vaccination: reserve completed status event key: %w", err)
	}
	err = tx.QueryRow(ctx, `
	INSERT INTO obligation_status_events (
	  tenant_id, obligation_id, event_type, occurred_at, payload, idempotency_key
	) VALUES (
	  $1::uuid, $2::uuid, 'completed', now(), '{"event":"completed"}'::jsonb, $3
	)
	RETURNING obligation_event_id::text`, tenantID, obligationID, key).Scan(&eventID)
	if err != nil {
		return "", false, fmt.Errorf("vaccination: insert completed status event: %w", err)
	}
	if err := r.completeCompletedStatusIdempotencyKey(ctx, tx, tenantID, key, eventID); err != nil {
		return "", false, err
	}
	return eventID, true, nil
}

func (r *Repository) ensureCompletedStatusIdempotencyKey(ctx context.Context, tx pgx.Tx, tenantID, key, eventID string) error {
	if _, err := tx.Exec(ctx, `
	INSERT INTO idempotency_keys (idempotency_key, tenant_id, scope, request_hash, status, result_type, result_id, completed_at)
	VALUES ($1, $2::uuid, 'obligation.status_event', 'completed', 'completed', 'obligation_status_event', $3::uuid, now())
	ON CONFLICT (idempotency_key) DO NOTHING`, key, tenantID, eventID); err != nil {
		return fmt.Errorf("vaccination: backfill completed status idempotency key: %w", err)
	}
	return nil
}

func (r *Repository) completeCompletedStatusIdempotencyKey(ctx context.Context, tx pgx.Tx, tenantID, key, eventID string) error {
	if _, err := tx.Exec(ctx, `
	UPDATE idempotency_keys
	SET status = 'completed',
	    result_type = 'obligation_status_event',
	    result_id = $2::uuid,
	    completed_at = now()
	WHERE idempotency_key = $1
	  AND tenant_id = $3::uuid`, key, eventID, tenantID); err != nil {
		return fmt.Errorf("vaccination: complete status idempotency key: %w", err)
	}
	return nil
}

func insertVaccinationCompletedOutbox(ctx context.Context, tx pgx.Tx, tenantID, obligationID string) (bool, error) {
	eventID := deterministicOutboxUUID("vaccination.completed:" + tenantID + ":" + obligationID)
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
		return false, fmt.Errorf("vaccination: completed envelope: %w", err)
	}
	headers, err := json.Marshal(map[string]any{
		"producer":        "vaccination.AcceptCompletionAtomic",
		"schema_version":  vaccinationCompletedSchemaVersion,
		"obligation_id":   obligationID,
		"idempotency_key": idempotencyKey,
	})
	if err != nil {
		return false, fmt.Errorf("vaccination: completed headers: %w", err)
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
		return false, fmt.Errorf("vaccination: completed outbox: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

func deterministicOutboxUUID(seed string) string {
	sum := md5.Sum([]byte(seed))
	sum[6] = (sum[6] & 0x0f) | 0x30
	sum[8] = (sum[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", sum[0:4], sum[4:6], sum[6:8], sum[8:10], sum[10:16])
}

func optionalString(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// GetRecordedCompletion returns the SM-5 context before a recorded completion is accepted.
func (r *Repository) GetRecordedCompletion(ctx context.Context, tenantID, completionID string) (domain.AcceptedCompletion, bool, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tenant, err := pgconv.UUID(tenantID)
	if err != nil {
		return domain.AcceptedCompletion{}, false, fmt.Errorf("vaccination: tenant id: %w", err)
	}
	cid, err := pgconv.UUID(completionID)
	if err != nil {
		return domain.AcceptedCompletion{}, false, fmt.Errorf("vaccination: completion id: %w", err)
	}
	row, err := r.queries.GetRecordedVaccinationCompletion(ctx, vaccinationdb.GetRecordedVaccinationCompletionParams{
		TenantID:     tenant,
		CompletionID: cid,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.AcceptedCompletion{}, false, nil
	}
	if err != nil {
		return domain.AcceptedCompletion{}, false, fmt.Errorf("vaccination: get recorded completion: %w", err)
	}
	return domain.AcceptedCompletion{
		CompletionID:   completionID,
		Status:         "recorded",
		ObligationID:   row.ObligationID,
		GoatID:         row.GoatID,
		BatchID:        row.BatchID,
		LotID:          row.VaccineInventoryLotID,
		Doses:          row.Doses,
		AdministeredAt: row.AdministeredAt.Time,
	}, true, nil
}

// GetAcceptableCompletion returns context for a recorded or already-accepted completion so SM-5
// retries can resume idempotent side effects after a partial failure.
func (r *Repository) GetAcceptableCompletion(ctx context.Context, tenantID, completionID string) (domain.AcceptedCompletion, bool, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tenant, err := pgconv.UUID(tenantID)
	if err != nil {
		return domain.AcceptedCompletion{}, false, fmt.Errorf("vaccination: tenant id: %w", err)
	}
	cid, err := pgconv.UUID(completionID)
	if err != nil {
		return domain.AcceptedCompletion{}, false, fmt.Errorf("vaccination: completion id: %w", err)
	}
	row, err := r.queries.GetAcceptableVaccinationCompletion(ctx, vaccinationdb.GetAcceptableVaccinationCompletionParams{
		TenantID:     tenant,
		CompletionID: cid,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.AcceptedCompletion{}, false, nil
	}
	if err != nil {
		return domain.AcceptedCompletion{}, false, fmt.Errorf("vaccination: get acceptable completion: %w", err)
	}
	return domain.AcceptedCompletion{
		CompletionID:   row.CompletionID,
		Status:         row.Status,
		ObligationID:   row.ObligationID,
		GoatID:         row.GoatID,
		BatchID:        row.BatchID,
		LotID:          row.VaccineInventoryLotID,
		Doses:          row.Doses,
		AdministeredAt: row.AdministeredAt.Time,
	}, true, nil
}

// GetAcceptedCompletionForObligation returns the accepted completion that completed an obligation.
// The vaccination.completed consumer uses this to keep booster scheduling derived from canonical DB
// state instead of widening the event payload.
func (r *Repository) GetAcceptedCompletionForObligation(ctx context.Context, tenantID, obligationID string) (domain.AcceptedCompletion, bool, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	if _, err := pgconv.UUID(tenantID); err != nil {
		return domain.AcceptedCompletion{}, false, fmt.Errorf("vaccination: tenant id: %w", err)
	}
	if _, err := pgconv.UUID(obligationID); err != nil {
		return domain.AcceptedCompletion{}, false, fmt.Errorf("vaccination: obligation id: %w", err)
	}
	var row domain.AcceptedCompletion
	err := r.pool.QueryRow(ctx, `
	SELECT completion_id::text,
	       status,
	       obligation_id::text,
	       goat_id::text,
	       COALESCE(batch_id::text, ''),
	       COALESCE(vaccine_inventory_lot_id::text, ''),
	       COALESCE(doses, 1)::int,
	       administered_at
	FROM vaccination_completions
	WHERE tenant_id = $1::uuid
	  AND obligation_id = $2::uuid
	  AND status = 'accepted'
	ORDER BY verified_at DESC NULLS LAST, administered_at DESC, completion_id DESC
	LIMIT 1`, tenantID, obligationID).Scan(
		&row.CompletionID,
		&row.Status,
		&row.ObligationID,
		&row.GoatID,
		&row.BatchID,
		&row.LotID,
		&row.Doses,
		&row.AdministeredAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.AcceptedCompletion{}, false, nil
	}
	if err != nil {
		return domain.AcceptedCompletion{}, false, fmt.Errorf("vaccination: get accepted completion for obligation: %w", err)
	}
	return row, true, nil
}

// GetAcceptableCompletionByIdempotency returns context for a recorded or already-accepted direct
// completion keyed by idempotency key.
func (r *Repository) GetAcceptableCompletionByIdempotency(ctx context.Context, tenantID, idempotencyKey string) (domain.AcceptedCompletion, bool, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tenant, err := pgconv.UUID(tenantID)
	if err != nil {
		return domain.AcceptedCompletion{}, false, fmt.Errorf("vaccination: tenant id: %w", err)
	}
	row, err := r.queries.GetAcceptableVaccinationCompletionByIdempotency(ctx, vaccinationdb.GetAcceptableVaccinationCompletionByIdempotencyParams{
		TenantID:       tenant,
		IdempotencyKey: idempotencyKey,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.AcceptedCompletion{}, false, nil
	}
	if err != nil {
		return domain.AcceptedCompletion{}, false, fmt.Errorf("vaccination: get acceptable completion by idempotency: %w", err)
	}
	return domain.AcceptedCompletion{
		CompletionID:   row.CompletionID,
		Status:         row.Status,
		ObligationID:   row.ObligationID,
		GoatID:         row.GoatID,
		BatchID:        row.BatchID,
		LotID:          row.VaccineInventoryLotID,
		Doses:          row.Doses,
		AdministeredAt: row.AdministeredAt.Time,
	}, true, nil
}

// RejectCompletion marks a recorded completion rejected (SM-5 rework). applied is false on replay.
func (r *Repository) RejectCompletion(ctx context.Context, tenantID, completionID, reason string, verifiedBy *string) (bool, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tenant, err := pgconv.UUID(tenantID)
	if err != nil {
		return false, fmt.Errorf("vaccination: tenant id: %w", err)
	}
	cid, err := pgconv.UUID(completionID)
	if err != nil {
		return false, fmt.Errorf("vaccination: completion id: %w", err)
	}
	n, err := r.queries.RejectVaccinationCompletion(ctx, vaccinationdb.RejectVaccinationCompletionParams{
		VerifiedBy:      pgconv.NullableUUID(verifiedBy),
		RejectionReason: pgconv.Text(reason),
		TenantID:        tenant,
		CompletionID:    cid,
	})
	if err != nil {
		return false, fmt.Errorf("vaccination: reject completion: %w", err)
	}
	return n == 1, nil
}

// ListRecordedCompletionsByTask returns the still-recorded completion ids captured under a SOP
// task's submissions (the verify fan-out source).
func (r *Repository) ListRecordedCompletionsByTask(ctx context.Context, tenantID, taskID string) ([]string, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tenant, err := pgconv.UUID(tenantID)
	if err != nil {
		return nil, fmt.Errorf("vaccination: tenant id: %w", err)
	}
	task, err := pgconv.UUID(taskID)
	if err != nil {
		return nil, fmt.Errorf("vaccination: task id: %w", err)
	}
	ids, err := r.queries.ListRecordedCompletionsByTask(ctx, vaccinationdb.ListRecordedCompletionsByTaskParams{
		TenantID: tenant, TaskID: task,
	})
	if err != nil {
		return nil, fmt.Errorf("vaccination: list recorded completions by task: %w", err)
	}
	return ids, nil
}

// RecordCompletionsFromSubmission records one completion per per-goat SOP submission item for a
// vaccination task. The matching obligation comes from the generic obligation batch/task linkage.
func (r *Repository) RecordCompletionsFromSubmission(ctx context.Context, tenantID, taskID, submissionID, recordedBy string) (int, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tenant, err := pgconv.UUID(tenantID)
	if err != nil {
		return 0, fmt.Errorf("vaccination: tenant id: %w", err)
	}
	task, err := pgconv.UUID(taskID)
	if err != nil {
		return 0, fmt.Errorf("vaccination: task id: %w", err)
	}
	submission, err := pgconv.UUID(submissionID)
	if err != nil {
		return 0, fmt.Errorf("vaccination: submission id: %w", err)
	}
	rows, err := r.pool.Query(ctx, `
INSERT INTO vaccination_completions (
  tenant_id,
  obligation_id,
  batch_id,
  goat_id,
  sop_submission_item_id,
  vaccine_inventory_lot_id,
  doses,
  dose_ml_given,
  route_site,
  adverse_reaction,
  cold_chain_verified,
  administered_at,
  status,
  withdrawal_until_date,
  recorded_by,
  idempotency_key
)
SELECT
  si.tenant_id,
  oi.obligation_id,
  oi.batch_id,
  si.goat_id,
  si.item_id,
  nullif(ss.answers ->> 'vaccine_lot_id', '')::uuid,
  COALESCE(nullif(ss.answers ->> 'doses', '')::int, 1),
  nullif(ss.answers ->> 'dose_ml_given', '')::numeric,
  nullif(ss.answers ->> 'route_site', ''),
  COALESCE((ss.answers ->> 'adverse_reaction')::boolean, false),
  COALESCE((ss.answers ->> 'cold_chain_verified')::boolean, false),
  COALESCE(nullif(ss.answers ->> 'administered_at', '')::timestamptz, ss.submitted_at),
  'recorded',
  COALESCE(
    (nullif(ss.answers ->> 'withdrawal_until', '')::timestamptz)::date,
    nullif(ss.answers ->> 'withdrawal_until_date', '')::date,
    CASE
      WHEN pr.withdrawal_days IS NOT NULL THEN
        (COALESCE(nullif(ss.answers ->> 'administered_at', '')::timestamptz, ss.submitted_at)::date + pr.withdrawal_days)
      ELSE NULL
    END
  ),
  nullif($4, '')::uuid,
  'vaccination:sop_submission_item:' || si.item_id::text
FROM sop_submission_items si
JOIN sop_submissions ss
  ON ss.tenant_id = si.tenant_id
 AND ss.submission_id = si.submission_id
JOIN sop_tasks st
  ON st.tenant_id = si.tenant_id
 AND st.task_id = si.task_id
JOIN sop_definitions sd
  ON sd.tenant_id = st.tenant_id
 AND sd.sop_id = st.sop_id
LEFT JOIN obligation_batches ob
  ON ob.tenant_id = st.tenant_id
 AND ob.sop_task_id = st.task_id
JOIN obligation_instances oi
  ON oi.tenant_id = si.tenant_id
 AND oi.target_type = 'goat'
 AND oi.target_id = si.goat_id
 AND (
      oi.sop_task_id = st.task_id
      OR (ob.batch_id IS NOT NULL AND oi.batch_id = ob.batch_id)
 )
JOIN protocol_rules pr
  ON pr.tenant_id = oi.tenant_id
 AND pr.rule_id = oi.rule_id
WHERE si.tenant_id = $1
  AND si.task_id = $2
  AND si.submission_id = $3
  AND si.goat_id IS NOT NULL
  AND si.state IN ('accepted', 'needs_review')
  AND oi.status NOT IN ('completed', 'waived', 'canceled', 'superseded')
  AND (
    oi.batch_id IS NULL
    OR (
      nullif(ss.answers ->> 'vaccine_lot_id', '') IS NOT NULL
      AND COALESCE((ss.answers ->> 'cold_chain_verified')::boolean, false)
    )
  )
  AND (
    sd.code IN ('vaccination.drive', 'vaccination.session')
    OR st.task_type IN ('vaccination', 'vaccination_drive', 'vaccination_session')
  )
ON CONFLICT DO NOTHING
RETURNING completion_id::text`,
		tenant, task, submission, recordedBy)
	if err != nil {
		return 0, fmt.Errorf("vaccination: record completions from submission: %w", err)
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		count++
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("vaccination: record completions from submission rows: %w", err)
	}
	if _, err := r.pool.Exec(ctx, `
UPDATE vaccination_completions vc
SET withdrawal_until_date = COALESCE(
      (nullif(ss.answers ->> 'withdrawal_until', '')::timestamptz)::date,
      nullif(ss.answers ->> 'withdrawal_until_date', '')::date,
      CASE
        WHEN pr.withdrawal_days IS NOT NULL THEN
          (COALESCE(nullif(ss.answers ->> 'administered_at', '')::timestamptz, ss.submitted_at)::date + pr.withdrawal_days)
        ELSE NULL
      END
    ),
    row_version = vc.row_version + 1,
    updated_at = now()
FROM sop_submission_items si
JOIN sop_submissions ss
  ON ss.tenant_id = si.tenant_id
 AND ss.submission_id = si.submission_id
JOIN obligation_instances oi
  ON oi.tenant_id = si.tenant_id
JOIN protocol_rules pr
  ON pr.tenant_id = oi.tenant_id
 AND pr.rule_id = oi.rule_id
WHERE vc.tenant_id = $1
  AND si.tenant_id = vc.tenant_id
  AND si.item_id = vc.sop_submission_item_id
  AND si.task_id = $2
  AND si.submission_id = $3
  AND oi.tenant_id = vc.tenant_id
  AND oi.obligation_id = vc.obligation_id
  AND vc.withdrawal_until_date IS NULL
  AND (
    nullif(ss.answers ->> 'withdrawal_until', '') IS NOT NULL
    OR nullif(ss.answers ->> 'withdrawal_until_date', '') IS NOT NULL
    OR pr.withdrawal_days IS NOT NULL
  )`, tenant, task, submission); err != nil {
		return 0, fmt.Errorf("vaccination: backfill submission withdrawal date: %w", err)
	}
	eligibleItems, materializedItems, err := r.submissionFanoutCounts(ctx, tenant, task, submission)
	if err != nil {
		return 0, err
	}
	if materializedItems < eligibleItems {
		return materializedItems, fmt.Errorf("vaccination: materialized %d of %d eligible submission items", materializedItems, eligibleItems)
	}
	if materializedItems > 0 {
		return materializedItems, nil
	}
	return count, nil
}

func (r *Repository) submissionFanoutCounts(ctx context.Context, tenant, task, submission pgtype.UUID) (eligibleItems, materializedItems int, err error) {
	err = r.pool.QueryRow(ctx, `
	SELECT count(si.item_id)::int,
	       count(DISTINCT vc.sop_submission_item_id)::int
	FROM sop_submission_items si
	LEFT JOIN vaccination_completions vc
	  ON vc.tenant_id = si.tenant_id
	 AND vc.sop_submission_item_id = si.item_id
	WHERE si.tenant_id = $1
	  AND si.task_id = $2
	  AND si.submission_id = $3
	  AND si.goat_id IS NOT NULL
	  AND si.state IN ('accepted', 'needs_review')`,
		tenant, task, submission).Scan(&eligibleItems, &materializedItems)
	if err != nil {
		return 0, 0, fmt.Errorf("vaccination: count submission fanout rows: %w", err)
	}
	return eligibleItems, materializedItems, nil
}

// ListRecordedCompletions returns completions awaiting review (status='recorded'), earliest
// administered first (the Verification queue).
func (r *Repository) ListRecordedCompletions(ctx context.Context, tenantID, parkID string, limit int32) ([]domain.RecordedCompletion, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tenant, err := pgconv.UUID(tenantID)
	if err != nil {
		return nil, fmt.Errorf("vaccination: tenant id: %w", err)
	}
	var park pgtype.UUID
	if parkID != "" {
		park, err = pgconv.UUID(parkID)
		if err != nil {
			return nil, fmt.Errorf("vaccination: park id: %w", err)
		}
	}
	if limit <= 0 {
		limit = 100
	}
	rows, err := r.queries.ListRecordedCompletions(ctx, vaccinationdb.ListRecordedCompletionsParams{
		TenantID: tenant, ParkID: park, RowLimit: limit,
	})
	if err != nil {
		return nil, fmt.Errorf("vaccination: list recorded completions: %w", err)
	}
	out := make([]domain.RecordedCompletion, 0, len(rows))
	for _, row := range rows {
		out = append(out, domain.RecordedCompletion{
			CompletionID:   row.CompletionID,
			ObligationID:   row.ObligationID,
			GoatID:         row.GoatID,
			BatchID:        row.BatchID,
			SOPTaskID:      row.SopTaskID,
			SOPTaskVersion: row.SopTaskRowVersion,
			AdministeredAt: row.AdministeredAt.Time,
			Doses:          row.Doses,
			RouteSite:      row.RouteSite,
		})
	}
	return out, nil
}

// ListCompletionsByGoat returns a goat's vaccination history (most recent first).
func (r *Repository) ListCompletionsByGoat(ctx context.Context, tenantID, goatID string, limit int32) ([]domain.CompletionHistoryItem, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tenant, err := pgconv.UUID(tenantID)
	if err != nil {
		return nil, fmt.Errorf("vaccination: tenant id: %w", err)
	}
	goat, err := pgconv.UUID(goatID)
	if err != nil {
		return nil, fmt.Errorf("vaccination: goat id: %w", err)
	}
	if limit <= 0 {
		limit = 100
	}
	rows, err := r.queries.ListVaccinationCompletionsByGoat(ctx, vaccinationdb.ListVaccinationCompletionsByGoatParams{
		TenantID: tenant, GoatID: goat, RowLimit: limit,
	})
	if err != nil {
		return nil, fmt.Errorf("vaccination: list completions: %w", err)
	}
	out := make([]domain.CompletionHistoryItem, 0, len(rows))
	for _, row := range rows {
		out = append(out, domain.CompletionHistoryItem{
			CompletionID:        row.CompletionID,
			ObligationID:        row.ObligationID,
			BatchID:             row.BatchID,
			AdministeredAt:      row.AdministeredAt.Time,
			Status:              row.Status,
			Doses:               row.Doses,
			RouteSite:           row.RouteSite,
			AdverseReaction:     row.AdverseReaction,
			WithdrawalUntilDate: pgconv.DateValue(row.WithdrawalUntilDate),
		})
	}
	return out, nil
}

// GetLastAcceptedForGoat returns the most recent accepted administration (found=false when none).
func (r *Repository) GetLastAcceptedForGoat(ctx context.Context, tenantID, goatID string) (domain.LastAccepted, bool, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tenant, err := pgconv.UUID(tenantID)
	if err != nil {
		return domain.LastAccepted{}, false, fmt.Errorf("vaccination: tenant id: %w", err)
	}
	goat, err := pgconv.UUID(goatID)
	if err != nil {
		return domain.LastAccepted{}, false, fmt.Errorf("vaccination: goat id: %w", err)
	}
	row, err := r.queries.GetLastAcceptedCompletionForGoat(ctx, vaccinationdb.GetLastAcceptedCompletionForGoatParams{TenantID: tenant, GoatID: goat})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.LastAccepted{}, false, nil
	}
	if err != nil {
		return domain.LastAccepted{}, false, fmt.Errorf("vaccination: last accepted: %w", err)
	}
	return domain.LastAccepted{
		CompletionID:   row.CompletionID,
		ObligationID:   row.ObligationID,
		AdministeredAt: row.AdministeredAt.Time,
	}, true, nil
}

func (r *Repository) eligParams(f domain.ImpactFilter) (vaccinationdb.CountEligibleGoatsParams, error) {
	tenant, err := pgconv.UUID(f.TenantID)
	if err != nil {
		return vaccinationdb.CountEligibleGoatsParams{}, fmt.Errorf("vaccination: tenant id: %w", err)
	}
	return vaccinationdb.CountEligibleGoatsParams{
		TenantID: tenant,
		Stage:    f.Stage,
		Sex:      f.Sex,
		Breed:    f.Breed,
		Health:   f.Health,
		ParkID:   pgconv.NullableUUID(f.ParkID),
	}, nil
}

// CountEligibleGoats counts alive goats matching the eligibility filter.
func (r *Repository) CountEligibleGoats(ctx context.Context, f domain.ImpactFilter) (int64, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	p, err := r.eligParams(f)
	if err != nil {
		return 0, err
	}
	n, err := r.queries.CountEligibleGoats(ctx, p)
	if err != nil {
		return 0, fmt.Errorf("vaccination: count eligible: %w", err)
	}
	return n, nil
}

// CountCatchupGoats counts eligible goats with a prior accepted completion.
func (r *Repository) CountCatchupGoats(ctx context.Context, f domain.ImpactFilter) (int64, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	p, err := r.eligParams(f)
	if err != nil {
		return 0, err
	}
	n, err := r.queries.CountCatchupGoats(ctx, vaccinationdb.CountCatchupGoatsParams(p))
	if err != nil {
		return 0, fmt.Errorf("vaccination: count catchup: %w", err)
	}
	return n, nil
}

// CountEligibleShedScopes counts distinct sheds holding eligible goats (≈ drive batches).
func (r *Repository) CountEligibleShedScopes(ctx context.Context, f domain.ImpactFilter) (int64, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	p, err := r.eligParams(f)
	if err != nil {
		return 0, err
	}
	n, err := r.queries.CountEligibleShedScopes(ctx, vaccinationdb.CountEligibleShedScopesParams(p))
	if err != nil {
		return 0, fmt.Errorf("vaccination: count shed scopes: %w", err)
	}
	return n, nil
}

// ListEligibleGoatsForGeneration returns a chunked page of the in-care cohort for SM-1.
func (r *Repository) ListEligibleGoatsForGeneration(ctx context.Context, f domain.ImpactFilter, afterGoatID string, limit int32) ([]domain.EligibleGoat, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tenant, err := pgconv.UUID(f.TenantID)
	if err != nil {
		return nil, fmt.Errorf("vaccination: tenant id: %w", err)
	}
	after := afterGoatID
	if after == "" {
		after = "00000000-0000-0000-0000-000000000000"
	}
	afterUUID, err := pgconv.UUID(after)
	if err != nil {
		return nil, fmt.Errorf("vaccination: cursor: %w", err)
	}
	if limit <= 0 {
		limit = 500
	}
	rows, err := r.queries.ListEligibleGoatsForGeneration(ctx, vaccinationdb.ListEligibleGoatsForGenerationParams{
		TenantID:    tenant,
		Stage:       f.Stage,
		Sex:         f.Sex,
		Breed:       f.Breed,
		Health:      f.Health,
		ParkID:      pgconv.NullableUUID(f.ParkID),
		AfterGoatID: afterUUID,
		RowLimit:    limit,
	})
	if err != nil {
		return nil, fmt.Errorf("vaccination: list eligible goats: %w", err)
	}
	out := make([]domain.EligibleGoat, 0, len(rows))
	for _, row := range rows {
		out = append(out, domain.EligibleGoat{
			GoatID:               row.GoatID,
			DOB:                  pgconv.DateValue(row.Dob),
			EntryDate:            pgconv.DateValue(row.EntryDate),
			LifecycleStatus:      row.LifecycleStatus,
			HealthStatus:         row.HealthStatus,
			ReproductiveStatus:   row.ReproductiveStatus,
			ShedID:               row.ShedID,
			ParkID:               row.ParkID,
			Sex:                  row.Sex,
			Breed:                row.Breed,
			Stage:                row.ManagementStage,
			LocationIsQuarantine: row.LocationIsQuarantine,
			LocationIsICU:        row.LocationIsIcu,
		})
	}
	return out, nil
}

// GetGoatForGeneration loads one goat's generation fields (incl sex/breed/stage).
func (r *Repository) GetGoatForGeneration(ctx context.Context, tenantID, goatID string) (domain.EligibleGoat, bool, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tenant, err := pgconv.UUID(tenantID)
	if err != nil {
		return domain.EligibleGoat{}, false, fmt.Errorf("vaccination: tenant id: %w", err)
	}
	goat, err := pgconv.UUID(goatID)
	if err != nil {
		return domain.EligibleGoat{}, false, fmt.Errorf("vaccination: goat id: %w", err)
	}
	row, err := r.queries.GetGoatForGeneration(ctx, vaccinationdb.GetGoatForGenerationParams{TenantID: tenant, GoatID: goat})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.EligibleGoat{}, false, nil
	}
	if err != nil {
		return domain.EligibleGoat{}, false, fmt.Errorf("vaccination: get goat for generation: %w", err)
	}
	return domain.EligibleGoat{
		GoatID:               row.GoatID,
		DOB:                  pgconv.DateValue(row.Dob),
		EntryDate:            pgconv.DateValue(row.EntryDate),
		LifecycleStatus:      row.LifecycleStatus,
		HealthStatus:         row.HealthStatus,
		ReproductiveStatus:   row.ReproductiveStatus,
		ShedID:               row.ShedID,
		ParkID:               row.ParkID,
		Sex:                  row.Sex,
		Breed:                row.Breed,
		Stage:                row.ManagementStage,
		LocationIsQuarantine: row.LocationIsQuarantine,
		LocationIsICU:        row.LocationIsIcu,
	}, true, nil
}

// HasTrustedCompletionEvidence returns true when prior trusted evidence already satisfies the
// matching protocol rule for this goat as of this generation run, so SM-1 must not re-issue the dose.
func (r *Repository) HasTrustedCompletionEvidence(ctx context.Context, tenantID, goatID, protocolVersionID, ruleID, doseCode string, dueAt, generationAt time.Time) (bool, error) {
	candidate := domain.TrustedCompletionCandidate{
		GoatID:   goatID,
		RuleID:   ruleID,
		DoseCode: doseCode,
		DueAt:    dueAt,
	}
	hits, err := r.HasTrustedCompletionEvidenceBatch(ctx, tenantID, protocolVersionID, []domain.TrustedCompletionCandidate{candidate}, generationAt)
	if err != nil {
		return false, err
	}
	return hits[candidate.Key()], nil
}

type trustedCompletionCandidatePayload struct {
	CandidateKey string `json:"candidate_key"`
	GoatID       string `json:"goat_id"`
	RuleID       string `json:"rule_id"`
	DoseCode     string `json:"dose_code"`
	DueAt        string `json:"due_at"`
	RuleRepeat   string `json:"rule_repeat"`
}

// HasTrustedCompletionEvidenceBatch resolves trusted-history suppression for a generation page.
// Two trusted sources suppress due work:
//  1. Reviewed supplier/HF evidence (procurement_hf_vaccination_evidence, review_status='trusted').
//  2. Accepted and verified Goat OS administrations (vaccination_completions, status='accepted',
//     verified_at set).
//
// Both sources match the SAME protocol (protocol_id), dose_code, and sequence across protocol
// versions so that publishing a new version of a protocol the goat already completed does not
// duplicate the dose. Repeated rules additionally include the candidate due date as a cycle fence:
// accepted Goat OS completions must belong to the same obligation due date, and reviewed imported/HF
// evidence must be administered on or after that cycle's due date.
func (r *Repository) HasTrustedCompletionEvidenceBatch(ctx context.Context, tenantID, protocolVersionID string, candidates []domain.TrustedCompletionCandidate, generationAt time.Time) (map[string]bool, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	hits := make(map[string]bool, len(candidates))
	tenant, err := pgconv.UUID(tenantID)
	if err != nil {
		return nil, fmt.Errorf("vaccination: tenant id: %w", err)
	}
	version, err := pgconv.UUID(protocolVersionID)
	if err != nil {
		return nil, fmt.Errorf("vaccination: protocol version id: %w", err)
	}
	if len(candidates) == 0 {
		return hits, nil
	}
	payloadRows := make([]trustedCompletionCandidatePayload, 0, len(candidates))
	for _, candidate := range candidates {
		payloadRows = append(payloadRows, trustedCompletionCandidatePayload{
			CandidateKey: candidate.Key(),
			GoatID:       candidate.GoatID,
			RuleID:       candidate.RuleID,
			DoseCode:     candidate.DoseCode,
			DueAt:        candidate.DueAt.UTC().Format(time.RFC3339Nano),
			RuleRepeat:   candidate.Repeat,
		})
	}
	payload, err := json.Marshal(payloadRows)
	if err != nil {
		return nil, fmt.Errorf("vaccination: trusted completion evidence payload: %w", err)
	}
	rows, err := r.pool.Query(ctx, `
	WITH candidate AS (
	  SELECT c.candidate_key,
	         c.goat_id::uuid AS goat_id,
	         c.rule_id::uuid AS rule_id,
	         c.dose_code,
	         c.due_at::timestamptz AS due_at,
	         COALESCE(NULLIF(c.rule_repeat, ''), 'none') AS requested_repeat
	  FROM jsonb_to_recordset($4::jsonb) AS c(
	    candidate_key text,
	    goat_id text,
	    rule_id text,
	    dose_code text,
	    due_at text,
	    rule_repeat text
	  )
	),
	target AS (
	  SELECT c.candidate_key,
	         c.goat_id,
	         c.rule_id,
	         c.dose_code,
	         c.due_at,
	         target_pr.sequence,
	         COALESCE(NULLIF(target_pr."repeat", ''), c.requested_repeat, 'none') AS rule_repeat,
	         target_pv.protocol_id
	  FROM candidate c
	  JOIN protocol_rules target_pr
	    ON target_pr.tenant_id = $1::uuid
	   AND target_pr.protocol_version_id = $2::uuid
	   AND target_pr.rule_id = c.rule_id
	   AND target_pr.dose_code = c.dose_code
	  JOIN protocol_versions target_pv
	    ON target_pv.tenant_id = target_pr.tenant_id
	   AND target_pv.protocol_version_id = target_pr.protocol_version_id
	),
	trusted_procurement AS (
	  SELECT DISTINCT t.candidate_key
	  FROM target t
	  JOIN procurement_hf_vaccination_evidence ev
	    ON ev.tenant_id = $1::uuid
	   AND ev.goat_id = t.goat_id
	   AND ev.review_status = 'trusted'
	   AND ev.reviewed_at IS NOT NULL
	  JOIN protocol_rules ev_pr
	    ON ev_pr.tenant_id = ev.tenant_id
	   AND ev_pr.rule_id = ev.rule_id
	  JOIN protocol_versions ev_pv
	    ON ev_pv.tenant_id = ev.tenant_id
	   AND ev_pv.protocol_version_id = ev.protocol_version_id
	  JOIN goats g
	    ON g.tenant_id = ev.tenant_id
	   AND g.goat_id = ev.goat_id
	  LEFT JOIN procurement_load_goats plg
	    ON plg.tenant_id = ev.tenant_id
	   AND plg.load_id = ev.load_id
	   AND plg.goat_id = ev.goat_id
	  WHERE ev.dose_code = t.dose_code
	    AND ev_pr.dose_code = t.dose_code
	    AND ev_pr.sequence = t.sequence
	    AND ev_pv.protocol_id = t.protocol_id
	    AND ev.administered_at <= $3::timestamptz
	    AND ev.reviewed_at <= $3::timestamptz
	    AND (t.rule_repeat = 'none' OR ev.administered_at >= t.due_at)
	    AND (
	      plg.intake_accepted_at IS NULL
	      OR ev.administered_at <= plg.intake_accepted_at
	    )
	    AND (
	      g.entry_date IS NULL
	      OR ev.administered_at < (g.entry_date::timestamptz + interval '1 day')
	    )
	),
	trusted_completion AS (
	  SELECT DISTINCT t.candidate_key
	  FROM target t
	  JOIN vaccination_completions vc
	    ON vc.tenant_id = $1::uuid
	   AND vc.goat_id = t.goat_id
	   AND vc.status = 'accepted'
	   AND vc.verified_at IS NOT NULL
	  JOIN obligation_instances oi
	    ON oi.tenant_id = vc.tenant_id
	   AND oi.obligation_id = vc.obligation_id
	  JOIN protocol_rules cpr
	    ON cpr.tenant_id = oi.tenant_id
	   AND cpr.rule_id = oi.rule_id
	  JOIN protocol_versions cpv
	    ON cpv.tenant_id = oi.tenant_id
	   AND cpv.protocol_version_id = oi.protocol_version_id
	  WHERE cpr.dose_code = t.dose_code
	    AND cpr.sequence = t.sequence
	    AND cpv.protocol_id = t.protocol_id
	    AND vc.administered_at <= $3::timestamptz
	    AND vc.verified_at <= $3::timestamptz
	    AND (t.rule_repeat = 'none' OR oi.due_at = t.due_at)
	)
	SELECT candidate_key FROM trusted_procurement
	UNION
	SELECT candidate_key FROM trusted_completion`, tenant, version, generationAt, payload)
	if err != nil {
		return nil, fmt.Errorf("vaccination: trusted completion evidence: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, fmt.Errorf("vaccination: trusted completion evidence scan: %w", err)
		}
		hits[key] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("vaccination: trusted completion evidence rows: %w", err)
	}
	return hits, nil
}

// SumAvailableStock returns available (unreserved) quantity + earliest expiry for an item.
func (r *Repository) SumAvailableStock(ctx context.Context, tenantID, itemID string, locationID *string) (string, *time.Time, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tenant, err := pgconv.UUID(tenantID)
	if err != nil {
		return "", nil, fmt.Errorf("vaccination: tenant id: %w", err)
	}
	item, err := pgconv.UUID(itemID)
	if err != nil {
		return "", nil, fmt.Errorf("vaccination: item id: %w", err)
	}
	row, err := r.queries.SumAvailableStockForItem(ctx, vaccinationdb.SumAvailableStockForItemParams{
		TenantID:   tenant,
		ItemID:     item,
		LocationID: pgconv.NullableUUID(locationID),
	})
	if err != nil {
		return "", nil, fmt.Errorf("vaccination: sum available stock: %w", err)
	}
	return pgconv.NumericString(row.Available), pgconv.DateValue(row.EarliestExpiry), nil
}
