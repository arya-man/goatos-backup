package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/configuration/domain"
	"github.com/vgoats/goatos/backend/internal/configuration/ports"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/platform/readcache/readcachetest"
)

// Register writes (stores_places: pens/partitions/locations; stores_animals: stage vocabulary and
// animals) change inputs of the shared analytics read cache. Every register write goes through one
// transaction helper; each shape is exercised: create, update, status.
func TestRegisterWritesEvictTheSharedReadCacheOnBothInstances(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pool := pgtest.StartPostgres(t, ctx)
	seedConfigurationFixture(t, ctx, pool)
	pair := readcachetest.NewPair(t, ctx, pool)
	repo := NewRepository(pool, 15*time.Second).WithReadCacheInvalidator(pair.Writer)
	write := func(key string) ports.WriteParams {
		return ports.WriteParams{TenantID: cfgTenant, ActorID: cfgActor, IdempotencyKey: key, TraceID: "trace-" + key}
	}
	parks := []string{cfgParkCBE}

	var pen domain.Row
	pair.Check(t, ctx, "configuration Create pen (stores_places)", cfgTenant, parks, true, func(t *testing.T) {
		var err error
		if pen, err = repo.Create(ctx, write("ryw-pen"), domain.RegPens, map[string]any{"park_id": cfgParkCBE, "name": "Ryw Pen"}); err != nil {
			t.Fatalf("create pen: %v", err)
		}
	})
	pair.Check(t, ctx, "configuration Update pen (stores_places)", cfgTenant, parks, true, func(t *testing.T) {
		if _, err := repo.Update(ctx, write("ryw-pen-rename"), domain.RegPens, pen.ID, map[string]any{"name": "Ryw Pen Renamed"}, pen.RowVersion); err != nil {
			t.Fatalf("update pen: %v", err)
		}
	})
	pair.Check(t, ctx, "configuration Create partition (stores_places)", cfgTenant, parks, true, func(t *testing.T) {
		if _, err := repo.Create(ctx, write("ryw-part"), domain.RegPartitions, map[string]any{"park_id": cfgParkCBE, "pen_id": pen.ID, "label": "Part 7", "sort_order": int64(7), "capacity": int64(50)}); err != nil {
			t.Fatalf("create partition: %v", err)
		}
	})
	pair.Check(t, ctx, "configuration Create stage (stores_animals)", cfgTenant, parks, true, func(t *testing.T) {
		if _, err := repo.Create(ctx, write("ryw-stage"), domain.RegStages, map[string]any{"code": "RYW1", "name": "Ryw Stage"}); err != nil {
			t.Fatalf("create stage: %v", err)
		}
	})
}
