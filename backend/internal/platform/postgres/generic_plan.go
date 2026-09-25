package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// Generic-plan reads.
//
// The pool runs pgx's default QueryExecModeCacheStatement, so every statement is prepared once per
// connection. Postgres' plan_cache_mode=auto then keeps building a fresh CUSTOM plan on every
// execution whenever the generic plan's *estimated* cost is higher than the custom plans' average --
// which is always the case for the big catch-all canonical CTEs (`$n IS NULL OR ...` guards). For the
// calendar single-event statements that meant ~60 ms of planning on every request for ~40 ms of
// execution, and ~80 ms on the first five calls of the calendar list on each connection.
//
// For a small, measured allow-list of read statements whose generic plan executes as fast as the
// custom one (checked on STG with EXPLAIN ANALYZE EXECUTE under force_custom_plan vs
// force_generic_plan, results compared row-for-row), GenericPlanReadTx forces the
// generic plan for exactly one read-only transaction. The GUC is SET LOCAL, so it never leaks onto
// the pooled connection. BEGIN and SET LOCAL travel in one simple-protocol round trip.
//
// Do NOT add statements here without measuring: for the process-integrity canonical CTE and the
// command-board shed-dose matrix and the calendar drive-targets list the generic plan is 2-3x SLOWER
// than a custom plan, which is why those call sites pin a custom plan per call
// (pgx.QueryExecModeExec / QueryExecModeDescribeExec).
//
// Schema changes: a migration that changes a statement's result type makes the next EXECUTE of the
// cached statement fail with "cached plan must not change result type"; pgx invalidates the cached
// statement on any error (rows.Close), so exactly one request per connection fails and the next one
// re-prepares. This is the same behaviour every default-mode statement in the backend already has.
var GenericPlanReadTx = pgx.TxOptions{
	AccessMode: pgx.ReadOnly,
	BeginQuery: "BEGIN READ ONLY; SET LOCAL plan_cache_mode = force_generic_plan",
}

// TxBeginner is satisfied by *pgxpool.Pool and *pgx.Conn.
type TxBeginner interface {
	BeginTx(ctx context.Context, txOptions pgx.TxOptions) (pgx.Tx, error)
}

// WithGenericPlanReadTx runs fn inside one GenericPlanReadTx transaction and always ends it (rolled
// back -- the transaction is read-only). fn must fully consume and close any rows it opens.
func WithGenericPlanReadTx(ctx context.Context, db TxBeginner, fn func(pgx.Tx) error) error {
	tx, err := db.BeginTx(ctx, GenericPlanReadTx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	return fn(tx)
}
