package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/growthdirector/domain"
	"github.com/vgoats/goatos/backend/internal/growthdirector/ports"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// The sale price is fenced on the PRICE THE DRAWER LOADED (PR #320 review finding): the table is
// append-only and effective-dated, so it has no row_version, and without a fence two editors who
// both opened ₹425 would each land their figure with the second silently overwriting the first.
//
//  1. editor A loads 425 and saves 440 -> lands;
//  2. editor B, who also loaded 425, saves 450 -> ErrAssumptionConflict, and 440 still stands;
//  3. editor A replays the exact same save (loaded 425, new 440) -> a no-op, not a conflict;
//  4. editor B reloads (440) and saves 450 -> lands.
func TestPutAssumptionsFencesSalePriceOnTheLoadedPrice(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	seedFCRFixture(t, ctx, pool)
	repo := NewRepository(pool, 30*time.Second)
	today := time.Now()
	price := func(species string) float64 {
		out, err := repo.GetSalePrices(ctx, gdTenant, today)
		if err != nil {
			t.Fatalf("GetSalePrices: %v", err)
		}
		for _, p := range out.Prices {
			if p.Species == species && p.IsDefault() {
				return p.PricePerKgINR
			}
		}
		t.Fatalf("no %s price", species)
		return 0
	}
	loaded := price("goat")
	f := func(v float64) *float64 { return &v }

	if _, err := repo.PutAssumptions(ctx, gdTenant, gdOperator, today, domain.AssumptionsUpdate{
		SalePrices: []domain.SalePriceUpdate{{Species: "goat", PricePerKgINR: f(440), LoadedPricePerKgINR: f(loaded)}},
	}); err != nil {
		t.Fatalf("editor A: %v", err)
	}
	if got := price("goat"); got != 440 {
		t.Fatalf("editor A's price = %v want 440", got)
	}
	_, err := repo.PutAssumptions(ctx, gdTenant, gdOperator, today, domain.AssumptionsUpdate{
		SalePrices: []domain.SalePriceUpdate{{Species: "goat", PricePerKgINR: f(450), LoadedPricePerKgINR: f(loaded)}},
	})
	if !errors.Is(err, ports.ErrAssumptionConflict) {
		t.Fatalf("editor B on a stale price must conflict, got %v", err)
	}
	if got := price("goat"); got != 440 {
		t.Fatalf("a refused save must change nothing: %v", got)
	}
	if _, err := repo.PutAssumptions(ctx, gdTenant, gdOperator, today, domain.AssumptionsUpdate{
		SalePrices: []domain.SalePriceUpdate{{Species: "goat", PricePerKgINR: f(440), LoadedPricePerKgINR: f(loaded)}},
	}); err != nil {
		t.Fatalf("an exact replay must be a no-op, got %v", err)
	}
	if _, err := repo.PutAssumptions(ctx, gdTenant, gdOperator, today, domain.AssumptionsUpdate{
		SalePrices: []domain.SalePriceUpdate{{Species: "goat", PricePerKgINR: f(450), LoadedPricePerKgINR: f(440)}},
	}); err != nil {
		t.Fatalf("editor B after reloading: %v", err)
	}
	if got := price("goat"); got != 450 {
		t.Fatalf("price after reload+save = %v want 450", got)
	}
	// A drawer that loaded NO price (nil) is stale the moment a row exists.
	_, err = repo.PutAssumptions(ctx, gdTenant, gdOperator, today, domain.AssumptionsUpdate{
		SalePrices: []domain.SalePriceUpdate{{Species: "goat", PricePerKgINR: f(460)}},
	})
	if !errors.Is(err, ports.ErrAssumptionConflict) {
		t.Fatalf("a save that loaded no price must conflict once one exists, got %v", err)
	}
}

func TestPutAssumptionsRejectsSaleReadyLineInversionInsideTheTransaction(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	seedFCRFixture(t, ctx, pool)
	repo := NewRepository(pool, 30*time.Second)
	today := time.Now()

	assumptions, err := repo.GetAssumptions(ctx, gdTenant, today)
	if err != nil {
		t.Fatalf("GetAssumptions: %v", err)
	}
	var lowerVersion int
	for _, row := range assumptions.Values {
		if row.Key == domain.AssumptionSaleReadyLowerKg {
			lowerVersion = row.RowVersion
		}
	}
	if lowerVersion == 0 {
		t.Fatal("missing sale_ready_lower_kg row")
	}

	_, err = repo.PutAssumptions(ctx, gdTenant, gdOperator, today, domain.AssumptionsUpdate{
		Values: []domain.ValueUpdate{{Key: domain.AssumptionSaleReadyLowerKg, Value: 36, RowVersion: lowerVersion}},
	})
	if !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("inverted sale lines must be refused in the repository transaction, got %v", err)
	}
	after, err := repo.GetAssumptions(ctx, gdTenant, today)
	if err != nil {
		t.Fatalf("GetAssumptions after rejected write: %v", err)
	}
	for _, row := range after.Values {
		if row.Key == domain.AssumptionSaleReadyLowerKg && row.Value != domain.DefaultSaleReadyLowerKg {
			t.Fatalf("rejected write changed lower line to %v", row.Value)
		}
	}
}
