package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/sqlbind"
	"github.com/vgoats/goatos/backend/internal/workboard/ports"
)

// StatementBatch sends the board sources' own statements as one pgx.Batch: one round trip
// for all of them. It reads no table of its own; every statement is a source's SQL.
type StatementBatch struct {
	pool    *pgxpool.Pool
	timeout time.Duration
}

// NewStatementBatch constructs the batch runner.
func NewStatementBatch(pool *pgxpool.Pool, timeout time.Duration) *StatementBatch {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	return &StatementBatch{pool: pool, timeout: timeout}
}

// RunBatch implements ports.StatementBatch.
func (b *StatementBatch) RunBatch(ctx context.Context, stmts []ports.Statement) error {
	if len(stmts) == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, b.timeout)
	defer cancel()
	batch := &pgx.Batch{}
	for _, st := range stmts {
		bound := sqlbind.MustBind(st.Query.SQL(), st.Query.Args()...)
		batch.Queue(bound.SQL(), bound.Args()...)
	}
	br := b.pool.SendBatch(ctx, batch)
	defer br.Close()
	for i, st := range stmts {
		// scale-guard:ignore: drains the ONE batch's queued results in order; no round trip per iteration.
		rows, err := br.Query()
		if err != nil {
			return fmt.Errorf("workboard batch statement %d: %w", i, err)
		}
		readErr := st.Read(rows)
		rows.Close()
		if readErr != nil {
			return fmt.Errorf("workboard batch statement %d: %w", i, readErr)
		}
		if err := rows.Err(); err != nil {
			return fmt.Errorf("workboard batch statement %d: %w", i, err)
		}
	}
	return br.Close()
}
