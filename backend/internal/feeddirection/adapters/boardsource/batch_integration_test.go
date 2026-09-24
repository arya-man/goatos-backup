package boardsource

import (
	"context"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/workboard/ports"
)

// roundTripTracer counts what reaches the wire: single statements and pgx batches.
type roundTripTracer struct{ queries, batches atomic.Int32 }

func (t *roundTripTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	t.queries.Add(1)
	return ctx
}
func (t *roundTripTracer) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}
func (t *roundTripTracer) TraceBatchStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceBatchStartData) context.Context {
	t.batches.Add(1)
	return ctx
}
func (t *roundTripTracer) TraceBatchQuery(context.Context, *pgx.Conn, pgx.TraceBatchQueryData) {}
func (t *roundTripTracer) TraceBatchEnd(context.Context, *pgx.Conn, pgx.TraceBatchEndData)     {}

// TestFeedActivityCardsAreOneRoundTripPerRequest: a board request (summary counts + every lane
// list) reads the four activity cards as ONE pgx batch on one connection -- not four parallel
// statements on four pool connections -- and the cards are identical to the per-card reads.
func TestFeedActivityCardsAreOneRoundTripPerRequest(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seed(t, ctx, pool)

	want, err := New(pool, 5*time.Second).ListRows(ctx, query(""))
	if err != nil {
		t.Fatal(err)
	}

	cfg := pool.Config().Copy()
	tr := &roundTripTracer{}
	cfg.ConnConfig.Tracer = tr
	traced, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer traced.Close()
	src := New(traced, 5*time.Second)

	reqCtx := ports.WithRequestReadMemo(ctx)
	if _, err := src.CountByState(reqCtx, query("")); err != nil {
		t.Fatal(err)
	}
	got, err := src.ListRows(reqCtx, query(""))
	if err != nil {
		t.Fatal(err)
	}
	if q, b := tr.queries.Load(), tr.batches.Load(); q != 0 || b != 1 {
		t.Fatalf("want 0 single statements + 1 batch per request, got %d statements + %d batches", q, b)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("batched cards differ:\n got %+v\nwant %+v", got, want)
	}
}

// TestFeedActivityListRowsWithoutRequestMemoIsOneBatch: a lane read with no request memo on ctx
// (Service.FindRow's keyset walk before a subtasks page) sends the card batch once, not once per
// activity.
func TestFeedActivityListRowsWithoutRequestMemoIsOneBatch(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seed(t, ctx, pool)

	want, err := New(pool, 5*time.Second).ListRows(ports.WithRequestReadMemo(ctx), query(""))
	if err != nil {
		t.Fatal(err)
	}
	cfg := pool.Config().Copy()
	tr := &roundTripTracer{}
	cfg.ConnConfig.Tracer = tr
	traced, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer traced.Close()
	got, err := New(traced, 5*time.Second).ListRows(ctx, query(""))
	if err != nil {
		t.Fatal(err)
	}
	if q, b := tr.queries.Load(), tr.batches.Load(); q != 0 || b != 1 {
		t.Fatalf("want 0 single statements + 1 batch, got %d statements + %d batches", q, b)
	}
	if len(got) == 0 || !reflect.DeepEqual(got, want) {
		t.Fatalf("rows differ or empty:\n got %+v\nwant %+v", got, want)
	}
}
