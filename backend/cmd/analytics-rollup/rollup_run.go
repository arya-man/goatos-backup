package main

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// startRollupRun inserts the audit row for this invocation and returns its
// run_id. Recorded before any BigQuery/Postgres rollup work so a crash mid-run
// still leaves a "running" row behind for on-call triage (status is later
// closed out by finishRollupRun).
func startRollupRun(ctx context.Context, pool *pgxpool.Pool, sourceDate time.Time) (int64, error) {
	var runID int64
	err := pool.QueryRow(ctx, `
		INSERT INTO analytics.rollup_run (source_date, started_at, status)
		VALUES ($1, now(), 'running')
		RETURNING run_id
	`, sourceDate).Scan(&runID)
	return runID, err
}

// finishRollupRun closes out the audit row with the run's outcome. status
// must be one of succeeded/failed/skipped (enforced by the
// rollup_run_status_check constraint). errMsg is stored verbatim (truncated
// by the caller if needed) for failed runs; nil for succeeded/skipped.
func finishRollupRun(ctx context.Context, pool *pgxpool.Pool, runID int64, status string, rowsWritten, bytesBilled int64, runErr error) error {
	var errMsg *string
	if runErr != nil {
		msg := runErr.Error()
		errMsg = &msg
	}
	_, err := pool.Exec(ctx, `
		UPDATE analytics.rollup_run
		SET finished_at = now(), rows_written = $2, bytes_billed = $3, status = $4, error = $5
		WHERE run_id = $1
	`, runID, rowsWritten, bytesBilled, status, errMsg)
	return err
}
