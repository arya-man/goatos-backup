package postgres

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

type recordingTracer struct{ starts, ends, batchStarts, batchEnds int }

func (r *recordingTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	r.starts++
	return ctx
}
func (r *recordingTracer) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) { r.ends++ }
func (r *recordingTracer) TraceBatchStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceBatchStartData) context.Context {
	r.batchStarts++
	return ctx
}
func (r *recordingTracer) TraceBatchQuery(context.Context, *pgx.Conn, pgx.TraceBatchQueryData) {}
func (r *recordingTracer) TraceBatchEnd(context.Context, *pgx.Conn, pgx.TraceBatchEndData) {
	r.batchEnds++
}

// The tracer is installed ONLY when GOATOS_PG_QUERY_TRACE_FILE names a path; otherwise the inner
// (otel) tracer is returned untouched, so production pays nothing.
func TestQueryTracerIsOptIn(t *testing.T) {
	t.Setenv(queryTraceFileEnv, "")
	inner := &recordingTracer{}
	if got := newQueryTracer(inner); got != pgx.QueryTracer(inner) {
		t.Fatalf("without the env var the inner tracer must be returned as-is, got %T", got)
	}
}

// With the file set, every query and every batch writes ONE line (a batch is one round trip),
// and the inner tracer still sees every start/end so no otel span is lost.
func TestQueryTracerLogsOneLinePerRoundTripAndForwards(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trace.log")
	t.Setenv(queryTraceFileEnv, path)
	inner := &recordingTracer{}
	tr, ok := newQueryTracer(inner).(*queryTracer)
	if !ok {
		t.Fatalf("expected the file tracer to be installed")
	}
	ctx := tr.TraceQueryStart(context.Background(), nil, pgx.TraceQueryStartData{SQL: "SELECT   1\n  FROM   x"})
	tr.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{})
	batch := &pgx.Batch{}
	batch.Queue("SELECT 1")
	batch.Queue("SELECT 2")
	bctx := tr.TraceBatchStart(context.Background(), nil, pgx.TraceBatchStartData{Batch: batch})
	tr.TraceBatchQuery(bctx, nil, pgx.TraceBatchQueryData{})
	tr.TraceBatchEnd(bctx, nil, pgx.TraceBatchEndData{})
	// The remaining interfaces forward without panicking when the inner tracer lacks them.
	_ = tr.TraceCopyFromStart(context.Background(), nil, pgx.TraceCopyFromStartData{})
	tr.TraceCopyFromEnd(context.Background(), nil, pgx.TraceCopyFromEndData{})
	_ = tr.TracePrepareStart(context.Background(), nil, pgx.TracePrepareStartData{})
	tr.TracePrepareEnd(context.Background(), nil, pgx.TracePrepareEndData{})
	_ = tr.TraceConnectStart(context.Background(), pgx.TraceConnectStartData{})
	tr.TraceConnectEnd(context.Background(), pgx.TraceConnectEndData{})

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) != 2 {
		t.Fatalf("want 2 trace lines (one query, one batch), got %d: %q", len(lines), raw)
	}
	if !strings.HasSuffix(lines[0], "\tSELECT 1 FROM x") {
		t.Fatalf("query line must carry the whitespace-collapsed SQL, got %q", lines[0])
	}
	if !strings.HasSuffix(lines[1], "\tBATCH(2 statements)") {
		t.Fatalf("batch line must count statements, got %q", lines[1])
	}
	if inner.starts != 1 || inner.ends != 1 || inner.batchStarts != 1 || inner.batchEnds != 1 {
		t.Fatalf("inner tracer must see every start/end: %+v", inner)
	}
}
