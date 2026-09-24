package pgtest

import (
	"context"
	"strings"
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
	trace      []string
	open       map[*pgx.Conn]int
}

// Reset zeroes the counters, so a caller can warm a statement cache first and then measure.
func (c *RoundTrips) Reset() {
	c.trips.Store(0)
	c.statements.Store(0)
	c.mu.Lock()
	c.sql = nil
	c.trace = nil
	c.open = nil
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

// Trace lists every round trip since the last Reset, one line each: a single statement's
// first words, or "batch[n]:" and the first words of each statement it pipelined.
func (c *RoundTrips) Trace() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.trace...)
}

func (c *RoundTrips) note(conn *pgx.Conn, sql string, batched bool) {
	c.statements.Add(1)
	short := strings.Join(strings.Fields(sql), " ")
	if len(short) > 90 {
		short = short[:90]
	}
	if len(sql) > 120 {
		sql = sql[:120]
	}
	c.mu.Lock()
	c.sql = append(c.sql, sql)
	if i, ok := c.open[conn]; batched && ok {
		c.trace[i] += "\n      + " + short
	} else {
		c.trace = append(c.trace, short)
	}
	c.mu.Unlock()
}

func (c *RoundTrips) openBatch(conn *pgx.Conn) {
	c.mu.Lock()
	if c.open == nil {
		c.open = map[*pgx.Conn]int{}
	}
	c.open[conn] = len(c.trace)
	c.trace = append(c.trace, "batch:")
	c.mu.Unlock()
}

func (c *RoundTrips) TraceQueryStart(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	c.trips.Add(1)
	c.note(conn, data.SQL, false)
	return ctx
}

func (c *RoundTrips) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func (c *RoundTrips) TraceBatchStart(ctx context.Context, conn *pgx.Conn, _ pgx.TraceBatchStartData) context.Context {
	c.trips.Add(1)
	c.openBatch(conn)
	return ctx
}

func (c *RoundTrips) TraceBatchQuery(_ context.Context, conn *pgx.Conn, data pgx.TraceBatchQueryData) {
	c.note(conn, data.SQL, true)
}

func (c *RoundTrips) TraceBatchEnd(_ context.Context, conn *pgx.Conn, _ pgx.TraceBatchEndData) {
	c.mu.Lock()
	delete(c.open, conn)
	c.mu.Unlock()
}

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
