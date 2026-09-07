package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/feedwaterremoval/ports"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

const readerTenant = "7f000000-0000-4000-8000-0000000000fe"

// The reader is the ONLY place that names feed_water_removal_config. Proved on
// a migrated database: migration 000276 leaves no tenant without a row; a
// tenant created AFTER it has no row and is refused rather than defaulted;
// and an UPDATE is read on the very next call — there is no cache to stale.
func TestReaderReadsTheConfiguredCutoffAndRefusesAnUnconfiguredTenant(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	reader := NewReader(pool, 5*time.Second)

	var orphans int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM tenants t
LEFT JOIN feed_water_removal_config c ON c.tenant_id = t.tenant_id
WHERE c.tenant_id IS NULL`).Scan(&orphans); err != nil {
		t.Fatalf("count unseeded tenants: %v", err)
	}
	if orphans != 0 {
		t.Fatalf("migration 000276 left %d tenant(s) without a cutoff row", orphans)
	}

	if _, err := pool.Exec(ctx, `INSERT INTO tenants (tenant_id, name, status) VALUES ($1::uuid, 'Cutoff Test', 'active')
ON CONFLICT (tenant_id) DO NOTHING`, readerTenant); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}
	if _, err := reader.FeedWaterRemovalCutoff(ctx, readerTenant); !errors.Is(err, ports.ErrCutoffNotConfigured) {
		t.Fatalf("unconfigured tenant err = %v, want ErrCutoffNotConfigured (never a literal default)", err)
	}

	if _, err := pool.Exec(ctx, `INSERT INTO feed_water_removal_config (tenant_id, cutoff_time) VALUES ($1::uuid, TIME '20:00')`, readerTenant); err != nil {
		t.Fatalf("seed cutoff: %v", err)
	}
	cutoff, err := reader.FeedWaterRemovalCutoff(ctx, readerTenant)
	if err != nil {
		t.Fatalf("read seeded cutoff: %v", err)
	}
	if !cutoff.Valid() || cutoff.String() != "20:00" || cutoff.SQLTime() != "20:00:00" {
		t.Fatalf("seeded cutoff = %s valid=%v, want 20:00", cutoff, cutoff.Valid())
	}

	// A change is live on the next call: the farm moves its evening to 21:30
	// and every subsequent plan/list reads 21:30, minute for minute.
	if _, err := pool.Exec(ctx, `UPDATE feed_water_removal_config SET cutoff_time = TIME '21:30', updated_at = now() WHERE tenant_id = $1::uuid`, readerTenant); err != nil {
		t.Fatalf("update cutoff: %v", err)
	}
	cutoff, err = reader.FeedWaterRemovalCutoff(ctx, readerTenant)
	if err != nil {
		t.Fatalf("read updated cutoff: %v", err)
	}
	if cutoff.Hour() != 21 || cutoff.Minute() != 30 {
		t.Fatalf("updated cutoff = %s, want 21:30", cutoff)
	}
}
