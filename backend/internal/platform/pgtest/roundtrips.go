package pgtest

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// RoundTrips counts the client->server round trips a read makes: every Query/QueryRow/Exec
// outside a batch is one, and every SendBatch is one no matter how many statements it queues
// (pgx pipelines a batch as one message train with one Sync). It is the P10 proof: a request
// path's round-trip count, asserted in a test, cannot silently creep back up.
type RoundTrips struct {
	trips      atomic.Int64
	statements atomic.Int64
	mu         sync.Mutex
	sql        []string
}

// Reset zeroes the counters, so a caller can warm a statement cache first and then measure.
func (c *RoundTrips) Reset() {
	c.trips.Store(0)
	c.statements.Store(0)
	c.mu.Lock()
	c.sql = nil
	c.mu.Unlock()
}

// Trips is the number of round trips since the last Reset.
func (c *RoundTrips) Trips() int { return int(c.trips.Load()) }

// Statements is the number of SQL statements (batched or not) since the last Reset.
func (c *RoundTrips) Statements() int { return int(c.statements.Load()) }

// SQL lists the statements seen since the last Reset, for failure messages.
func (c *RoundTrips) SQL() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.sql...)
}

func (c *RoundTrips) note(sql string) {
	c.statements.Add(1)
	if len(sql) > 120 {
		sql = sql[:120]
	}
	c.mu.Lock()
	c.sql = append(c.sql, sql)
	c.mu.Unlock()
}

func (c *RoundTrips) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	c.trips.Add(1)
	c.note(data.SQL)
	return ctx
}

func (c *RoundTrips) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func (c *RoundTrips) TraceBatchStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceBatchStartData) context.Context {
	c.trips.Add(1)
	return ctx
}

func (c *RoundTrips) TraceBatchQuery(_ context.Context, _ *pgx.Conn, data pgx.TraceBatchQueryData) {
	c.note(data.SQL)
}

func (c *RoundTrips) TraceBatchEnd(context.Context, *pgx.Conn, pgx.TraceBatchEndData) {}

// CountingPool opens a second pool on the same database as `pool` whose every connection
// reports to the returned RoundTrips. The pool is closed on test cleanup.
func CountingPool(t *testing.T, ctx context.Context, pool *pgxpool.Pool) (*pgxpool.Pool, *RoundTrips) {
	t.Helper()
	counter := &RoundTrips{}
	cfg := pool.Config().Copy()
	cfg.ConnConfig.Tracer = counter
	counted, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("pgtest: counting pool: %v", err)
	}
	t.Cleanup(counted.Close)
	return counted, counter
}
