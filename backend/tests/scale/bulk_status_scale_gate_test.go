//go:build scale_kernel

// Package scale hosts the opt-in high-scale kernel gate. It is excluded from the
// default build/test set by the `scale_kernel` build tag and is meant to be run
// deliberately (see `make scale-kernel-gate`), because it seeds and drains a
// 1,000,000-row bulk_status_job against a throwaway Postgres.
//
// This 1,000,000-row run is the FUTURE 1-5M-animal certification bar per
// docs/decisions/operational-kernel-5k-50k-scale-envelope.md, not the current
// release requirement (the present release envelope is 5,000-50,000 animals, with
// query-plan proof at the ~500k obligation-row upper bound). It stays opt-in and is
// run deliberately; GOATOS_SCALE_GATE_ROWS scales it down for a quick local smoke.
//
// It proves, at scale, the durable bulk status-update kernel invariants:
//   - ZERO double-apply (each goat advances exactly one row_version; one event id per row);
//   - ZERO missing outbox events for applied rows;
//   - ZERO rows stuck in pending|claimed|retry after drain;
//   - a crash/restart mid-drain re-claims pending|retry (+ lease-expired claimed) and finishes clean;
//   - duplicate-delivery/replay of a commit is idempotent (no new job, no double-apply);
//   - DB locks stay bounded (no lock explosion proportional to row count);
//   - the tenant throttle actually bounds dispatch rate AND concurrency;
//
// and MEASURES + prints drain time, throughput, outbox/recompute backlog and the
// peak lock count.
//
// Assertions are never weakened to pass. Tunables (row count, concurrency, batch,
// timeout) come from env so the same gate runs at 1M in a full certification and
// at a smaller size for a quick local smoke; the default is the literal 1,000,000.
package scale

import (
	"bytes"
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	bulkpg "github.com/vgoats/goatos/backend/internal/bulkstatus/adapters/postgres"
	bulkapp "github.com/vgoats/goatos/backend/internal/bulkstatus/app"
	"github.com/vgoats/goatos/backend/internal/bulkstatus/identitybridge"
	identitypg "github.com/vgoats/goatos/backend/internal/identity/adapters/postgres"
	identityapp "github.com/vgoats/goatos/backend/internal/identity/app"
)

const (
	scaleTenant = "00000000-0000-4000-8000-000000000001" // seeded Mesha tenant
	scaleParty  = "00000000-0000-4000-8000-000000001001" // seeded Mesha party
	scaleActor  = "90000000-0000-4000-8000-000000000101" // enqueuing operator (no FK)
	targetRepro = "pregnant"
	startRepro  = "non_pregnant"

	// Real commit idempotency identity, set by the initial EnqueueJob so the
	// replay/duplicate-delivery assertions exercise the true ON CONFLICT path.
	scaleCommitIdemKey     = "scale-gate-commit-0001"
	scaleCommitFingerprint = "scale-gate-fp"
)

func envInt(key string, def int) int {
	if raw := strings.TrimSpace(os.Getenv(key)); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 {
			return v
		}
	}
	return def
}

func envDur(key string, def time.Duration) time.Duration {
	if raw := strings.TrimSpace(os.Getenv(key)); raw != "" {
		if v, err := time.ParseDuration(raw); err == nil && v > 0 {
			return v
		}
	}
	return def
}

func TestBulkStatusKernelScaleGate(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not available")
	}
	rowCount := envInt("GOATOS_SCALE_GATE_ROWS", 1_000_000)
	concurrency := envInt("GOATOS_SCALE_GATE_CONCURRENCY", 32)
	batchSize := envInt("GOATOS_SCALE_GATE_BATCH", 2000)
	overallTimeout := envDur("GOATOS_SCALE_GATE_TIMEOUT", 60*time.Minute)

	ctx := context.Background()
	container := startTunedPostgres(t)
	pool := openPool(t, ctx, container, int32(concurrency+6))
	defer pool.Close()
	// Separate small pool for the lock sampler so it is never starved by the worker.
	obsPool := openPool(t, ctx, container, 2)
	defer obsPool.Close()

	t.Logf("scale gate: rows=%d concurrency=%d batch=%d", rowCount, concurrency, batchSize)

	// --- seed N goats (COPY) --------------------------------------------------
	seedStart := time.Now()
	copyGoats(t, ctx, pool, rowCount)
	t.Logf("seeded %d goats via COPY in %s", rowCount, time.Since(seedStart).Round(time.Millisecond))

	// --- enqueue one durable job with one row per goat (set-based) ------------
	enqStart := time.Now()
	jobID, total := enqueueReproductiveJob(t, ctx, pool, scaleTenant, scaleActor, targetRepro)
	if total != rowCount {
		t.Fatalf("enqueued %d rows, want %d", total, rowCount)
	}
	t.Logf("enqueued job %s with %d rows in %s", jobID, total, time.Since(enqStart).Round(time.Millisecond))

	baselineLocks := scalarInt(t, ctx, obsPool, `SELECT count(*) FROM pg_locks`)

	// --- duplicate-delivery / replay: re-enqueue same key+fingerprint ---------
	assertCommitReplayIsIdempotent(t, ctx, pool, jobID, total)

	// --- drain with a simulated crash + resume --------------------------------
	worker := func(c int, lease time.Duration) *bulkapp.WorkerService {
		bulkRepo := bulkpg.NewRepository(pool, 30*time.Second)
		idSvc := identityapp.NewService(identitypg.NewRepository(pool, 30*time.Second))
		return bulkapp.NewWorkerService(bulkRepo, identitybridge.New(idSvc), bulkapp.WorkerSettings{
			Concurrency: c, RowsPerSecond: 0, Burst: 1, BatchSize: batchSize,
			MaxRetries: 5, MaxIterations: rowCount, ClaimLease: lease,
		}, discardLogger())
	}

	// Phase A: crash. Run a worker with a short lease under a short deadline so it
	// applies only a portion, then abandons in-flight claims (context canceled).
	crashCtx, cancelCrash := context.WithTimeout(ctx, 5*time.Second)
	_, _ = worker(concurrency, 5*time.Second).RunUntilDrained(crashCtx, scaleTenant, jobID)
	cancelCrash()
	appliedAfterCrash := scalarInt(t, ctx, pool, `SELECT count(*) FROM bulk_status_job_row WHERE job_id=$1::uuid AND row_state='applied'`, jobID)
	remainingAfterCrash := scalarInt(t, ctx, pool, `SELECT count(*) FROM bulk_status_job_row WHERE job_id=$1::uuid AND row_state IN ('pending','retry','claimed')`, jobID)
	stuckClaimed := scalarInt(t, ctx, pool, `SELECT count(*) FROM bulk_status_job_row WHERE job_id=$1::uuid AND row_state='claimed'`, jobID)
	t.Logf("crash phase: applied=%d remaining=%d (stuck-claimed=%d)", appliedAfterCrash, remainingAfterCrash, stuckClaimed)
	if appliedAfterCrash == 0 {
		t.Fatalf("crash phase applied nothing; cannot prove resume (throughput too low for a 5s slice at rows=%d)", rowCount)
	}
	if remainingAfterCrash == 0 {
		t.Fatalf("crash phase drained everything; increase GOATOS_SCALE_GATE_ROWS to exercise resume")
	}

	// Wait past the claim lease so the resuming worker can reclaim any rows left
	// 'claimed' by the crashed worker (resume truth is the row_state ledger).
	time.Sleep(6 * time.Second)

	// Phase B: resume + finish. Sample pg_locks concurrently to bound lock growth.
	stopSampler := make(chan struct{})
	var maxLocks int64
	var samplerWG sync.WaitGroup
	samplerWG.Add(1)
	go func() {
		defer samplerWG.Done()
		ticker := time.NewTicker(50 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stopSampler:
				return
			case <-ticker.C:
				var n int64
				if err := obsPool.QueryRow(context.Background(), `SELECT count(*) FROM pg_locks`).Scan(&n); err == nil {
					for {
						cur := atomic.LoadInt64(&maxLocks)
						if n <= cur || atomic.CompareAndSwapInt64(&maxLocks, cur, n) {
							break
						}
					}
				}
			}
		}
	}()

	resumeCtx, cancelResume := context.WithTimeout(ctx, overallTimeout)
	defer cancelResume()
	drainStart := time.Now()
	drain, err := worker(concurrency, 5*time.Second).RunUntilDrained(resumeCtx, scaleTenant, jobID)
	drainElapsed := time.Since(drainStart)
	close(stopSampler)
	samplerWG.Wait()
	if err != nil {
		t.Fatalf("resume drain: %v", err)
	}

	// --- correctness assertions (scoped to the job) ---------------------------
	if drain.FinalCounts.State != bulkapp.JobStateCompleted || drain.FinalCounts.Remaining != 0 {
		t.Fatalf("job not completed cleanly: %+v", drain.FinalCounts)
	}
	applied := scalarInt(t, ctx, pool, `SELECT count(*) FROM bulk_status_job_row WHERE job_id=$1::uuid AND row_state='applied'`, jobID)
	if applied != rowCount {
		var reason string
		_ = pool.QueryRow(ctx, `SELECT COALESCE(failure_reason,'') FROM bulk_status_job_row WHERE job_id=$1::uuid AND row_state<>'applied' LIMIT 1`, jobID).Scan(&reason)
		t.Fatalf("applied=%d want %d (first non-applied failure_reason=%q)", applied, rowCount, reason)
	}
	if stuck := scalarInt(t, ctx, pool, `SELECT count(*) FROM bulk_status_job_row WHERE job_id=$1::uuid AND row_state IN ('pending','claimed','retry')`, jobID); stuck != 0 {
		t.Fatalf("ZERO-stuck violated: %d rows still pending|claimed|retry", stuck)
	}
	if failed := scalarInt(t, ctx, pool, `SELECT count(*) FROM bulk_status_job_row WHERE job_id=$1::uuid AND row_state IN ('error','skipped')`, jobID); failed != 0 {
		t.Fatalf("expected 0 error/skipped rows, got %d", failed)
	}
	if missing := scalarInt(t, ctx, pool, `SELECT count(*) FROM bulk_status_job_row WHERE job_id=$1::uuid AND event_id IS NULL`, jobID); missing != 0 {
		t.Fatalf("expected event_id on every applied row, %d missing", missing)
	}
	if distinct := scalarInt(t, ctx, pool, `SELECT count(DISTINCT event_id) FROM bulk_status_job_row WHERE job_id=$1::uuid`, jobID); distinct != rowCount {
		t.Fatalf("expected %d distinct event ids (one per row), got %d", rowCount, distinct)
	}
	events := scalarInt(t, ctx, pool, `
SELECT count(*) FROM goat_identity_events e
JOIN bulk_status_job_row r ON r.event_id = e.identity_event_id
WHERE r.job_id=$1::uuid AND e.event_type='goat.reproductive.changed'`, jobID)
	if events != rowCount {
		t.Fatalf("expected %d reproductive events, got %d", rowCount, events)
	}
	outbox := scalarInt(t, ctx, pool, `
SELECT count(*) FROM outbox_messages o
JOIN bulk_status_job_row r ON r.event_id = o.event_id
WHERE r.job_id=$1::uuid`, jobID)
	if outbox != rowCount {
		t.Fatalf("ZERO-missing-outbox violated: %d outbox messages for %d applied rows", outbox, rowCount)
	}
	// No double-apply: every goat advanced exactly one row_version to the target.
	atTarget := scalarInt(t, ctx, pool, `SELECT count(*) FROM goats WHERE tenant_id=$1::uuid AND reproductive_status=$2 AND row_version=2`, scaleTenant, targetRepro)
	if atTarget != rowCount {
		t.Fatalf("expected %d goats at target with row_version=2, got %d", rowCount, atTarget)
	}
	if over := scalarInt(t, ctx, pool, `SELECT count(*) FROM goats WHERE tenant_id=$1::uuid AND row_version>2`, scaleTenant); over != 0 {
		t.Fatalf("DOUBLE-APPLY detected: %d goats have row_version>2", over)
	}

	// --- bounded locks --------------------------------------------------------
	// The invariant is that lock count is a function of the claim batch size (a
	// fixed operational knob) and worker concurrency, NOT of the total row count:
	// a claim briefly FOR-UPDATE-SKIP-LOCKs up to batchSize rows, and each of the
	// `concurrency` in-flight applies holds a single-goat FOR UPDATE. So the bound
	// scales with batch/concurrency, and must stay far below the row count.
	peakLocks := atomic.LoadInt64(&maxLocks)
	lockBound := int64(3*batchSize + 200*concurrency + 3000)
	if peakLocks >= lockBound {
		t.Fatalf("lock explosion: peak pg_locks=%d (batch-relative bound %d)", peakLocks, lockBound)
	}
	if int64(rowCount) > lockBound && peakLocks >= int64(rowCount) {
		t.Fatalf("locks scaled with row count: peak=%d rows=%d", peakLocks, rowCount)
	}

	// --- measurements ---------------------------------------------------------
	throughput := float64(rowCount) / drainElapsed.Seconds()
	// The recompute backlog is the set of domain events the worker produced that
	// downstream vaccination/obligation recompute must still consume (outbox depth).
	recomputeBacklog := scalarInt(t, ctx, pool, `
SELECT count(*) FROM outbox_messages o
JOIN bulk_status_job_row r ON r.event_id = o.event_id
WHERE r.job_id=$1::uuid AND o.status='pending'`, jobID)
	t.Logf("=== SCALE GATE MEASUREMENTS ===")
	t.Logf("rows applied:            %d", applied)
	t.Logf("resume drain time:       %s (crash+resume, excludes seed/enqueue)", drainElapsed.Round(time.Millisecond))
	t.Logf("apply throughput:        %.0f rows/sec", throughput)
	t.Logf("outbox events produced:  %d (drained into outbox in the same tx as each apply)", outbox)
	t.Logf("recompute backlog:       %d pending outbox messages awaiting downstream recompute", recomputeBacklog)
	t.Logf("peak DB locks:           %d (baseline %d, batch-relative bound %d, rows %d) -> no lock explosion", peakLocks, baselineLocks, lockBound, rowCount)
	t.Logf("crash/resume:            applied %d before crash, resumed and finished the remaining %d", appliedAfterCrash, rowCount-appliedAfterCrash)

	// --- throttle / rate control verification ---------------------------------
	verifyConcurrencyCap(t, ctx, pool)
	verifyRateCap(t, ctx, pool)
}

// assertCommitReplayIsIdempotent re-runs EnqueueJob with the SAME
// (tenant, idempotency_key, fingerprint) as the seeded job and proves no new job
// and no extra rows are created (duplicate-delivery is a no-op). It also proves a
// same-key different-fingerprint replay is rejected.
func assertCommitReplayIsIdempotent(t *testing.T, ctx context.Context, pool *pgxpool.Pool, jobID string, total int) {
	t.Helper()
	// The initial EnqueueJob already set scaleCommitIdemKey + fingerprint, so this
	// re-enqueue exercises the real ON CONFLICT DO NOTHING -> replayEnqueue path
	// (no retrofit). Dummy row is never inserted because replay commits without
	// insertJobRows.
	repo := bulkpg.NewRepository(pool, 30*time.Second)
	replay, err := repo.EnqueueJob(ctx, bulkapp.EnqueueJobParams{
		TenantID: scaleTenant, ActorID: scaleActor, Axis: bulkapp.AxisReproductive,
		IdempotencyKey: scaleCommitIdemKey, Fingerprint: scaleCommitFingerprint,
		Rows: []bulkapp.EnqueueRow{{GoatID: "10000000-0000-4000-8000-000000009999", Target: targetRepro}},
	})
	if err != nil {
		t.Fatalf("replay enqueue: %v", err)
	}
	if !replay.Replayed || replay.JobID != jobID {
		t.Fatalf("duplicate delivery must return the original job, got %+v", replay)
	}
	if rows := scalarInt(t, ctx, pool, `SELECT count(*) FROM bulk_status_job_row WHERE job_id=$1::uuid`, jobID); rows != total {
		t.Fatalf("duplicate delivery added rows: %d != %d", rows, total)
	}
	if jobs := scalarInt(t, ctx, pool, `SELECT count(*) FROM bulk_status_job WHERE tenant_id=$1::uuid AND idempotency_key=$2`, scaleTenant, scaleCommitIdemKey); jobs != 1 {
		t.Fatalf("duplicate delivery created a second job: %d", jobs)
	}
	if _, err := repo.EnqueueJob(ctx, bulkapp.EnqueueJobParams{
		TenantID: scaleTenant, ActorID: scaleActor, Axis: bulkapp.AxisReproductive,
		IdempotencyKey: scaleCommitIdemKey, Fingerprint: "scale-gate-fp-DIFFERENT",
		Rows: []bulkapp.EnqueueRow{{GoatID: "10000000-0000-4000-8000-000000009999", Target: targetRepro}},
	}); err == nil {
		t.Fatal("same key + different fingerprint must be rejected")
	}
}

// instrumentedApplier records peak concurrency without touching goats, so the
// WorkerService throttle knobs (concurrency cap + dispatch rate) can be verified
// against the REAL claim/mark repo path deterministically.
type instrumentedApplier struct {
	inFlight    int64
	maxInFlight int64
	work        time.Duration
}

func (a *instrumentedApplier) ApplyBulkStatusRow(_ context.Context, _ bulkapp.ApplyRowRequest) (bulkapp.ApplyRowResult, error) {
	n := atomic.AddInt64(&a.inFlight, 1)
	for {
		cur := atomic.LoadInt64(&a.maxInFlight)
		if n <= cur || atomic.CompareAndSwapInt64(&a.maxInFlight, cur, n) {
			break
		}
	}
	if a.work > 0 {
		time.Sleep(a.work)
	}
	atomic.AddInt64(&a.inFlight, -1)
	return bulkapp.ApplyRowResult{Outcome: bulkapp.OutcomeApplied}, nil
}

func verifyConcurrencyCap(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	const rows, cap = 2000, 8
	jobID := seedSyntheticJob(t, ctx, pool, rows)
	applier := &instrumentedApplier{work: 5 * time.Millisecond}
	repo := bulkpg.NewRepository(pool, 30*time.Second)
	w := bulkapp.NewWorkerService(repo, applier, bulkapp.WorkerSettings{
		Concurrency: cap, RowsPerSecond: 0, Burst: 1, BatchSize: 500, MaxRetries: 2, MaxIterations: 100000, ClaimLease: 30 * time.Second,
	}, discardLogger())
	if _, err := w.RunUntilDrained(ctx, scaleTenant, jobID); err != nil {
		t.Fatalf("concurrency-cap drain: %v", err)
	}
	peak := atomic.LoadInt64(&applier.maxInFlight)
	if peak > cap {
		t.Fatalf("concurrency cap breached: peak in-flight %d > %d", peak, cap)
	}
	if peak < cap {
		t.Fatalf("concurrency cap not reached (peak %d < %d): throttle may not be dispatching in parallel", peak, cap)
	}
	t.Logf("throttle concurrency cap: peak in-flight %d == Concurrency %d (bounded)", peak, cap)
}

func verifyRateCap(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	const rows = 2000
	const rps = 500.0
	const burst = 50
	jobID := seedSyntheticJob(t, ctx, pool, rows)
	applier := &instrumentedApplier{} // no artificial work: the rate limiter is the only brake
	repo := bulkpg.NewRepository(pool, 30*time.Second)
	w := bulkapp.NewWorkerService(repo, applier, bulkapp.WorkerSettings{
		Concurrency: 16, RowsPerSecond: rps, Burst: burst, BatchSize: 500, MaxRetries: 2, MaxIterations: 100000, ClaimLease: 30 * time.Second,
	}, discardLogger())
	start := time.Now()
	if _, err := w.RunUntilDrained(ctx, scaleTenant, jobID); err != nil {
		t.Fatalf("rate-cap drain: %v", err)
	}
	elapsed := time.Since(start)
	// With a token-bucket at `rps` and an initial `burst`, draining N rows cannot
	// take less than ~ (N-burst)/rps. Allow slack for scheduling.
	lowerBound := time.Duration(float64(rows-burst) / rps * float64(time.Second) * 0.7)
	if elapsed < lowerBound {
		t.Fatalf("rate cap not enforced: drained %d rows in %s, faster than the %s floor for %.0f rows/sec", rows, elapsed, lowerBound, rps)
	}
	effective := float64(rows) / elapsed.Seconds()
	if applier.maxInFlight > 16 {
		t.Fatalf("concurrency cap breached during rate test: %d", applier.maxInFlight)
	}
	t.Logf("throttle rate cap: drained %d rows in %s (effective %.0f rows/sec, configured %.0f, floor %s)", rows, elapsed.Round(time.Millisecond), effective, rps, lowerBound.Round(time.Millisecond))
}

// seedSyntheticJob creates a job whose rows point at synthetic (non-existent)
// goat ids with a non-null expected_row_version, so the worker's claim/mark path
// is real but the (fake) applier never has to touch the goats table.
func seedSyntheticJob(t *testing.T, ctx context.Context, pool *pgxpool.Pool, rows int) string {
	t.Helper()
	var jobID string
	if err := pool.QueryRow(ctx, `
INSERT INTO bulk_status_job (tenant_id, actor_id, axis, params, total_rows, state, idempotency_key)
VALUES ($1::uuid, $2::uuid, 'reproductive', '{}'::jsonb, $3, 'pending', NULL)
RETURNING bulk_status_job_id::text`, scaleTenant, scaleActor, rows).Scan(&jobID); err != nil {
		t.Fatalf("insert synthetic job: %v", err)
	}
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	defer conn.Release()
	copied, err := conn.Conn().CopyFrom(ctx,
		pgx.Identifier{"bulk_status_job_row"},
		[]string{"tenant_id", "job_id", "goat_id", "axis", "target", "expected_row_version", "row_state"},
		pgx.CopyFromSlice(rows, func(int) ([]any, error) {
			return []any{scaleTenant, jobID, randomUUID(), "reproductive", targetRepro, int64(1), "pending"}, nil
		}))
	if err != nil {
		t.Fatalf("copy synthetic rows: %v", err)
	}
	if int(copied) != rows {
		t.Fatalf("copied %d synthetic rows, want %d", copied, rows)
	}
	return jobID
}

// ---- seeding + harness --------------------------------------------------------

func copyGoats(t *testing.T, ctx context.Context, pool *pgxpool.Pool, n int) {
	t.Helper()
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	defer conn.Release()
	// display_id is set EXPLICITLY (unique, 7-digit) rather than relying on the
	// goats default next_goat_display_id(). That generator, `'G-' ||
	// lpad(nextval::text, 6, '0')`, TRUNCATES 7+ digit sequence values (Postgres
	// lpad truncates when the input is longer than the width), so it produces a
	// duplicate display_id once the sequence passes 999,999 (e.g. nextval 1000000
	// -> 'G-100000', colliding with nextval 100000). That is a real
	// million-scale identity-foundation bug (reported separately); seeding
	// explicit ids here keeps the bulk-status gate focused on the kernel.
	base := 2_000_000
	copied, err := conn.Conn().CopyFrom(ctx,
		pgx.Identifier{"goats"},
		[]string{"tenant_id", "species", "sex", "lifecycle_status", "reproductive_status", "custodian_party_id", "display_id"},
		pgx.CopyFromSlice(n, func(i int) ([]any, error) {
			return []any{scaleTenant, "goat", "female", "alive", startRepro, scaleParty, fmt.Sprintf("G-%07d", base+i)}, nil
		}))
	if err != nil {
		t.Fatalf("copy goats: %v", err)
	}
	if int(copied) != n {
		t.Fatalf("copied %d goats, want %d", copied, n)
	}
}

func enqueueReproductiveJob(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenant, actor, target string) (string, int) {
	t.Helper()
	// Build the enqueue row set from the seeded goats that still need the change,
	// then enqueue through the REAL Repository.EnqueueJob primitive — the same
	// set-based, chunked, row_version-capturing path the service Commit uses, with
	// a real idempotency key + fingerprint. Proves the actual enqueue path at
	// scale instead of a raw INSERT shortcut. (The public API caps one request at
	// 20k rows and splits large sets across independent jobs; this drives the
	// underlying primitive that each such commit calls.)
	rs, err := pool.Query(ctx, `
SELECT goat_id::text FROM goats
WHERE tenant_id = $1::uuid AND reproductive_status IS DISTINCT FROM $2`, tenant, target)
	if err != nil {
		t.Fatalf("select goats: %v", err)
	}
	enqRows := make([]bulkapp.EnqueueRow, 0, 1024)
	for rs.Next() {
		var id string
		if err := rs.Scan(&id); err != nil {
			rs.Close()
			t.Fatalf("scan goat id: %v", err)
		}
		enqRows = append(enqRows, bulkapp.EnqueueRow{GoatID: id, Target: target})
	}
	rs.Close()
	if err := rs.Err(); err != nil {
		t.Fatalf("iterate goats: %v", err)
	}

	repo := bulkpg.NewRepository(pool, 120*time.Second)
	res, err := repo.EnqueueJob(ctx, bulkapp.EnqueueJobParams{
		TenantID:       tenant,
		ActorID:        actor,
		Axis:           bulkapp.AxisReproductive,
		IdempotencyKey: scaleCommitIdemKey,
		Fingerprint:    scaleCommitFingerprint,
		Rows:           enqRows,
	})
	if err != nil {
		t.Fatalf("enqueue job: %v", err)
	}
	return res.JobID, res.TotalRows
}

func scalarInt(t *testing.T, ctx context.Context, pool *pgxpool.Pool, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(ctx, sql, args...).Scan(&n); err != nil {
		t.Fatalf("query %q: %v", sql, err)
	}
	return n
}

func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func randomUUID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// startTunedPostgres launches a throwaway Postgres with durability knobs relaxed
// (safe: the container is discarded after the run) so the 1M-row seed + drain is
// I/O-bound on nothing but the kernel's own work, and with a higher connection cap.
func startTunedPostgres(t *testing.T) string {
	t.Helper()
	image := os.Getenv("GOATOS_POSTGRES_IMAGE")
	if image == "" {
		image = "postgres:16.9-alpine"
	}
	container := fmt.Sprintf("goatos-bulkstatus-scale-%d", time.Now().UnixNano())
	if err := exec.Command("docker", "run", "--rm", "--name", container,
		"-e", "POSTGRES_PASSWORD=goatos", "-e", "POSTGRES_DB=goatos",
		"-p", "127.0.0.1::5432", "-d", image,
		"-c", "fsync=off", "-c", "synchronous_commit=off", "-c", "full_page_writes=off",
		"-c", "max_connections=200",
	).Run(); err != nil {
		t.Fatalf("docker run: %v", err)
	}
	t.Cleanup(func() { _ = exec.Command("docker", "rm", "-f", container).Run() })
	ready := false
	for i := 0; i < 120; i++ {
		if exec.Command("docker", "exec", container, "pg_isready", "-h", "127.0.0.1", "-U", "postgres", "-d", "goatos").Run() == nil {
			ready = true
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if !ready {
		out, _ := exec.Command("docker", "logs", container).CombinedOutput()
		t.Fatalf("postgres not ready:\n%s", out)
	}
	applyMigrations(t, container)
	return container
}

func openPool(t *testing.T, ctx context.Context, container string, maxConns int32) *pgxpool.Pool {
	t.Helper()
	out, err := exec.Command("docker", "port", container, "5432/tcp").Output()
	if err != nil {
		t.Fatalf("docker port: %v", err)
	}
	parts := strings.Split(strings.TrimSpace(string(out)), ":")
	port := parts[len(parts)-1]
	cfg, err := pgxpool.ParseConfig("postgres://postgres:goatos@127.0.0.1:" + port + "/goatos?sslmode=disable")
	if err != nil {
		t.Fatalf("parse config: %v", err)
	}
	cfg.MaxConns = maxConns
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("ping: %v", err)
	}
	return pool
}

func applyMigrations(t *testing.T, container string) {
	t.Helper()
	root := repoRoot(t)
	migrations, err := filepath.Glob(filepath.Join(root, "backend", "migrations", "postgres", "*.sql"))
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(migrations)
	for _, migration := range migrations {
		sqlBytes, err := os.ReadFile(migration)
		if err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command("docker", "exec", "-i", container, "psql", "-v", "ON_ERROR_STOP=1", "-h", "127.0.0.1", "-U", "postgres", "-d", "goatos")
		cmd.Stdin = strings.NewReader(extractGooseUp(string(sqlBytes)))
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			t.Fatalf("apply %s: %v\n%s", filepath.Base(migration), err, stderr.String())
		}
	}
}

func extractGooseUp(sqlText string) string {
	var out []string
	inUp := false
	for _, line := range strings.Split(sqlText, "\n") {
		if strings.HasPrefix(line, "-- +goose Up") {
			inUp = true
			continue
		}
		if strings.HasPrefix(line, "-- +goose Down") {
			break
		}
		if inUp {
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}

func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(wd, "backend", "migrations", "postgres")); err == nil {
			return wd
		}
		next := filepath.Dir(wd)
		if next == wd {
			t.Fatal("repo root not found")
		}
		wd = next
	}
}
