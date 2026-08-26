// Package postgres persists toxin test rounds: the 7-step aflatoxin flow per purchased
// feed load, with per-step video evidence, server-clock wait gates, and the CEO/CXO-only
// verdict. A cancelled round (Invalid strip, rejected review) mints its retest round in
// the SAME transaction, so a load always has exactly one live round.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/audit"
	"github.com/vgoats/goatos/backend/internal/toxin/domain"
	"github.com/vgoats/goatos/backend/internal/toxin/ports"
)

const (
	idemScopeToxinStep    = "toxin.step.complete"
	idemScopeToxinSubmit  = "toxin.test.submit"
	idemScopeToxinVerdict = "toxin.test.verdict"
)

// Repository is the toxin module's Postgres adapter.
type Repository struct {
	pool    *pgxpool.Pool
	timeout time.Duration
}

// NewRepository wires the adapter over the shared pool.
func NewRepository(pool *pgxpool.Pool, timeout time.Duration) *Repository {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	return &Repository{pool: pool, timeout: timeout}
}

var _ ports.Repository = (*Repository)(nil)

// querier is the subset of pgx both the pool and a transaction satisfy, so reads run
// inside or outside a transaction with one implementation.
type querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// taskColumns is the single projection every task read uses. Column order here and in
// scanTask must move together.
const taskColumns = `
	t.task_id, t.tenant_id, t.feed_purchase_id, t.round_no,
	COALESCE(t.retest_of_task_id::text, ''), t.origin,
	t.farm_label, t.feed_item_key, t.feed_item_label, t.vendor, t.batch_no,
	t.purchase_date, t.quantity_kg,
	t.status, COALESCE(t.outcome, ''), t.strip_photo_ref,
	COALESCE(t.submitted_by::text, ''), t.submitted_at,
	COALESCE(t.reviewed_by::text, ''), t.reviewed_at,
	t.review_reason, t.cancel_reason, COALESCE(t.superseded_by_task_id::text, ''),
	t.row_version, t.created_at`

// scanTask reads one row of taskColumns, in that exact order.
func scanTask(row pgx.Row) (domain.Task, error) {
	var (
		t                       domain.Task
		purchaseDate            time.Time
		submittedAt, reviewedAt *time.Time
		createdAt               time.Time
	)
	err := row.Scan(
		&t.TaskID, &t.TenantID, &t.FeedPurchaseID, &t.RoundNo,
		&t.RetestOfTaskID, &t.Origin,
		&t.FarmLabel, &t.FeedItemKey, &t.FeedItemLabel, &t.Vendor, &t.BatchNo,
		&purchaseDate, &t.QuantityKg,
		&t.Status, &t.Outcome, &t.StripPhotoRef,
		&t.SubmittedBy, &submittedAt,
		&t.ReviewedBy, &reviewedAt,
		&t.ReviewReason, &t.CancelReason, &t.SupersededByTaskID,
		&t.RowVersion, &createdAt,
	)
	if err != nil {
		return domain.Task{}, err
	}
	// The purchase date is a business DATE: rendered as its calendar day, never shifted
	// through a timezone conversion.
	t.PurchaseDate = purchaseDate.Format("2006-01-02")
	if submittedAt != nil {
		t.SubmittedAt = submittedAt.UTC().Format(time.RFC3339)
	}
	if reviewedAt != nil {
		t.ReviewedAt = reviewedAt.UTC().Format(time.RFC3339)
	}
	t.CreatedAt = createdAt.UTC().Format(time.RFC3339Nano)
	return t, nil
}

// CreateTaskFromPurchase materializes the purchased load's round-1 task. Idempotent on
// the open-round partial unique index: an event replay inserts nothing.
func (r *Repository) CreateTaskFromPurchase(ctx context.Context, p ports.CreateTaskParams) error {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("toxin: begin create task: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var taskID string
	err = tx.QueryRow(ctx, `
INSERT INTO public.toxin_test_tasks (
  tenant_id, feed_purchase_id, round_no, origin,
  farm_label, feed_item_key, feed_item_label, vendor, batch_no, purchase_date, quantity_kg
) VALUES (
  $1::uuid, $2::uuid, 1, 'purchase',
  $3, $4, $5, $6, $7, $8::date, $9
)
ON CONFLICT (tenant_id, feed_purchase_id, round_no) DO NOTHING
RETURNING task_id::text`,
		p.TenantID, p.FeedPurchaseID,
		p.FarmLabel, p.FeedItemKey, p.FeedItemLabel, p.Vendor, p.BatchNo, p.PurchaseDate, p.QuantityKg,
	).Scan(&taskID)
	if errors.Is(err, pgx.ErrNoRows) {
		// Replay: the load's round-1 task already exists.
		return tx.Commit(ctx)
	}
	if err != nil {
		return fmt.Errorf("toxin: create task from purchase: %w", err)
	}

	if err := audit.NewTxRecorder(tx).Record(ctx, audit.Event{
		TenantID:     p.TenantID,
		ActorID:      "",
		ActorType:    "system",
		Action:       "toxin.test.created",
		ResourceType: "toxin_test_task",
		ResourceID:   taskID,
		Metadata: map[string]any{
			"domain":           "toxin",
			"module":           "toxin",
			"feed_purchase_id": p.FeedPurchaseID,
			"round_no":         1,
			"farm":             p.FarmLabel,
			"feed_item":        p.FeedItemLabel,
			"batch_no":         p.BatchNo,
			"purchase_date":    p.PurchaseDate,
			"source_event_id":  p.SourceEventID,
		},
	}); err != nil {
		return fmt.Errorf("toxin: audit task creation: %w", err)
	}
	return tx.Commit(ctx)
}

// ListTasks pages the tenant's rounds by keyset, newest first, and batch-loads each page
// row's step completions in ONE query.
//
// projection-review: membership=toxin_test_tasks at its (tenant_id, feed_purchase_id,
// round_no) natural key — one row per testing round; group_key=status for the
// whole-tenant counts, which range over the SAME tenant predicate as the page rows;
// join_cardinality=toxin_test_step_completions is at most 6 rows per task (PK tenant,
// task, step_no), fetched as a second bounded query keyed by the page's task ids, never
// multiplied into the page query; pagination=keyset on (created_at DESC, task_id DESC)
// with counts computed whole-tenant, never page-local; scope=tenant_id on every branch.
func (r *Repository) ListTasks(ctx context.Context, p ports.ListTasksParams) (ports.TaskPage, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	args := []any{p.TenantID}
	where := "t.tenant_id = $1"
	if len(p.Statuses) > 0 {
		args = append(args, p.Statuses)
		where += fmt.Sprintf(" AND t.status = ANY($%d)", len(args))
	}
	if p.Cursor != "" {
		createdAt, taskID, err := decodeCursor(p.Cursor)
		if err != nil {
			return ports.TaskPage{}, err
		}
		args = append(args, createdAt, taskID)
		where += fmt.Sprintf(" AND (t.created_at, t.task_id) < ($%d::timestamptz, $%d::uuid)", len(args)-1, len(args))
	}
	limit := p.Limit
	if limit <= 0 {
		limit = 20
	}

	query := fmt.Sprintf(`SELECT %s FROM public.toxin_test_tasks t
WHERE %s
ORDER BY t.created_at DESC, t.task_id DESC
LIMIT %d`, taskColumns, where, limit+1)

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return ports.TaskPage{}, fmt.Errorf("toxin: list tasks: %w", err)
	}
	defer rows.Close()

	tasks := make([]domain.Task, 0, limit)
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return ports.TaskPage{}, fmt.Errorf("toxin: list tasks scan: %w", err)
		}
		tasks = append(tasks, t)
	}
	if err := rows.Err(); err != nil {
		return ports.TaskPage{}, fmt.Errorf("toxin: list tasks rows: %w", err)
	}

	page := ports.TaskPage{StatusCounts: map[string]int{}}
	if len(tasks) > limit {
		last := tasks[limit-1]
		page.NextCursor = encodeCursor(last.CreatedAt, last.TaskID)
		tasks = tasks[:limit]
	}

	// Batch-load the page's completions: one query, bounded by page size x 6.
	ids := make([]string, 0, len(tasks))
	for _, t := range tasks {
		ids = append(ids, t.TaskID)
	}
	completionsByTask, err := r.completionsByTaskIDs(ctx, r.pool, p.TenantID, ids)
	if err != nil {
		return ports.TaskPage{}, err
	}
	page.Rows = make([]ports.TaskRow, 0, len(tasks))
	for _, t := range tasks {
		page.Rows = append(page.Rows, ports.TaskRow{Task: t, Completions: completionsByTask[t.TaskID]})
	}

	countRows, err := r.pool.Query(ctx, `
SELECT status, count(*) FROM public.toxin_test_tasks WHERE tenant_id = $1 GROUP BY status`, p.TenantID)
	if err != nil {
		return ports.TaskPage{}, fmt.Errorf("toxin: status counts: %w", err)
	}
	defer countRows.Close()
	for countRows.Next() {
		var status string
		var n int
		if err := countRows.Scan(&status, &n); err != nil {
			return ports.TaskPage{}, fmt.Errorf("toxin: status counts scan: %w", err)
		}
		page.StatusCounts[status] = n
	}
	if err := countRows.Err(); err != nil {
		return ports.TaskPage{}, fmt.Errorf("toxin: status counts rows: %w", err)
	}
	return page, nil
}

// completionsByTaskIDs loads step completions for a bounded id set.
func (r *Repository) completionsByTaskIDs(ctx context.Context, q querier, tenantID string, taskIDs []string) (map[string][]domain.StepCompletion, error) {
	out := make(map[string][]domain.StepCompletion, len(taskIDs))
	if len(taskIDs) == 0 {
		return out, nil
	}
	// The person is resolved to a NAME here, in the SAME bounded query (one LEFT JOIN, never a
	// per-row lookup). A completion whose person does not resolve returns '' so the screen omits
	// the attribution — an operator is never shown a raw user id.
	rows, err := q.Query(ctx, `
SELECT c.task_id::text,
       c.step_no,
       c.proof_ref,
       COALESCE(NULLIF(BTRIM(COALESCE(m.display_name, BTRIM(COALESCE(m.first_name, '') || ' ' || COALESCE(m.last_name, '')))), ''), '') AS completed_by_name,
       c.completed_at
FROM public.toxin_test_step_completions c
LEFT JOIN public.workforce_members m
       ON m.tenant_id = c.tenant_id
      AND m.user_id = c.completed_by
WHERE c.tenant_id = $1 AND c.task_id = ANY($2::uuid[])
ORDER BY c.task_id, c.step_no`, tenantID, taskIDs)
	if err != nil {
		return nil, fmt.Errorf("toxin: list step completions: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var taskID string
		var c domain.StepCompletion
		if err := rows.Scan(&taskID, &c.StepNo, &c.ProofRef, &c.CompletedBy, &c.CompletedAt); err != nil {
			return nil, fmt.Errorf("toxin: step completions scan: %w", err)
		}
		c.CompletedAt = c.CompletedAt.UTC()
		out[taskID] = append(out[taskID], c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("toxin: step completions rows: %w", err)
	}
	return out, nil
}

// encodeCursor/decodeCursor carry the keyset position as "<created_at>|<task_id>".
func encodeCursor(createdAt, taskID string) string { return createdAt + "|" + taskID }

func decodeCursor(cursor string) (string, string, error) {
	parts := strings.SplitN(cursor, "|", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("toxin: bad cursor")
	}
	if _, err := time.Parse(time.RFC3339Nano, parts[0]); err != nil {
		return "", "", fmt.Errorf("toxin: bad cursor: %w", err)
	}
	return parts[0], parts[1], nil
}

// GetTask reads one round with its step completions.
func (r *Repository) GetTask(ctx context.Context, tenantID, taskID string) (ports.TaskRow, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	return r.getRow(ctx, r.pool, tenantID, taskID)
}

func (r *Repository) getRow(ctx context.Context, q querier, tenantID, taskID string) (ports.TaskRow, error) {
	query := fmt.Sprintf(`SELECT %s FROM public.toxin_test_tasks t WHERE t.tenant_id = $1 AND t.task_id = $2`, taskColumns)
	t, err := scanTask(q.QueryRow(ctx, query, tenantID, taskID))
	if errors.Is(err, pgx.ErrNoRows) {
		return ports.TaskRow{}, ports.ErrTaskNotFound
	}
	if err != nil {
		return ports.TaskRow{}, fmt.Errorf("toxin: get task: %w", err)
	}
	byTask, err := r.completionsByTaskIDs(ctx, q, tenantID, []string{taskID})
	if err != nil {
		return ports.TaskRow{}, err
	}
	return ports.TaskRow{Task: t, Completions: byTask[taskID]}, nil
}

// lockTask reads the round FOR UPDATE inside tx.
func (r *Repository) lockTask(ctx context.Context, tx pgx.Tx, tenantID, taskID string) (domain.Task, error) {
	query := fmt.Sprintf(`SELECT %s FROM public.toxin_test_tasks t
WHERE t.tenant_id = $1 AND t.task_id = $2
FOR UPDATE`, taskColumns)
	t, err := scanTask(tx.QueryRow(ctx, query, tenantID, taskID))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Task{}, ports.ErrTaskNotFound
	}
	if err != nil {
		return domain.Task{}, fmt.Errorf("toxin: lock task: %w", err)
	}
	return t, nil
}

// CompleteStep records one working step's video under the row lock, enforcing order and
// the server-clock wait gates.
func (r *Repository) CompleteStep(ctx context.Context, p ports.CompleteStepParams) (ports.TaskRow, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return ports.TaskRow{}, fmt.Errorf("toxin: begin step: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	fingerprint := requestFingerprint(p.TaskID, fmt.Sprintf("%d", p.StepNo), p.ProofRef)
	reservation, err := reserveIdempotency(ctx, tx, p.TenantID, idemScopeToxinStep, p.IdempotencyKey, fingerprint)
	if err != nil {
		return ports.TaskRow{}, err
	}
	if !reservation.proceed {
		if err := tx.Commit(ctx); err != nil {
			return ports.TaskRow{}, fmt.Errorf("toxin: commit step replay read: %w", err)
		}
		return r.getRow(ctx, r.pool, p.TenantID, p.TaskID)
	}

	task, err := r.lockTask(ctx, tx, p.TenantID, p.TaskID)
	if err != nil {
		return ports.TaskRow{}, err
	}
	byTask, err := r.completionsByTaskIDs(ctx, tx, p.TenantID, []string{p.TaskID})
	if err != nil {
		return ports.TaskRow{}, err
	}
	completions := byTask[p.TaskID]

	if opensAt, err := domain.CheckStepCompletable(task.Status, p.StepNo, completions, p.Now); err != nil {
		if errors.Is(err, domain.ErrWaitNotElapsed) {
			return ports.TaskRow{}, domain.WaitNotElapsed{OpensAt: opensAt}
		}
		return ports.TaskRow{}, err
	}

	if _, err := tx.Exec(ctx, `
INSERT INTO public.toxin_test_step_completions (tenant_id, task_id, step_no, proof_ref, completed_by)
VALUES ($1::uuid, $2::uuid, $3, $4, nullif($5, '')::uuid)`,
		p.TenantID, p.TaskID, p.StepNo, p.ProofRef, p.ActorID); err != nil {
		if isProofReuse(err) {
			return ports.TaskRow{}, domain.ErrProofAlreadyUsed
		}
		return ports.TaskRow{}, fmt.Errorf("toxin: insert step completion: %w", err)
	}
	if _, err := tx.Exec(ctx, `
UPDATE public.toxin_test_tasks
SET row_version = row_version + 1, updated_at = now()
WHERE tenant_id = $1 AND task_id = $2`, p.TenantID, p.TaskID); err != nil {
		return ports.TaskRow{}, fmt.Errorf("toxin: bump task version: %w", err)
	}

	if err := audit.NewTxRecorder(tx).Record(ctx, audit.Event{
		TenantID:     p.TenantID,
		ActorID:      p.ActorID,
		ActorType:    "human",
		Action:       "toxin.step.completed",
		ResourceType: "toxin_test_task",
		ResourceID:   p.TaskID,
		Metadata: map[string]any{
			"domain":          "toxin",
			"module":          "toxin",
			"step_no":         p.StepNo,
			"proof_ref":       p.ProofRef,
			"idempotency_key": p.IdempotencyKey,
			"operation_id":    p.IdempotencyKey,
		},
	}); err != nil {
		return ports.TaskRow{}, fmt.Errorf("toxin: audit step: %w", err)
	}
	if err := completeIdempotency(ctx, tx, p.TenantID, idemScopeToxinStep, p.IdempotencyKey, "toxin_test_task", p.TaskID); err != nil {
		return ports.TaskRow{}, fmt.Errorf("toxin: complete step idempotency: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return ports.TaskRow{}, fmt.Errorf("toxin: commit step: %w", err)
	}
	return r.getRow(ctx, r.pool, p.TenantID, p.TaskID)
}

// mintRetestInTx cancels nothing itself — the caller has already flipped the old round —
// and inserts the fresh round for the same load, linking the lineage both ways.
func mintRetestInTx(ctx context.Context, tx pgx.Tx, old domain.Task, origin string) (string, error) {
	var newTaskID string
	if err := tx.QueryRow(ctx, `
INSERT INTO public.toxin_test_tasks (
  tenant_id, feed_purchase_id, round_no, retest_of_task_id, origin,
  farm_label, feed_item_key, feed_item_label, vendor, batch_no, purchase_date, quantity_kg
) VALUES (
  $1::uuid, $2::uuid, $3, $4::uuid, $5,
  $6, $7, $8, $9, $10, $11::date, $12
)
RETURNING task_id::text`,
		old.TenantID, old.FeedPurchaseID, old.RoundNo+1, old.TaskID, origin,
		old.FarmLabel, old.FeedItemKey, old.FeedItemLabel, old.Vendor, old.BatchNo, old.PurchaseDate, old.QuantityKg,
	).Scan(&newTaskID); err != nil {
		return "", fmt.Errorf("toxin: mint retest round: %w", err)
	}
	if _, err := tx.Exec(ctx, `
UPDATE public.toxin_test_tasks
SET superseded_by_task_id = $3::uuid, updated_at = now()
WHERE tenant_id = $1 AND task_id = $2`, old.TenantID, old.TaskID, newTaskID); err != nil {
		return "", fmt.Errorf("toxin: link retest round: %w", err)
	}
	return newTaskID, nil
}

// SubmitReading records step 7: the strip photo plus the reading. An Invalid reading
// cancels the round and mints its retest in the same transaction.
func (r *Repository) SubmitReading(ctx context.Context, p ports.SubmitParams) (ports.TaskRow, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return ports.TaskRow{}, fmt.Errorf("toxin: begin submit: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	fingerprint := requestFingerprint(p.TaskID, p.Outcome, p.StripPhotoRef)
	reservation, err := reserveIdempotency(ctx, tx, p.TenantID, idemScopeToxinSubmit, p.IdempotencyKey, fingerprint)
	if err != nil {
		return ports.TaskRow{}, err
	}
	if !reservation.proceed {
		if err := tx.Commit(ctx); err != nil {
			return ports.TaskRow{}, fmt.Errorf("toxin: commit submit replay read: %w", err)
		}
		return r.getRow(ctx, r.pool, p.TenantID, p.TaskID)
	}

	task, err := r.lockTask(ctx, tx, p.TenantID, p.TaskID)
	if err != nil {
		return ports.TaskRow{}, err
	}
	byTask, err := r.completionsByTaskIDs(ctx, tx, p.TenantID, []string{p.TaskID})
	if err != nil {
		return ports.TaskRow{}, err
	}
	if opensAt, err := domain.CheckStepCompletable(task.Status, domain.FinalStepNo, byTask[p.TaskID], p.Now); err != nil {
		if errors.Is(err, domain.ErrWaitNotElapsed) {
			return ports.TaskRow{}, domain.WaitNotElapsed{OpensAt: opensAt}
		}
		return ports.TaskRow{}, err
	}
	decision, err := domain.SubmitDecision(p.Outcome)
	if err != nil {
		return ports.TaskRow{}, err
	}

	if _, err := tx.Exec(ctx, `
INSERT INTO public.toxin_test_step_completions (tenant_id, task_id, step_no, proof_ref, completed_by)
VALUES ($1::uuid, $2::uuid, $3, $4, nullif($5, '')::uuid)`,
		p.TenantID, p.TaskID, domain.FinalStepNo, p.StripPhotoRef, p.ActorID); err != nil {
		if isProofReuse(err) {
			return ports.TaskRow{}, domain.ErrProofAlreadyUsed
		}
		return ports.TaskRow{}, fmt.Errorf("toxin: insert reading completion: %w", err)
	}
	if _, err := tx.Exec(ctx, `
UPDATE public.toxin_test_tasks
SET status = $3, outcome = $4, strip_photo_ref = $5,
    submitted_by = nullif($6, '')::uuid, submitted_at = now(),
    cancel_reason = $7,
    row_version = row_version + 1, updated_at = now()
WHERE tenant_id = $1 AND task_id = $2`,
		p.TenantID, p.TaskID, decision.NextStatus, p.Outcome, p.StripPhotoRef, p.ActorID, decision.CancelReason); err != nil {
		return ports.TaskRow{}, fmt.Errorf("toxin: apply reading: %w", err)
	}

	retestTaskID := ""
	if decision.CreatesRetest {
		retestTaskID, err = mintRetestInTx(ctx, tx, task, decision.RetestOrigin)
		if err != nil {
			return ports.TaskRow{}, err
		}
	}

	if err := audit.NewTxRecorder(tx).Record(ctx, audit.Event{
		TenantID:     p.TenantID,
		ActorID:      p.ActorID,
		ActorType:    "human",
		Action:       "toxin.test.submitted",
		ResourceType: "toxin_test_task",
		ResourceID:   p.TaskID,
		Metadata: map[string]any{
			"domain":          "toxin",
			"module":          "toxin",
			"outcome":         p.Outcome,
			"next_status":     decision.NextStatus,
			"strip_photo_ref": p.StripPhotoRef,
			"retest_task_id":  retestTaskID,
			"idempotency_key": p.IdempotencyKey,
			"operation_id":    p.IdempotencyKey,
		},
	}); err != nil {
		return ports.TaskRow{}, fmt.Errorf("toxin: audit submit: %w", err)
	}
	if err := completeIdempotency(ctx, tx, p.TenantID, idemScopeToxinSubmit, p.IdempotencyKey, "toxin_test_task", p.TaskID); err != nil {
		return ports.TaskRow{}, fmt.Errorf("toxin: complete submit idempotency: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return ports.TaskRow{}, fmt.Errorf("toxin: commit submit: %w", err)
	}
	return r.getRow(ctx, r.pool, p.TenantID, p.TaskID)
}

// RecordVerdict applies the CEO/CXO accept/reject, fenced on the row_version the
// reviewer had on screen. A reject cancels the round and mints its retest in the same
// transaction.
func (r *Repository) RecordVerdict(ctx context.Context, p ports.VerdictParams) (ports.TaskRow, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return ports.TaskRow{}, fmt.Errorf("toxin: begin verdict: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	fingerprint := requestFingerprint(p.TaskID, p.Decision, p.Reason, fmt.Sprintf("%d", p.RowVersion))
	reservation, err := reserveIdempotency(ctx, tx, p.TenantID, idemScopeToxinVerdict, p.IdempotencyKey, fingerprint)
	if err != nil {
		return ports.TaskRow{}, err
	}
	if !reservation.proceed {
		if err := tx.Commit(ctx); err != nil {
			return ports.TaskRow{}, fmt.Errorf("toxin: commit verdict replay read: %w", err)
		}
		return r.getRow(ctx, r.pool, p.TenantID, p.TaskID)
	}

	task, err := r.lockTask(ctx, tx, p.TenantID, p.TaskID)
	if err != nil {
		return ports.TaskRow{}, err
	}
	decision, err := domain.VerdictDecision(task.Status, p.Decision, p.Reason)
	if err != nil {
		return ports.TaskRow{}, err
	}
	if p.RowVersion != 0 && p.RowVersion != task.RowVersion {
		return ports.TaskRow{}, ports.ErrVersionConflict
	}

	reviewReason := ""
	if p.Decision == domain.VerdictReject {
		reviewReason = p.Reason
	}
	if _, err := tx.Exec(ctx, `
UPDATE public.toxin_test_tasks
SET status = $3, reviewed_by = nullif($4, '')::uuid, reviewed_at = now(),
    review_reason = $5, cancel_reason = $6,
    row_version = row_version + 1, updated_at = now()
WHERE tenant_id = $1 AND task_id = $2`,
		p.TenantID, p.TaskID, decision.NextStatus, p.ActorID, reviewReason, decision.CancelReason); err != nil {
		return ports.TaskRow{}, fmt.Errorf("toxin: apply verdict: %w", err)
	}

	retestTaskID := ""
	if decision.CreatesRetest {
		retestTaskID, err = mintRetestInTx(ctx, tx, task, decision.RetestOrigin)
		if err != nil {
			return ports.TaskRow{}, err
		}
	}

	if err := audit.NewTxRecorder(tx).Record(ctx, audit.Event{
		TenantID:     p.TenantID,
		ActorID:      p.ActorID,
		ActorType:    "human",
		Action:       "toxin.test.verdict",
		ResourceType: "toxin_test_task",
		ResourceID:   p.TaskID,
		Metadata: map[string]any{
			"domain":          "toxin",
			"module":          "toxin",
			"decision":        p.Decision,
			"reason":          reviewReason,
			"next_status":     decision.NextStatus,
			"retest_task_id":  retestTaskID,
			"idempotency_key": p.IdempotencyKey,
			"operation_id":    p.IdempotencyKey,
		},
	}); err != nil {
		return ports.TaskRow{}, fmt.Errorf("toxin: audit verdict: %w", err)
	}
	if err := completeIdempotency(ctx, tx, p.TenantID, idemScopeToxinVerdict, p.IdempotencyKey, "toxin_test_task", p.TaskID); err != nil {
		return ports.TaskRow{}, fmt.Errorf("toxin: complete verdict idempotency: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return ports.TaskRow{}, fmt.Errorf("toxin: commit verdict: %w", err)
	}
	return r.getRow(ctx, r.pool, p.TenantID, p.TaskID)
}

// toxinProofReuseConstraint is the unique index a repeated capture violates. Matched by NAME
// rather than by SQLSTATE 23505 alone: the completions table also carries the (task, step)
// primary key, and reporting "already used elsewhere" for a duplicate STEP would send a tester
// hunting for a reuse that did not happen.
const toxinProofReuseConstraint = "toxin_test_step_completions_proof_uq"

// isProofReuse reports whether err is the one-capture-one-step violation.
func isProofReuse(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.ConstraintName == toxinProofReuseConstraint
}
