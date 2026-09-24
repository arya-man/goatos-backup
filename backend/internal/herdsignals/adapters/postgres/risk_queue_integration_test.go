package postgres

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/herdsignals/domain"
	"github.com/vgoats/goatos/backend/internal/herdsignals/ports"
)

func classifyNone(_ context.Context, tags []domain.TagLatest) ([]ports.TagRisk, error) {
	out := make([]ports.TagRisk, len(tags))
	for i, t := range tags {
		out[i] = ports.TagRisk{TagID: t.TagID}
	}
	return out, nil
}

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
			res, err := repo.ClassifyRiskBatch(ctx, hsiTenant, ports.RiskBatchQueue, 20, classifyNone)
			if err != nil || !res.Locked {
				t.Fatalf("batch: %+v %v", res, err)
			}
			total += res.Processed
			if res.Processed < 20 {
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
	res, err := repo.ClassifyRiskBatch(ctx, hsiTenant, ports.RiskBatchQueue, 20, classifyNone)
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
