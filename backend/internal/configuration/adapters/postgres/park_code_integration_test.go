package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/configuration/domain"
	"github.com/vgoats/goatos/backend/internal/configuration/ports"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// A park added on Configuration > Items & settings is recorded against BY ITS CODE (sales, feed
// purchases, purchase loads, the first letters of every kid's tag). So the code must be letters and
// numbers, and once a record stores it the code cannot change -- that record would stop matching
// the park. A code nothing has used yet may still change.
func TestParkCodeIsTagSafeAndFixedOnceInUse(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	pool := pgtest.StartPostgres(t, ctx)
	seedConfigurationFixture(t, ctx, pool)
	repo := NewRepository(pool, 15*time.Second)
	write := func(key string) ports.WriteParams {
		return ports.WriteParams{TenantID: cfgTenant, ActorID: cfgActor, IdempotencyKey: key, TraceID: "trace-" + key}
	}

	var vErr *domain.ValidationError
	if _, err := repo.Create(ctx, write("park-bad"), domain.RegParks, map[string]any{"name": "Hosur", "code": "HSR farm"}); !errors.As(err, &vErr) || vErr.Fields[0].Field != "code" {
		t.Fatalf("a code that cannot start a tag must be refused on code, got %v", err)
	}
	park, err := repo.Create(ctx, write("park-hsr"), domain.RegParks, map[string]any{"name": "Hosur", "code": "hsr"})
	if err != nil {
		t.Fatalf("create park: %v", err)
	}
	renamed, err := repo.Update(ctx, write("park-recode-free"), domain.RegParks, park.ID, map[string]any{"code": "HSR2"}, park.RowVersion)
	if err != nil {
		t.Fatalf("a code nothing uses yet may change: %v", err)
	}

	// A sale recorded at the new park -- which the relaxed sales_deals_farm_check (000428) allows.
	if _, err := pool.Exec(ctx, `
INSERT INTO sales_deals (tenant_id, sale_date, farm, buyer_name, product_type, breed)
VALUES ($1::uuid, DATE '2026-09-20', 'HSR2', 'Irshad', 'Goat', 'Sojat')`, cfgTenant); err != nil {
		t.Fatalf("a sale at a new park must be storable: %v", err)
	}
	if _, err := repo.Update(ctx, write("park-recode-used"), domain.RegParks, park.ID, map[string]any{"code": "HSR3"}, renamed.RowVersion); !errors.As(err, &vErr) || vErr.Fields[0].Code != "in_use" {
		t.Fatalf("recoding a park that sales are stored under must be refused, got %v", err)
	}
	if _, err := repo.Update(ctx, write("park-rename"), domain.RegParks, park.ID, map[string]any{"name": "Hosur farm"}, renamed.RowVersion); err != nil {
		t.Fatalf("renaming (not recoding) a park in use is fine: %v", err)
	}
}
