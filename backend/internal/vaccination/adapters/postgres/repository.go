// Package postgres implements the vaccination Repository over generated sqlc queries.
package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/audit"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	platformoutbox "github.com/vgoats/goatos/backend/internal/platform/outbox"
	"github.com/vgoats/goatos/backend/internal/platform/pgconv"
	vaccinationdb "github.com/vgoats/goatos/backend/internal/vaccination/adapters/postgres/sqlc"
	"github.com/vgoats/goatos/backend/internal/vaccination/domain"
	"github.com/vgoats/goatos/backend/internal/vaccination/ports"
)

const defaultQueryTimeout = 3 * time.Second

// defaultDailyVaccinationCap mirrors the operator-drive default used by vaccinationexecution. Used when a
// tenant has no vaccination_capacity_config row yet.
const defaultDailyVaccinationCap int64 = 200

const (
	vaccinationCompletedEventType     = "vaccination.completed"
	vaccinationCompletedSchemaVersion = "1.0.0"
	vaccinationCompletedSchemaRef     = "domain-event-envelope.v1"
	vaccinationCompletedTopic         = "vaccination.events"
	// obligationInProgressEventType/obligationLifecycle* mirror the obligation module's own
	// "obligation.in_progress" producer constants exactly (same event type, schema version/ref,
	// topic) so recomputeObligationBatchStatusOnComplete's sibling in_progress trigger -- this
	// module's twin of obligation.Repository.MarkCompleted's -- emits an identical envelope shape.
	obligationInProgressEventType    = "obligation.in_progress"
	obligationLifecycleSchemaVersion = "1.0.0"
	obligationLifecycleSchemaRef     = "domain-event-envelope.v1"
	obligationLifecycleTopic         = "obligation.events"
)

// Repository is the Postgres-backed vaccination repository.
type Repository struct {
	// An INSTANCE logger, not package-level slog: check-boundaries.sh bans slog.Error/Warn/
	// Info/Debug outside platform/observability, and an adapter that cannot report an
	// inventory anomaly would just swallow it again.
	log          *slog.Logger
	pool         *pgxpool.Pool
	queries      *vaccinationdb.Queries
	queryTimeout time.Duration
}

// NewRepository builds a Repository bound to a pgx pool.
func NewRepository(pool *pgxpool.Pool, queryTimeout time.Duration) *Repository {
	if queryTimeout <= 0 {
		queryTimeout = defaultQueryTimeout
	}
	return &Repository{log: slog.Default(), pool: pool, queries: vaccinationdb.New(pool), queryTimeout: queryTimeout}
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

func nullUUID(v string) any {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	return v
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
    failed_goat_count = 0,
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
          suppressed_trusted_history_count, failed_goat_count, COALESCE(cursor_goat_id::text, ''),
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
    failed_goat_count = $8,
    skipped_no_due_date_count = $9,
    suppressed_trusted_history_count = $10,
    cursor_goat_id = nullif($11::text, '')::uuid,
    last_error = nullif($12, ''),
    updated_at = now(),
    row_version = row_version + 1
WHERE tenant_id = $1::uuid
  AND run_id = $2::uuid`,
		tenantID, runID, status, completedAt, result.Generated, result.Deferred,
		result.Reopened, result.FailedGoats, result.SkippedNoDueDate, result.SuppressedByTrustedHistory, cursorGoatID, lastError)
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
       suppressed_trusted_history_count, failed_goat_count, COALESCE(cursor_goat_id::text, ''),
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
		&run.FailedGoats,
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
		&run.FailedGoats,
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
		// Maintainer state-model: the obligation closes the moment its completion is RECORDED
		// (sopbridge.closeObligationsForCompletions / obligation.Repository.MarkCompleted at
		// record time), not on verification. So by the time a verifier approves, this obligation
		// is routinely ALREADY 'completed' -- that is the expected, common case now, not a stale
		// or conflicting state. Only refuse when there is genuinely nothing left to accept (the
		// completion itself is not 'recorded' -- e.g. it was rejected/archived out from under this
		// call). The completed-obligation branch below (`if obligationStatus != "completed"`)
		// already knows to skip re-completing an obligation that is completed for this reason.
		if row.status != "recorded" {
			return domain.AcceptCompletionAtomicResult{}, domain.ErrCompletionNotOpen
		}
	} else if obligationStatus != "scheduled" && obligationStatus != "due" && obligationStatus != "in_progress" && obligationStatus != "missed" {
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
	    withdrawal_until_date = COALESCE($4::date, withdrawal_until_date),
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
	// PEND-1 drive-close/in-progress trigger (judge Finding 1 gap found while verifying the fix):
	// this atomic accept path completes the obligation via its OWN direct obligation_instances UPDATE
	// above, entirely bypassing obligation.Repository.MarkCompleted -- and per
	// vaccination/app/completion.go's CompletionService.AcceptExisting/Accept, this atomic path is
	// the one every real Postgres-backed production call takes (s.vacc.AcceptCompletionAtomic /
	// RecordAndAcceptCompletionAtomic succeed whenever the adapter implements them, which the real
	// Postgres repository always does; the s.obl.MarkCompleted fallback only runs for non-Postgres
	// fakes). Without recomputing the owning batch's status here too, a batch with still-open
	// siblings would never reach 'completed' in production -- a finished drive would show "running"
	// forever. Mirror MarkCompleted's own recompute AND its sibling in_progress trigger exactly
	// (including the TOCTOU fix, judge Finding 3) -- see recomputeObligationBatchStatusOnComplete.
	if result.Completed && row.batchID != "" {
		if err := r.recomputeObligationBatchStatusOnComplete(ctx, tx, in.TenantID, row.batchID, row.obligationID); err != nil {
			return domain.AcceptCompletionAtomicResult{}, err
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
				"domain":   "vaccination",
				"module":   "vaccination",
				"category": "completion",
				"result":   "recorded",
				"status":   "completed",
				"source":   "vaccination_accept_completion_atomic",
			},
			TraceID: "vaccination.completed:" + row.obligationID,
		}); err != nil {
			return domain.AcceptCompletionAtomicResult{}, fmt.Errorf("vaccination: completed audit: %w", err)
		}
	}
	return result, nil
}

// recomputeObligationBatchStatusOnComplete is the atomic-accept-path twin of
// obligation.Repository.MarkCompleted's own batch recompute AND sibling in_progress trigger (see
// that function's doc comment for the full PEND-1 rationale): completed when no sibling obligation
// on the batch remains open, else planned -> in_progress -- and when the batch stays open, every
// still-open (scheduled/due) sibling obligation on that batch is ALSO flipped to in_progress in this
// same transaction, with one 'in_progress' status event + outbox row per newly-transitioned sibling.
// This mirror exists because this atomic path -- not obligation.Repository.MarkCompleted -- is the
// one every real Postgres-backed vaccination completion takes in production (CompletionService.
// Accept/AcceptExisting only fall back to obligation.Repository.MarkCompleted for non-Postgres
// fakes); without duplicating the sibling trigger here too, PEND-1 would remain unreachable for the
// dominant vaccination-drive completion path. Already-terminal batches (completed/canceled/
// superseded) are left untouched by the WHERE guard.
//
// TOCTOU fix (judge Finding 3, applied here too since this path duplicates the same recompute):
// lock the batch row FIRST, as its own statement, before evaluating the sibling-open subquery.
// READ COMMITTED + EvalPlanQual only guarantees a fresh view of the ROW BEING LOCKED/UPDATED after
// unblocking from a concurrent writer on that SAME row -- NOT of other rows read via a subquery
// embedded in that same (previously blocked) statement. Two obligations on the same batch
// completing concurrently -- one via this atomic path, one via obligation.Repository.MarkCompleted,
// or both via this path -- would otherwise race exactly as described there.
func (r *Repository) recomputeObligationBatchStatusOnComplete(ctx context.Context, tx pgx.Tx, tenantID, batchID, completedObligationID string) error {
	if _, err := tx.Exec(ctx, `SELECT 1 FROM obligation_batches WHERE tenant_id = $1::uuid AND batch_id = $2::uuid FOR UPDATE`, tenantID, batchID); err != nil {
		return fmt.Errorf("vaccination: lock batch for completed recompute: %w", err)
	}
	if _, err := tx.Exec(ctx, `
UPDATE obligation_batches
SET status = CASE
      WHEN NOT EXISTS (
        SELECT 1 FROM obligation_instances sib
        WHERE sib.tenant_id = obligation_batches.tenant_id
          AND sib.batch_id = obligation_batches.batch_id
          AND sib.status NOT IN ('completed', 'canceled', 'superseded', 'missed', 'waived')
      ) THEN 'completed'
      WHEN status = 'planned' THEN 'in_progress'
      ELSE status
    END,
    row_version = row_version + 1,
    updated_at = now()
WHERE tenant_id = $1::uuid
  AND batch_id = $2::uuid
  AND status IN ('planned', 'in_progress')`, tenantID, batchID); err != nil {
		return fmt.Errorf("vaccination: recompute batch status on complete: %w", err)
	}

	// PEND-1 in_progress trigger (mirrors obligation.Repository.MarkCompleted's sibling UPDATE
	// exactly): flip every still-open (scheduled/due) sibling on this batch to in_progress in ONE
	// set-based UPDATE. completedObligationID is excluded defensively; it is already 'completed' by
	// this point in the same tx, so it can never match status IN ('scheduled', 'due') anyway.
	// Idempotent by construction: a sibling already in_progress no longer matches this WHERE clause,
	// so later completions on the same batch make this a zero-row no-op (O(N) across the drive).
	siblingRows, err := tx.Query(ctx, `
UPDATE obligation_instances
SET status = 'in_progress', row_version = row_version + 1, updated_at = now()
WHERE tenant_id = $1::uuid
  AND batch_id = $2::uuid
  AND obligation_id <> $3::uuid
  AND status IN ('scheduled', 'due')
RETURNING obligation_id::text`, tenantID, batchID, completedObligationID)
	if err != nil {
		return fmt.Errorf("vaccination: mark sibling in_progress: %w", err)
	}
	siblingIDs := make([]string, 0)
	for siblingRows.Next() {
		var sid string
		if err := siblingRows.Scan(&sid); err != nil {
			siblingRows.Close()
			return fmt.Errorf("vaccination: scan sibling in_progress id: %w", err)
		}
		siblingIDs = append(siblingIDs, sid)
	}
	if err := siblingRows.Err(); err != nil {
		siblingRows.Close()
		return fmt.Errorf("vaccination: sibling in_progress rows: %w", err)
	}
	siblingRows.Close()
	if len(siblingIDs) == 0 {
		return nil
	}

	siblingUUIDs := make([]string, len(siblingIDs))
	siblingKeys := make([]string, len(siblingIDs))
	for i, sid := range siblingIDs {
		siblingUUIDs[i] = sid
		siblingKeys[i] = sid + ":in_progress"
	}
	siblingPayload, _ := json.Marshal(map[string]string{"event": "in_progress"})
	siblingNow := time.Now().UTC()
	if _, err := tx.Exec(ctx, `
INSERT INTO obligation_status_events (
  tenant_id, obligation_id, event_type, occurred_at, payload, idempotency_key
) SELECT $1::uuid, obligation_id::uuid, 'in_progress', $2, $3, idempotency_key
FROM UNNEST($4::uuid[], $5::text[]) AS t(obligation_id, idempotency_key)
-- Same idempotency rule as the obligation repository's twin of this insert: the key replays on
-- any second submission for the same obligation (every rework rescan), and a duplicate-key error
-- here aborts the entire submission, silently losing the operator's redo. Re-asserting the fact
-- must be a no-op.
ON CONFLICT (tenant_id, idempotency_key) DO NOTHING`,
		tenantID, siblingNow, siblingPayload, siblingUUIDs, siblingKeys); err != nil {
		return fmt.Errorf("vaccination: bulk insert sibling in_progress events: %w", err)
	}
	for _, sid := range siblingIDs {
		if err := insertObligationInProgressOutbox(ctx, tx, tenantID, sid, siblingNow); err != nil {
			return err
		}
		if err := audit.NewTxRecorder(tx).Record(ctx, audit.Event{
			TenantID:     tenantID,
			ActorType:    "system",
			Action:       obligationInProgressEventType,
			ResourceType: "obligation_instance",
			ResourceID:   sid,
			ScopeType:    "obligation.status_event",
			ScopeID:      sid,
			AfterState: map[string]any{
				"status":      "in_progress",
				"occurred_at": siblingNow.Format(time.RFC3339Nano),
			},
			Metadata: map[string]any{
				"domain":               "vaccination",
				"module":               "vaccination",
				"category":             "obligation_lifecycle",
				"result":               "recorded",
				"status":               "in_progress",
				"source":               "vaccination_accept_completion_atomic_sibling_in_progress",
				"completed_obligation": completedObligationID,
			},
			TraceID: "obligation.in_progress:" + sid,
		}); err != nil {
			return fmt.Errorf("vaccination: sibling in_progress audit: %w", err)
		}
	}
	return nil
}

// insertObligationInProgressOutbox mirrors obligation.Repository's insertObligationLifecycleOutbox
// exactly for the 'obligation.in_progress' event (same idempotency_key/event_id/envelope shape, same
// "obligation.events" topic) so a downstream consumer sees an identical envelope regardless of which
// module's completion path produced it.
func insertObligationInProgressOutbox(ctx context.Context, tx pgx.Tx, tenantID, obligationID string, occurredAt time.Time) error {
	idempotencyKey := obligationInProgressEventType + ":" + obligationID
	eventID := platformoutbox.DeterministicUUID(obligationInProgressEventType + ":" + tenantID + ":" + obligationID)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	occurred := occurredAt.UTC().Format(time.RFC3339Nano)
	payload := map[string]any{
		"tenant_id":     tenantID,
		"obligation_id": obligationID,
		"status":        "in_progress",
	}
	envelope, err := json.Marshal(map[string]any{
		"event_id":       eventID,
		"event_type":     obligationInProgressEventType,
		"schema_version": obligationLifecycleSchemaVersion,
		"schema_ref":     obligationLifecycleSchemaRef,
		"aggregate_type": "obligation_instance",
		"aggregate_id":   obligationID,
		"occurred_at":    occurred,
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
			"evidence_id":   obligationID + ":in_progress",
		}},
		"payload":  payload,
		"trace_id": idempotencyKey,
	})
	if err != nil {
		return fmt.Errorf("vaccination: in_progress envelope: %w", err)
	}
	headers, err := json.Marshal(map[string]any{
		"producer":        "vaccination.acceptCompletionInTx",
		"schema_version":  obligationLifecycleSchemaVersion,
		"obligation_id":   obligationID,
		"idempotency_key": idempotencyKey,
	})
	if err != nil {
		return fmt.Errorf("vaccination: in_progress headers: %w", err)
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO outbox_messages (
  tenant_id, event_id, event_type, schema_version, aggregate_type, aggregate_id,
  topic, payload, headers, idempotency_key, trace_id, status, next_attempt_at
) VALUES (
  $1::uuid, $2::uuid, $3, $4, 'obligation_instance', $5::uuid,
  $6, $7::jsonb, $8::jsonb, $9, $9, 'pending', now()
)
ON CONFLICT DO NOTHING`,
		tenantID, eventID, obligationInProgressEventType, obligationLifecycleSchemaVersion,
		obligationID, obligationLifecycleTopic, envelope, headers, idempotencyKey); err != nil {
		return fmt.Errorf("vaccination: in_progress outbox: %w", err)
	}
	return nil
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
	// lotID is required here as an OPERATIONAL fact (which stock row to decrement), not a
	// re-validation of the record-time business rule -- that policy check (lot + cold-chain
	// required whenever a batch is present) belongs ONLY at record time
	// (CompletionService.validateStockGate). A verifier approving evidence hours/days later
	// must never be re-blocked by inventory state, and cold-chain confirmation happens once
	// at the drive/lot reservation, not per verifier decision -- so coldChain is intentionally
	// NOT re-checked here. A missing lotID at this point means the completion was recorded
	// without ever going through a batch context and there is nothing to consume against; it
	// is a data-shape guard, not a stock-gate re-check.
	if row.lotID == "" {
		// Nothing to decrement against -- but this must NOT fail the accept. Returning an error
		// here contradicted this function's own comment above ("a verifier approving evidence
		// hours/days later must never be re-blocked by inventory state") and did exactly that:
		// a rework rescan records its completion with a batch but no lot, so every approval of a
		// redone animal died with "stock gate blocked", the verdict saved but the completion
		// never flipped to accepted, acceptedCount stayed frozen, and the event retried
		// thousands of times because the failure is deterministic. Observed live 2026-08-05.
		//
		// Skipped consumption is an inventory ANOMALY, not a verification failure: the dose was
		// physically given at record time. So the accept proceeds and the gap is recorded loudly
		// rather than silently swallowed -- an under-decremented lot is a reconciliation problem
		// for whoever owns stock, never a reason to block the verifier.
		r.log.ErrorContext(ctx, "vaccination_accept_stock_not_consumed",
			slog.String("reason", "completion has a batch but no vaccine_inventory_lot_id"),
			slog.String("tenant_id", tenantID),
			slog.String("completion_id", row.completionID),
			slog.String("batch_id", row.batchID),
		)
		return false, nil
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
			// No active, unexpired stock row for this lot. Same rule as the missing-lot branch
			// above: an inventory bookkeeping gap must NOT veto the verifier. Skip the decrement,
			// record the anomaly loudly, let the accept proceed.
			r.log.ErrorContext(ctx, "vaccination_accept_stock_not_consumed",
				slog.String("reason", "no active unexpired stock row for lot"),
				slog.String("tenant_id", tenantID),
				slog.String("completion_id", row.completionID),
				slog.String("lot_id", row.lotID),
			)
			return false, nil
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
		// The batch/lot reservation ledger does not cover this dose. That is an inventory
		// reconciliation problem (under-reserved drive, manual lot edit, seed gap) and is NOT a
		// reason to refuse the verifier's verdict -- the animal was already vaccinated.
		r.log.ErrorContext(ctx, "vaccination_accept_stock_not_consumed",
			slog.String("reason", "batch reservation ledger below dose quantity"),
			slog.String("tenant_id", tenantID),
			slog.String("completion_id", row.completionID),
			slog.String("batch_id", row.batchID),
			slog.String("lot_id", row.lotID),
		)
		return false, nil
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
		// Reserved balance no longer covers this dose (already released, adjusted, or never
		// reserved). Consuming is impossible, but the verdict still stands: skip and record.
		r.log.ErrorContext(ctx, "vaccination_accept_stock_not_consumed",
			slog.String("reason", "reserved balance below dose quantity at consume time"),
			slog.String("tenant_id", tenantID),
			slog.String("completion_id", row.completionID),
			slog.String("lot_id", row.lotID),
		)
		return false, nil
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
	// A rejected completion MOVES out of vaccination_completions into the rejection archive
	// (migration 000093), rather than staying behind with status='rejected'.
	//
	// The animal has to become outstanding work again -- the operator must see it on the scan
	// screen as something to redo. While a rejected row remained, every read that asks "is there
	// a completion for this obligation" still answered yes, which is precisely why a rejected
	// animal stayed green on the scan screen while its four accepted shed-mates looked identical.
	// Removing the row makes the animal outstanding BY DEFAULT on every one of those reads,
	// instead of each of them having to remember to special-case a rejected status.
	//
	// Nothing is lost: the row is copied whole, in this same transaction, with the verdict that
	// caused it. "We injected this animal and the proof was refused" stays on the record and is a
	// different fact from "this never happened".
	//
	// Idempotent by construction: the archive INSERT is driven by the SELECT of the completion
	// row, so a replayed verdict finds nothing to move and reports applied=false, exactly as the
	// previous single-statement UPDATE did when the row was already rejected.
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("vaccination: begin reject completion tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var moved int64
	tag, err := tx.Exec(ctx, `
INSERT INTO vaccination_completion_rejections (
  completion_id, tenant_id, obligation_id, batch_id, goat_id, sop_submission_item_id,
  vaccine_inventory_lot_id, doses, dose_ml_given, route_site, adverse_reaction,
  adverse_reaction_problem_id, cold_chain_verified, administered_at, original_status,
  verified_by, verified_at, rejection_reason, withdrawal_until_date, recorded_by,
  original_idempotency_key, original_row_version, original_created_at, original_updated_at,
  rejected_by
)
SELECT
  vc.completion_id, vc.tenant_id, vc.obligation_id, vc.batch_id, vc.goat_id,
  vc.sop_submission_item_id, vc.vaccine_inventory_lot_id, vc.doses, vc.dose_ml_given,
  vc.route_site, vc.adverse_reaction, vc.adverse_reaction_problem_id, vc.cold_chain_verified,
  vc.administered_at, vc.status,
  $1::uuid, now(), NULLIF($2::text, ''), vc.withdrawal_until_date, vc.recorded_by,
  vc.idempotency_key, vc.row_version, vc.created_at, vc.updated_at,
  $1::uuid
FROM vaccination_completions vc
WHERE vc.tenant_id = $3::uuid
  AND vc.completion_id = $4::uuid
  -- Only work that is still standing can be sent back. An already-accepted animal is terminal
  -- (maintainer ruling): it is never re-judged, and must never be pulled back out of the record.
  AND vc.status = 'recorded'
ON CONFLICT (tenant_id, completion_id) DO NOTHING`,
		pgconv.NullableUUID(verifiedBy), reason, tenant, cid)
	if err != nil {
		return false, fmt.Errorf("vaccination: archive rejected completion: %w", err)
	}
	moved = tag.RowsAffected()
	if moved == 0 {
		// Nothing eligible to move: already rejected (replay), already accepted (terminal), or
		// no such completion. Not an error, and not applied.
		return false, nil
	}
	del, err := tx.Exec(ctx, `
DELETE FROM vaccination_completions
WHERE tenant_id = $1::uuid
  AND completion_id = $2::uuid
  AND status = 'recorded'`, tenant, cid)
	if err != nil {
		return false, fmt.Errorf("vaccination: remove rejected completion: %w", err)
	}
	// The archive INSERT and this DELETE must move exactly one row together. If the row were
	// archived but survived here, the animal would exist in BOTH tables: counted as rejected AND
	// still holding a live completion, so it would read as handled on every completion check while
	// also reading as sent back. Rolling back is the only safe answer -- a half-move of a clinical
	// record is worse than no move at all.
	if del.RowsAffected() != moved {
		return false, fmt.Errorf(
			"vaccination: rejected completion half-moved (archived %d, removed %d): completion_id=%s",
			moved, del.RowsAffected(), completionID)
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("vaccination: commit reject completion: %w", err)
	}
	return true, nil
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

// RecordCompletionsFromSubmission records one completion per matching vaccine obligation for each
// per-goat SOP submission item. One scanned animal can satisfy multiple vaccine obligations.
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
  -- Vaccine lot is reserved at the DRIVE/BATCH level, not collected as a per-shed operator form
  -- field anymore. Thread the batch's reserved lot into the completion so stock deduction + lot
  -- traceability survive the ack model. Prefer obligation_batches.primary_inventory_lot_id when a
  -- deliberate/manual path set it; otherwise fall back to the FEFO-reserved lot recorded by the
  -- sweeper reserve path in inventory_stock_movements (the batch reserve trigger only marks
  -- context.stock_reservation and never writes primary_inventory_lot_id, so sweeper-created batch
  -- drives carry the reserved lot ONLY as a reserve movement). Earliest (occurred_at, movement_id)
  -- reserve row = the earliest-expiry FEFO lot, matching consumeAcceptedCompletionStock's gate.
  COALESCE(
    ob.primary_inventory_lot_id,
    (
      SELECT ism.lot_id
      FROM inventory_stock_movements ism
      WHERE ism.tenant_id = si.tenant_id
        AND ism.batch_id = oi.batch_id
        AND ism.movement_type = 'reserve'
      ORDER BY ism.occurred_at, ism.movement_id
      LIMIT 1
    )
  ),
  COALESCE(nullif(ss.answers ->> 'doses', '')::int, 1),
  NULL::numeric,
  NULL::text,
  false,
  -- Cold chain is verified once at the DRIVE/BATCH reservation (same trust boundary as the
  -- lot COALESCE above), not re-collected per shed. Hardcoding this to false made every
  -- drive-recorded dose permanently unapprovable: consumeAcceptedCompletionStock's stock gate
  -- (repository.go, consume path) requires cold_chain_verified=true whenever a lot is present,
  -- and this bulk insert is the ONLY writer for the SOP shed-submission proof path, so the flag
  -- could never become true downstream. True here matches the trust already extended to the
  -- lot id on the line above.
  true,
  COALESCE(nullif(si.result ->> 'administered_at', '')::timestamptz, ss.submitted_at),
  'recorded',
  COALESCE(
    ((nullif(ss.answers ->> 'withdrawal_until', '')::timestamptz) AT TIME ZONE 'Asia/Kolkata')::date,
    nullif(ss.answers ->> 'withdrawal_until_date', '')::date,
    CASE
      WHEN pr.withdrawal_days IS NOT NULL THEN
        ((COALESCE(nullif(si.result ->> 'administered_at', '')::timestamptz, ss.submitted_at) AT TIME ZONE 'Asia/Kolkata')::date + pr.withdrawal_days)
      ELSE NULL
    END
  ),
  nullif($4, '')::uuid,
  'vaccination:sop_submission_item:' || si.item_id::text || ':obligation:' || oi.obligation_id::text
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
  -- Manual vaccine-batch/cold-chain answer gate removed: the generic SOP engine already
  -- enforces per-goat proof completeness (validatePerGoatProofRefs) before a submission item
  -- can reach 'accepted'/'needs_review', so no vaccination-specific answer gate is needed here.
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
      ((nullif(ss.answers ->> 'withdrawal_until', '')::timestamptz) AT TIME ZONE 'Asia/Kolkata')::date,
      nullif(ss.answers ->> 'withdrawal_until_date', '')::date,
      CASE
        WHEN pr.withdrawal_days IS NOT NULL THEN
          ((vc.administered_at AT TIME ZONE 'Asia/Kolkata')::date + pr.withdrawal_days)
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
		return materializedItems, fmt.Errorf("vaccination: materialized %d of %d eligible vaccine obligations", materializedItems, eligibleItems)
	}
	if materializedItems > 0 {
		return materializedItems, nil
	}
	return count, nil
}

// ShedCompletionSummary computes the FROZEN read-only shed-completion / submit summary for a
// vaccination task: how many animals were expected vs. scanned vs. proof-ready, a display-name
// vaccine breakdown, and whether Submit is enabled. Shed completion is an acknowledgement, not a
// manual form, so this reads exclusively from scan/proof/obligation state, never from submitted
// form answers. Every read here is a single tenant+task (or tenant+batch) equality lookup against
// an indexed column (sop_task_scan_captures_task_idx, obligation_instances_batch_idx,
// proof_artifacts_scope_idx) — bounded to one task's shed, never a table scan.
func (r *Repository) ShedCompletionSummary(ctx context.Context, tenantID, taskID, shedID string, partitionLabels ...string) (domain.ShedCompletionSummary, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tenant, err := pgconv.UUID(tenantID)
	if err != nil {
		return domain.ShedCompletionSummary{}, fmt.Errorf("vaccination: tenant id: %w", err)
	}
	task, err := pgconv.UUID(taskID)
	if err != nil {
		return domain.ShedCompletionSummary{}, fmt.Errorf("vaccination: task id: %w", err)
	}
	var shed pgtype.UUID
	if shedID != "" {
		shed, err = pgconv.UUID(shedID)
		if err != nil {
			return domain.ShedCompletionSummary{}, fmt.Errorf("vaccination: shed id: %w", err)
		}
	}
	partitionLabel := ""
	if len(partitionLabels) > 0 {
		partitionLabel = strings.TrimSpace(partitionLabels[0])
	}

	var (
		shedName      string
		driveName     string
		state         string
		expectedCount int64
		handledCount  int64
		proofReady    int64
		pendingVerify int64
		minProofs     int64
		maxProofs     int64
		proofMode     string
		submitState   string
	)
	// projection-review: membership=obligation batch of this SOP task (obligation_batches.sop_task_id = the shed drive's batch); group_key=(tenant_id, task_id) resolving one batch_id, all counts keyed to that single batch/shed; join_cardinality=each count is a SEPARATE scalar sub-select over a 1-row-per-fact source (expected=one row per obligation_instance in the batch; handled=COUNT(DISTINCT goat_id) over scan_captures so multiple scans of one goat count once; proof_ready=per-goat mode counts COUNT(DISTINCT subject_id), shed-level mode counts completed shed proof_artifacts) — the buckets are never multiplied by a shared fan-out because they are computed independently, not from one wide JOIN; pagination=whole-shed totals computed server-side in one aggregation, NOT page-limited (no LIMIT/OFFSET on the counts); scope=explicit — the task's own scope_type='shed' resolves shed_name via locations, and expected animals come ONLY from obligation_instances joined to THIS task's batch_id, so no park/cohort/other-shed animals bleed in.
	// grain: one summary row per (tenant, task/shed drive). status buckets: expected excludes terminal obligations ('completed','waived','canceled','superseded') to mirror RecordCompletionsFromSubmission; submit is enabled only when handled animals match expected and the SOP proof-mode gate is satisfied.
	// Bounded to one SOP task/batch by (tenant_id, task_id) equality; scalar
	// subqueries read indexed scan/proof/obligation facts for that one shed and
	// are regression-covered by ShedCompletionSummary
	// OneToMany/PageBoundary/ParkScope/StatusBuckets tests.
	// scale-guard:ignore: 5k-50k-envelope
	err = r.pool.QueryRow(ctx, `
WITH t AS (
  SELECT st.task_id,
         st.tenant_id,
         st.title,
         st.scope_type,
         st.scope_id,
         st.state,
         COALESCE(NULLIF(sv.proof_policy ->> 'proof_mode', ''), CASE WHEN sv.proof_policy ->> 'subject_scope' = 'shed' THEN 'shed_level_video' ELSE 'per_goat_video' END) AS proof_mode,
         COALESCE(NULLIF(sv.proof_policy ->> 'minimum_count', '')::int, 1) AS min_proofs,
         COALESCE(NULLIF(sv.proof_policy ->> 'maximum_count', '')::int, COALESCE(NULLIF(sv.proof_policy ->> 'maximum_count_per_subject', '')::int, 5)) AS max_proofs
  FROM sop_tasks st
  JOIN sop_versions sv ON sv.tenant_id = st.tenant_id AND sv.sop_version_id = st.sop_version_id
  WHERE st.tenant_id = $1 AND st.task_id = $2
),
batch AS (
  SELECT ob.batch_id
  FROM obligation_batches ob
  JOIN t ON t.tenant_id = ob.tenant_id AND t.task_id = ob.sop_task_id
),
eligible AS (
  SELECT oi.obligation_id, oi.target_id AS goat_id, g.shed_id
  FROM obligation_instances oi
  JOIN batch b ON b.batch_id = oi.batch_id
  JOIN obligation_batches ob ON ob.tenant_id = oi.tenant_id AND ob.batch_id = oi.batch_id
  JOIN goats g ON g.tenant_id = oi.tenant_id AND g.goat_id = oi.target_id
  LEFT JOIN goat_shed_partitions gsp ON gsp.tenant_id = g.tenant_id AND gsp.goat_id = g.goat_id
  WHERE oi.tenant_id = $1
    AND oi.status NOT IN ('completed', 'waived', 'canceled', 'superseded')
    AND (oi.status <> 'scheduled' OR COALESCE(ob.planned_date::timestamp AT TIME ZONE 'Asia/Kolkata', oi.due_at) <= now())
    AND (NOT $3::boolean OR g.shed_id = $4)
    AND (
      $5::text = ''
      OR regexp_replace(lower(btrim(COALESCE(gsp.partition_label, 'whole'))), '^part[[:space:]]+', '')
       = regexp_replace(lower(btrim($5::text)), '^part[[:space:]]+', '')
    )
),
-- expected counts animals in this shed's batch that STILL need vaccination. The exclusion set
-- MUST match RecordCompletionsFromSubmission's obligation filter exactly ('completed', 'waived',
-- 'canceled', 'superseded'): a vaccination task is re-submittable via the rework path (task state
-- rework_requested -> re-submit), and obligations transition to 'completed' only at verify/accept.
-- Excluding already-completed obligations keeps expected == "animals submit will actually
-- materialize", so drive-% is correct across re-reads and an over-scan can never be masked by a
-- stale, inflated expected_count.
expected AS (
  SELECT count(DISTINCT goat_id) AS n
  FROM eligible
),
handled AS (
  SELECT count(DISTINCT c.goat_id) AS n
  FROM sop_task_scan_captures c
  JOIN goats g ON g.tenant_id = c.tenant_id AND g.goat_id = c.goat_id
  LEFT JOIN goat_shed_partitions gsp ON gsp.tenant_id = g.tenant_id AND gsp.goat_id = g.goat_id
  WHERE c.tenant_id = $1
    AND c.task_id = $2
    AND c.field_key IN ('goat_ids', '__scan_roster__')
    AND c.goat_id IS NOT NULL
    AND (NOT $3::boolean OR g.shed_id = $4)
    AND (
      $5::text = ''
      OR regexp_replace(lower(btrim(COALESCE(gsp.partition_label, 'whole'))), '^part[[:space:]]+', '')
       = regexp_replace(lower(btrim($5::text)), '^part[[:space:]]+', '')
    )
),
proofed_goat AS (
  SELECT count(DISTINCT p.subject_id) AS n
  FROM proof_artifacts p
  JOIN eligible e ON e.goat_id = p.subject_id
  WHERE p.tenant_id = $1
    AND p.scope_type = 'task'
    AND p.scope_id = $2
	    AND p.subject_type = 'goat'
	    AND p.subject_id IS NOT NULL
	    AND p.upload_state = 'completed'
	    AND p.created_at >= COALESCE((
	      SELECT max(c.captured_at)
	      FROM sop_task_scan_captures c
	      WHERE c.tenant_id = p.tenant_id
	        AND c.task_id = p.scope_id
	        AND c.field_key IN ('goat_ids', '__scan_roster__')
	        AND c.goat_id = p.subject_id
	    ), '-infinity'::timestamptz)
	),
proofed_shed AS (
  SELECT count(*) AS n
  FROM proof_artifacts p
  WHERE p.tenant_id = $1
    AND p.scope_type = 'shed'
    AND p.subject_type = 'shed'
    AND p.upload_state = 'completed'
	    AND EXISTS (
	      SELECT 1
	      FROM eligible e
	      WHERE e.shed_id = p.scope_id
	        AND (NOT $3::boolean OR e.shed_id = $4)
	    )
	    AND (
	      $5::text = ''
	      OR regexp_replace(lower(btrim(COALESCE(p.metadata ->> 'partition_label', 'whole'))), '^part[[:space:]]+', '')
	       = regexp_replace(lower(btrim($5::text)), '^part[[:space:]]+', '')
	    )
	    AND (p.subject_id IS NULL OR p.subject_id = p.scope_id)
	),
verification_pending AS (
  -- SHED-SCOPED. verification_items carries its own shed_id (set from the submitting
  -- completion's shed at CreateItem time -- internal/sopbridge/vaccination_submission.go), so a
  -- sibling shed's still-pending verification item must never inflate THIS shed's pending count.
  -- Before this fix the count was task-wide: any shed on the same shared park drive task with a
  -- live pending item made every OTHER shed on that task read as "submitted" too.
  SELECT count(*) AS n
  FROM verification_items vi
  WHERE vi.tenant_id = $1
    AND vi.source_task_id = $2
    AND vi.status = 'pending'
    AND vi.closed_at IS NULL
    AND (NOT $3::boolean OR vi.shed_id = $4)
    AND (
      $5::text = ''
      OR regexp_replace(lower(btrim(COALESCE(vi.partition_label, 'whole'))), '^part[[:space:]]+', '')
       = regexp_replace(lower(btrim($5::text)), '^part[[:space:]]+', '')
    )
),
shed_submission_state AS (
  SELECT CASE ss.state
    WHEN 'accepted' THEN 'verified'
    WHEN 'rejected' THEN 'closed'
    WHEN 'voided' THEN 'closed'
    ELSE 'submitted'
  END AS state
  FROM sop_submissions ss
  CROSS JOIN LATERAL jsonb_array_elements(ss.proof_refs) AS proof(ref)
  WHERE ss.tenant_id = $1
	    AND ss.task_id = $2
	    AND ss.state IN ('submitted', 'needs_review', 'accepted', 'rejected', 'voided')
	    AND (
	      $5::text = ''
	      OR regexp_replace(lower(btrim(COALESCE(ss.partition_label, 'whole'))), '^part[[:space:]]+', '')
	       = regexp_replace(lower(btrim($5::text)), '^part[[:space:]]+', '')
	    )
	    AND proof.ref ->> 'upload_state' = 'completed'
    AND proof.ref ->> 'proof_type' = 'video'
    AND proof.ref ->> 'subject_type' = 'shed'
    AND EXISTS (
      SELECT 1
      FROM eligible e
      WHERE e.shed_id::text = proof.ref ->> 'subject_id'
        AND (NOT $3::boolean OR e.shed_id = $4)
    )
  ORDER BY ss.submitted_at DESC NULLS LAST, ss.submission_id DESC
  LIMIT 1
),
shed AS (
  SELECT l.name
  FROM t
  JOIN locations l ON l.tenant_id = t.tenant_id AND l.location_id = t.scope_id
  WHERE t.scope_type = 'shed'
),
obligation_shed AS (
  SELECT CASE
    WHEN count(DISTINCT l.location_id) = 1 THEN max(l.name)
    ELSE 'Multiple sheds'
  END AS name
  FROM eligible e
  JOIN goats g ON g.tenant_id = $1 AND g.goat_id = e.goat_id
  JOIN locations l ON l.tenant_id = g.tenant_id AND l.location_id = g.shed_id
)
SELECT
  COALESCE((SELECT name FROM obligation_shed), shed.name, t.title),
	t.title,
	t.state,
	t.proof_mode,
  t.min_proofs,
  t.max_proofs,
  COALESCE((SELECT n FROM expected), 0),
  COALESCE((SELECT n FROM handled), 0),
  CASE WHEN t.proof_mode = 'shed_level_video'
    THEN COALESCE((SELECT n FROM proofed_shed), 0)
    ELSE COALESCE((SELECT n FROM proofed_goat), 0)
  END,
  COALESCE((SELECT n FROM verification_pending), 0),
  CASE WHEN t.proof_mode = 'shed_level_video'
    THEN COALESCE((SELECT state FROM shed_submission_state), 'draft')
    ELSE ''
  END
FROM t
LEFT JOIN shed ON true`,
		tenant, task, shed.Valid, shed, partitionLabel,
	).Scan(&shedName, &driveName, &state, &proofMode, &minProofs, &maxProofs, &expectedCount, &handledCount, &proofReady, &pendingVerify, &submitState)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ShedCompletionSummary{}, fmt.Errorf("vaccination: shed completion summary: %w", ports.ErrNotFound)
		}
		return domain.ShedCompletionSummary{}, fmt.Errorf("vaccination: shed completion summary: %w", err)
	}

	breakdown, err := r.shedCompletionVaccineBreakdown(ctx, tenant, task, shed, partitionLabel)
	if err != nil {
		return domain.ShedCompletionSummary{}, err
	}

	// Round facts: every obligation in this shed's batch (ALL statuses, not just currently
	// eligible ones) paired with its own row_version. Postgres already bumps
	// obligation_instances.row_version on the two transitions that define a round --
	// MarkObligationCompleted at submission (internal/sopbridge/vaccination_submission.go's
	// OnTaskSubmitted -> obligation.MarkCompleted, fired BEFORE verification, not on accept) and
	// ReopenObligation on a verifier rejection (internal/vaccination/app/completion.go
	// RejectExisting -> obligation.ReopenObligation). RoundID is a hash of this fact set, and
	// RoundSubmitted is computed from the SAME fact set below, so the two can never disagree.
	roundFacts, err := r.shedCompletionRoundFacts(ctx, tenant, task, shed.Valid, shed, partitionLabel)
	if err != nil {
		return domain.ShedCompletionSummary{}, err
	}
	roundID, roundState, roundSubmitted := shedCompletionRoundState(roundFacts, pendingVerify)

	summary := domain.ShedCompletionSummary{
		TaskID:           taskID,
		ShedName:         shedName,
		PartitionLabel:   partitionLabel,
		DriveName:        driveName,
		ExpectedCount:    expectedCount,
		HandledCount:     handledCount,
		ProofReadyCount:  proofReady,
		ProofMode:        proofMode,
		VaccineBreakdown: breakdown,
		SubmitState:      submitState,
		RoundID:          roundID,
	}
	if proofMode != "shed_level_video" {
		// Per-goat mode: derive SubmitState/RoundSubmitted from the shed-scoped round-facts
		// above, never from the shared park-level t.state. See AGENTS.md "Shared vaccination
		// drive tasks are aggregate bookkeeping only."
		summary.SubmitState = roundState
		summary.RoundSubmitted = roundSubmitted
	} else {
		// Shed-level mode already derives submit_state from shed-scoped sop_submissions/
		// proof_refs (shed_submission_state, above); RoundSubmitted mirrors that word rather than
		// switching this mode's proven-working derivation.
		summary.RoundSubmitted = shedLevelRoundSubmitted(submitState)
	}
	summary.SubmitEnabled, summary.BlockingReason = shedCompletionReadinessForMode(expectedCount, handledCount, proofReady, proofMode, minProofs, maxProofs)
	return summary, nil
}

// shedRoundObligationFact is one obligation's identity/version/status inside a shed's batch,
// used to compute RoundID/RoundSubmitted. Unlike `eligible` in the main query, this is NOT
// filtered to non-terminal obligations -- a completed obligation IS the round's live evidence
// until it is either accepted (stays completed) or reopened by rejection (goes back to due).
func (r *Repository) shedCompletionRoundFacts(ctx context.Context, tenant, task pgtype.UUID, hasShed bool, shed pgtype.UUID, partitionLabel string) ([]shedRoundObligationFact, error) {
	// projection-review: membership=obligation instances in this task's batch (obligation_batches.sop_task_id filters exactly one batch); group_key=(tenant_id, task_id) → one batch_id per SOP task; join_cardinality=one row per obligation_instance in the batch, each obligation appearing once (obligation_id is unique, no fan-out); pagination=none — whole-batch obligation list returned without LIMIT (shed drives have dozens, not thousands of obligations); scope=explicit — hasShed and shed_id filter constrain to target shed: obligation_instances via obligation_batches.batch_id resolve only to THIS task, and goats.shed_id filter further scopes to the requested shed.
	rows, err := r.pool.Query(ctx, `
WITH t AS (
  SELECT st.task_id, st.tenant_id
  FROM sop_tasks st
  WHERE st.tenant_id = $1 AND st.task_id = $2
),
batch AS (
  SELECT ob.batch_id
  FROM obligation_batches ob
  JOIN t ON t.tenant_id = ob.tenant_id AND t.task_id = ob.sop_task_id
)
SELECT oi.obligation_id::text, oi.row_version, oi.status
FROM obligation_instances oi
JOIN batch b ON b.batch_id = oi.batch_id
JOIN goats g ON g.tenant_id = oi.tenant_id AND g.goat_id = oi.target_id
LEFT JOIN goat_shed_partitions gsp ON gsp.tenant_id = oi.tenant_id AND gsp.goat_id = oi.target_id
WHERE oi.tenant_id = $1
  AND (NOT $3::boolean OR g.shed_id = $4)
  AND (
    $5::text = ''
    OR regexp_replace(lower(btrim(COALESCE(gsp.partition_label, 'whole'))), '^part[[:space:]]+', '')
     = regexp_replace(lower(btrim($5::text)), '^part[[:space:]]+', '')
  )
ORDER BY oi.obligation_id`, tenant, task, hasShed, shed, partitionLabel)
	if err != nil {
		return nil, fmt.Errorf("vaccination: shed completion round facts: %w", err)
	}
	defer rows.Close()
	var out []shedRoundObligationFact
	for rows.Next() {
		var f shedRoundObligationFact
		if err := rows.Scan(&f.ObligationID, &f.RowVersion, &f.Status); err != nil {
			return nil, fmt.Errorf("vaccination: shed completion round facts row: %w", err)
		}
		out = append(out, f)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("vaccination: shed completion round facts rows: %w", err)
	}
	return out, nil
}

// shedCompletionVaccineBreakdown returns the display-name/count breakdown of vaccines expected in
// this task's shed/batch, bounded by the same batch_id index as the summary counts above.
func (r *Repository) shedCompletionVaccineBreakdown(ctx context.Context, tenant, task, shed pgtype.UUID, partitionLabel string) ([]domain.VaccineBreakdownItem, error) {
	// projection-review: membership=obligation batch of this SOP task (obligation_batches.sop_task_id); group_key=(tenant_id, task_id) -> one batch_id; join_cardinality=one row per obligation_instance in the batch — protocol_rule_dimensions is many-rows-per-rule, so it is collapsed to ONE label per rule via LEFT JOIN LATERAL ... LIMIT 1 (NOT a plain JOIN) to stop count(*) double-counting an obligation when a rule has multiple selector/dimension rows; pagination=whole-shed totals, LIMIT 50 caps the number of DISTINCT vaccine labels (a shed drive has a handful of vaccines), never the per-vaccine COUNT; scope=explicit — obligations come only from THIS task's batch_id, so other sheds/parks never contribute.
	// grain: one row per vaccine label for this shed drive. status: excludes terminal ('completed','waived','canceled','superseded') to mirror the summary's expected bucket.
	rows, err := r.pool.Query(ctx, `
WITH t AS (
  SELECT st.task_id, st.tenant_id
  FROM sop_tasks st
  WHERE st.tenant_id = $1 AND st.task_id = $2
),
batch AS (
  SELECT ob.batch_id
  FROM obligation_batches ob
  JOIN t ON t.tenant_id = ob.tenant_id AND t.task_id = ob.sop_task_id
)
SELECT COALESCE(v.vaccine, NULLIF(pv.rule_dsl -> 'vaccine' ->> 'name', ''), NULLIF(pv.rule_dsl -> 'vaccine' ->> 'code', ''), NULLIF(pr.dose_code, ''), 'Unspecified') AS vaccine,
       count(*) AS n
FROM obligation_instances oi
JOIN batch b ON b.batch_id = oi.batch_id
JOIN goats g ON g.tenant_id = oi.tenant_id AND g.goat_id = oi.target_id
LEFT JOIN goat_shed_partitions gsp ON gsp.tenant_id = oi.tenant_id AND gsp.goat_id = oi.target_id
JOIN protocol_rules pr
  ON pr.tenant_id = oi.tenant_id
 AND pr.protocol_version_id = oi.protocol_version_id
 AND pr.rule_id = oi.rule_id
JOIN protocol_versions pv
  ON pv.tenant_id = oi.tenant_id
 AND pv.protocol_version_id = oi.protocol_version_id
LEFT JOIN LATERAL (
  SELECT COALESCE(
    CASE upper(nullif(prd.vaccine_code, ''))
      WHEN 'ET_TT' THEN 'ET+TT'
      WHEN 'ETTT' THEN 'ET+TT'
      WHEN 'BLUE_TONGUE' THEN 'Blue Tongue'
      WHEN 'GOAT_POX' THEN 'Goat Pox'
      WHEN 'SHEEP_POX' THEN 'Sheep Pox'
      ELSE nullif(prd.vaccine_code, '')
    END,
    nullif(prd.vaccine_type, '')
  ) AS vaccine
  FROM protocol_rule_dimensions prd
  WHERE prd.tenant_id = oi.tenant_id
    AND prd.rule_id = oi.rule_id
  ORDER BY prd.vaccine_type, prd.vaccine_code
  LIMIT 1
) v ON true
WHERE oi.tenant_id = $1
  AND oi.status NOT IN ('completed', 'waived', 'canceled', 'superseded')
  AND (NOT $3::boolean OR g.shed_id = $4)
  AND (
    $5::text = ''
    OR regexp_replace(lower(btrim(COALESCE(gsp.partition_label, 'whole'))), '^part[[:space:]]+', '')
     = regexp_replace(lower(btrim($5::text)), '^part[[:space:]]+', '')
  )
GROUP BY 1
ORDER BY 1
LIMIT 50`, tenant, task, shed.Valid, shed, partitionLabel)
	if err != nil {
		return nil, fmt.Errorf("vaccination: shed completion vaccine breakdown: %w", err)
	}
	defer rows.Close()
	var out []domain.VaccineBreakdownItem
	for rows.Next() {
		var item domain.VaccineBreakdownItem
		if err := rows.Scan(&item.Vaccine, &item.Count); err != nil {
			return nil, fmt.Errorf("vaccination: shed completion vaccine breakdown row: %w", err)
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("vaccination: shed completion vaccine breakdown rows: %w", err)
	}
	return out, nil
}

// shedRoundObligationFact is one obligation's identity/version/status snapshot, read by
// shedCompletionRoundFacts and consumed by shedCompletionRoundState to compute RoundID and
// RoundSubmitted from the SAME underlying facts.
type shedRoundObligationFact struct {
	ObligationID string
	RowVersion   int64
	Status       string
}

// shedCompletionRoundState computes RoundID (a deterministic fingerprint of this shed's current
// obligation-round state), the frozen ShedCompletionSummary submit_state vocabulary (draft |
// submitted | verified | closed) for per_goat_video mode, and the unambiguous RoundSubmitted
// boolean -- all from the SAME per-obligation fact set, so RoundID and RoundSubmitted can never
// disagree.
//
// obligation_instances.status == 'completed' is this shed's "a round was submitted" signal:
// MarkObligationCompleted flips an obligation to 'completed' the instant a shed submits
// (internal/sopbridge/vaccination_submission.go OnTaskSubmitted -> obligation.MarkCompleted),
// BEFORE any verifier review, and it stays 'completed' through acceptance. A verifier REJECTION
// is the only thing that moves it back off 'completed' (ReopenObligation -> 'due'), which is
// exactly the live defect this replaces: a reopened obligation on shed A could never reach
// Submit because the shared park-level sop_tasks.state stayed "needs_review" thanks to a still-
// pending sibling shed B. Reading each obligation's OWN status/row_version instead of the shared
// task word fixes that, per AGENTS.md: "Shared vaccination drive tasks are aggregate bookkeeping
// only. A hidden park/batch-level sop_tasks.state must not be used as per-shed submitted/proof/
// verification truth."
//
//   - open (any non-terminal, non-completed status: scheduled/due/in_progress/deferred) present
//     anywhere in the shed's batch means the round is NOT fully submitted -> draft/false,
//     regardless of what any other obligation in the shed is doing.
//   - no open obligations and at least one 'completed' obligation means every currently-tracked
//     obligation in this shed has been submitted for verification (or already accepted).
//     pendingVerify (shed-scoped, vi.shed_id-filtered verification_items) then distinguishes
//     "submitted, awaiting verifier" from "verified" (pendingVerify == 0, already accepted).
//   - no open AND no completed obligations (only terminal waived/canceled/superseded, or no
//     obligations at all) means this shed never had a submittable round -> draft/false.
func shedCompletionRoundState(facts []shedRoundObligationFact, pendingVerify int64) (roundID, state string, roundSubmitted bool) {
	if len(facts) == 0 {
		return "", "draft", false
	}
	parts := make([]string, 0, len(facts))
	var open, completed int
	for _, f := range facts {
		parts = append(parts, f.ObligationID+":"+strconv.FormatInt(f.RowVersion, 10))
		switch f.Status {
		case "completed":
			completed++
		case "waived", "canceled", "superseded":
			// Terminal, but not part of this round's "submitted" evidence.
		default:
			open++
		}
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "|")))
	roundID = hex.EncodeToString(sum[:])[:16]

	if open > 0 || completed == 0 {
		return roundID, "draft", false
	}
	if pendingVerify > 0 {
		return roundID, "submitted", true
	}
	return roundID, "verified", true
}

// shedLevelRoundSubmitted maps the existing shed_level_video submit_state (already derived from
// shed-scoped sop_submissions/proof_refs by shed_submission_state) onto RoundSubmitted: a live or
// accepted submission trail for this shed's current round. "closed" (rejected/voided) and "draft"
// both mean the operator still needs to send a fresh submission.
func shedLevelRoundSubmitted(state string) bool {
	switch state {
	case "submitted", "verified":
		return true
	default:
		return false
	}
}

// shedCompletionReadiness applies the frozen submit_enabled rule STRICTLY: enabled only when the
// scanned set and the proof-ready set each EXACTLY equal the expected set (handled == expected AND
// proof_ready == expected). Any mismatch — under-scan OR over-scan — blocks submit with a human
// reason. Over-scan matters for data integrity: RecordCompletionsFromSubmission INNER-JOINs each
// scanned goat to an open obligation in this shed's batch, so a scanned animal that is not expected
// here has no obligation to join and would be silently dropped. Blocking on over-scan surfaces the
// stray animal to the operator instead of losing it.
func shedCompletionReadiness(expected, handled, proofReady int64) (bool, *string) {
	if expected <= 0 {
		reason := "No animals are expected in this pen for this drive yet."
		return false, &reason
	}
	if handled < expected {
		reason := fmt.Sprintf("%d of %d animals are not yet scanned.", expected-handled, expected)
		return false, &reason
	}
	if handled > expected {
		reason := fmt.Sprintf("%d scanned animals are not expected in this pen for this drive.", handled-expected)
		return false, &reason
	}
	if proofReady < expected {
		reason := fmt.Sprintf("%d of %d animals are missing proof.", expected-proofReady, expected)
		return false, &reason
	}
	if proofReady > expected {
		reason := fmt.Sprintf("%d proof clips belong to animals not expected in this pen for this drive.", proofReady-expected)
		return false, &reason
	}
	return true, nil
}

func shedCompletionReadinessForMode(expected, handled, proofReady int64, proofMode string, minProofs, maxProofs int64) (bool, *string) {
	if proofMode != "shed_level_video" {
		return shedCompletionReadiness(expected, handled, proofReady)
	}
	if expected <= 0 {
		reason := "No animals are expected in this pen for this drive yet."
		return false, &reason
	}
	if handled < expected {
		reason := fmt.Sprintf("%d of %d animals are not yet scanned.", expected-handled, expected)
		return false, &reason
	}
	if handled > expected {
		reason := fmt.Sprintf("%d scanned animals are not expected in this pen for this drive.", handled-expected)
		return false, &reason
	}
	if minProofs <= 0 {
		minProofs = 1
	}
	if maxProofs <= 0 {
		maxProofs = 5
	}
	if proofReady < minProofs {
		reason := fmt.Sprintf("%d pen video(s) still need proof.", minProofs-proofReady)
		return false, &reason
	}
	if proofReady > maxProofs {
		reason := fmt.Sprintf("At most %d pen video(s) can be submitted.", maxProofs)
		return false, &reason
	}
	return true, nil
}

// ListSubmissionCompletions returns the materialized completion rows for one SOP submission.
// Grain is completion, ordered by (goat_id, completion_id); callers partition rows by goat. The query is
// tenant + submission scoped, uses the unique sop_submission_item linkage, and is hard bounded by
// the SOP fan-out ceiling so it cannot become a herd-scale read.
func (r *Repository) ListSubmissionCompletions(ctx context.Context, tenantID, submissionID string) ([]domain.SubmissionCompletion, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tenant, err := pgconv.UUID(tenantID)
	if err != nil {
		return nil, fmt.Errorf("vaccination: tenant id: %w", err)
	}
	submission, err := pgconv.UUID(submissionID)
	if err != nil {
		return nil, fmt.Errorf("vaccination: submission id: %w", err)
	}
	rows, err := r.pool.Query(ctx, `
SELECT vc.completion_id::text,
       si.submission_id::text,
       vc.obligation_id::text,
       vc.goat_id::text,
       g.display_id,
       COALESCE(g.shed_id::text, ''),
       COALESCE(NULLIF(shed.name, ''), NULLIF(shed.location_code, ''), g.shed_id::text, '')::text AS shed_label,
       COALESCE(NULLIF(gsp.partition_label, ''), 'whole')::text,
       COALESCE(g.park_id::text, ''),
       COALESCE(proofs.proof_ids, ARRAY[]::text[]),
       vc.administered_at,
       -- Protocol name + dose code feed domain.DoseDisplayLabel below. They are the ONLY inputs
       -- that formatter needs, and both are bounded single-row lookups off the completion's own
       -- obligation, so this stays one statement rather than a per-row label resolve.
       COALESCE(pd.name, ''),
       COALESCE(pr.dose_code, '')
FROM vaccination_completions vc
JOIN sop_submission_items si
  ON si.tenant_id = vc.tenant_id
 AND si.item_id = vc.sop_submission_item_id
JOIN sop_submissions ss
  ON ss.tenant_id = si.tenant_id
 AND ss.submission_id = si.submission_id
JOIN goats g
  ON g.tenant_id = vc.tenant_id
 AND g.goat_id = vc.goat_id
LEFT JOIN locations shed
  ON shed.tenant_id = g.tenant_id
 AND shed.location_id = g.shed_id
 AND shed.location_type = 'shed'
LEFT JOIN goat_shed_partitions gsp
  ON gsp.tenant_id = g.tenant_id
 AND gsp.goat_id = g.goat_id
 AND gsp.shed_id = g.shed_id
-- The verifier must be told WHICH vaccine the clip is evidence for. All four joins are LEFT so a
-- completion whose protocol chain does not resolve still yields its row (the label degrades to
-- empty and the subject simply omits the vaccine) rather than vanishing from the submission.
LEFT JOIN obligation_instances oi
  ON oi.tenant_id = vc.tenant_id
 AND oi.obligation_id = vc.obligation_id
LEFT JOIN protocol_rules pr
  ON pr.tenant_id = oi.tenant_id
 AND pr.rule_id = oi.rule_id
LEFT JOIN protocol_versions pv
  ON pv.protocol_version_id = pr.protocol_version_id
LEFT JOIN protocol_definitions pd
  ON pd.protocol_id = pv.protocol_id
LEFT JOIN LATERAL (
  SELECT array_agg(ref.value ->> 'proof_id' ORDER BY ref.ordinality) AS proof_ids
  FROM jsonb_array_elements(COALESCE(ss.proof_refs, '[]'::jsonb)) WITH ORDINALITY AS ref(value, ordinality)
  WHERE ref.value ->> 'upload_state' = 'completed'
    AND ref.value ->> 'proof_type' = 'video'
    AND ref.value ->> 'subject_type' = 'goat'
    AND ref.value ->> 'subject_id' = vc.goat_id::text
) proofs ON true
WHERE vc.tenant_id = $1
  AND si.submission_id = $2
ORDER BY vc.goat_id, vc.completion_id
LIMIT 5000`, tenant, submission)
	if err != nil {
		return nil, fmt.Errorf("vaccination: list submission completions: %w", err)
	}
	defer rows.Close()
	out := make([]domain.SubmissionCompletion, 0)
	for rows.Next() {
		var item domain.SubmissionCompletion
		var protocolName, doseCode string
		var partitionLabelRaw string
		if err := rows.Scan(
			&item.CompletionID,
			&item.SubmissionID,
			&item.ObligationID,
			&item.GoatID,
			&item.GoatLabel,
			&item.ShedID,
			&item.ShedLabel,
			&partitionLabelRaw,
			&item.ParkID,
			&item.ProofRefIDs,
			&item.AdministeredAt,
			&protocolName,
			&doseCode,
		); err != nil {
			return nil, fmt.Errorf("vaccination: scan submission completion: %w", err)
		}
		// Convert 'whole' sentinel to empty string for display (never show 'whole' to operators)
		if partitionLabelRaw != "" && partitionLabelRaw != "whole" {
			item.PartitionLabel = partitionLabelRaw
		}
		item.AdministeredAt = item.AdministeredAt.UTC()
		// Derived through the ONE canonical formatter every module shares, so the verifier's
		// queue, the calendar, execution, and push copy can never name the same dose three
		// different ways -- and so a raw dose_code can never reach a screen.
		item.VaccineLabel = domain.DoseDisplayLabel(protocolName, doseCode)
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("vaccination: list submission completion rows: %w", err)
	}
	return out, nil
}

// CompletedProofRefsByTask returns completed proof ids keyed by goat for one vaccination task.
// Grain is one completed proof artifact; callers use this as the hidden evidence bridge for shed
// completion acknowledgements, where the operator sees no manual proof_refs form field.
func (r *Repository) CompletedProofRefsByTask(ctx context.Context, tenantID, taskID string) (map[string][]string, error) {
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
	rows, err := r.pool.Query(ctx, `
SELECT subject_id::text, proof_id::text
FROM proof_artifacts
WHERE tenant_id = $1
  AND scope_type = 'task'
  AND scope_id = $2
  AND subject_type = 'goat'
  AND subject_id IS NOT NULL
  AND upload_state = 'completed'
ORDER BY subject_id, created_at, proof_id
LIMIT 5000`, tenant, task)
	if err != nil {
		return nil, fmt.Errorf("vaccination: completed proof refs by task: %w", err)
	}
	defer rows.Close()
	out := make(map[string][]string)
	for rows.Next() {
		var goatID, proofID string
		if err := rows.Scan(&goatID, &proofID); err != nil {
			return nil, fmt.Errorf("vaccination: scan completed proof ref: %w", err)
		}
		out[goatID] = append(out[goatID], proofID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("vaccination: completed proof refs by task rows: %w", err)
	}
	return out, nil
}

// submissionFanoutCounts compares how many vaccine obligations are expected to end up backed by an
// ACTIVE vaccination_completions row against how many actually are, so RecordCompletionsFromSubmission
// can fail closed on a genuine short-materialization instead of silently dropping a vaccine for a
// scanned goat.
//
// Root cause of the false "materialized N of M eligible submission items" failure this replaces:
// the INSERT above uses a BARE `ON CONFLICT DO NOTHING` (no conflict target), so it silently no-ops
// on ANY unique-constraint hit for a row -- not just the idempotency_key one it names in comments.
// vaccination_completions also carries a second, partial unique index,
// vaccination_completions_obligation_goat_active_unique_idx on (tenant_id, obligation_id, goat_id)
// WHERE status IN ('recorded','accepted'), which allows at most one ACTIVE completion per
// obligation+goat at a time. The Android client posts a CUMULATIVE proof/answer payload per shed
// (see the P0 shed-leak comment in sopbridge/vaccination_submission.go), so a REWORK submission for
// 2 rejected goats still carries sop_submission_items for the whole shed -- including goats whose
// ORIGINAL completion is still 'recorded' and pending verification (never rejected, never
// reworked). For those goats the INSERT's attempt correctly collides with the active-completion
// index and is swallowed by the bare ON CONFLICT: nothing new needs to be recorded, the existing
// active completion already stands. That is correct, not a bug -- but the old eligibleItems count
// (every accepted/needs_review submission item) did not know that, so it counted those goats as
// "should have materialized" too, eligibleItems (5) outran materializedItems (2 -- only the truly
// reworked goats), and the whole submission's fanout was marked failed. That abort ran BEFORE
// emitVerificationItems, so NEITHER a new verification_items row NOR the
// verification.item.pending outbox event was ever produced for the two goats that legitimately DID
// get reworked -- the rework loop dead-ended even though the completions themselves were recorded.
//
// The fix: an item counts as "materialized" when its goat has ANY active completion (status
// recorded/accepted) for its resolved obligation -- whether that completion was just inserted by
// this submission or already existed from an earlier one. That mirrors exactly what the partial
// unique index (and therefore the swallowed ON CONFLICT) considers "already satisfied," so a
// legitimate no-op is no longer misreported as a failure, while a goat that ends up with NO active
// completion at all (a genuine insert failure -- e.g. a missing protocol_rules row) still fails the
// count and is reported.
func (r *Repository) submissionFanoutCounts(ctx context.Context, tenant, task, submission pgtype.UUID) (eligibleItems, materializedItems int, err error) {
	err = r.pool.QueryRow(ctx, `
	WITH eligible AS (
	  SELECT si.tenant_id, si.goat_id, oi.obligation_id
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
	  WHERE si.tenant_id = $1
	    AND si.task_id = $2
	    AND si.submission_id = $3
	    AND si.goat_id IS NOT NULL
	    AND si.state IN ('accepted', 'needs_review')
	    AND oi.status NOT IN ('completed', 'waived', 'canceled', 'superseded')
	    AND (
	      sd.code IN ('vaccination.drive', 'vaccination.session')
	      OR st.task_type IN ('vaccination', 'vaccination_drive', 'vaccination_session')
	    )
	)
	SELECT count(*)::int,
	       count(*) FILTER (
	         WHERE EXISTS (
	           SELECT 1
	           FROM vaccination_completions vc2
	           WHERE vc2.tenant_id = eligible.tenant_id
	             AND vc2.goat_id = eligible.goat_id
	             AND vc2.obligation_id = eligible.obligation_id
	             AND vc2.status IN ('recorded', 'accepted')
	         )
	       )::int
	FROM eligible`,
		tenant, task, submission).Scan(&eligibleItems, &materializedItems)
	if err != nil {
		return 0, 0, fmt.Errorf("vaccination: count submission fanout rows: %w", err)
	}
	return eligibleItems, materializedItems, nil
}

// ListRecordedCompletions returns completions awaiting review (status='recorded'), earliest
// administered first (the Verification queue).
func (r *Repository) ListRecordedCompletions(ctx context.Context, tenantID, parkID string, cursor *domain.RecordedCompletionCursor, limit int32) (domain.RecordedCompletionPage, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tenant, err := pgconv.UUID(tenantID)
	if err != nil {
		return domain.RecordedCompletionPage{}, fmt.Errorf("vaccination: tenant id: %w", err)
	}
	var park pgtype.UUID
	if parkID != "" {
		park, err = pgconv.UUID(parkID)
		if err != nil {
			return domain.RecordedCompletionPage{}, fmt.Errorf("vaccination: park id: %w", err)
		}
	}
	if limit <= 0 {
		limit = 100
	}
	totalCount, err := r.queries.CountRecordedCompletions(ctx, vaccinationdb.CountRecordedCompletionsParams{
		TenantID: tenant,
		ParkID:   park,
	})
	if err != nil {
		return domain.RecordedCompletionPage{}, fmt.Errorf("vaccination: count recorded completions: %w", err)
	}
	var cursorAdministeredAt pgtype.Timestamptz
	var cursorCompletionID pgtype.UUID
	if cursor != nil {
		cursorAdministeredAt = pgtype.Timestamptz{Time: cursor.AdministeredAt.UTC(), Valid: true}
		cursorCompletionID, err = pgconv.UUID(cursor.CompletionID)
		if err != nil {
			return domain.RecordedCompletionPage{}, fmt.Errorf("vaccination: cursor completion id: %w", err)
		}
	}
	rows, err := r.queries.ListRecordedCompletions(ctx, vaccinationdb.ListRecordedCompletionsParams{
		TenantID:             tenant,
		ParkID:               park,
		CursorAdministeredAt: cursorAdministeredAt,
		CursorCompletionID:   cursorCompletionID,
		RowLimitPlusOne:      limit + 1,
	})
	if err != nil {
		return domain.RecordedCompletionPage{}, fmt.Errorf("vaccination: list recorded completions: %w", err)
	}
	hasMore := len(rows) > int(limit)
	if hasMore {
		rows = rows[:int(limit)]
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
	var nextCursor *string
	if hasMore && len(rows) > 0 {
		last := rows[len(rows)-1]
		encoded, err := domain.EncodeRecordedCompletionCursor(domain.RecordedCompletionCursor{
			AdministeredAt: last.AdministeredAt.Time,
			CompletionID:   last.CompletionID,
		})
		if err != nil {
			return domain.RecordedCompletionPage{}, fmt.Errorf("vaccination: encode recorded completion cursor: %w", err)
		}
		nextCursor = &encoded
	}
	return domain.RecordedCompletionPage{
		Items:      out,
		TotalCount: totalCount,
		NextCursor: nextCursor,
	}, nil
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
			DoseCode:            row.DoseCode,
			VaccineLabel:        row.VaccineLabel,
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
	asOf := f.AsOf
	if asOf.IsZero() {
		asOf = time.Now().In(biztime.DefaultLocation())
	}
	return vaccinationdb.CountEligibleGoatsParams{
		TenantID:                tenant,
		WarmupNoVaccinationDays: f.WarmupNoVaccinationDays,
		AsOf:                    pgtype.Timestamptz{Time: asOf, Valid: true},
		Species:                 eligibilityWildcard(f.Species),
		Stage:                   eligibilityWildcard(f.Stage),
		Sex:                     eligibilityWildcard(f.Sex),
		Breed:                   eligibilityWildcard(f.Breed),
		Health:                  eligibilityWildcard(f.Health),
		ParkID:                  pgconv.NullableUUID(f.ParkID),
	}, nil
}

func eligibilityWildcard(value string) string {
	value = strings.TrimSpace(value)
	switch strings.ToLower(value) {
	case "", "all", "any":
		return ""
	default:
		return value
	}
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

// SumEligibilityRollup reads the config impact-preview aggregate from the vaccination_eligibility_rollups
// READ MODEL only — it never scans goats. Returns total usable animals + distinct sheds holding them for
// the tenant + optional eligibility filter dims, plus the read model's freshness stamp. Empty scope
// yields zeros with source_revision 0 (rollup not yet recomputed / no matching animals).
func (r *Repository) SumEligibilityRollup(ctx context.Context, f domain.ImpactFilter) (domain.EligibilityRollupAggregate, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tenant, err := pgconv.UUID(f.TenantID)
	if err != nil {
		return domain.EligibilityRollupAggregate{}, fmt.Errorf("vaccination: tenant id: %w", err)
	}
	var out domain.EligibilityRollupAggregate
	var recomputedAt pgtype.Timestamptz
	err = r.pool.QueryRow(ctx, `
SELECT COALESCE(SUM(animal_count), 0)::bigint AS eligible_animals,
       COUNT(DISTINCT shed_id) FILTER (WHERE animal_count > 0 AND shed_id IS NOT NULL)::bigint AS affected_sheds,
       COALESCE(MAX(source_revision), 0)::bigint AS source_revision,
       MAX(recomputed_at) AS recomputed_at
FROM vaccination_eligibility_rollups
WHERE tenant_id = $1::uuid
  AND usable_for_vaccination = true
  AND ($2::text = '' OR species = $2::text)
  AND ($3::text = '' OR management_stage = $3::text)
  AND ($4::text = '' OR sex = $4::text)
  AND ($5::text = '' OR breed = $5::text)
  AND ($6::text = '' OR health_status = $6::text)
  AND ($7::uuid IS NULL OR park_id = $7::uuid)`,
		tenant,
		eligibilityWildcard(f.Species),
		eligibilityWildcard(f.Stage),
		eligibilityWildcard(f.Sex),
		eligibilityWildcard(f.Breed),
		eligibilityWildcard(f.Health),
		pgconv.NullableUUID(f.ParkID),
	).Scan(&out.EligibleAnimals, &out.AffectedSheds, &out.SourceRevision, &recomputedAt)
	if err != nil {
		return domain.EligibilityRollupAggregate{}, fmt.Errorf("vaccination: sum eligibility rollup: %w", err)
	}
	if recomputedAt.Valid {
		t := recomputedAt.Time
		out.RecomputedAt = &t
	}
	return out, nil
}

// CapacityMaxPerDay returns the tenant's configured daily operator animal cap, falling back to the code
// default when no vaccination_capacity_config row exists.
func (r *Repository) CapacityMaxPerDay(ctx context.Context, tenantID string) (int64, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tenant, err := pgconv.UUID(tenantID)
	if err != nil {
		return 0, fmt.Errorf("vaccination: tenant id: %w", err)
	}
	var maxPerDay int64
	err = r.pool.QueryRow(ctx, `
SELECT max_per_day::bigint
FROM vaccination_capacity_config
WHERE tenant_id = $1::uuid`, tenant).Scan(&maxPerDay)
	if errors.Is(err, pgx.ErrNoRows) {
		return defaultDailyVaccinationCap, nil
	}
	if err != nil {
		return 0, fmt.Errorf("vaccination: capacity max per day: %w", err)
	}
	if maxPerDay < 1 {
		return defaultDailyVaccinationCap, nil
	}
	return maxPerDay, nil
}

// RecomputeEligibilityRollup fully rebuilds vaccination_eligibility_rollups for one tenant from the
// source tables (goats + location operational attributes + shed profiles + animal stage lookup). It is
// the PROJECTOR write path — delete-then-insert per tenant inside a single transaction. This is a heavy
// full-herd aggregate and must run off the UI request path (CLI / future event-driven projector). The
// caller owns the timeout via ctx (the full-herd scan can exceed the default query timeout).
func (r *Repository) RecomputeEligibilityRollup(ctx context.Context, tenantID string) (domain.RollupRecomputeResult, error) {
	tenant, err := pgconv.UUID(tenantID)
	if err != nil {
		return domain.RollupRecomputeResult{}, fmt.Errorf("vaccination: tenant id: %w", err)
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return domain.RollupRecomputeResult{}, fmt.Errorf("vaccination: begin recompute rollup: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()

	var sourceRevision int64
	var recomputedAt time.Time
	if err := tx.QueryRow(ctx, `SELECT (extract(epoch FROM now()) * 1000)::bigint, now()`).Scan(&sourceRevision, &recomputedAt); err != nil {
		return domain.RollupRecomputeResult{}, fmt.Errorf("vaccination: recompute stamp: %w", err)
	}

	if _, err := tx.Exec(ctx, `DELETE FROM vaccination_eligibility_rollups WHERE tenant_id = $1::uuid`, tenant); err != nil {
		return domain.RollupRecomputeResult{}, fmt.Errorf("vaccination: clear rollup: %w", err)
	}

	// projection-review: membership=canonical live goats for the tenant, LEFT JOINed 1:{0,1} to their own goat_shed_partitions row (PK (tenant_id, goat_id)) so an animal is counted exactly once; group_key=the existing (tenant_id, park_id, shed_id, species, management_stage, sex, breed, health_status, usable_for_vaccination) key PLUS NULLIF(gsp.partition_label,'whole'), which SPLITS a shed's rollup across its pens instead of multiplying it, and keeps a single NULL-partition row for every non-partitioned shed; join_cardinality=the goat_shed_partitions LEFT JOIN is 1:{0,1} per goat and adds an ATTRIBUTE before grouping, so it cannot fan out the count(*); pagination=none, this is a full recompute of a derived rollup, never paged; scope=tenant_id, with park/shed/partition carried as columns for the two ceo_ai views (migration 000114) that match on the identical (tenant_id, shed_id, NULLIF(partition_label,'whole')) key
	// partition_review: producer key adds NULLIF(gsp.partition_label,'whole') to
	// the existing (tenant_id, park_id, shed_id, species, management_stage, sex,
	// breed, health_status, usable_for_vaccination) GROUP BY. goat_shed_partitions
	// PK is (tenant_id, goat_id), 1:{0,1} per goat, so this LEFT JOIN cannot fan
	// out the per-goat membership the count(*) is grouping over -- it only adds
	// an ATTRIBUTE to each goat row before grouping. A goat in a non-partitioned
	// shed (no goat_shed_partitions row, or one stamped 'whole') groups into the
	// partition_label = NULL grain, keeping today's single-row-per-shed behavior
	// for every non-partitioned shed. group_key=consumer (the two ceo_ai views
	// this feeds, migration 000114) matches on the identical (tenant_id, shed_id,
	// NULLIF(partition_label,'whole')) key.
	tag, err := tx.Exec(ctx, `
INSERT INTO vaccination_eligibility_rollups (
  tenant_id, park_id, shed_id, species, management_stage, sex, breed, health_status,
  usable_for_vaccination, animal_count, source_revision, recomputed_at, updated_at, partition_label
)
SELECT
  g.tenant_id,
  g.park_id,
  g.shed_id,
  COALESCE(g.species, '')::text,
  COALESCE(asl.stage_code, g.management_stage, '')::text,
  COALESCE(g.sex, '')::text,
  COALESCE(g.breed, '')::text,
  COALESCE(g.health_status, '')::text,
  (
    COALESCE(g.health_status, '') NOT IN ('sick', 'under_treatment', 'recovering', 'quarantine', 'icu')
    AND COALESCE(loa.usable_for_vaccination, true)
    AND NOT COALESCE(loa.is_quarantine, false)
    AND NOT COALESCE(loa.is_icu, false)
  ) AS usable_for_vaccination,
  count(*)::bigint,
  $2::bigint,
  $3::timestamptz,
  $3::timestamptz,
  NULLIF(gsp.partition_label, 'whole')
FROM goats g
LEFT JOIN location_operational_attributes loa
  ON loa.tenant_id = g.tenant_id
 AND loa.location_id = COALESCE(g.current_location_id, g.shed_id)
LEFT JOIN shed_profiles sp
  ON sp.tenant_id = g.tenant_id
 AND sp.location_id = g.shed_id
LEFT JOIN animal_stage_lookup asl
  ON asl.tenant_id = sp.tenant_id
 AND asl.animal_stage_id = sp.animal_stage_id
 AND asl.status = 'active'
LEFT JOIN goat_shed_partitions gsp
  ON gsp.tenant_id = g.tenant_id
 AND gsp.goat_id = g.goat_id
WHERE g.tenant_id = $1::uuid
  AND g.lifecycle_status = 'alive'
  AND g.merged_into_goat_id IS NULL
GROUP BY g.tenant_id, g.park_id, g.shed_id,
         COALESCE(g.species, ''),
         COALESCE(asl.stage_code, g.management_stage, ''),
         COALESCE(g.sex, ''),
         COALESCE(g.breed, ''),
         COALESCE(g.health_status, ''),
         (
           COALESCE(g.health_status, '') NOT IN ('sick', 'under_treatment', 'recovering', 'quarantine', 'icu')
           AND COALESCE(loa.usable_for_vaccination, true)
           AND NOT COALESCE(loa.is_quarantine, false)
           AND NOT COALESCE(loa.is_icu, false)
         ),
         NULLIF(gsp.partition_label, 'whole')`, tenant, sourceRevision, recomputedAt)
	if err != nil {
		return domain.RollupRecomputeResult{}, fmt.Errorf("vaccination: rebuild rollup: %w", err)
	}

	var eligibleAnimals int64
	if err := tx.QueryRow(ctx, `
SELECT COALESCE(SUM(animal_count), 0)::bigint
FROM vaccination_eligibility_rollups
WHERE tenant_id = $1::uuid AND usable_for_vaccination = true`, tenant).Scan(&eligibleAnimals); err != nil {
		return domain.RollupRecomputeResult{}, fmt.Errorf("vaccination: recompute eligible total: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return domain.RollupRecomputeResult{}, fmt.Errorf("vaccination: commit recompute rollup: %w", err)
	}
	committed = true
	return domain.RollupRecomputeResult{
		TenantID:        tenantID,
		Grains:          tag.RowsAffected(),
		EligibleAnimals: eligibleAnimals,
		SourceRevision:  sourceRevision,
		RecomputedAt:    recomputedAt,
	}, nil
}

// ListRecoverableDeferredVaccinationGoatIDs returns a bounded set of goats with old deferred
// vaccination obligations whose current animal/location state no longer requires a clinical or
// procurement exclusion. Missed obligations are repaired by the obligation-owned stale-missed
// batch-link pass before this selector runs, then re-enter batching through the normal sweeper.
// The generation job uses this before the full-herd scan so recovery repair does not depend on
// reaching late goat-id pages.
func (r *Repository) ListRecoverableDeferredVaccinationGoatIDs(ctx context.Context, tenantID string, olderThan time.Time, limit int32) ([]string, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tenant, err := pgconv.UUID(tenantID)
	if err != nil {
		return nil, fmt.Errorf("vaccination: tenant id: %w", err)
	}
	if olderThan.IsZero() {
		olderThan = time.Now().In(biztime.DefaultLocation())
	}
	if limit <= 0 {
		limit = 1000
	}
	rows, err := r.pool.Query(ctx, `
WITH earliest_by_goat AS (
  SELECT DISTINCT ON (oi.target_id)
         oi.target_id::text AS goat_id,
         oi.due_at,
         oi.obligation_id
  FROM obligation_instances oi
  JOIN protocol_versions pv
    ON pv.tenant_id = oi.tenant_id
   AND pv.protocol_version_id = oi.protocol_version_id
  JOIN protocol_definitions pd
    ON pd.tenant_id = pv.tenant_id
   AND pd.protocol_id = pv.protocol_id
  JOIN goats g
    ON g.tenant_id = oi.tenant_id
   AND g.goat_id = oi.target_id
  LEFT JOIN location_operational_attributes loa
    ON loa.tenant_id = g.tenant_id
   AND loa.location_id = COALESCE(g.current_location_id, g.shed_id)
  WHERE oi.tenant_id = $1::uuid
    AND oi.target_type = 'goat'
    AND oi.status = 'deferred'
    AND oi.due_at <= $2::timestamptz
    AND pd.category = 'vaccination'
    AND g.lifecycle_status = 'alive'
    AND COALESCE(g.health_status, '') NOT IN ('sick', 'under_treatment', 'recovering', 'quarantine', 'icu')
    AND COALESCE(loa.usable_for_vaccination, true)
    AND NOT COALESCE(loa.is_quarantine, false)
    AND NOT COALESCE(loa.is_icu, false)
    AND NOT EXISTS (
      SELECT 1
      FROM vw_procurement_vaccination_excluded_goats ex
      WHERE ex.tenant_id = oi.tenant_id
        AND ex.goat_id = oi.target_id
    )
  ORDER BY oi.target_id, oi.due_at ASC, oi.obligation_id ASC
)
SELECT goat_id
FROM earliest_by_goat
ORDER BY due_at ASC, obligation_id ASC
LIMIT $3`, tenant, pgconv.Timestamptz(olderThan), limit)
	if err != nil {
		return nil, fmt.Errorf("vaccination: list recoverable deferred goats: %w", err)
	}
	defer rows.Close()
	out := make([]string, 0, limit)
	for rows.Next() {
		var goatID string
		if err := rows.Scan(&goatID); err != nil {
			return nil, fmt.Errorf("vaccination: scan recoverable deferred goat: %w", err)
		}
		out = append(out, goatID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("vaccination: iterate recoverable deferred goats: %w", err)
	}
	return out, nil
}

// CountRecoverableDeferredVaccinationObligations is the stuck-deferred detector used by the
// recovery repair job report. A nonzero count after repair means recovered goats still have old
// deferred vaccination obligations and need operator attention. Missed obligations are repaired
// by the obligation-owned stale-missed batch-link pass and then picked up by the normal sweeper.
func (r *Repository) CountRecoverableDeferredVaccinationObligations(ctx context.Context, tenantID string, olderThan time.Time) (int64, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tenant, err := pgconv.UUID(tenantID)
	if err != nil {
		return 0, fmt.Errorf("vaccination: tenant id: %w", err)
	}
	if olderThan.IsZero() {
		olderThan = time.Now().In(biztime.DefaultLocation())
	}
	var total int64
	if err := r.pool.QueryRow(ctx, `
SELECT count(*)::bigint
FROM obligation_instances oi
JOIN protocol_versions pv
  ON pv.tenant_id = oi.tenant_id
 AND pv.protocol_version_id = oi.protocol_version_id
JOIN protocol_definitions pd
  ON pd.tenant_id = pv.tenant_id
 AND pd.protocol_id = pv.protocol_id
JOIN goats g
  ON g.tenant_id = oi.tenant_id
 AND g.goat_id = oi.target_id
LEFT JOIN location_operational_attributes loa
  ON loa.tenant_id = g.tenant_id
 AND loa.location_id = COALESCE(g.current_location_id, g.shed_id)
WHERE oi.tenant_id = $1::uuid
  AND oi.target_type = 'goat'
  AND oi.status = 'deferred'
  AND oi.due_at <= $2::timestamptz
  AND pd.category = 'vaccination'
  AND g.lifecycle_status = 'alive'
  AND COALESCE(g.health_status, '') NOT IN ('sick', 'under_treatment', 'recovering', 'quarantine', 'icu')
  AND COALESCE(loa.usable_for_vaccination, true)
  AND NOT COALESCE(loa.is_quarantine, false)
  AND NOT COALESCE(loa.is_icu, false)
  AND NOT EXISTS (
    SELECT 1
    FROM vw_procurement_vaccination_excluded_goats ex
    WHERE ex.tenant_id = oi.tenant_id
      AND ex.goat_id = oi.target_id
  )`, tenant, pgconv.Timestamptz(olderThan)).Scan(&total); err != nil {
		return 0, fmt.Errorf("vaccination: count recoverable deferred obligations: %w", err)
	}
	return total, nil
}

func timestamptzValue(t pgtype.Timestamptz) *time.Time {
	if !t.Valid {
		return nil
	}
	tt := t.Time.UTC()
	return &tt
}

func eligibleGoatFromGenerationRow(
	goatID string,
	dob, entryDate, breedingDate, lastDeliveryDate pgtype.Date,
	lifecycle, health, reproductive, species, originType string,
	procurementPurpose string,
	warmingEntryAt pgtype.Timestamptz,
	shedID, parkID, partitionLabel, sex, breed, stage, ageBand string,
	locationIsQuarantine, locationIsICU bool,
) domain.EligibleGoat {
	return domain.EligibleGoat{
		GoatID:               goatID,
		DOB:                  pgconv.DateValue(dob),
		EntryDate:            pgconv.DateValue(entryDate),
		BreedingDate:         pgconv.DateValue(breedingDate),
		LastDeliveryDate:     pgconv.DateValue(lastDeliveryDate),
		WarmingEntryAt:       timestamptzValue(warmingEntryAt),
		Species:              species,
		OriginType:           originType,
		LifecycleStatus:      lifecycle,
		HealthStatus:         health,
		ReproductiveStatus:   reproductive,
		ProcurementPurpose:   procurementPurpose,
		ShedID:               shedID,
		ParkID:               parkID,
		PartitionLabel:       partitionLabel,
		Sex:                  sex,
		Breed:                breed,
		Stage:                stage,
		AgeBand:              ageBand,
		LocationIsQuarantine: locationIsQuarantine,
		LocationIsICU:        locationIsICU,
	}
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
	if strings.TrimSpace(f.ProtocolVersionID) != "" {
		return r.listEligibleGoatsForGenerationWithCompiledDimensions(ctx, tenant, f, afterUUID, limit)
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
		out = append(out, eligibleGoatFromGenerationRow(
			row.GoatID, row.Dob, row.EntryDate, row.BreedingDate, row.LastDeliveryDate,
			row.LifecycleStatus, row.HealthStatus, row.ReproductiveStatus, row.Species, row.OriginType,
			row.ProcurementPurpose,
			row.WarmingEntryAt,
			row.ShedID, row.ParkID, row.PartitionLabel, row.Sex, row.Breed, row.ManagementStage, row.AgeBand,
			row.LocationIsQuarantine, row.LocationIsIcu,
		))
	}
	return out, nil
}

func (r *Repository) listEligibleGoatsForGenerationWithCompiledDimensions(ctx context.Context, tenant pgtype.UUID, f domain.ImpactFilter, afterUUID pgtype.UUID, limit int32) ([]domain.EligibleGoat, error) {
	vid, err := pgconv.UUID(f.ProtocolVersionID)
	if err != nil {
		return nil, fmt.Errorf("vaccination: protocol version id: %w", err)
	}
	asOf := f.AsOf
	if asOf.IsZero() {
		asOf = time.Now().In(biztime.DefaultLocation())
	}
	rows, err := r.pool.Query(ctx, `
WITH compiled AS (
  SELECT EXISTS (
    SELECT 1
    FROM protocol_rule_dimensions prd
    WHERE prd.tenant_id = $1
      AND prd.protocol_version_id = $2
      AND prd.category = 'vaccination'
  ) AS has_dimensions
)
SELECT g.goat_id::text AS goat_id, g.dob, g.entry_date, g.breeding_date, g.last_delivery_date, g.lifecycle_status,
       COALESCE(g.health_status, '')::text AS health_status,
       COALESCE(g.reproductive_status, '')::text AS reproductive_status,
       COALESCE(g.species, 'goat')::text AS species,
       COALESCE(g.origin_type, '')::text AS origin_type,
       COALESCE(proc.procurement_purpose, '')::text AS procurement_purpose,
       proc.warming_entry_at,
       COALESCE(shed.location_id::text, '')::text AS shed_id,
       COALESCE(park.location_id::text, '')::text AS park_id,
       COALESCE(gsp.partition_label, '')::text AS partition_label,
       COALESCE(g.sex, '')::text AS sex,
       COALESCE(g.breed, '')::text AS breed,
       COALESCE(asl.stage_code, g.management_stage, '')::text AS management_stage,
       COALESCE(g.age_band, '')::text AS age_band,
       COALESCE(loa.is_quarantine, false)::boolean AS location_is_quarantine,
       COALESCE(loa.is_icu, false)::boolean AS location_is_icu
FROM goats g
CROSS JOIN compiled c
LEFT JOIN LATERAL (
  SELECT plg.purpose AS procurement_purpose,
         COALESCE(plg.warmup_started_at, plg.intake_accepted_at, g.entry_date::timestamptz) AS warming_entry_at
  FROM procurement_load_goats plg
  WHERE plg.tenant_id = g.tenant_id
    AND plg.goat_id = g.goat_id
  ORDER BY COALESCE(plg.warmup_started_at, plg.intake_accepted_at, plg.created_at) DESC NULLS LAST
  LIMIT 1
) proc ON true
LEFT JOIN location_operational_attributes loa
  ON loa.tenant_id = g.tenant_id
 AND loa.location_id = COALESCE(g.current_location_id, g.shed_id)
LEFT JOIN locations current_loc
  ON current_loc.tenant_id = g.tenant_id
 AND current_loc.location_id = COALESCE(g.current_location_id, g.shed_id, g.park_id)
LEFT JOIN shed_profiles sp
  ON sp.tenant_id = g.tenant_id
 AND sp.location_id = g.shed_id
LEFT JOIN animal_stage_lookup asl
  ON asl.tenant_id = sp.tenant_id
 AND asl.animal_stage_id = sp.animal_stage_id
 AND asl.status = 'active'
LEFT JOIN locations shed
  ON shed.tenant_id = g.tenant_id
 AND shed.location_id = g.shed_id
 AND shed.location_type = 'shed'
LEFT JOIN locations park
  ON park.tenant_id = g.tenant_id
 AND park.location_id = g.park_id
 AND park.location_type = 'park'
LEFT JOIN goat_shed_partitions gsp
  ON gsp.tenant_id = g.tenant_id
 AND gsp.goat_id = g.goat_id
WHERE g.tenant_id = $1
  AND g.lifecycle_status IN ('alive', 'sick', 'under_treatment', 'quarantine', 'icu')
  AND ($3::text = '' OR lower(COALESCE(asl.stage_code, g.management_stage, '')) = lower($3::text))
  AND ($4::text = '' OR lower(g.sex) = lower($4::text))
  AND ($5::text = '' OR lower(g.breed) = lower($5::text))
  AND ($6::uuid IS NULL OR g.park_id = $6::uuid)
  AND g.goat_id > $7::uuid
  AND (
    NOT c.has_dimensions
    OR EXISTS (
      SELECT 1
      FROM protocol_rule_dimensions prd
      WHERE prd.tenant_id = g.tenant_id
        AND prd.protocol_version_id = $2
        AND prd.category = 'vaccination'
        AND (prd.species = 'all' OR prd.species = COALESCE(g.species, 'goat'))
        AND (prd.animal_stage = 'all' OR lower(prd.animal_stage) = lower(COALESCE(asl.stage_code, g.management_stage, '')))
        AND (prd.sex = 'all' OR prd.sex = lower(g.sex))
        AND (prd.breed = 'all' OR lower(prd.breed) = lower(g.breed))
        AND (prd.procurement_purpose = 'all' OR lower(prd.procurement_purpose) = lower(COALESCE(proc.procurement_purpose, '')))
        AND (
          (prd.min_age_days IS NULL AND prd.max_age_days IS NULL)
          OR (
            g.dob IS NOT NULL
            AND (
              prd.min_age_days IS NULL
              OR (($8::timestamptz AT TIME ZONE 'Asia/Kolkata')::date - g.dob) >= prd.min_age_days
            )
            AND (
              prd.max_age_days IS NULL
              OR (($8::timestamptz AT TIME ZONE 'Asia/Kolkata')::date - g.dob) <= prd.max_age_days
            )
          )
        )
    )
  )
ORDER BY g.goat_id
LIMIT $9`, tenant, vid, f.Stage, f.Sex, f.Breed, pgconv.NullableUUID(f.ParkID), afterUUID, pgconv.Timestamptz(asOf), limit)
	if err != nil {
		return nil, fmt.Errorf("vaccination: list eligible goats by compiled dimensions: %w", err)
	}
	defer rows.Close()
	out := make([]domain.EligibleGoat, 0, limit)
	for rows.Next() {
		var (
			goatID, lifecycle, health, reproductive, species, originType, procurementPurpose string
			shedID, parkID, partitionLabel, sex, breed, stage, ageBand                       string
			dob, entryDate, breedingDate, lastDeliveryDate                                   pgtype.Date
			warmingEntryAt                                                                   pgtype.Timestamptz
			locationIsQuarantine, locationIsICU                                              bool
		)
		if err := rows.Scan(
			&goatID,
			&dob,
			&entryDate,
			&breedingDate,
			&lastDeliveryDate,
			&lifecycle,
			&health,
			&reproductive,
			&species,
			&originType,
			&procurementPurpose,
			&warmingEntryAt,
			&shedID,
			&parkID,
			&partitionLabel,
			&sex,
			&breed,
			&stage,
			&ageBand,
			&locationIsQuarantine,
			&locationIsICU,
		); err != nil {
			return nil, fmt.Errorf("vaccination: scan eligible goat by compiled dimensions: %w", err)
		}
		out = append(out, eligibleGoatFromGenerationRow(
			goatID, dob, entryDate, breedingDate, lastDeliveryDate,
			lifecycle, health, reproductive, species, originType,
			procurementPurpose,
			warmingEntryAt,
			shedID, parkID, partitionLabel, sex, breed, stage, ageBand,
			locationIsQuarantine, locationIsICU,
		))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("vaccination: iterate eligible goats by compiled dimensions: %w", err)
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
	return eligibleGoatFromGenerationRow(
		row.GoatID, row.Dob, row.EntryDate, row.BreedingDate, row.LastDeliveryDate,
		row.LifecycleStatus, row.HealthStatus, row.ReproductiveStatus, row.Species, row.OriginType,
		row.ProcurementPurpose,
		row.WarmingEntryAt,
		row.ShedID, row.ParkID, row.PartitionLabel, row.Sex, row.Breed, row.ManagementStage, row.AgeBand,
		row.LocationIsQuarantine, row.LocationIsIcu,
	), true, nil
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
			DueAt:        candidate.DueAt.Format(time.RFC3339Nano),
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
	goat_context AS (
	  SELECT DISTINCT
	         c.goat_id,
	         g.entry_date,
	         'Asia/Kolkata'::text AS timezone
	  FROM candidate c
	  JOIN goats g
	    ON g.tenant_id = $1::uuid
	   AND g.goat_id = c.goat_id
	  LEFT JOIN locations current_loc
	    ON current_loc.tenant_id = g.tenant_id
	   AND current_loc.location_id = COALESCE(g.current_location_id, g.shed_id, g.park_id)
	  LEFT JOIN locations shed
	    ON shed.tenant_id = g.tenant_id
	   AND shed.location_id = g.shed_id
	   AND shed.location_type = 'shed'
	  LEFT JOIN locations park
	    ON park.tenant_id = g.tenant_id
	   AND park.location_id = g.park_id
	   AND park.location_type = 'park'
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
	  JOIN goat_context gc
	    ON gc.goat_id = ev.goat_id
		  JOIN procurement_load_goats plg
		    ON plg.tenant_id = ev.tenant_id
		   AND plg.load_id = ev.load_id
		   AND plg.goat_id = ev.goat_id
		  JOIN proof_artifacts proof
		    ON proof.tenant_id = ev.tenant_id
		   AND proof.proof_id = ev.proof_ref_id
		   AND proof.upload_state = 'completed'
		  WHERE ev.dose_code = t.dose_code
		    AND ev_pr.dose_code = t.dose_code
		    AND ev_pr.sequence = t.sequence
		    AND ev_pv.protocol_id = t.protocol_id
		    AND ev.administered_at <= $3::timestamptz
		    AND ev.reviewed_at <= $3::timestamptz
		    AND ev.proof_ref_id IS NOT NULL
		    AND plg.holding_location_id IS NOT NULL
		    AND plg.warmup_started_at IS NOT NULL
		    AND COALESCE(
		      plg.warmup_days,
		      floor(extract(epoch FROM (COALESCE(plg.warmup_ended_at, ev.administered_at) - plg.warmup_started_at)) / 86400)::int
		    ) BETWEEN 28 AND 35
		    AND ev.administered_at >= plg.warmup_started_at
		    AND (plg.warmup_ended_at IS NULL OR ev.administered_at <= plg.warmup_ended_at)
		    AND (
		      t.rule_repeat = 'none'
		      OR (ev.administered_at AT TIME ZONE gc.timezone)::date >= (t.due_at AT TIME ZONE gc.timezone)::date
		    )
		    AND (
		      plg.intake_accepted_at IS NULL
	      OR ev.administered_at <= plg.intake_accepted_at
	    )
	    AND (
	      gc.entry_date IS NULL
	      OR ev.administered_at < (gc.entry_date::timestamptz + interval '1 day')
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
	  JOIN goat_context gc
	    ON gc.goat_id = vc.goat_id
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
	    AND (
	      t.rule_repeat = 'none'
	      OR (oi.due_at AT TIME ZONE gc.timezone)::date = (t.due_at AT TIME ZONE gc.timezone)::date
	    )
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

// RecentVaccineAdministrationsForGoats returns every recent accepted/trusted administration per goat
// needed for cross-vaccine gap enforcement. A later killed dose must not hide an earlier live dose
// when another live vaccine is being scheduled.
func (r *Repository) RecentVaccineAdministrationsForGoats(ctx context.Context, tenantID string, goatIDs []string, before time.Time) (map[string][]domain.RecentVaccineAdministration, error) {
	out := make(map[string][]domain.RecentVaccineAdministration)
	if len(goatIDs) == 0 {
		return out, nil
	}
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tenant, err := pgconv.UUID(tenantID)
	if err != nil {
		return nil, fmt.Errorf("vaccination: tenant id: %w", err)
	}
	uuids := make([]pgtype.UUID, 0, len(goatIDs))
	for _, goatID := range goatIDs {
		id, err := pgconv.UUID(goatID)
		if err != nil {
			return nil, fmt.Errorf("vaccination: goat id %q: %w", goatID, err)
		}
		uuids = append(uuids, id)
	}
	rows, err := r.pool.Query(ctx, `
WITH completion_admins AS (
  SELECT vc.goat_id::text AS goat_id,
         vc.administered_at,
         COALESCE(
           NULLIF(pr.eligibility_json -> 'vaccine' ->> 'code', ''),
           NULLIF(pv.rule_dsl -> 'vaccine' ->> 'code', ''),
           ''
         )::text AS vaccine_code,
         COALESCE(
           NULLIF(pr.eligibility_json -> 'vaccine' ->> 'type', ''),
           NULLIF(pv.rule_dsl -> 'vaccine' ->> 'type', ''),
           ''
         )::text AS vaccine_type,
         COALESCE(
           NULLIF(pr.eligibility_json -> 'vaccine' ->> 'pathogen_class', ''),
           NULLIF(pv.rule_dsl -> 'vaccine' ->> 'pathogen_class', ''),
           ''
         )::text AS pathogen_class,
         COALESCE(NULLIF(pr.dose_code, ''), '')::text AS dose_code,
         COALESCE(pr.sequence, 0)::int AS sequence,
         oi.protocol_version_id::text AS protocol_version_id,
         pv.protocol_id::text AS protocol_id
  FROM vaccination_completions vc
  JOIN obligation_instances oi
    ON oi.tenant_id = vc.tenant_id
   AND oi.obligation_id = vc.obligation_id
  JOIN protocol_versions pv
    ON pv.tenant_id = oi.tenant_id
   AND pv.protocol_version_id = oi.protocol_version_id
  LEFT JOIN protocol_rules pr
    ON pr.tenant_id = oi.tenant_id
   AND pr.protocol_version_id = oi.protocol_version_id
   AND pr.rule_id = oi.rule_id
  WHERE vc.tenant_id = $1
    AND vc.goat_id = ANY($2::uuid[])
    AND vc.status = 'accepted'
    AND vc.verified_at IS NOT NULL
    AND vc.administered_at <= $3::timestamptz
    AND vc.verified_at <= $3::timestamptz
),
trusted_admins AS (
  SELECT ev.goat_id::text AS goat_id,
         ev.administered_at,
         COALESCE(
           NULLIF(pr.eligibility_json -> 'vaccine' ->> 'code', ''),
           NULLIF(ev.metadata ->> 'vaccine_code', ''),
           NULLIF(ev.vaccine_name, ''),
           NULLIF(pv.rule_dsl -> 'vaccine' ->> 'code', ''),
           ''
         )::text AS vaccine_code,
         COALESCE(
           NULLIF(pr.eligibility_json -> 'vaccine' ->> 'type', ''),
           NULLIF(ev.metadata ->> 'vaccine_type', ''),
           NULLIF(pv.rule_dsl -> 'vaccine' ->> 'type', ''),
           ''
         )::text AS vaccine_type,
         COALESCE(
           NULLIF(pr.eligibility_json -> 'vaccine' ->> 'pathogen_class', ''),
           NULLIF(ev.metadata ->> 'pathogen_class', ''),
           NULLIF(pv.rule_dsl -> 'vaccine' ->> 'pathogen_class', ''),
           ''
         )::text AS pathogen_class,
         COALESCE(NULLIF(pr.dose_code, ''), '')::text AS dose_code,
         COALESCE(pr.sequence, 0)::int AS sequence,
         ev.protocol_version_id::text AS protocol_version_id,
         pv.protocol_id::text AS protocol_id
  FROM procurement_hf_vaccination_evidence ev
  JOIN procurement_load_goats plg
    ON plg.tenant_id = ev.tenant_id
   AND plg.load_id = ev.load_id
   AND plg.goat_id = ev.goat_id
  JOIN proof_artifacts proof
    ON proof.tenant_id = ev.tenant_id
   AND proof.proof_id = ev.proof_ref_id
   AND proof.upload_state = 'completed'
  JOIN protocol_versions pv
    ON pv.tenant_id = ev.tenant_id
   AND pv.protocol_version_id = ev.protocol_version_id
  LEFT JOIN protocol_rules pr
    ON pr.tenant_id = ev.tenant_id
   AND pr.protocol_version_id = ev.protocol_version_id
   AND pr.rule_id = ev.rule_id
  WHERE ev.tenant_id = $1
    AND ev.goat_id = ANY($2::uuid[])
    AND ev.review_status = 'trusted'
    AND ev.reviewed_at IS NOT NULL
    AND ev.administered_at <= $3::timestamptz
    AND ev.reviewed_at <= $3::timestamptz
    AND ev.proof_ref_id IS NOT NULL
    AND plg.holding_location_id IS NOT NULL
    AND plg.warmup_started_at IS NOT NULL
    AND COALESCE(
      plg.warmup_days,
      floor(extract(epoch FROM (COALESCE(plg.warmup_ended_at, ev.administered_at) - plg.warmup_started_at)) / 86400)::int
    ) BETWEEN 28 AND 35
    AND ev.administered_at >= plg.warmup_started_at
    AND (plg.warmup_ended_at IS NULL OR ev.administered_at <= plg.warmup_ended_at)
),
-- BUG-017: reviewed PRE-ARRIVAL supplier-claimed history. A separate channel from trusted_admins
-- on purpose: those claims have no proof artifact and predate the holding-farm warm-up window, so
-- they can never satisfy that gate. They earn history status only by surviving the pre-arrival
-- review (validated against published rules + the animal's independently classified schedule
-- path), which is what review_status='accepted' records. Rejected rows are never selected here, so
-- an impossible supplier claim can never suppress due work.
prearrival_admins AS (
  SELECT ph.goat_id::text AS goat_id,
         ph.administered_at,
         COALESCE(
           NULLIF(pr.eligibility_json -> 'vaccine' ->> 'code', ''),
           NULLIF(ph.vaccine_code, ''),
           NULLIF(pv.rule_dsl -> 'vaccine' ->> 'code', ''),
           ''
         )::text AS vaccine_code,
         COALESCE(
           NULLIF(pr.eligibility_json -> 'vaccine' ->> 'type', ''),
           NULLIF(pv.rule_dsl -> 'vaccine' ->> 'type', ''),
           ''
         )::text AS vaccine_type,
         COALESCE(
           NULLIF(pr.eligibility_json -> 'vaccine' ->> 'pathogen_class', ''),
           NULLIF(pv.rule_dsl -> 'vaccine' ->> 'pathogen_class', ''),
           ''
         )::text AS pathogen_class,
         COALESCE(NULLIF(pr.dose_code, ''), NULLIF(ph.dose_code, ''), '')::text AS dose_code,
         COALESCE(pr.sequence, ph.sequence, 0)::int AS sequence,
         ph.protocol_version_id::text AS protocol_version_id,
         pv.protocol_id::text AS protocol_id
  FROM vaccination_prearrival_history_entries ph
  JOIN protocol_versions pv
    ON pv.tenant_id = ph.tenant_id
   AND pv.protocol_version_id = ph.protocol_version_id
  LEFT JOIN protocol_rules pr
    ON pr.tenant_id = ph.tenant_id
   AND pr.protocol_version_id = ph.protocol_version_id
   AND pr.rule_id = ph.rule_id
  WHERE ph.tenant_id = $1
    AND ph.goat_id = ANY($2::uuid[])
    AND ph.review_status = 'accepted'
    AND ph.administered_at <= $3::timestamptz
    AND ph.reviewed_at <= $3::timestamptz
),
anchor_admins AS (
  SELECT DISTINCT ON (g.goat_id, vae.vaccination_anchor_event_id)
         g.goat_id::text AS goat_id,
         vae.anchor_date::timestamptz AS administered_at,
         vae.vaccine_code::text AS vaccine_code,
         COALESCE(NULLIF(pr.eligibility_json -> 'vaccine' ->> 'type', ''), NULLIF(pv.rule_dsl -> 'vaccine' ->> 'type', ''), '')::text AS vaccine_type,
         COALESCE(NULLIF(pr.eligibility_json -> 'vaccine' ->> 'pathogen_class', ''), NULLIF(pv.rule_dsl -> 'vaccine' ->> 'pathogen_class', ''), '')::text AS pathogen_class,
         pr.dose_code::text AS dose_code,
         pr.sequence::int AS sequence,
         pv.protocol_version_id::text AS protocol_version_id,
         pv.protocol_id::text AS protocol_id
  FROM goats g
  LEFT JOIN goat_shed_partitions gsp
    ON gsp.tenant_id = g.tenant_id
   AND gsp.goat_id = g.goat_id
   AND gsp.shed_id = g.shed_id
  JOIN vaccination_anchor_events vae
    ON vae.tenant_id = g.tenant_id
   AND vae.canceled_at IS NULL
   AND vae.chain_future_from_anchor
   AND vae.protocol_version_id IS NOT NULL
   AND vae.dose_code IS NOT NULL
   AND vae.source_system <> 'vaccination_plan_publish'
   AND vae.anchor_date < $3::date
   AND (
     vae.scope_type = 'tenant'
     OR (vae.scope_type = 'animal_set' AND vae.scope_payload ? 'animal_ids' AND (vae.scope_payload -> 'animal_ids') ? g.goat_id::text)
     OR (vae.scope_type = 'park' AND COALESCE(vae.scope_payload ->> 'park_id', '') = g.park_id::text)
     OR (vae.scope_type = 'shed' AND COALESCE(vae.scope_payload ->> 'shed_id', '') = g.shed_id::text)
     OR (vae.scope_type = 'partition' AND COALESCE(vae.scope_payload ->> 'shed_id', '') = g.shed_id::text AND COALESCE(vae.scope_payload ->> 'partition_label', '') = COALESCE(gsp.partition_label, 'whole'))
   )
  JOIN protocol_versions anchor_pv
    ON anchor_pv.tenant_id = g.tenant_id
   AND anchor_pv.protocol_version_id = vae.protocol_version_id
  JOIN protocol_rules anchor_pr
    ON anchor_pr.tenant_id = anchor_pv.tenant_id
   AND anchor_pr.protocol_version_id = anchor_pv.protocol_version_id
   AND lower(btrim(anchor_pr.dose_code)) = lower(btrim(vae.dose_code))
   AND lower(btrim(COALESCE(NULLIF(anchor_pr.eligibility_json -> 'vaccine' ->> 'code', ''), NULLIF(anchor_pv.rule_dsl -> 'vaccine' ->> 'code', '')))) = lower(btrim(vae.vaccine_code))
  JOIN protocol_rule_lineage anchor_lineage
    ON anchor_lineage.tenant_id = anchor_pr.tenant_id
   AND anchor_lineage.protocol_version_id = anchor_pr.protocol_version_id
   AND anchor_lineage.rule_id = anchor_pr.rule_id
  JOIN protocol_versions pv
    ON pv.tenant_id = g.tenant_id
   AND pv.protocol_id = anchor_pv.protocol_id
   AND pv.status = 'published'
   AND pv.effective_from <= ($3::timestamptz AT TIME ZONE 'Asia/Kolkata')::date
   AND (pv.effective_to IS NULL OR pv.effective_to > ($3::timestamptz AT TIME ZONE 'Asia/Kolkata')::date)
   AND (pv.scope_type = 'tenant' OR (pv.scope_type = 'park' AND pv.scope_id = g.park_id))
  JOIN protocol_rule_lineage current_lineage
    ON current_lineage.tenant_id = pv.tenant_id
   AND current_lineage.protocol_version_id = pv.protocol_version_id
   AND current_lineage.identity_key = anchor_lineage.identity_key
  JOIN protocol_rules pr
    ON pr.tenant_id = current_lineage.tenant_id
   AND pr.protocol_version_id = current_lineage.protocol_version_id
   AND pr.rule_id = current_lineage.rule_id
   AND lower(btrim(pr.dose_code)) = lower(btrim(vae.dose_code))
   AND lower(btrim(COALESCE(NULLIF(pr.eligibility_json -> 'vaccine' ->> 'code', ''), NULLIF(pv.rule_dsl -> 'vaccine' ->> 'code', '')))) = lower(btrim(vae.vaccine_code))
  WHERE g.tenant_id = $1
    AND g.goat_id = ANY($2::uuid[])
    AND (
      NOT vae.enforce_age_eligibility
      OR pr.trigger_type <> 'birth_age'
      OR (g.dob IS NOT NULL AND g.dob + pr.offset_days <= vae.anchor_date)
    )
  ORDER BY g.goat_id, vae.vaccination_anchor_event_id,
           CASE WHEN pv.scope_type = 'park' THEN 0 ELSE 1 END,
           pv.effective_from DESC,
           pv.protocol_version_id
)
SELECT goat_id, administered_at, vaccine_code, vaccine_type, pathogen_class, dose_code, sequence, protocol_version_id, protocol_id
FROM (
  SELECT * FROM completion_admins
  UNION ALL
  SELECT * FROM trusted_admins
  UNION ALL
  SELECT * FROM prearrival_admins
  UNION ALL
  SELECT * FROM anchor_admins
) admins
ORDER BY goat_id, administered_at DESC`, tenant, uuids, pgconv.Timestamptz(before))
	if err != nil {
		return nil, fmt.Errorf("vaccination: recent vaccine administrations: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var goatID string
		var admin domain.RecentVaccineAdministration
		var administeredAt pgtype.Timestamptz
		if err := rows.Scan(
			&goatID,
			&administeredAt,
			&admin.VaccineCode,
			&admin.VaccineType,
			&admin.PathogenClass,
			&admin.DoseCode,
			&admin.Sequence,
			&admin.ProtocolVersionID,
			&admin.ProtocolID,
		); err != nil {
			return nil, fmt.Errorf("vaccination: scan recent vaccine administration: %w", err)
		}
		admin.AdministeredAt = administeredAt.Time
		out[goatID] = append(out[goatID], admin)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("vaccination: recent vaccine administrations rows: %w", err)
	}
	return out, nil
}

// LastRecentVaccineAdministrationsForGoats preserves the older port for callers that need one row.
// Cross-vaccine generation now uses RecentVaccineAdministrationsForGoats.
func (r *Repository) LastRecentVaccineAdministrationsForGoats(ctx context.Context, tenantID string, goatIDs []string, before time.Time) (map[string]domain.RecentVaccineAdministration, error) {
	out := make(map[string]domain.RecentVaccineAdministration)
	history, err := r.RecentVaccineAdministrationsForGoats(ctx, tenantID, goatIDs, before)
	if err != nil {
		return nil, err
	}
	for goatID, admins := range history {
		if len(admins) > 0 {
			out[goatID] = admins[0]
		}
	}
	return out, nil
}
