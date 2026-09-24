package liveload

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	herdpg "github.com/vgoats/goatos/backend/internal/herdsignals/adapters/postgres"
	herdapp "github.com/vgoats/goatos/backend/internal/herdsignals/app"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// TestHerdSignalsRiskClassifierSteadyState measures the classifier's per-minute DB cost at
// steady state: after the initial classification, LOAD_REPORT_PCT% of tags report each simulated
// minute, then one scheduled tick runs. Compares with one forced full pass (RecomputeRisk).
func TestHerdSignalsRiskClassifierSteadyState(t *testing.T) {
	if os.Getenv("GOATOS_HERD_LIVE_LOADTEST") != "1" {
		t.Skip("set GOATOS_HERD_LIVE_LOADTEST=1 to run the live load harness")
	}
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	admin := pgtest.StartPostgres(t, ctx)
	tags := envInt("LOAD_TAGS", 20000)
	pct := envInt("LOAD_REPORT_PCT", 10)
	seed(t, ctx, admin, tags, tags/100)

	cfg := admin.Config().Copy()
	tracer := &countingTracer{}
	cfg.ConnConfig.Tracer = tracer
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	svc := herdapp.NewService(herdpg.NewRepository(pool))

	measure := func(label string, fn func() (int, error)) {
		q0 := tracer.n.Load()
		start := time.Now()
		n, err := fn()
		if err != nil {
			t.Fatalf("%s: %v", label, err)
		}
		fmt.Printf("STEADY tags=%d %-26s tags_classified=%6d queries=%5d wall=%s\n", tags, label, n, tracer.n.Load()-q0, time.Since(start).Round(time.Millisecond))
	}
	measure("initial_full_classification", func() (int, error) { return svc.RecomputeRisk(ctx, loadTenant) })
	for minute := 1; minute <= envInt("LOAD_MINUTES", 8); minute++ {
		// Simulate one minute passing for the classifier's clocks, then this minute's packets.
		if _, err := admin.Exec(ctx, `UPDATE herd_signal_tag_latest SET risk_due_at = risk_due_at - interval '1 minute',
			risk_evaluated_at = risk_evaluated_at - interval '1 minute', last_seen_at = last_seen_at - interval '1 minute'
			WHERE tenant_id = $1`, loadTenant); err != nil {
			t.Fatal(err)
		}
		if _, err := admin.Exec(ctx, `UPDATE herd_signal_tag_latest SET last_seen_at = now(), updated_at = now()
			WHERE tenant_id = $1 AND (hashtext(tag_id || $2::text) & 2147483647) % 100 < $3`, loadTenant, fmt.Sprint(minute), pct); err != nil {
			t.Fatal(err)
		}
		measure(fmt.Sprintf("tick_min%d_%dpct_reported", minute, pct), func() (int, error) {
			st, err := svc.RiskTick(ctx, loadTenant)
			return st.Processed, err
		})
	}
	measure("tick_no_change", func() (int, error) {
		st, err := svc.RiskTick(ctx, loadTenant)
		return st.Processed, err
	})
	// The previous design: every tick re-classified the whole herd.
	if _, err := admin.Exec(ctx, `UPDATE herd_signal_tag_latest SET risk_evaluated_at = NULL WHERE tenant_id = $1`, loadTenant); err != nil {
		t.Fatal(err)
	}
	measure("old_design_full_pass", func() (int, error) {
		st, err := svc.RiskTick(ctx, loadTenant)
		if err != nil {
			return 0, err
		}
		total := st.Processed
		for st.Processed > 0 {
			if st, err = svc.RiskTick(ctx, loadTenant); err != nil {
				return total, err
			}
			total += st.Processed
		}
		return total, nil
	})
}
