package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/identity/ports"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	salesidentitybridge "github.com/vgoats/goatos/backend/internal/sales/adapters/identitybridge"
	salespg "github.com/vgoats/goatos/backend/internal/sales/adapters/postgres"
	salesdomain "github.com/vgoats/goatos/backend/internal/sales/domain"
	salesports "github.com/vgoats/goatos/backend/internal/sales/ports"
)

// A FAILED SALE (maintainer decision 2026-09-25, docs/decisions/sales-sop.md -> "A failed sale"),
// across the two modules on real Postgres: the tagging confirm (identity) and the status change
// (sales) lock the same deal row, so a sale with animals tagged to it can never be marked failed
// and a failed sale can never have animals tagged to it.

const saleDealFailed = "77777777-7777-4777-8777-7777777777f1"

// startSaleWriteDBOnPgtest is startCorrectionWriteDB on the shared pgtest harness (a migrated
// throwaway database; OCI admin DSN or Docker), seeded with the same synthetic fixture.
func startSaleWriteDBOnPgtest(t *testing.T, ctx context.Context) (*pgxpool.Pool, *Repository) {
	t.Helper()
	pgtest.SkipIfNoDocker(t)
	pool := pgtest.StartPostgres(t, ctx)
	t.Cleanup(pool.Close)
	if _, err := pool.Exec(ctx, repositorySeedSQL()); err != nil {
		t.Fatalf("seed synthetic fixture: %v", err)
	}
	return pool, NewRepository(pool, 5*time.Second)
}

func statusChangedEvents(t *testing.T, ctx context.Context, pool *pgxpool.Pool, dealID string) []string {
	t.Helper()
	rows, err := pool.Query(ctx, `
SELECT payload->'payload'->>'status'
FROM outbox_messages
WHERE tenant_id = $1::uuid AND event_type = 'sales.deal.status_changed' AND aggregate_id = $2::uuid
ORDER BY created_at`, ssTenant, dealID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatal(err)
		}
		out = append(out, s)
	}
	return out
}

func dealStatus(t *testing.T, ctx context.Context, pool *pgxpool.Pool, dealID string) string {
	t.Helper()
	var s string
	if err := pool.QueryRow(ctx, `SELECT status FROM sales_deals WHERE tenant_id = $1::uuid AND id = $2::uuid`, ssTenant, dealID).Scan(&s); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestASaleWithTaggedAnimalsCannotBeMarkedFailed(t *testing.T) {
	ctx := context.Background()
	pool, repo := startSaleWriteDBOnPgtest(t, ctx)
	f := seedShedStageFixture(t, ctx, pool)
	one := seedStageGoat(t, ctx, pool, f.castroShed, "1", "F2", "adult")
	seedSaleAllocationDeal(t, ctx, pool, saleDealA, 1)
	if _, err := repo.RecordSaleAllocations(ctx, saleAllocCmd(saleDealA, []ports.SaleAllocationRow{
		{GoatID: one, RowVersion: goatRowVersion(t, pool, one)},
	}, "fail-tagged")); err != nil {
		t.Fatalf("tag: %v", err)
	}

	sales := salespg.NewRepository(pool, 15*time.Second).WithTaggedAnimals(salesidentitybridge.New(pool))
	_, err := sales.SetDealStatus(ctx, ssTenant, saleDealA, salesdomain.StatusDealFailed, "")
	var tagged salesports.ErrDealHasTaggedAnimals
	if !errors.As(err, &tagged) || tagged.Count != 1 {
		t.Fatalf("marking a sale with a tagged animal failed must be refused with the count, got %v", err)
	}
	if got := dealStatus(t, ctx, pool, saleDealA); got != salesdomain.StatusDealClosed {
		t.Fatalf("a refused change must leave the status, got %q", got)
	}
	if ev := statusChangedEvents(t, ctx, pool, saleDealA); len(ev) != 0 {
		t.Fatalf("a refused change must emit nothing, got %v", ev)
	}
	if got := goatLifecycle(t, ctx, pool, one); got != "sold" {
		t.Fatalf("the tagged animal must stay sold, got %q", got)
	}
	// Any other status change still goes through and is announced.
	if _, err := sales.SetDealStatus(ctx, ssTenant, saleDealA, salesdomain.StatusAdvancePaid, ""); err != nil {
		t.Fatalf("a non-failed change: %v", err)
	}
	if ev := statusChangedEvents(t, ctx, pool, saleDealA); len(ev) != 1 || ev[0] != salesdomain.StatusAdvancePaid {
		t.Fatalf("status_changed events = %v, want [Advance Paid]", ev)
	}
}

func TestAFailedSaleEmitsItsEventAndTakesNoAnimals(t *testing.T) {
	ctx := context.Background()
	pool, repo := startSaleWriteDBOnPgtest(t, ctx)
	f := seedShedStageFixture(t, ctx, pool)
	one := seedStageGoat(t, ctx, pool, f.castroShed, "1", "F2", "adult")
	seedSaleAllocationDeal(t, ctx, pool, saleDealFailed, 1)

	sales := salespg.NewRepository(pool, 15*time.Second).WithTaggedAnimals(salesidentitybridge.New(pool))
	for i := 0; i < 2; i++ { // the second is a no-op change: no second event
		if _, err := sales.SetDealStatus(ctx, ssTenant, saleDealFailed, salesdomain.StatusDealFailed, ""); err != nil {
			t.Fatalf("mark failed (pass %d): %v", i+1, err)
		}
	}
	if ev := statusChangedEvents(t, ctx, pool, saleDealFailed); len(ev) != 1 || ev[0] != salesdomain.StatusDealFailed {
		t.Fatalf("status_changed events = %v, want exactly one Deal Failed", ev)
	}
	_, err := repo.RecordSaleAllocations(ctx, saleAllocCmd(saleDealFailed, []ports.SaleAllocationRow{
		{GoatID: one, RowVersion: goatRowVersion(t, pool, one)},
	}, "tag-failed"))
	if !errors.Is(err, ports.ErrSaleDealFailed) {
		t.Fatalf("tagging onto a failed sale must be refused, got %v", err)
	}
	if got := goatLifecycle(t, ctx, pool, one); got != "alive" {
		t.Fatalf("a refused tagging must leave the animal in the herd, got %q", got)
	}
}
