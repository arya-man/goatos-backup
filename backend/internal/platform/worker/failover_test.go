package worker

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// onceWorkStage completes a single work item exactly once using an idempotent
// INSERT ... ON CONFLICT DO NOTHING keyed by the work id. Re-running the stage
// (replay, or a takeover after a peer already finished) does not duplicate the
// effect. It records completed_by so a takeover can be attributed.
type onceWorkStage struct {
	name     string
	pool     *pgxpool.Pool
	workID   string
	instance string
	runCount int
}

func (w *onceWorkStage) Run(ctx context.Context) error {
	w.runCount++
	_, err := w.pool.Exec(ctx, `
		INSERT INTO failover_work (id, completed_by)
		VALUES ($1, $2)
		ON CONFLICT (id) DO NOTHING
	`, w.workID, w.instance)
	return err
}

func (w *onceWorkStage) Name() string { return w.name }

// TestFailoverTakeoverIsIdempotent proves the HA failover contract (REQ-1):
//   - Instance A acquires the stage lock and crashes MID-stage (before doing the
//     work) — its lock-holding session is killed.
//   - A standby Supervisor (instance B, on an independent pool to the SAME db)
//     acquires the now-free lock and completes the work.
//   - No lost work: the work item is completed, attributed to B (A never wrote it).
//   - Exactly-once / no double-run: the idempotent write leaves exactly one row,
//     and a replay by B does not duplicate it.
func TestFailoverTakeoverIsIdempotent(t *testing.T) {
	t.Parallel()
	pgtest.SkipIfNoDocker(t)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	poolA := pgtest.StartPostgres(t, ctx)
	poolB := secondPoolSameDB(t, ctx, poolA)
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))

	if _, err := poolA.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS failover_work (
			id           TEXT PRIMARY KEY,
			completed_by TEXT NOT NULL,
			completed_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)
	`); err != nil {
		t.Fatalf("create test table: %v", err)
	}

	salt := int64(91003)
	workID := "drive-item-1"

	// --- Instance A: hold the stage lock, then crash BEFORE doing the work. ---
	connA, err := poolA.Acquire(ctx)
	if err != nil {
		t.Fatalf("A acquire conn: %v", err)
	}
	var locked bool
	if err := connA.QueryRow(ctx, "SELECT pg_try_advisory_lock($1)", salt).Scan(&locked); err != nil {
		t.Fatalf("A advisory lock: %v", err)
	}
	if !locked {
		t.Fatal("A should hold the stage lock")
	}

	// Work is not yet done while A holds the lock (A is mid-stage).
	var before int
	if err := poolA.QueryRow(ctx, `SELECT count(*) FROM failover_work WHERE id=$1`, workID).Scan(&before); err != nil {
		t.Fatalf("count before crash: %v", err)
	}
	if before != 0 {
		t.Fatalf("expected no work done before crash, found %d rows", before)
	}

	// Crash A mid-stage: kill its lock-holding session. Postgres frees the
	// advisory lock; the work item is left unfinished for B to complete.
	raw := connA.Hijack()
	_ = raw.Close(ctx)

	// --- Instance B: a real Supervisor takes over and completes the work. ---
	stage := &onceWorkStage{name: "failover-stage", pool: poolB, workID: workID, instance: "B"}
	supervisorB := NewSupervisor(logger, poolB, 1*time.Second)
	// Register with the SAME salt A held, so B contends for the exact lock.
	supervisorB.mu.Lock()
	supervisorB.continuous = append(supervisorB.continuous, CadenceStage{stage: stage, lockSalt: salt})
	supervisorB.mu.Unlock()

	runCtx, runCancel := context.WithTimeout(ctx, 10*time.Second)
	defer runCancel()
	if err := supervisorB.Run(runCtx); err != nil &&
		!errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("supervisor B run: %v", err)
	}

	if stage.runCount == 0 {
		t.Fatal("B did not run the stage — failover takeover failed")
	}

	// No lost work + takeover: exactly one row, completed by B.
	var count int
	if err := poolB.QueryRow(ctx, `SELECT count(*) FROM failover_work WHERE id=$1`, workID).Scan(&count); err != nil {
		t.Fatalf("count after takeover: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected exactly 1 work row after takeover, got %d (loss or duplication)", count)
	}
	var completedBy string
	if err := poolB.QueryRow(ctx, `SELECT completed_by FROM failover_work WHERE id=$1`, workID).Scan(&completedBy); err != nil {
		t.Fatalf("read completed_by: %v", err)
	}
	if completedBy != "B" {
		t.Fatalf("expected work completed_by B (takeover of A's unfinished work), got %q", completedBy)
	}

	// --- Idempotency: a replay must NOT duplicate the completed work. ---
	runCtx2, runCancel2 := context.WithTimeout(ctx, 10*time.Second)
	defer runCancel2()
	if err := supervisorB.Run(runCtx2); err != nil &&
		!errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("supervisor B replay run: %v", err)
	}
	var countAfterReplay int
	if err := poolB.QueryRow(ctx, `SELECT count(*) FROM failover_work WHERE id=$1`, workID).Scan(&countAfterReplay); err != nil {
		t.Fatalf("count after replay: %v", err)
	}
	if countAfterReplay != 1 {
		t.Fatalf("idempotency broken: expected 1 work row after replay, got %d", countAfterReplay)
	}
}
