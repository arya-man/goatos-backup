package postgres

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/herdsignals/domain"
	"github.com/vgoats/goatos/backend/internal/herdsignals/ports"
)

// N1: the queue holds only never-classified tags, tags seen since their last evaluation, and
// tags that went stale after it; everything else is left alone. Another instance holding the
// tenant's xact lock makes the batch a no-op.
func TestClassifyRiskBatchQueueIsChangeDriven(t *testing.T) {
	ctx := context.Background()
	repo, pool := setupHerdSignalsDB(t, ctx)
	if _, err := pool.Exec(ctx, fmt.Sprintf(`
		INSERT INTO herd_signal_tag_latest (tenant_id, tag_id, last_seen_at, mapping_state, gap_delta)
		SELECT '%s', 'Q' || g, now() - interval '1 minute', 'unmapped', false FROM generate_series(1, 50) g`, hsiTenant)); err != nil {
		t.Fatal(err)
	}
	drain := func() int {
		total := 0
		for {
			res, err := repo.ClassifyRiskBatch(ctx, hsiTenant, ports.RiskBatchQueue, 20, time.Minute)
			if err != nil || !res.Locked {
				t.Fatalf("batch: %+v %v", res, err)
			}
			total += res.Processed
			if res.Picked < 20 {
				return total
			}
		}
	}
	if got := drain(); got != 50 {
		t.Fatalf("first drain classified %d, want all 50 never-classified", got)
	}
	if got := drain(); got != 0 {
		t.Fatalf("steady-state drain classified %d, want 0", got)
	}
	// 3 tags report again inside the 5-minute re-evaluation floor; 1 tag goes stale after its
	// evaluation (no new packet): only the stale one is due now.
	if _, err := pool.Exec(ctx, `UPDATE herd_signal_tag_latest SET last_seen_at = now() + interval '1 second' WHERE tenant_id = $1 AND tag_id IN ('Q1','Q2','Q3')`, hsiTenant); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE herd_signal_tag_latest SET last_seen_at = now() - interval '40 minutes', risk_evaluated_at = now() - interval '39 minutes' WHERE tenant_id = $1 AND tag_id = 'Q9'`, hsiTenant); err != nil {
		t.Fatal(err)
	}
	if got := drain(); got != 1 {
		t.Fatalf("drain inside the re-evaluation floor classified %d, want only the stale tag", got)
	}
	// Past the floor, the 3 reporting tags are due.
	if _, err := pool.Exec(ctx, `UPDATE herd_signal_tag_latest SET risk_due_at = now() - interval '1 second' WHERE tenant_id = $1 AND tag_id IN ('Q1','Q2','Q3')`, hsiTenant); err != nil {
		t.Fatal(err)
	}
	if got := drain(); got != 3 {
		t.Fatalf("drain after the floor classified %d, want the 3 reporting tags", got)
	}

	// Another instance holds the tenant lock: the batch must not touch anything.
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if _, err := tx.Exec(ctx, riskTryLockSQL, riskClassifierLockClass, hsiTenant); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE herd_signal_tag_latest SET risk_evaluated_at = NULL WHERE tenant_id = $1 AND tag_id = 'Q5'`, hsiTenant); err != nil {
		t.Fatal(err)
	}
	res, err := repo.ClassifyRiskBatch(ctx, hsiTenant, ports.RiskBatchQueue, 20, time.Minute)
	if err != nil || res.Locked || res.Processed != 0 {
		t.Fatalf("batch under another instance's lock: %+v %v, want not locked / 0", res, err)
	}
}

// N6/N7: migrations follow the repo's lock-safe conventions and the classifier lock is
// transaction-scoped (it can never be returned to the pool held).
func TestRiskMigrationAndLockConventions(t *testing.T) {
	read := func(p string) string {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	m402 := read("../../../../migrations/postgres/000402_herd_signals_persisted_risk.sql")
	if !strings.Contains(m402, "NOT VALID;") || !strings.Contains(m402, "VALIDATE CONSTRAINT herd_signal_tag_latest_risk_state_check") || strings.Contains(m402, "CREATE INDEX") {
		t.Fatalf("000402 must add the CHECK NOT VALID + VALIDATE and create no index inline")
	}
	m404 := read("../../../../migrations/postgres/000404_herd_signals_risk_indexes.sql")
	if !strings.Contains(m404, "-- +goose NO TRANSACTION") || strings.Count(m404, "CREATE INDEX CONCURRENTLY") != 4 {
		t.Fatalf("000404 must build the 4 risk indexes CONCURRENTLY in a no-transaction migration")
	}
	src := read("risk.go")
	if !strings.Contains(src, "pg_try_advisory_xact_lock") || strings.Contains(src, "pg_advisory_unlock") || strings.Contains(src, ".Acquire(") {
		t.Fatalf("classifier must use a transaction-scoped advisory lock, never a session lock on a held conn")
	}
}

// R1: the classifier holds no row locks while it scores a batch, so an ingest upsert landing
// mid-batch is not blocked; the optimistic guard then leaves that tag unwritten and queued.
func TestClassifierBatchDoesNotBlockIngest(t *testing.T) {
	ctx := context.Background()
	repo, pool := setupHerdSignalsDB(t, ctx)
	if _, err := pool.Exec(ctx, fmt.Sprintf(`
		INSERT INTO herd_signal_tag_latest (tenant_id, tag_id, last_seen_at, mapping_state, gap_delta)
		SELECT '%s', 'R' || g, now() - interval '1 minute', 'unmapped', false FROM generate_series(1, 10) g`, hsiTenant)); err != nil {
		t.Fatal(err)
	}
	var ingestErr error
	var ingestTook time.Duration
	riskBatchScoredHook = func() {
		c, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		start := time.Now()
		_, ingestErr = pool.Exec(c, `UPDATE herd_signal_tag_latest SET last_seen_at = now(), motion_delta = 5 WHERE tenant_id = $1 AND tag_id = 'R3'`, hsiTenant)
		ingestTook = time.Since(start)
	}
	defer func() { riskBatchScoredHook = nil }()
	res, err := repo.ClassifyRiskBatch(ctx, hsiTenant, ports.RiskBatchQueue, 50, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if ingestErr != nil {
		t.Fatalf("ingest upsert during a classifier batch failed/blocked after %s: %v", ingestTook, ingestErr)
	}
	if res.Picked != 10 || res.Processed != 9 {
		t.Fatalf("batch picked %d wrote %d, want 10 scored / 9 written (R3 changed mid-batch)", res.Picked, res.Processed)
	}
	var evaluated *time.Time
	if err := pool.QueryRow(ctx, `SELECT risk_evaluated_at FROM herd_signal_tag_latest WHERE tenant_id = $1 AND tag_id = 'R3'`, hsiTenant).Scan(&evaluated); err != nil {
		t.Fatal(err)
	}
	if evaluated != nil {
		t.Fatalf("R3 changed after scoring but was written anyway (evaluated %v)", evaluated)
	}
}

// R2: ingest pulls risk_due_at forward (to just below the new last_seen_at, so the next tick
// picks the tag up) when the movement/pattern state changes, and leaves it alone otherwise.
func TestIngestPullsRiskDueForwardOnStateChange(t *testing.T) {
	ctx := context.Background()
	repo, pool := setupHerdSignalsDB(t, ctx)
	gw := domain.Gateway{TenantID: hsiTenant, GatewayID: "gw-r2", Status: "active"}
	base := time.Now().UTC().Add(-40 * time.Minute).Truncate(time.Second)
	ingest := func(at time.Time, count int64) {
		if _, _, err := repo.IngestPackets(ctx, hsiTenant, gw, []domain.Packet{makePacket(hsiTenant, hsiMappedTag, hsiMappedMAC, "gw-r2", at, count, -60)}); err != nil {
			t.Fatal(err)
		}
	}
	state := func() (string, string, time.Time, time.Time) {
		var mv, pat string
		var seen, due time.Time
		if err := pool.QueryRow(ctx, `SELECT movement_state, pattern_state, last_seen_at, risk_due_at FROM herd_signal_tag_latest WHERE tenant_id = $1 AND tag_id = $2`, hsiTenant, hsiMappedTag).Scan(&mv, &pat, &seen, &due); err != nil {
			t.Fatal(err)
		}
		return mv, pat, seen, due
	}
	classify := func() {
		if _, err := repo.ClassifyRiskBatch(ctx, hsiTenant, ports.RiskBatchQueue, 50, time.Hour); err != nil {
			t.Fatal(err)
		}
	}
	ingest(base, 1000)
	ingest(base.Add(5*time.Minute), 1000)
	classify()
	mv0, pat0, _, due0 := state()
	// Same state again: the far-future due time must stand.
	ingest(base.Add(10*time.Minute), 1000)
	if mv, pat, seen, due := state(); mv == mv0 && pat == pat0 && (!due.Equal(due0) || seen.After(due)) {
		t.Fatalf("unchanged state moved risk_due_at %v -> %v", due0, due)
	}
	// A burst of movement changes the movement state: due is pulled below the new last_seen.
	ingest(base.Add(15*time.Minute), 5000)
	mv1, pat1, seen1, due1 := state()
	if mv1 == mv0 && pat1 == pat0 {
		t.Fatalf("fixture did not change state (%s/%s)", mv1, pat1)
	}
	if !seen1.After(due1) {
		t.Fatalf("state %s/%s -> %s/%s left risk_due_at %v >= last_seen %v; the tag is not re-queued", mv0, pat0, mv1, pat1, due1, seen1)
	}
}
