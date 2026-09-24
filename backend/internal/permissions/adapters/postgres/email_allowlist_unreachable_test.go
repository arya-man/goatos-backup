package postgres

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// unreachablePool dials a port nothing listens on; with a blackhole address the
// dial blocks until the context ends, which is what the deadline test needs.
func unreachablePool(t *testing.T, dsn string) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// A failed allowlist read must surface as an ERROR, never as "not allowed".
func TestAllowedEmailSourceReportsLoadFailureAsError(t *testing.T) {
	src := NewAllowedEmailSource(unreachablePool(t, "postgres://u:p@127.0.0.1:1/none?sslmode=disable&connect_timeout=1"),
		time.Second, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	allowed, err := src.EmailAllowedErr(context.Background(), "20000000-0000-4000-8000-000000000001", "a@mesha.sg")
	if err == nil || allowed {
		t.Fatalf("allowed=%v err=%v; want a load error", allowed, err)
	}
}

// The request's deadline governs the load, not only the source's own timeout.
func TestAllowedEmailSourceHonoursRequestDeadline(t *testing.T) {
	// 10.255.255.1 is non-routable: the dial hangs until the context ends.
	src := NewAllowedEmailSource(unreachablePool(t, "postgres://u:p@10.255.255.1:5432/none?sslmode=disable&connect_timeout=30"),
		10*time.Second, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := src.EmailAllowedErr(ctx, "20000000-0000-4000-8000-000000000001", "a@mesha.sg")
	if took := time.Since(start); took > 2*time.Second {
		t.Fatalf("load ignored the 200ms request deadline: took %s (err=%v)", took, err)
	}
	if err == nil {
		t.Fatal("want a deadline error")
	}
}
