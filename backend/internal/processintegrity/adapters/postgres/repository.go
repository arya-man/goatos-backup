// Package postgres implements process-integrity projections over Postgres.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/processintegrity/domain"
	"github.com/vgoats/goatos/backend/internal/processintegrity/ports"
)

const (
	defaultQueryTimeout      = 3 * time.Second
	defaultClosedHistoryAge  = 14 * 24 * time.Hour
	defaultProjectionFresh   = 5 * time.Minute
	defaultLimit             = 100
	maxLimit                 = 500
	countQueryArgCount       = 15
	rowsQueryArgCount        = 19
	projectionPruneBatchSize = 5000
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

// ListRows serves the Action Center / adherence / drilldown read model exclusively from the bounded,
// incrementally maintained process-integrity projection (indexed keyset lookup). When the tenant has no
// serving projection version, it returns domain.ErrProjectionUnavailable rather than replaying the
// canonical obligation/SOP/proof/completion history through a god-CTE — that compute-on-read fallback is
// the slowest possible path at 1-5M animals and is exactly what C35-002 removes.
func (r *Repository) ListRows(ctx context.Context, q domain.Query) (domain.ListResult, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	q = normalizeQuery(q)
	args := queryArgs(q)
	projection, err := r.servingProjection(ctx, q)
	if err != nil {
		slog.Default().WarnContext(ctx,
			"processintegrity: read model projection cannot serve current read; refusing canonical compute-on-read fallback",
			"tenant_id", q.TenantID, "read_path", "list_rows")
		return domain.ListResult{}, err
	}
	return r.listRowsProjected(ctx, q, args, projection)
}

func (r *Repository) CountByWorkState(ctx context.Context, q domain.Query) ([]domain.CountByWorkState, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	q = normalizeQuery(q)
	args := queryArgs(q)
	projection, err := r.servingProjection(ctx, q)
	if err != nil {
		slog.Default().WarnContext(ctx,
			"processintegrity: read model projection cannot serve current read; refusing canonical compute-on-read fallback",
			"tenant_id", q.TenantID, "read_path", "count_by_work_state")
		return nil, err
	}
	counts, _, err := r.countByWorkStateProjected(ctx, countQueryArgs(args))
	if err != nil {
		return nil, err
	}
	_ = projection
	return counts, nil
}

func (r *Repository) listRowsProjected(ctx context.Context, q domain.Query, args []any, projection domain.ProjectionMetadata) (domain.ListResult, error) {
	rows, err := r.pool.Query(ctx, processIntegrityProjectionRowsSQL, args...)
	if err != nil {
		return domain.ListResult{}, fmt.Errorf("processintegrity: list projection rows: %w", err)
	}
	defer rows.Close()

	out := []domain.Row{}
	var lastCursor *domain.Cursor
	seenExtra := false
	for rows.Next() {
		row, cursor, err := scanRow(rows)
		if err != nil {
			return domain.ListResult{}, err
		}
		if len(out) < q.Limit {
			out = append(out, row)
			lastCursor = &cursor
		} else {
			seenExtra = true
		}
	}
	if err := rows.Err(); err != nil {
		return domain.ListResult{}, fmt.Errorf("processintegrity: iterate projection rows: %w", err)
	}

	counts := []domain.CountByWorkState{}
	var totalCount int64
	summary := domain.AdherenceSummary{}
	if q.RowID != nil {
		totalCount = int64(len(out))
	} else if q.IncludeAdherenceSummary {
		summaryRows := r.pool.QueryRow(ctx, processIntegrityProjectionAdherenceSummarySQL, countQueryArgs(args)...)
		if err := summaryRows.Scan(
			&summary.ExpectedCount,
			&summary.CompletedCount,
			&summary.OpenGapCount,
			&summary.DeferredCount,
			&summary.ProcessIntactCount,
		); err != nil {
			return domain.ListResult{}, fmt.Errorf("processintegrity: projection adherence summary: %w", err)
		}
		totalCount = int64(summary.OpenGapCount + summary.ProcessIntactCount)
	} else {
		var err error
		counts, totalCount, err = r.countByWorkStateProjected(ctx, countQueryArgs(args))
		if err != nil {
			return domain.ListResult{}, err
		}
	}

	var next *string
	if seenExtra && lastCursor != nil {
		encoded, err := domain.EncodeCursor(*lastCursor)
		if err != nil {
			return domain.ListResult{}, err
		}
		next = &encoded
	}
	return domain.ListResult{Rows: out, CountsByWorkState: counts, TotalCount: totalCount, AdherenceSummary: summary, NextCursor: next, Projection: projection}, nil
}

func (r *Repository) countByWorkStateProjected(ctx context.Context, args []any) ([]domain.CountByWorkState, int64, error) {
	countRows, err := r.pool.Query(ctx, processIntegrityProjectionCountsSQL, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("processintegrity: count projection rows: %w", err)
	}
	defer countRows.Close()
	counts := []domain.CountByWorkState{}
	var totalCount int64
	for countRows.Next() {
		var state string
		var count int64
		if err := countRows.Scan(&state, &count); err != nil {
			return nil, 0, fmt.Errorf("processintegrity: scan projection count: %w", err)
		}
		counts = append(counts, domain.CountByWorkState{WorkState: domain.WorkState(state), Count: count})
		totalCount += count
	}
	if err := countRows.Err(); err != nil {
		return nil, 0, fmt.Errorf("processintegrity: iterate projection counts: %w", err)
	}
	return counts, totalCount, nil
}

// servingProjectionVersion returns the tenant's serving process-integrity projection version and whether
// one exists. Every request-path read requires a serving version; there is no canonical compute-on-read
// fallback. The projection stores completed rows and honours the IncludeCompleted ($14) / adherence
// filters, so it serves Action Center, Protocol Adherence, counts, and single-row drilldowns alike.
func (r *Repository) servingProjection(ctx context.Context, q domain.Query) (domain.ProjectionMetadata, error) {
	var meta domain.ProjectionMetadata
	err := r.pool.QueryRow(ctx, `
SELECT serving_projection_version, projected_at, as_of, freshness_status, serving_state
FROM process_integrity_projection_state
WHERE tenant_id = $1::uuid
	  AND serving_projection_version IS NOT NULL`, q.TenantID).Scan(
		&meta.ProjectionVersion, &meta.ProjectedAt, &meta.AsOf, &meta.FreshnessStatus, &meta.ServingState,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ProjectionMetadata{}, domain.ErrProjectionUnavailable
	}
	if err != nil {
		return domain.ProjectionMetadata{}, fmt.Errorf("processintegrity: read projection state: %w", err)
	}
	projectionAge := q.AsOf.Sub(meta.AsOf)
	buildAge := q.AsOf.Sub(meta.ProjectedAt)
	meta.Stale = meta.ProjectionVersion <= 0 || meta.ServingState != "fresh" || meta.FreshnessStatus != "green" ||
		projectionAge > defaultProjectionFresh || projectionAge < -time.Minute || buildAge > defaultProjectionFresh
	if meta.Stale {
		return meta, domain.ErrProjectionStale
	}
	return meta, nil
}

func (r *Repository) GetRow(ctx context.Context, q domain.Query, rowID string) (domain.Row, bool, error) {
	q.RowID = &rowID
	q.Limit = 1
	q.IncludeCompleted = true
	result, err := r.ListRows(ctx, q)
	if err != nil {
		return domain.Row{}, false, err
	}
	if len(result.Rows) == 0 {
		return domain.Row{}, false, nil
	}
	return result.Rows[0], true, nil
}

// RecomputeProjection refreshes the tenant process-integrity read model from canonical
// obligation/SOP/proof/completion/feed-exception tables. This is the projector path: it is allowed to
// replay raw source state because it runs off the request path and publishes one indexed serving table.
func (r *Repository) RecomputeProjection(ctx context.Context, req domain.ProjectionRecomputeRequest) (result domain.ProjectionRecomputeResult, retErr error) {
	if strings.TrimSpace(req.TenantID) == "" {
		return domain.ProjectionRecomputeResult{}, fmt.Errorf("processintegrity: tenant id is required")
	}
	asOf := req.AsOf
	if asOf.IsZero() {
		asOf = time.Now().In(biztime.DefaultLocation())
	}
	var projectionVersion int64
	var projectedAt time.Time
	if err := r.pool.QueryRow(ctx, `SELECT (extract(epoch FROM now()) * 1000)::bigint, now()`).Scan(&projectionVersion, &projectedAt); err != nil {
		return domain.ProjectionRecomputeResult{}, fmt.Errorf("processintegrity: projection stamp: %w", err)
	}
	if err := r.markProjectionRebuilding(ctx, req.TenantID, projectionVersion, projectedAt, asOf); err != nil {
		return domain.ProjectionRecomputeResult{}, err
	}
	committed := false
	defer func() {
		// A post-commit maintenance failure must be visible, but must not mark the
		// newly committed serving projection as failed. Readers can continue from
		// last-known-good while the scheduled projector retries cleanup.
		if retErr != nil && !committed {
			r.markProjectionFailed(req.TenantID, retErr)
		}
	}()

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return domain.ProjectionRecomputeResult{}, fmt.Errorf("processintegrity: begin projection recompute: %w", err)
	}
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()

	if _, err := tx.Exec(ctx, processIntegrityProjectionInsertSQL, projectionBuildArgs(req.TenantID, asOf, projectionVersion, projectedAt)...); err != nil {
		return domain.ProjectionRecomputeResult{}, fmt.Errorf("processintegrity: upsert projection rows: %w", err)
	}
	if _, err := tx.Exec(ctx, processIntegrityProjectionSummaryInsertSQL, req.TenantID, projectionVersion, projectedAt); err != nil {
		return domain.ProjectionRecomputeResult{}, fmt.Errorf("processintegrity: upsert projection summaries: %w", err)
	}

	counts, err := projectionCountsForTenant(ctx, tx, req.TenantID, projectionVersion)
	if err != nil {
		return domain.ProjectionRecomputeResult{}, err
	}
	var rowCount int64
	for _, count := range counts {
		rowCount += count.Count
	}

	if _, err := tx.Exec(ctx, `
INSERT INTO process_integrity_projection_state (
  tenant_id, projection_version, serving_projection_version, projected_at, as_of, row_count,
  freshness_status, serving_state, last_error, updated_at
) VALUES (
  $1::uuid, $2::bigint, $2::bigint, $3::timestamptz, $4::timestamptz, $5::bigint,
  'green', 'fresh', NULL, now()
)
ON CONFLICT (tenant_id) DO UPDATE SET
  projection_version = EXCLUDED.projection_version,
  serving_projection_version = EXCLUDED.serving_projection_version,
  projected_at = EXCLUDED.projected_at,
  as_of = EXCLUDED.as_of,
  row_count = EXCLUDED.row_count,
  freshness_status = EXCLUDED.freshness_status,
  serving_state = EXCLUDED.serving_state,
  last_error = NULL,
  updated_at = now()`,
		req.TenantID, projectionVersion, projectedAt, asOf, rowCount); err != nil {
		return domain.ProjectionRecomputeResult{}, fmt.Errorf("processintegrity: upsert projection state: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return domain.ProjectionRecomputeResult{}, fmt.Errorf("processintegrity: commit projection recompute: %w", err)
	}
	committed = true
	result = domain.ProjectionRecomputeResult{
		TenantID:           req.TenantID,
		ProjectionVersion:  projectionVersion,
		ProjectedAt:        projectedAt,
		AsOf:               asOf,
		Rows:               rowCount,
		CountsByWorkState:  counts,
		ProjectionFreshFor: defaultProjectionFresh,
	}
	if err := r.pruneOldProjectionRows(ctx, req.TenantID); err != nil {
		r.markProjectionMaintenanceError(req.TenantID, err)
		return result, fmt.Errorf("processintegrity: serving projection committed but stale-row prune failed: %w", err)
	}
	return result, nil
}

func (r *Repository) markProjectionRebuilding(ctx context.Context, tenantID string, projectionVersion int64, projectedAt, asOf time.Time) error {
	if _, err := r.pool.Exec(ctx, `
INSERT INTO process_integrity_projection_state (
  tenant_id, projection_version, serving_projection_version, projected_at, as_of, row_count,
  freshness_status, serving_state, last_error, updated_at
) VALUES (
  $1::uuid, $2::bigint, NULL, $3::timestamptz, $4::timestamptz, 0,
  'unknown', 'rebuilding', NULL, now()
)
ON CONFLICT (tenant_id) DO UPDATE SET
  freshness_status = CASE
    WHEN process_integrity_projection_state.row_count > 0 THEN 'yellow'
    ELSE 'unknown'
  END,
  serving_state = 'rebuilding',
  last_error = NULL,
  updated_at = now()`, tenantID, projectionVersion, projectedAt, asOf); err != nil {
		return fmt.Errorf("processintegrity: mark projection rebuilding: %w", err)
	}
	return nil
}

func (r *Repository) markProjectionFailed(tenantID string, cause error) {
	if strings.TrimSpace(tenantID) == "" || cause == nil {
		return
	}
	stateCtx, cancel := context.WithTimeout(context.Background(), r.timeout)
	defer cancel()
	message := cause.Error()
	if len(message) > 2000 {
		message = message[:2000]
	}
	_, _ = r.pool.Exec(stateCtx, `
UPDATE process_integrity_projection_state
SET freshness_status = 'red',
    serving_state = 'failed',
    last_error = $2::text,
    updated_at = now()
WHERE tenant_id = $1::uuid`, tenantID, message)
}

func (r *Repository) markProjectionMaintenanceError(tenantID string, cause error) {
	if strings.TrimSpace(tenantID) == "" || cause == nil {
		return
	}
	stateCtx, cancel := context.WithTimeout(context.Background(), r.timeout)
	defer cancel()
	message := "projection_maintenance: " + cause.Error()
	if len(message) > 2000 {
		message = message[:2000]
	}
	_, _ = r.pool.Exec(stateCtx, `
UPDATE process_integrity_projection_state
SET freshness_status = 'yellow',
    last_error = $2::text,
    updated_at = now()
WHERE tenant_id = $1::uuid
  AND serving_projection_version IS NOT NULL`, tenantID, message)
}

func (r *Repository) pruneOldProjectionRows(ctx context.Context, tenantID string) error {
	return r.pruneOldProjectionRowsWithBatchSize(ctx, tenantID, projectionPruneBatchSize)
}

func (r *Repository) pruneOldProjectionRowsWithBatchSize(ctx context.Context, tenantID string, batchSize int32) error {
	if strings.TrimSpace(tenantID) == "" || batchSize <= 0 {
		return fmt.Errorf("invalid prune request: tenant_id and positive batch_size are required")
	}
	const (
		maxBatchesPerCall = 1000      // prevent runaway prune loop
		maxRowsPerCall    = 1_000_000 // maximum rows to prune in one call
	)
	var totalRowsDeleted int64

	for i := 0; i < maxBatchesPerCall; i++ {
		// Check context deadline before each iteration
		if ctx.Err() != nil {
			return fmt.Errorf("prune canceled after %d rows: %w", totalRowsDeleted, ctx.Err())
		}

		// Check if we've hit the row budget
		if totalRowsDeleted >= maxRowsPerCall {
			return fmt.Errorf("prune row budget exhausted after %d rows; retry required", totalRowsDeleted)
		}

		tag, err := r.pool.Exec(ctx, `
WITH serving AS (
  SELECT serving_projection_version
  FROM process_integrity_projection_state
  WHERE tenant_id = $1::uuid
    AND serving_projection_version IS NOT NULL
),
doomed AS (
  SELECT rows.process_integrity_projection_row_id
  FROM process_integrity_projection_rows rows
  JOIN serving ON true
  WHERE rows.tenant_id = $1::uuid
    AND rows.projection_version <> serving.serving_projection_version
  ORDER BY rows.projection_version, rows.process_integrity_projection_row_id
  LIMIT $2::int
)
DELETE FROM process_integrity_projection_rows rows
USING doomed
WHERE rows.process_integrity_projection_row_id = doomed.process_integrity_projection_row_id`,
			tenantID, batchSize)
		if err != nil {
			return fmt.Errorf("delete stale projection batch after %d rows: %w", totalRowsDeleted, err)
		}
		rowsAffected := tag.RowsAffected()
		totalRowsDeleted += rowsAffected

		// If the last batch was smaller than requested, all old rows are deleted
		if rowsAffected < int64(batchSize) {
			if _, err := r.pool.Exec(ctx, `
DELETE FROM process_integrity_projection_summaries summaries
USING process_integrity_projection_state state
WHERE summaries.tenant_id = $1::uuid
  AND state.tenant_id = summaries.tenant_id
  AND state.serving_projection_version IS NOT NULL
  AND summaries.projection_version <> state.serving_projection_version`, tenantID); err != nil {
				return fmt.Errorf("delete stale projection summaries: %w", err)
			}
			return nil
		}
	}
	return fmt.Errorf("prune batch budget exhausted after %d rows; retry required", totalRowsDeleted)
}

func projectionBuildArgs(tenantID string, asOf time.Time, projectionVersion int64, projectedAt time.Time) []any {
	q := normalizeQuery(domain.Query{
		TenantID:         tenantID,
		AsOf:             asOf,
		DueBefore:        time.Date(9999, 12, 31, 23, 59, 59, 0, time.UTC),
		IncludeCompleted: false,
		Limit:            maxLimit,
	})
	args := queryArgs(q)
	return append(args, projectionVersion, pgtype.Timestamptz{Time: projectedAt, Valid: true})
}

func projectionCountsForTenant(ctx context.Context, q interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}, tenantID string, projectionVersion int64) ([]domain.CountByWorkState, error) {
	rows, err := q.Query(ctx, `
SELECT work_state, COUNT(*)::bigint
FROM process_integrity_projection_summaries
WHERE tenant_id = $1::uuid
  AND projection_version = $2::bigint
  AND owner_id = ''
GROUP BY work_state
ORDER BY work_state`, tenantID, projectionVersion)
	if err != nil {
		return nil, fmt.Errorf("processintegrity: projection count summary: %w", err)
	}
	defer rows.Close()
	counts := []domain.CountByWorkState{}
	for rows.Next() {
		var state string
		var count int64
		if err := rows.Scan(&state, &count); err != nil {
			return nil, fmt.Errorf("processintegrity: scan projection count summary: %w", err)
		}
		counts = append(counts, domain.CountByWorkState{WorkState: domain.WorkState(state), Count: count})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("processintegrity: iterate projection count summary: %w", err)
	}
	return counts, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanRow(rows rowScanner) (domain.Row, domain.Cursor, error) {
	var row domain.Row
	var batchID, taskID, submissionID, completionID, cohortID, goatID, driveName, sopVersionID pgtype.Text
	var taskRowVersion pgtype.Int4
	var windowStart, windowEnd, latestEvidenceAt pgtype.Timestamptz
	var batchStatus, submissionState, completionState, blockerReason, latestRejection, auditRef pgtype.Text
	var operatorID, operatorName, parkHeadID, parkHeadName, verifierID, verifierName, escalationOwnerID, escalationOwnerName pgtype.Text
	var dueAt pgtype.Timestamptz
	var proofIDsCSV string
	var sortPriority int
	var sopState, proofState, verificationState, ownerState, workState, severity string
	if err := rows.Scan(
		&sortPriority,
		&row.RowID,
		&row.ProcessKey,
		&row.Category,
		&row.ObligationID,
		&batchID,
		&taskID,
		&taskRowVersion,
		&submissionID,
		&completionID,
		&row.ParkID,
		&row.ParkName,
		&row.ShedID,
		&row.ShedName,
		&cohortID,
		&goatID,
		&row.AnimalStage,
		&row.ProtocolID,
		&row.ProtocolVersionID,
		&row.RuleID,
		&row.ProtocolName,
		&row.DoseCode,
		&driveName,
		&sopVersionID,
		&row.ProofPolicy,
		&dueAt,
		&windowStart,
		&windowEnd,
		&row.ExpectedCount,
		&row.ObligationStatus,
		&batchStatus,
		&sopState,
		&submissionState,
		&proofState,
		&verificationState,
		&completionState,
		&row.CompletedCount,
		&row.ProofCount,
		&row.RejectedCount,
		&row.DeferredCount,
		&workState,
		&row.GapType,
		&severity,
		&blockerReason,
		&ownerState,
		&row.NextAction,
		&row.ProcessIntact,
		&operatorID,
		&operatorName,
		&parkHeadID,
		&parkHeadName,
		&verifierID,
		&verifierName,
		&escalationOwnerID,
		&escalationOwnerName,
		&proofIDsCSV,
		&row.Evidence.EvidenceCount,
		&latestEvidenceAt,
		&latestRejection,
		&auditRef,
	); err != nil {
		return domain.Row{}, domain.Cursor{}, fmt.Errorf("processintegrity: scan row: %w", err)
	}
	row.BatchID = textPtr(batchID)
	row.SOPTaskID = textPtr(taskID)
	row.SOPTaskVersion = int32Ptr(taskRowVersion)
	row.SOPSubmissionID = textPtr(submissionID)
	row.CompletionID = textPtr(completionID)
	row.CohortID = textPtr(cohortID)
	row.GoatID = textPtr(goatID)
	row.DriveName = textPtr(driveName)
	row.SOPVersionID = textPtr(sopVersionID)
	row.DueAt = dueAt.Time
	row.WindowStart = timePtr(windowStart)
	row.WindowEnd = timePtr(windowEnd)
	row.BatchStatus = textPtr(batchStatus)
	row.SOPTaskState = domain.SOPState(sopState)
	row.SubmissionState = textPtr(submissionState)
	row.ProofState = domain.ProofState(proofState)
	row.VerificationState = domain.VerificationState(verificationState)
	row.CompletionState = textPtr(completionState)
	row.WorkState = domain.WorkState(workState)
	row.Severity = domain.Severity(severity)
	row.BlockerReason = textPtr(blockerReason)
	row.OwnerState = domain.OwnerState(ownerState)
	row.Owner = domain.Owner{
		OperatorID:          textPtr(operatorID),
		OperatorName:        textPtr(operatorName),
		ParkHeadID:          textPtr(parkHeadID),
		ParkHeadName:        textPtr(parkHeadName),
		VerifierID:          textPtr(verifierID),
		VerifierName:        textPtr(verifierName),
		EscalationOwnerID:   textPtr(escalationOwnerID),
		EscalationOwnerName: textPtr(escalationOwnerName),
	}
	row.Evidence.ProofIDs = splitCSV(proofIDsCSV)
	row.Evidence.LatestEvidenceAt = timePtr(latestEvidenceAt)
	row.Evidence.LatestRejectionReason = textPtr(latestRejection)
	row.Evidence.AuditRef = textPtr(auditRef)
	return row, domain.Cursor{SortPriority: sortPriority, DueAt: row.DueAt, RowID: row.RowID}, nil
}

func normalizeQuery(q domain.Query) domain.Query {
	if q.Limit <= 0 {
		q.Limit = defaultLimit
	}
	if q.Limit > maxLimit {
		q.Limit = maxLimit
	}
	if q.AsOf.IsZero() {
		q.AsOf = time.Now().In(biztime.DefaultLocation())
	}
	if q.DueBefore.IsZero() {
		q.DueBefore = q.AsOf.Add(30 * 24 * time.Hour)
	}
	// Process-integrity due filters are an operational business-date contract. Normalizing both row
	// and summary reads to the same IST day boundaries keeps totals exact while allowing the projector
	// to collapse arbitrarily many same-day obligations into bounded summary grains.
	if q.DueAfter != nil {
		start := startOfBusinessDay(*q.DueAfter)
		q.DueAfter = &start
	}
	q.DueBefore = startOfBusinessDay(q.DueBefore).Add(24*time.Hour - time.Nanosecond)
	return q
}

func startOfBusinessDay(t time.Time) time.Time {
	local := t.In(biztime.DefaultLocation())
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, biztime.DefaultLocation())
}

func queryArgs(q domain.Query) []any {
	closedAfter := startOfBusinessDay(q.AsOf.Add(-defaultClosedHistoryAge))
	cursorSort := -1
	cursorDue := pgtype.Timestamptz{}
	cursorRow := ""
	if q.Cursor != nil {
		cursorSort = q.Cursor.SortPriority
		cursorDue = pgtype.Timestamptz{Time: q.Cursor.DueAt, Valid: true}
		cursorRow = q.Cursor.RowID
	}
	args := []any{
		q.TenantID,
		textValue(q.ParkID),
		textValue(q.ShedID),
		timestamptzValue(q.DueAfter),
		pgtype.Timestamptz{Time: q.DueBefore, Valid: true},
		textEnum(q.WorkState),
		textEnum(q.Severity),
		textValue(q.OwnerID),
		textValue(q.ProtocolVersionID),
		pgtype.Timestamptz{Time: q.AsOf, Valid: true},
		pgtype.Timestamptz{Time: closedAfter, Valid: true},
		textValue(q.RowID),
		q.OnlyBrokenOrAtRisk,
		q.IncludeCompleted,
		textValue(q.Category),
		cursorSort,
		cursorDue,
		cursorRow,
		int32(q.Limit + 1),
	}
	if len(args) != rowsQueryArgCount {
		panic(fmt.Sprintf("processintegrity: query arg count drifted: got %d want %d", len(args), rowsQueryArgCount))
	}
	return args
}

func countQueryArgs(args []any) []any {
	if len(args) != rowsQueryArgCount {
		panic(fmt.Sprintf("processintegrity: rows arg count drifted: got %d want %d", len(args), rowsQueryArgCount))
	}
	if countQueryArgCount <= 0 || countQueryArgCount >= rowsQueryArgCount {
		panic(fmt.Sprintf("processintegrity: count arg boundary drifted: count=%d rows=%d", countQueryArgCount, rowsQueryArgCount))
	}
	return args[:countQueryArgCount]
}

func textValue(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

func textEnum[T ~string](v *T) string {
	if v == nil {
		return ""
	}
	return string(*v)
}

func timestamptzValue(v *time.Time) pgtype.Timestamptz {
	if v == nil || v.IsZero() {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: *v, Valid: true}
}

func textPtr(v pgtype.Text) *string {
	if !v.Valid || v.String == "" {
		return nil
	}
	s := v.String
	return &s
}

func timePtr(v pgtype.Timestamptz) *time.Time {
	if !v.Valid {
		return nil
	}
	t := v.Time
	return &t
}

func int32Ptr(v pgtype.Int4) *int32 {
	if !v.Valid || v.Int32 <= 0 {
		return nil
	}
	i := v.Int32
	return &i
}

func splitCSV(v string) []string {
	if strings.TrimSpace(v) == "" {
		return []string{}
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// processIntegrityBaseSQL is point-in-time correct as of $10 (as_of) for the dose/evidence state it can
// reconstruct: completions and SOP-submission evidence are bounded by as_of, and obligation status is
// reconstructed AT as_of (completed via completed_at/as_of-bounded completion; missed/waived/deferred via the latest
// terminal event AT OR BEFORE as_of in the obligation_status_events log; scheduled/due/overdue from
// due_at/window) instead of being read off the current obligation_instances.status. A missed/waived/deferred row is
// only treated as terminal when a terminal event is proven at/before as_of; an obligation whose terminal
// events are all after as_of re-buckets to open, while one with no terminal history at all falls back to its
// current stored status (explicit, see asof_terminal). Documented residuals (no event history to reconstruct
// exactly): (a) sop_tasks.state and obligation_batches.status remain current-state, so a task/batch
// transition recorded after as_of is trusted as-is; (b) a missed->reschedule->missed churn is not reopen-
// aware. Full task/batch + reopen event-history replay is a later pass.
// scale-guard:ignore: off-request projector recompute only (RecomputeProjection -> processIntegrityProjectionInsertSQL); no request path uses this CTE after C35-002.
const processIntegrityBaseSQL = `
WITH completions AS (
  -- One effective completion per obligation, as_of-bounded. Migration 000082 keeps rejected/reversed
  -- history alongside one active row, so a direct join fans out (double-counting rework) and also leaks
  -- doses recorded AFTER as_of. Bound by event time (administered_at, falling back to created_at) <= as_of,
  -- exclude reversed, and prefer the active (recorded/accepted) attempt, else the latest historical one.
  SELECT DISTINCT ON (obligation_id)
    obligation_id,
    completion_id,
    asof_status AS completion_status,
    -- Only a verification PROVEN to be after as_of (downgrade) is stripped of its verifier/rejection at
    -- as_of. A NULL verified_at is no proof, so the stored status/fields are trusted (no faked history).
    CASE WHEN downgrade THEN NULL ELSE verified_by END AS verified_by,
    CASE WHEN downgrade THEN NULL ELSE verified_at END AS verified_at,
    CASE WHEN downgrade THEN NULL ELSE rejection_reason END AS rejection_reason,
    completion_updated_at
  FROM (
    SELECT
      obligation_id, completion_id, verified_by, verified_at, rejection_reason,
      updated_at AS completion_updated_at, administered_at, created_at,
      (verified_at IS NOT NULL AND verified_at > $10::timestamptz) AS downgrade,
      -- as_of correctness on the VERIFICATION: accept/reject PROVEN after as_of was only 'recorded' at as_of.
      CASE
        WHEN status IN ('accepted', 'rejected') AND verified_at IS NOT NULL AND verified_at > $10::timestamptz THEN 'recorded'
        ELSE status
      END AS asof_status
    FROM vaccination_completions
    WHERE tenant_id = $1::uuid
      AND status <> 'reversed'
      AND COALESCE(administered_at, created_at) <= $10::timestamptz
  ) c
  ORDER BY obligation_id,
    CASE WHEN asof_status IN ('recorded', 'accepted') THEN 0 ELSE 1 END,
    administered_at DESC NULLS LAST,
    created_at DESC
),
asof_terminal AS (
  -- Latest TERMINAL transition (missed/waived/deferred: statuses with no timestamp column on obligation_instances)
  -- AT OR BEFORE as_of, from the append-only event log. asof_terminal_type is the terminal status in effect
  -- at as_of (latest of missed/waived/deferred <= as_of, so sequences resolve to whichever was last
  -- at as_of). It is NULL when the obligation's only terminal events are AFTER as_of (it was still open at
  -- as_of); has_terminal_event then distinguishes that "future-only" case from "no terminal history at all"
  -- (row absent -> cannot reconstruct -> trust the current stored status, documented residual). Restricted
  -- to missed/waived/deferred, which are exceptions at herd scale, so this stays small and index-bound; open buckets
  -- need no log (derived from due_at/window). Residual: a missed->reschedule->missed churn is not reopen-
  -- aware (no reopen event type yet), so the last terminal event at/before as_of wins; full multi-transition
  -- replay (incl. sop_tasks/obligation_batches history) is the deeper task/batch pass.
  SELECT
    obligation_id,
    (ARRAY_AGG(event_type ORDER BY occurred_at DESC, obligation_event_id DESC)
       FILTER (WHERE occurred_at <= $10::timestamptz))[1] AS asof_terminal_type,
    true AS has_terminal_event
  FROM obligation_status_events
  WHERE tenant_id = $1::uuid
    AND event_type IN ('missed', 'waived', 'deferred')
  GROUP BY obligation_id
),
raw AS (
  SELECT
    oi.obligation_id,
    oi.protocol_version_id,
    oi.rule_id,
    oi.batch_id,
    oi.sop_task_id AS obligation_sop_task_id,
    oi.target_type,
    oi.target_id,
    oi.scope_type,
    oi.scope_id,
    oi.due_at,
    oi.window_start,
    oi.window_end,
    oi.status AS obligation_status,
    oi.created_at AS obligation_created_at,
    pv.protocol_id,
    COALESCE(pr.sop_version_id, pv.sop_version_id) AS configured_sop_version_id,
    COALESCE(NULLIF(pr.proof_policy, '{}'::jsonb), pv.proof_policy, '{}'::jsonb)::text AS proof_policy,
    pv.published_at,
    pd.name AS protocol_name,
    pr.dose_code,
    ob.status AS batch_status,
    ob.sop_task_id AS batch_sop_task_id,
    ob.conducted_by,
    ob.created_at AS batch_created_at,
    st.task_id,
    st.row_version AS task_row_version,
    st.state AS task_state,
    st.assigned_to,
    st.sop_version_id AS task_sop_version_id,
    ss.submission_id,
    ss.state AS submission_state,
    ss.proof_refs,
    ss.submitted_at,
    ss.accepted_at,
    g.goat_id,
    g.lifecycle_status AS goat_lifecycle_status,
    g.health_status AS goat_health_status,
    g.management_stage AS goat_stage,
    g.cohort_id AS goat_cohort_id,
    oi.completed_at,
    te.asof_terminal_type,
    te.has_terminal_event,
    c.completion_id,
    c.completion_status,
    c.verified_by,
    c.verified_at,
    c.rejection_reason,
    c.completion_updated_at,
    CASE
      WHEN g.shed_id IS NOT NULL THEN g.shed_id
      WHEN oi.target_type = 'shed' THEN oi.target_id
      WHEN oi.scope_type = 'shed' THEN oi.scope_id
      ELSE NULL
    END AS shed_uuid,
    CASE
      WHEN g.park_id IS NOT NULL THEN g.park_id
      WHEN oi.target_type = 'park' THEN oi.target_id
      WHEN oi.scope_type = 'park' THEN oi.scope_id
      ELSE NULL
    END AS direct_park_uuid
  FROM obligation_instances oi
  JOIN protocol_versions pv
    ON pv.tenant_id = oi.tenant_id
   AND pv.protocol_version_id = oi.protocol_version_id
  JOIN protocol_definitions pd
    ON pd.tenant_id = pv.tenant_id
   AND pd.protocol_id = pv.protocol_id
   AND pd.category = 'vaccination'
  JOIN protocol_rules pr
    ON pr.tenant_id = oi.tenant_id
   AND pr.rule_id = oi.rule_id
  LEFT JOIN goats g
    ON oi.target_type = 'goat'
   AND g.tenant_id = oi.tenant_id
   AND g.goat_id = oi.target_id
   AND g.merged_into_goat_id IS NULL
  LEFT JOIN obligation_batches ob
    ON ob.tenant_id = oi.tenant_id
   AND ob.batch_id = oi.batch_id
  LEFT JOIN sop_tasks st
    ON st.tenant_id = oi.tenant_id
   AND st.task_id = COALESCE(oi.sop_task_id, ob.sop_task_id)
  LEFT JOIN LATERAL (
    SELECT submission_id, state, proof_refs, submitted_at, accepted_at
    FROM sop_submissions sub
    WHERE sub.tenant_id = oi.tenant_id
      AND sub.task_id = COALESCE(oi.sop_task_id, ob.sop_task_id)
      -- as_of correctness: evidence submitted AFTER as_of must not be seen.
      AND sub.submitted_at <= $10::timestamptz
    ORDER BY sub.submitted_at DESC, sub.submission_id DESC
    LIMIT 1
  ) ss ON true
  LEFT JOIN completions c
    ON c.obligation_id = oi.obligation_id
  LEFT JOIN asof_terminal te
    ON te.obligation_id = oi.obligation_id
  WHERE oi.tenant_id = $1::uuid
    AND oi.status IN ('scheduled', 'due', 'in_progress', 'deferred', 'completed', 'missed', 'waived')
    AND ($4::timestamptz IS NULL OR oi.due_at >= $4::timestamptz)
    AND oi.due_at <= $5::timestamptz
    AND (
      oi.status IN ('scheduled', 'due', 'in_progress', 'deferred', 'missed', 'waived')
      OR $14::boolean
      OR oi.due_at >= $11::timestamptz
      -- A row that is 'completed' NOW but finalized AFTER as_of was still open at as_of, so it must be
      -- pulled (even if old) to re-bucket; completed_at > as_of only matches historical as_of queries, so
      -- the common as_of = now path pulls no extra closed history.
      OR (oi.status = 'completed' AND oi.completed_at > $10::timestamptz)
      -- A row completed within the closed-history window ending at as_of must also be pulled so a
      -- just-completed alert shows as 'completed' even when its due_at is older than the window. The
      -- recency key for completed work is completion time ($11 = as_of - closed-history-age), NOT due
      -- date: a batch drive can finalize an obligation whose due_at is weeks old, and at as_of = now that
      -- row would otherwise vanish from the projection entirely (NEW-E2E-001).
      OR (oi.status = 'completed' AND oi.completed_at >= $11::timestamptz AND oi.completed_at <= $10::timestamptz)
    )
),
located AS (
  SELECT
    raw.*,
    COALESCE(raw.direct_park_uuid, shed_loc.parent_location_id) AS park_uuid,
    -- as_of-effective obligation status. Reconstructs the state AT as_of instead of reading the current
    -- obligation_instances.status, so a transition recorded after as_of is not treated as already true.
    CASE
      WHEN raw.obligation_status = 'completed' THEN
        CASE
          WHEN raw.completed_at IS NOT NULL AND raw.completed_at <= $10::timestamptz THEN 'completed'
          WHEN raw.completed_at IS NULL AND raw.completion_status IS NOT NULL THEN 'completed'
          ELSE (CASE WHEN raw.due_at < $10::timestamptz THEN 'overdue' WHEN COALESCE(raw.window_start, raw.due_at) <= $10::timestamptz THEN 'due' ELSE 'scheduled' END)
        END
      WHEN raw.obligation_status IN ('missed', 'waived', 'deferred') THEN
        CASE
          -- The latest terminal transition at/before as_of was in effect at as_of.
          WHEN raw.asof_terminal_type IS NOT NULL THEN raw.asof_terminal_type
          -- Terminal events exist but only AFTER as_of: the obligation was still open at as_of.
          WHEN raw.has_terminal_event THEN (CASE WHEN raw.due_at < $10::timestamptz THEN 'overdue' WHEN COALESCE(raw.window_start, raw.due_at) <= $10::timestamptz THEN 'due' ELSE 'scheduled' END)
          -- No terminal history at all: cannot reconstruct, trust the current stored status (documented residual).
          ELSE raw.obligation_status
        END
      WHEN raw.obligation_status = 'in_progress' THEN 'in_progress'
      ELSE (CASE WHEN raw.due_at < $10::timestamptz THEN 'overdue' WHEN COALESCE(raw.window_start, raw.due_at) <= $10::timestamptz THEN 'due' ELSE 'scheduled' END)
    END AS eff_status
  FROM raw
  LEFT JOIN locations shed_loc
    ON shed_loc.tenant_id = $1::uuid
   AND shed_loc.location_id = raw.shed_uuid
   AND shed_loc.location_type = 'shed'
  WHERE raw.shed_uuid IS NOT NULL
),
grouped AS (
  SELECT
    located.park_uuid,
    located.shed_uuid,
    located.batch_id,
    located.rule_id,
    located.protocol_id,
    located.protocol_version_id,
    located.protocol_name,
    located.dose_code,
    MIN(located.obligation_id::text) AS obligation_id,
    (ARRAY_AGG(located.task_id::text ORDER BY located.due_at DESC NULLS LAST) FILTER (WHERE located.task_id IS NOT NULL))[1] AS task_id,
    (ARRAY_AGG(located.task_row_version ORDER BY located.due_at DESC NULLS LAST) FILTER (WHERE located.task_id IS NOT NULL))[1] AS task_row_version,
    (ARRAY_AGG(located.submission_id::text ORDER BY located.submitted_at DESC NULLS LAST) FILTER (WHERE located.submission_id IS NOT NULL))[1] AS submission_id,
    (ARRAY_AGG(located.completion_id::text ORDER BY located.completion_updated_at DESC NULLS LAST) FILTER (WHERE located.completion_id IS NOT NULL))[1] AS completion_id,
    CASE WHEN COUNT(DISTINCT located.goat_id) = 1 THEN MAX(located.goat_id::text) ELSE NULL END AS goat_id,
    CASE WHEN COUNT(DISTINCT located.goat_cohort_id) = 1 THEN MAX(located.goat_cohort_id::text) ELSE NULL END AS cohort_id,
    COALESCE(MAX(located.configured_sop_version_id::text), MAX(located.task_sop_version_id::text)) AS sop_version_id,
    MAX(located.proof_policy) AS proof_policy,
    MIN(located.due_at) AS due_at,
    MIN(located.window_start) AS window_start,
    MAX(located.window_end) AS window_end,
    COUNT(*)::int AS expected_count,
    -- Representative status + bucket counts use the as_of-effective status, not the stored status.
    (ARRAY_AGG(located.eff_status ORDER BY
      CASE located.eff_status
        WHEN 'overdue' THEN 0
        WHEN 'missed' THEN 1
        WHEN 'in_progress' THEN 2
        WHEN 'due' THEN 3
        WHEN 'scheduled' THEN 4
        WHEN 'deferred' THEN 5
        WHEN 'waived' THEN 6
        WHEN 'completed' THEN 7
        ELSE 8
      END,
      located.due_at ASC
    ))[1] AS obligation_status,
    COUNT(*) FILTER (WHERE located.eff_status = 'scheduled')::int AS scheduled_count,
    COUNT(*) FILTER (WHERE located.eff_status = 'due')::int AS due_count,
    COUNT(*) FILTER (WHERE located.eff_status = 'in_progress')::int AS in_progress_count,
    COUNT(*) FILTER (WHERE located.eff_status = 'completed')::int AS completed_count,
    COUNT(*) FILTER (WHERE located.eff_status = 'missed')::int AS missed_count,
    COUNT(*) FILTER (WHERE located.eff_status IN ('waived', 'deferred'))::int AS deferred_count,
    COUNT(*) FILTER (WHERE located.completion_status = 'recorded')::int AS completion_recorded,
    COUNT(*) FILTER (WHERE located.completion_status = 'accepted')::int AS completion_accepted,
    COUNT(*) FILTER (WHERE located.completion_status = 'rejected')::int AS completion_rejected,
    (ARRAY_AGG(located.completion_status ORDER BY located.completion_updated_at DESC NULLS LAST) FILTER (WHERE located.completion_status IS NOT NULL))[1] AS completion_state,
    (ARRAY_AGG(located.batch_status ORDER BY
      CASE located.batch_status
        WHEN 'in_progress' THEN 0
        WHEN 'planned' THEN 1
        WHEN 'completed' THEN 2
        WHEN 'superseded' THEN 3
        WHEN 'canceled' THEN 4
        ELSE 5
      END,
      located.due_at DESC NULLS LAST
    ) FILTER (WHERE located.batch_status IS NOT NULL))[1] AS batch_status,
    (ARRAY_AGG(located.task_state ORDER BY
      CASE located.task_state
        WHEN 'rework_requested' THEN 0
        WHEN 'rejected' THEN 1
        WHEN 'submitted' THEN 2
        WHEN 'needs_review' THEN 3
        WHEN 'in_progress' THEN 4
        WHEN 'accepted' THEN 5
        ELSE 6
      END,
      located.due_at DESC NULLS LAST
    ) FILTER (WHERE located.task_state IS NOT NULL))[1] AS task_state,
    (ARRAY_AGG(located.submission_state ORDER BY located.submitted_at DESC NULLS LAST) FILTER (WHERE located.submission_state IS NOT NULL))[1] AS submission_state,
    (ARRAY_AGG(located.proof_refs ORDER BY located.submitted_at DESC NULLS LAST) FILTER (WHERE located.proof_refs IS NOT NULL))[1] AS latest_proof_refs,
    MAX(jsonb_array_length(COALESCE(located.proof_refs, '[]'::jsonb)))::int AS proof_count,
    MAX(located.submitted_at) AS latest_evidence_at,
    (ARRAY_AGG(located.rejection_reason ORDER BY located.completion_updated_at DESC NULLS LAST) FILTER (WHERE located.rejection_reason IS NOT NULL AND located.rejection_reason <> ''))[1] AS latest_rejection_reason,
    (ARRAY_AGG(located.conducted_by::text ORDER BY located.due_at DESC NULLS LAST) FILTER (WHERE located.conducted_by IS NOT NULL))[1] AS explicit_conducted_by,
    (ARRAY_AGG(located.assigned_to::text ORDER BY located.due_at DESC NULLS LAST) FILTER (WHERE located.assigned_to IS NOT NULL))[1] AS assigned_to,
    (ARRAY_AGG(located.verified_by::text ORDER BY located.verified_at DESC NULLS LAST) FILTER (WHERE located.verified_by IS NOT NULL))[1] AS verified_by,
    COALESCE(MAX(stage.stage_code), MAX(stage.name), MAX(located.goat_stage), 'Unknown') AS animal_stage,
    COUNT(*) FILTER (
      WHERE located.eff_status NOT IN ('waived', 'deferred')
        AND (
          located.goat_lifecycle_status IN ('sick', 'under_treatment', 'quarantine', 'icu')
          OR COALESCE(located.goat_health_status, '') IN ('sick', 'under_treatment', 'quarantine', 'icu')
        )
    )::int AS health_deferred_count
  FROM located
  JOIN locations shed
    ON shed.tenant_id = $1::uuid
   AND shed.location_id = located.shed_uuid
   AND shed.location_type = 'shed'
   AND shed.status = 'active'
  JOIN locations park
    ON park.tenant_id = $1::uuid
   AND park.location_id = located.park_uuid
   AND park.location_type = 'park'
   AND park.status = 'active'
  LEFT JOIN shed_profiles sp
    ON sp.tenant_id = $1::uuid
   AND sp.location_id = located.shed_uuid
  LEFT JOIN animal_stage_lookup stage
    ON stage.tenant_id = $1::uuid
   AND stage.animal_stage_id = sp.animal_stage_id
  WHERE located.park_uuid IS NOT NULL
    AND ($2::text = '' OR located.park_uuid = $2::uuid)
    AND ($3::text = '' OR located.shed_uuid = $3::uuid)
    AND ($9::text = '' OR located.protocol_version_id = $9::uuid)
  GROUP BY located.park_uuid, located.shed_uuid, located.batch_id, located.rule_id, located.protocol_id, located.protocol_version_id, located.protocol_name, located.dose_code,
    CASE WHEN located.batch_id IS NULL THEN (located.due_at AT TIME ZONE 'Asia/Kolkata')::date ELSE NULL END
),
enriched AS (
  SELECT
    grouped.*,
    COALESCE(grouped.explicit_conducted_by, default_operator.workforce_member_id::text) AS conducted_by,
    COALESCE(loa.usable_for_vaccination, true) AS usable_for_vaccination,
    COALESCE(loa.is_quarantine, false) AS is_quarantine,
    COALESCE(loa.is_icu, false) AS is_icu
  FROM grouped
  LEFT JOIN location_operational_attributes loa
    ON loa.tenant_id = $1::uuid
   AND loa.location_id = grouped.shed_uuid
  LEFT JOIN LATERAL (
    SELECT wm.workforce_member_id, wm.primary_location_id, wm.updated_at
    FROM workforce_members wm
    WHERE wm.tenant_id = $1::uuid
      AND wm.status = 'active'
      AND wm.primary_role_hint = 'operator'
      AND wm.primary_location_id IN (grouped.shed_uuid, grouped.park_uuid)
    ORDER BY CASE WHEN wm.primary_location_id = grouped.shed_uuid THEN 0 WHEN wm.primary_location_id = grouped.park_uuid THEN 1 ELSE 2 END,
             wm.updated_at DESC, wm.workforce_member_id DESC
    LIMIT 1
  ) default_operator ON grouped.explicit_conducted_by IS NULL
),
stateful AS (
  SELECT
    enriched.*,
    CASE
      WHEN enriched.expected_count > 0
       AND enriched.completed_count = enriched.expected_count
       AND enriched.completion_rejected = 0
       AND enriched.completion_recorded = 0 THEN 'completed'
      WHEN enriched.completion_rejected > 0 THEN 'rejected'
      WHEN NOT enriched.usable_for_vaccination THEN 'blocked'
      WHEN enriched.deferred_count > 0
        OR enriched.health_deferred_count > 0
        OR enriched.is_quarantine
        OR enriched.is_icu THEN 'deferred'
      WHEN enriched.missed_count > 0 THEN 'missed'
      WHEN enriched.conducted_by IS NULL
       AND enriched.assigned_to IS NULL
       AND enriched.completed_count < enriched.expected_count THEN 'blocked'
      WHEN enriched.task_state IN ('rework_requested', 'rejected') THEN 'rejected'
      WHEN enriched.completion_recorded > 0
        OR enriched.task_state IN ('submitted', 'needs_review') THEN 'verification_pending'
      WHEN enriched.in_progress_count > 0
        OR enriched.batch_status = 'in_progress'
        OR enriched.task_state = 'in_progress' THEN 'in_progress'
      WHEN enriched.due_at < $10::timestamptz THEN 'overdue'
      WHEN enriched.due_count > 0 THEN 'due'
      ELSE 'scheduled'
    END AS work_state
  FROM enriched
),
derived AS (
  SELECT
    stateful.*,
    CASE stateful.work_state
      WHEN 'completed' THEN 'ok'
      WHEN 'scheduled' THEN 'watch'
      WHEN 'due' THEN 'watch'
      WHEN 'in_progress' THEN 'watch'
      WHEN 'deferred' THEN 'watch'
      WHEN 'verification_pending' THEN 'watch'
      WHEN 'proof_pending' THEN 'at_risk'
      WHEN 'overdue' THEN 'at_risk'
      ELSE 'broken'
    END AS severity,
    CASE stateful.work_state
      WHEN 'completed' THEN ''
      WHEN 'scheduled' THEN ''
      WHEN 'due' THEN ''
      WHEN 'in_progress' THEN ''
      WHEN 'deferred' THEN 'deferred_explained'
      WHEN 'verification_pending' THEN 'verification_pending'
      WHEN 'proof_pending' THEN 'proof_missing'
      WHEN 'overdue' THEN 'overdue'
      WHEN 'rejected' THEN 'proof_rejected'
      WHEN 'blocked' THEN CASE WHEN stateful.missed_count > 0 THEN 'missed' ELSE 'blocked' END
      ELSE stateful.work_state
    END AS gap_type,
    CASE
      WHEN stateful.work_state IN ('completed', 'scheduled', 'due', 'in_progress', 'deferred') THEN true
      ELSE false
    END AS process_intact,
    CASE
      WHEN stateful.conducted_by IS NULL AND stateful.assigned_to IS NULL THEN 'missing'
      ELSE 'assigned'
    END AS owner_state,
    CASE
      WHEN stateful.batch_id IS NOT NULL THEN 'batch:' || stateful.batch_id::text || ':rule:' || stateful.rule_id::text || ':shed:' || stateful.shed_uuid::text
      ELSE 'obligation:' || stateful.obligation_id
    END AS row_id,
    CASE stateful.work_state
      WHEN 'rejected' THEN 0
      WHEN 'blocked' THEN 1
      WHEN 'overdue' THEN 2
      WHEN 'proof_pending' THEN 3
      WHEN 'verification_pending' THEN 4
      WHEN 'due' THEN 5
      WHEN 'in_progress' THEN 6
      WHEN 'deferred' THEN 7
      WHEN 'scheduled' THEN 8
      WHEN 'completed' THEN 9
      ELSE 11
    END AS sort_priority,
    CASE
      WHEN NOT stateful.usable_for_vaccination THEN 'Shed is not marked usable for vaccination'
      WHEN stateful.is_icu THEN 'Shed is ICU; PC defer/approval required'
      WHEN stateful.is_quarantine THEN 'Shed is quarantine; PC defer/approval required'
      WHEN stateful.health_deferred_count > 0 THEN 'Some goats are sick, under treatment, quarantined, or in ICU'
      WHEN stateful.missed_count > 0 THEN 'Missed dose escalation required'
      WHEN stateful.conducted_by IS NULL AND stateful.assigned_to IS NULL AND stateful.completed_count < stateful.expected_count THEN 'Operator assignment required before execution'
      ELSE NULL
    END AS blocker_reason,
    CASE stateful.work_state
      WHEN 'completed' THEN 'No action - drive verified'
      WHEN 'rejected' THEN 'Review rejection and request rework'
      WHEN 'blocked' THEN CASE WHEN stateful.missed_count > 0 THEN 'Escalate missed dose to PC' ELSE 'Resolve blocker before execution' END
      WHEN 'deferred' THEN 'Confirm defer reason with PC'
      WHEN 'verification_pending' THEN 'Verifier to accept or reject proof'
      WHEN 'proof_pending' THEN 'Upload required SOP proof'
      WHEN 'in_progress' THEN 'Complete drive and submit proof'
      WHEN 'overdue' THEN 'Start SOP - overdue'
      WHEN 'due' THEN 'Start scheduled vaccination SOP'
      ELSE 'Monitor scheduled drive'
    END AS next_action,
    CASE
      WHEN stateful.completion_rejected > 0 THEN 'rework'
      WHEN stateful.task_state IN ('in_progress') THEN 'in_progress'
      WHEN stateful.task_state IN ('submitted', 'needs_review') THEN 'submitted'
      WHEN stateful.task_state = 'accepted' THEN 'accepted'
      WHEN stateful.task_state IN ('rework_requested', 'rejected') THEN 'rework'
      ELSE 'not_started'
    END AS sop_state,
    CASE
      WHEN stateful.completion_rejected > 0 THEN 'rejected'
      WHEN stateful.completion_accepted > 0 AND stateful.completion_recorded = 0 THEN 'accepted'
      WHEN stateful.proof_count > 0 OR stateful.completion_recorded > 0 OR stateful.task_state IN ('submitted', 'needs_review') THEN 'uploaded'
      ELSE 'missing'
    END AS proof_state,
    CASE
      WHEN stateful.completion_rejected > 0 THEN 'rejected'
      WHEN stateful.completion_recorded > 0 OR stateful.task_state IN ('submitted', 'needs_review') THEN 'pending'
      WHEN stateful.completion_accepted > 0 AND stateful.completed_count = stateful.expected_count THEN 'accepted'
      ELSE 'not_ready'
    END AS verification_state
  FROM stateful
),
with_locations AS (
  SELECT
    derived.*,
    park.name AS park_name,
    shed.name AS shed_name
  FROM derived
  JOIN locations park
    ON park.tenant_id = $1::uuid
   AND park.location_id = derived.park_uuid
  JOIN locations shed
    ON shed.tenant_id = $1::uuid
   AND shed.location_id = derived.shed_uuid
),
with_owners AS (
  SELECT
    with_locations.*,
    operator.workforce_member_id::text AS operator_id,
    operator.display_name AS operator_name,
    park_head.workforce_member_id::text AS park_head_id,
    park_head.display_name AS park_head_name,
    verifier.workforce_member_id::text AS verifier_id,
    verifier.display_name AS verifier_name,
    COALESCE(operator.workforce_member_id::text, park_head.workforce_member_id::text, verifier.workforce_member_id::text) AS escalation_owner_id,
    COALESCE(operator.display_name, park_head.display_name, verifier.display_name) AS escalation_owner_name
  FROM with_locations
  LEFT JOIN workforce_members operator
    ON operator.tenant_id = $1::uuid
   AND operator.workforce_member_id = COALESCE(with_locations.conducted_by::uuid, with_locations.assigned_to::uuid)
   AND operator.status = 'active'
  LEFT JOIN LATERAL (
    SELECT wm.workforce_member_id, wm.display_name
    FROM workforce_members wm
    WHERE wm.tenant_id = $1::uuid
      AND wm.status = 'active'
      AND wm.primary_role_hint = 'park_head'
      AND wm.primary_location_id IN (with_locations.shed_uuid, with_locations.park_uuid)
    ORDER BY CASE WHEN wm.primary_location_id = with_locations.shed_uuid THEN 0 ELSE 1 END, wm.updated_at DESC, wm.workforce_member_id DESC
    LIMIT 1
  ) park_head ON true
  LEFT JOIN LATERAL (
    SELECT wm.workforce_member_id, wm.display_name
    FROM workforce_members wm
    WHERE wm.tenant_id = $1::uuid
      AND wm.status = 'active'
      AND wm.primary_role_hint = 'verifier'
      AND (wm.primary_location_id IS NULL OR wm.primary_location_id IN (with_locations.shed_uuid, with_locations.park_uuid))
    ORDER BY CASE WHEN wm.primary_location_id = with_locations.shed_uuid THEN 0 WHEN wm.primary_location_id = with_locations.park_uuid THEN 1 ELSE 2 END,
             wm.updated_at DESC, wm.workforce_member_id DESC
    LIMIT 1
  ) verifier ON true
),
filtered AS (
  SELECT *
  FROM with_owners
  WHERE ($6::text = '' OR work_state = $6::text)
    AND ($7::text = '' OR severity = $7::text)
    AND (
      $8::text = ''
      OR operator_id = $8::text
      OR park_head_id = $8::text
      OR verifier_id = $8::text
      OR escalation_owner_id = $8::text
    )
    AND ($12::text = '' OR row_id = $12::text)
    AND (
      NOT $13::boolean
      OR work_state IN ('rejected', 'blocked', 'overdue', 'proof_pending', 'verification_pending')
    )
)
`

const processIntegrityFeedExceptionSQL = `,
feed_exception_rows AS (
  SELECT
    CASE
      WHEN e.status IN ('resolved', 'dismissed') THEN 10
      WHEN e.work_state = 'blocked' THEN 1
      ELSE 3
    END AS sort_priority,
    'feed_projection_exception:' || e.count_projection_exception_id::text AS row_id,
    'feed_projection_exception:' || e.count_projection_exception_id::text AS process_key,
    'feed_direction' AS category,
    e.count_projection_exception_id::text AS obligation_id,
    NULL::text AS batch_id,
    NULL::text AS sop_task_id,
    NULL::integer AS sop_task_row_version,
    NULL::text AS sop_submission_id,
    NULL::text AS completion_id,
    COALESCE(e.park_id::text, '') AS park_id,
    COALESCE(park.name, '') AS park_name,
    COALESCE(e.shed_id::text, '') AS shed_id,
    COALESCE(shed.name, '') AS shed_name,
    NULL::text AS cohort_id,
    NULL::text AS goat_id,
    COALESCE(NULLIF(e.stage_tag, ''), 'Counts/Shifting') AS animal_stage,
    ''::text AS protocol_id,
    ''::text AS protocol_version_id,
    ''::text AS rule_id,
    'Feed Direction Counts/Shifting' AS protocol_name,
    e.exception_type AS dose_code,
    NULLIF(
      'Feed exception - ' || e.exception_type ||
        CASE WHEN e.breed_key IS NOT NULL AND e.breed_key <> '' THEN ' / ' || e.breed_key ELSE '' END ||
        CASE WHEN e.stage_tag IS NOT NULL AND e.stage_tag <> '' THEN ' / ' || e.stage_tag ELSE '' END,
      ''
    ) AS drive_name,
    NULL::text AS sop_version_id,
    '{}'::text AS proof_policy,
    e.due_at,
    NULL::timestamptz AS window_start,
    NULL::timestamptz AS window_end,
    1::integer AS expected_count,
    e.status AS obligation_status,
    NULL::text AS batch_status,
    'not_started' AS sop_state,
    NULL::text AS submission_state,
    'not_required' AS proof_state,
    'not_ready' AS verification_state,
    e.status AS completion_state,
    CASE WHEN e.status IN ('resolved', 'dismissed') THEN 1 ELSE 0 END AS completed_count,
    0::integer AS proof_count,
    0::integer AS rejected_count,
    0::integer AS deferred_count,
    CASE
      WHEN e.status IN ('resolved', 'dismissed') THEN 'completed'
      ELSE e.work_state
    END AS work_state,
    e.exception_type AS gap_type,
    CASE
      WHEN e.status IN ('resolved', 'dismissed') THEN 'ok'
      WHEN e.severity = 'warning' THEN 'at_risk'
      ELSE 'broken'
    END AS severity,
    NULLIF(e.blocker_reason, '') AS blocker_reason,
    CASE WHEN e.owner_ref IS NULL OR e.owner_ref = '' THEN 'missing' ELSE 'assigned' END AS owner_state,
    CASE
      WHEN e.status IN ('resolved', 'dismissed') THEN 'No action - exception reviewed'
      ELSE e.next_action
    END AS next_action,
    e.status IN ('resolved', 'dismissed') AS process_intact,
    NULL::text AS operator_id,
    NULL::text AS operator_name,
    NULL::text AS park_head_id,
    NULL::text AS park_head_name,
    NULL::text AS verifier_id,
    NULL::text AS verifier_name,
    NULL::text AS escalation_owner_id,
    e.owner_ref AS escalation_owner_name,
    ''::text AS proof_ids,
    CASE WHEN e.evidence_json <> '{}'::jsonb THEN 1 ELSE 0 END AS evidence_count,
    e.updated_at AS latest_evidence_at,
    e.resolution_reason AS latest_rejection_reason,
    'count_projection_exception:' || e.count_projection_exception_id::text AS audit_ref
  FROM count_projection_exceptions e
  LEFT JOIN locations park
    ON park.tenant_id = e.tenant_id
   AND park.location_id = e.park_id
  LEFT JOIN locations shed
    ON shed.tenant_id = e.tenant_id
   AND shed.location_id = e.shed_id
  WHERE e.tenant_id = $1::uuid
    AND ($15::text = '' OR $15::text = 'feed_direction')
    AND ($2::text = '' OR e.park_id = $2::uuid)
    AND ($3::text = '' OR e.shed_id = $3::uuid)
    AND ($4::timestamptz IS NULL OR e.due_at >= $4::timestamptz)
    AND e.due_at <= $5::timestamptz
    AND ($9::text = '')
    AND (
      e.status = 'open'
      OR $14::boolean
      OR e.resolved_at >= $11::timestamptz
    )
    AND (
      $6::text = ''
      OR CASE WHEN e.status IN ('resolved', 'dismissed') THEN 'completed' ELSE e.work_state END = $6::text
    )
    AND (
      $7::text = ''
      OR CASE WHEN e.status IN ('resolved', 'dismissed') THEN 'ok' WHEN e.severity = 'warning' THEN 'at_risk' ELSE 'broken' END = $7::text
    )
    AND ($8::text = '' OR e.owner_ref = $8::text)
    AND ($12::text = '' OR 'feed_projection_exception:' || e.count_projection_exception_id::text = $12::text)
    AND (
      NOT $13::boolean
      OR CASE WHEN e.status IN ('resolved', 'dismissed') THEN 'completed' ELSE e.work_state END IN ('blocked')
    )
)
`

const processIntegrityAllRowsSQL = processIntegrityBaseSQL + processIntegrityFeedExceptionSQL + `,
all_rows AS (
  SELECT
    sort_priority,
    row_id,
    row_id AS process_key,
    'vaccination' AS category,
    obligation_id,
    batch_id::text AS batch_id,
    task_id AS sop_task_id,
    task_row_version AS sop_task_row_version,
    submission_id AS sop_submission_id,
    completion_id,
    park_uuid::text AS park_id,
    park_name,
    shed_uuid::text AS shed_id,
    shed_name,
    cohort_id,
    goat_id,
    animal_stage,
    protocol_id::text,
    protocol_version_id::text,
    rule_id::text,
    protocol_name,
    dose_code,
    NULLIF(protocol_name || CASE WHEN dose_code <> '' THEN ' - ' || dose_code ELSE '' END, '') AS drive_name,
    sop_version_id,
    proof_policy,
    due_at,
    window_start,
    window_end,
    expected_count,
    obligation_status,
    batch_status,
    sop_state,
    submission_state,
    proof_state,
    verification_state,
    completion_state,
    completed_count,
    proof_count,
    completion_rejected AS rejected_count,
    deferred_count + health_deferred_count AS deferred_count,
    work_state,
    gap_type,
    severity,
    blocker_reason,
    owner_state,
    next_action,
    process_intact,
    operator_id,
    operator_name,
    park_head_id,
    park_head_name,
    verifier_id,
    verifier_name,
    escalation_owner_id,
    escalation_owner_name,
    COALESCE(array_to_string(ARRAY(
      SELECT elem->>'proof_id'
      FROM jsonb_array_elements(COALESCE(latest_proof_refs, '[]'::jsonb)) elem
      WHERE elem->>'proof_id' IS NOT NULL AND elem->>'proof_id' <> ''
    ), ','), '') AS proof_ids,
    proof_count AS evidence_count,
    latest_evidence_at,
    latest_rejection_reason,
    CASE WHEN submission_id IS NOT NULL THEN 'sop_submission:' || submission_id ELSE NULL END AS audit_ref
  FROM filtered
  WHERE ($15::text = '' OR $15::text = 'vaccination')
  UNION ALL
  SELECT
    sort_priority,
    row_id,
    process_key,
    category,
    obligation_id,
    batch_id,
    sop_task_id,
    sop_task_row_version,
    sop_submission_id,
    completion_id,
    park_id,
    park_name,
    shed_id,
    shed_name,
    cohort_id,
    goat_id,
    animal_stage,
    protocol_id,
    protocol_version_id,
    rule_id,
    protocol_name,
    dose_code,
    drive_name,
    sop_version_id,
    proof_policy,
    due_at,
    window_start,
    window_end,
    expected_count,
    obligation_status,
    batch_status,
    sop_state,
    submission_state,
    proof_state,
    verification_state,
    completion_state,
    completed_count,
    proof_count,
    rejected_count,
    deferred_count,
    work_state,
    gap_type,
    severity,
    blocker_reason,
    owner_state,
    next_action,
    process_intact,
    operator_id,
    operator_name,
    park_head_id,
    park_head_name,
    verifier_id,
    verifier_name,
    escalation_owner_id,
    escalation_owner_name,
    proof_ids,
    evidence_count,
    latest_evidence_at,
    latest_rejection_reason,
    audit_ref
  FROM feed_exception_rows
)
`

// NOTE: the former request-path canonical SQL (processIntegrityRowsSQL / processIntegrityCountsSQL /
// processIntegrityAdherenceSummarySQL) was removed in C35-002. Request reads are projection-only; the
// base CTE (processIntegrityBaseSQL) now survives solely for the off-request projector recompute
// (processIntegrityProjectionInsertSQL, called by RecomputeProjection), which is allowed to replay
// canonical state because it runs off the request path and publishes one indexed serving table.

const processIntegrityProjectionFilterSQL = `
FROM process_integrity_projection_rows
WHERE tenant_id = $1::uuid
  AND projection_version = (
    SELECT serving_projection_version
    FROM process_integrity_projection_state
    WHERE tenant_id = $1::uuid
      AND serving_projection_version IS NOT NULL
      AND serving_state = 'fresh'
      AND freshness_status = 'green'
  )
  AND ($15::text = '' OR category = $15::text)
  AND ($2::text = '' OR park_id = $2::text)
  AND ($3::text = '' OR shed_id = $3::text)
  AND ($4::timestamptz IS NULL OR due_at >= $4::timestamptz)
  AND due_at <= $5::timestamptz
  AND ($6::text = '' OR work_state = $6::text)
  AND ($7::text = '' OR severity = $7::text)
  AND ($8::text = '' OR owner_refs @> ARRAY[$8::text])
  AND ($9::text = '' OR protocol_version_id = $9::text)
  AND ($10::timestamptz IS NULL OR $10::timestamptz IS NOT NULL)
  AND ($12::text = '' OR row_id = $12::text)
  AND (NOT $13::boolean OR work_state IN ('rejected', 'blocked', 'overdue', 'proof_pending', 'verification_pending'))
  AND ($14::boolean OR work_state <> 'completed' OR due_at >= $11::timestamptz)
`

const processIntegrityProjectionRowsSQL = `
SELECT
  sort_priority,
  row_id,
  process_key,
  category,
  obligation_id,
  batch_id,
  sop_task_id,
  sop_task_row_version,
  sop_submission_id,
  completion_id,
  park_id,
  park_name,
  shed_id,
  shed_name,
  cohort_id,
  goat_id,
  animal_stage,
  protocol_id,
  protocol_version_id,
  rule_id,
  protocol_name,
  dose_code,
  drive_name,
  sop_version_id,
  proof_policy,
  due_at,
  window_start,
  window_end,
  expected_count,
  obligation_status,
  batch_status,
  sop_state,
  submission_state,
  proof_state,
  verification_state,
  completion_state,
  completed_count,
  proof_count,
  rejected_count,
  deferred_count,
  work_state,
  gap_type,
  severity,
  blocker_reason,
  owner_state,
  next_action,
  process_intact,
  operator_id,
  operator_name,
  park_head_id,
  park_head_name,
  verifier_id,
  verifier_name,
  escalation_owner_id,
  escalation_owner_name,
  proof_ids,
  evidence_count,
  latest_evidence_at,
  latest_rejection_reason,
  audit_ref
` + processIntegrityProjectionFilterSQL + `
  AND (
    $16::int < 0
    OR (sort_priority, due_at, row_id) > ($16::int, $17::timestamptz, $18::text)
  )
ORDER BY sort_priority ASC, due_at ASC, row_id ASC
LIMIT $19;
`

const processIntegrityProjectionCountsSQL = `
SELECT work_state, COALESCE(SUM(row_count), 0)::bigint
` + processIntegrityProjectionSummaryFilterSQL + `
GROUP BY work_state
ORDER BY work_state;
`

const processIntegrityProjectionAdherenceSummarySQL = `
SELECT
  COALESCE(SUM(expected_count), 0)::integer AS expected_count,
  COALESCE(SUM(completed_count), 0)::integer AS completed_count,
  COALESCE(SUM(row_count) FILTER (WHERE NOT process_intact), 0)::integer AS open_gap_count,
  COALESCE(SUM(deferred_count), 0)::integer AS deferred_count,
  COALESCE(SUM(row_count) FILTER (WHERE process_intact), 0)::integer AS process_intact_count
` + processIntegrityProjectionSummaryFilterSQL + `;
`

const processIntegrityProjectionSummaryFilterSQL = `
FROM process_integrity_projection_summaries
WHERE tenant_id = $1::uuid
  AND projection_version = (
    SELECT serving_projection_version
    FROM process_integrity_projection_state
    WHERE tenant_id = $1::uuid
      AND serving_projection_version IS NOT NULL
      AND serving_state = 'fresh'
      AND freshness_status = 'green'
  )
  AND ($15::text = '' OR category = $15::text)
  AND ($2::text = '' OR park_id = $2::text)
  AND ($3::text = '' OR shed_id = $3::text)
  AND ($4::timestamptz IS NULL OR due_business_date >= ($4::timestamptz AT TIME ZONE 'Asia/Kolkata')::date)
  AND due_business_date <= ($5::timestamptz AT TIME ZONE 'Asia/Kolkata')::date
  AND ($6::text = '' OR work_state = $6::text)
  AND ($7::text = '' OR severity = $7::text)
  AND (($8::text = '' AND owner_id = '') OR ($8::text <> '' AND owner_id = $8::text))
  AND ($9::text = '' OR protocol_version_id = $9::text)
  -- Keep the shared row/count query argument contract typed. as_of ($10) is enforced by the
  -- projection-freshness gate and row_id ($12) never reaches summary reads, but Postgres still
  -- requires types for every positional parameter below the highest referenced placeholder.
  AND ($10::timestamptz IS NULL OR $10::timestamptz IS NOT NULL)
  AND ($12::text = $12::text)
  AND (NOT $13::boolean OR work_state IN ('rejected', 'blocked', 'overdue', 'proof_pending', 'verification_pending'))
  AND ($14::boolean OR work_state <> 'completed' OR due_business_date >= ($11::timestamptz AT TIME ZONE 'Asia/Kolkata')::date)
`

const processIntegrityProjectionSummaryInsertSQL = `
WITH grains AS (
  SELECT
    tenant_id, projection_version, category, park_id, shed_id, ''::text AS owner_id,
    protocol_version_id, (due_at AT TIME ZONE 'Asia/Kolkata')::date AS due_business_date, work_state, severity, process_intact,
    expected_count, completed_count,
    CASE WHEN work_state = 'deferred' THEN GREATEST(deferred_count, 1) ELSE 0 END AS deferred_count
  FROM process_integrity_projection_rows
  WHERE tenant_id = $1::uuid AND projection_version = $2::bigint

  UNION ALL

  SELECT
    rows.tenant_id, rows.projection_version, rows.category, rows.park_id, rows.shed_id, owners.owner_id,
    rows.protocol_version_id, (rows.due_at AT TIME ZONE 'Asia/Kolkata')::date, rows.work_state, rows.severity, rows.process_intact,
    rows.expected_count, rows.completed_count,
    CASE WHEN rows.work_state = 'deferred' THEN GREATEST(rows.deferred_count, 1) ELSE 0 END
  FROM process_integrity_projection_rows rows
  CROSS JOIN LATERAL (
    SELECT DISTINCT owner_id
    FROM unnest(rows.owner_refs) owner_id
    WHERE owner_id <> ''
  ) owners
  WHERE rows.tenant_id = $1::uuid AND rows.projection_version = $2::bigint
)
INSERT INTO process_integrity_projection_summaries (
  tenant_id, projection_version, category, park_id, shed_id, owner_id,
  protocol_version_id, due_business_date, work_state, severity, process_intact,
  row_count, expected_count, completed_count, deferred_count, projected_at
)
SELECT
  tenant_id, projection_version, category, park_id, shed_id, owner_id,
  protocol_version_id, due_business_date, work_state, severity, process_intact,
  COUNT(*)::bigint, SUM(expected_count)::bigint, SUM(completed_count)::bigint,
  SUM(deferred_count)::bigint, $3::timestamptz
FROM grains
GROUP BY tenant_id, projection_version, category, park_id, shed_id, owner_id,
  protocol_version_id, due_business_date, work_state, severity, process_intact
ON CONFLICT (
  tenant_id, projection_version, category, park_id, shed_id, owner_id,
  protocol_version_id, due_business_date, work_state, severity, process_intact
) DO UPDATE SET
  row_count = EXCLUDED.row_count,
  expected_count = EXCLUDED.expected_count,
  completed_count = EXCLUDED.completed_count,
  deferred_count = EXCLUDED.deferred_count,
  projected_at = EXCLUDED.projected_at;
`

const processIntegrityProjectionInsertSQL = processIntegrityAllRowsSQL + `,
projection_args AS (
  SELECT
    $16::int AS cursor_sort,
    $17::timestamptz AS cursor_due,
    $18::text AS cursor_row,
    $19::int AS row_limit
)
INSERT INTO process_integrity_projection_rows (
  tenant_id,
  sort_priority,
  row_id,
  process_key,
  category,
  obligation_id,
  batch_id,
  sop_task_id,
  sop_task_row_version,
  sop_submission_id,
  completion_id,
  park_id,
  park_name,
  shed_id,
  shed_name,
  cohort_id,
  goat_id,
  animal_stage,
  protocol_id,
  protocol_version_id,
  rule_id,
  protocol_name,
  dose_code,
  drive_name,
  sop_version_id,
  proof_policy,
  due_at,
  window_start,
  window_end,
  expected_count,
  obligation_status,
  batch_status,
  sop_state,
  submission_state,
  proof_state,
  verification_state,
  completion_state,
  completed_count,
  proof_count,
  rejected_count,
  deferred_count,
  work_state,
  gap_type,
  severity,
  blocker_reason,
  owner_state,
  next_action,
  process_intact,
  operator_id,
  operator_name,
  park_head_id,
  park_head_name,
  verifier_id,
  verifier_name,
  escalation_owner_id,
  escalation_owner_name,
  owner_refs,
  proof_ids,
  evidence_count,
  latest_evidence_at,
  latest_rejection_reason,
  audit_ref,
  projection_version,
  projected_at,
  updated_at
)
SELECT
  $1::uuid,
  sort_priority,
  row_id,
  process_key,
  category,
  obligation_id,
  batch_id,
  sop_task_id,
  sop_task_row_version,
  sop_submission_id,
  completion_id,
  park_id,
  park_name,
  shed_id,
  shed_name,
  cohort_id,
  goat_id,
  animal_stage,
  protocol_id,
  protocol_version_id,
  rule_id,
  protocol_name,
  dose_code,
  drive_name,
  sop_version_id,
  proof_policy,
  due_at,
  window_start,
  window_end,
  expected_count,
  obligation_status,
  batch_status,
  sop_state,
  submission_state,
  proof_state,
  verification_state,
  completion_state,
  completed_count,
  proof_count,
  rejected_count,
  deferred_count,
  work_state,
  gap_type,
  severity,
  blocker_reason,
  owner_state,
  next_action,
  process_intact,
  operator_id,
  operator_name,
  park_head_id,
  park_head_name,
  verifier_id,
  verifier_name,
  escalation_owner_id,
  escalation_owner_name,
  array_remove(ARRAY[
    NULLIF(operator_id, ''),
    NULLIF(park_head_id, ''),
    NULLIF(verifier_id, ''),
    NULLIF(escalation_owner_id, '')
  ], NULL)::text[],
  proof_ids,
  evidence_count,
  latest_evidence_at,
  latest_rejection_reason,
  audit_ref,
  $20::bigint,
  $21::timestamptz,
  $21::timestamptz
FROM all_rows
CROSS JOIN projection_args
ON CONFLICT (tenant_id, projection_version, row_id) DO UPDATE SET
  sort_priority = EXCLUDED.sort_priority,
  process_key = EXCLUDED.process_key,
  category = EXCLUDED.category,
  obligation_id = EXCLUDED.obligation_id,
  batch_id = EXCLUDED.batch_id,
  sop_task_id = EXCLUDED.sop_task_id,
  sop_task_row_version = EXCLUDED.sop_task_row_version,
  sop_submission_id = EXCLUDED.sop_submission_id,
  completion_id = EXCLUDED.completion_id,
  park_id = EXCLUDED.park_id,
  park_name = EXCLUDED.park_name,
  shed_id = EXCLUDED.shed_id,
  shed_name = EXCLUDED.shed_name,
  cohort_id = EXCLUDED.cohort_id,
  goat_id = EXCLUDED.goat_id,
  animal_stage = EXCLUDED.animal_stage,
  protocol_id = EXCLUDED.protocol_id,
  protocol_version_id = EXCLUDED.protocol_version_id,
  rule_id = EXCLUDED.rule_id,
  protocol_name = EXCLUDED.protocol_name,
  dose_code = EXCLUDED.dose_code,
  drive_name = EXCLUDED.drive_name,
  sop_version_id = EXCLUDED.sop_version_id,
  proof_policy = EXCLUDED.proof_policy,
  due_at = EXCLUDED.due_at,
  window_start = EXCLUDED.window_start,
  window_end = EXCLUDED.window_end,
  expected_count = EXCLUDED.expected_count,
  obligation_status = EXCLUDED.obligation_status,
  batch_status = EXCLUDED.batch_status,
  sop_state = EXCLUDED.sop_state,
  submission_state = EXCLUDED.submission_state,
  proof_state = EXCLUDED.proof_state,
  verification_state = EXCLUDED.verification_state,
  completion_state = EXCLUDED.completion_state,
  completed_count = EXCLUDED.completed_count,
  proof_count = EXCLUDED.proof_count,
  rejected_count = EXCLUDED.rejected_count,
  deferred_count = EXCLUDED.deferred_count,
  work_state = EXCLUDED.work_state,
  gap_type = EXCLUDED.gap_type,
  severity = EXCLUDED.severity,
  blocker_reason = EXCLUDED.blocker_reason,
  owner_state = EXCLUDED.owner_state,
  next_action = EXCLUDED.next_action,
  process_intact = EXCLUDED.process_intact,
  operator_id = EXCLUDED.operator_id,
  operator_name = EXCLUDED.operator_name,
  park_head_id = EXCLUDED.park_head_id,
  park_head_name = EXCLUDED.park_head_name,
  verifier_id = EXCLUDED.verifier_id,
  verifier_name = EXCLUDED.verifier_name,
  escalation_owner_id = EXCLUDED.escalation_owner_id,
  escalation_owner_name = EXCLUDED.escalation_owner_name,
  owner_refs = EXCLUDED.owner_refs,
  proof_ids = EXCLUDED.proof_ids,
  evidence_count = EXCLUDED.evidence_count,
  latest_evidence_at = EXCLUDED.latest_evidence_at,
  latest_rejection_reason = EXCLUDED.latest_rejection_reason,
  audit_ref = EXCLUDED.audit_ref,
  projection_version = EXCLUDED.projection_version,
  projected_at = EXCLUDED.projected_at,
  updated_at = EXCLUDED.updated_at;
`
