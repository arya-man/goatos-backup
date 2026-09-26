package postgres

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
)

// queryTraceFile is an opt-in, local-only per-statement log used to count DB
// round trips per request while tuning latency (tools/perf). It is enabled ONLY
// when GOATOS_PG_QUERY_TRACE_FILE names a writable path; production never sets
// it and pays nothing (the tracer is not installed). One line per statement:
// unix_ms<TAB>duration_ms<TAB>first 160 chars of SQL (collapsed whitespace).
const queryTraceFileEnv = "GOATOS_PG_QUERY_TRACE_FILE"

type queryTraceKey struct{}

type queryTracer struct {
	inner pgx.QueryTracer
	mu    sync.Mutex
	out   *os.File
}

func newQueryTracer(inner pgx.QueryTracer) pgx.QueryTracer {
	path := strings.TrimSpace(os.Getenv(queryTraceFileEnv))
	if path == "" {
		return inner
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return inner
	}
	return &queryTracer{inner: inner, out: f}
}

func (t *queryTracer) TraceQueryStart(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if t.inner != nil {
		ctx = t.inner.TraceQueryStart(ctx, conn, data)
	}
	return context.WithValue(ctx, queryTraceKey{}, &queryTraceEntry{start: time.Now(), sql: data.SQL})
}

type queryTraceEntry struct {
	start time.Time
	sql   string
}

func (t *queryTracer) TraceQueryEnd(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryEndData) {
	if t.inner != nil {
		t.inner.TraceQueryEnd(ctx, conn, data)
	}
	t.logEnd(ctx)
}

// logEnd writes the line for whatever TraceQueryStart/TraceBatchStart stamped on ctx. It is
// separate from TraceQueryEnd so a batch end never forwards a spurious query end to the inner
// (otel) tracer.
func (t *queryTracer) logEnd(ctx context.Context) {
	entry, _ := ctx.Value(queryTraceKey{}).(*queryTraceEntry)
	if entry == nil {
		return
	}
	sql := strings.Join(strings.Fields(entry.sql), " ")
	if len(sql) > 160 {
		sql = sql[:160]
	}
	line := fmt.Sprintf("%d\t%.1f\t%s\n", time.Now().UnixMilli(), float64(time.Since(entry.start).Microseconds())/1000, sql)
	t.mu.Lock()
	_, _ = t.out.WriteString(line)
	t.mu.Unlock()
}

// The remaining pgx tracer interfaces are forwarded so wrapping otelpgx loses
// none of its spans. Batches are logged as ONE line per batch (one round trip)
// so the trace counts trips, not statements.

func (t *queryTracer) TraceBatchStart(ctx context.Context, conn *pgx.Conn, data pgx.TraceBatchStartData) context.Context {
	if bt, ok := t.inner.(pgx.BatchTracer); ok {
		ctx = bt.TraceBatchStart(ctx, conn, data)
	}
	n := 0
	if data.Batch != nil {
		n = data.Batch.Len()
	}
	return context.WithValue(ctx, queryTraceKey{}, &queryTraceEntry{start: time.Now(), sql: fmt.Sprintf("BATCH(%d statements)", n)})
}

func (t *queryTracer) TraceBatchQuery(ctx context.Context, conn *pgx.Conn, data pgx.TraceBatchQueryData) {
	if bt, ok := t.inner.(pgx.BatchTracer); ok {
		bt.TraceBatchQuery(ctx, conn, data)
	}
}

func (t *queryTracer) TraceBatchEnd(ctx context.Context, conn *pgx.Conn, data pgx.TraceBatchEndData) {
	if bt, ok := t.inner.(pgx.BatchTracer); ok {
		bt.TraceBatchEnd(ctx, conn, data)
	}
	t.logEnd(ctx)
}

func (t *queryTracer) TraceCopyFromStart(ctx context.Context, conn *pgx.Conn, data pgx.TraceCopyFromStartData) context.Context {
	if ct, ok := t.inner.(pgx.CopyFromTracer); ok {
		return ct.TraceCopyFromStart(ctx, conn, data)
	}
	return ctx
}

func (t *queryTracer) TraceCopyFromEnd(ctx context.Context, conn *pgx.Conn, data pgx.TraceCopyFromEndData) {
	if ct, ok := t.inner.(pgx.CopyFromTracer); ok {
		ct.TraceCopyFromEnd(ctx, conn, data)
	}
}

func (t *queryTracer) TracePrepareStart(ctx context.Context, conn *pgx.Conn, data pgx.TracePrepareStartData) context.Context {
	if pt, ok := t.inner.(pgx.PrepareTracer); ok {
		return pt.TracePrepareStart(ctx, conn, data)
	}
	return ctx
}

func (t *queryTracer) TracePrepareEnd(ctx context.Context, conn *pgx.Conn, data pgx.TracePrepareEndData) {
	if pt, ok := t.inner.(pgx.PrepareTracer); ok {
		pt.TracePrepareEnd(ctx, conn, data)
	}
}

func (t *queryTracer) TraceConnectStart(ctx context.Context, data pgx.TraceConnectStartData) context.Context {
	if ct, ok := t.inner.(pgx.ConnectTracer); ok {
		return ct.TraceConnectStart(ctx, data)
	}
	return ctx
}

func (t *queryTracer) TraceConnectEnd(ctx context.Context, data pgx.TraceConnectEndData) {
	if ct, ok := t.inner.(pgx.ConnectTracer); ok {
		ct.TraceConnectEnd(ctx, data)
	}
}
