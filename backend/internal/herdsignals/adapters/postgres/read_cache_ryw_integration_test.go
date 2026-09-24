package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/herdsignals/domain"
	"github.com/vgoats/goatos/backend/internal/platform/readcache/readcachetest"
)

// Tag mapping writes goat_identifiers, which the shared analytics read cache joins by tag: bind,
// replace and unmap must each make the writer's next cached read fresh at once and a sibling
// instance's within a second.
func TestTagMappingWritesEvictTheSharedReadCacheOnBothInstances(t *testing.T) {
	ctx := context.Background()
	repo, pool := setupMappingDB(t, ctx)
	seedSecondGoat(t, ctx, pool)
	pair := readcachetest.NewPair(t, ctx, pool)
	repo = repo.WithReadCacheInvalidator(pair.Writer)
	parks := []string{hsiPark}

	ingestTag(t, ctx, repo, "gw-hsm-ryw", hsmTagA, hsmTagAMAC, time.Now().UTC().Add(-3*time.Minute), 100)
	ingestTag(t, ctx, repo, "gw-hsm-ryw", hsmTagB, hsmTagBMAC, time.Now().UTC().Add(-3*time.Minute), 200)

	pair.Check(t, ctx, "herdsignals BindTagMapping", hsiTenant, parks, true, func(t *testing.T) {
		if _, err := repo.BindTagMapping(ctx, hsiTenant, hsiActor, domain.BindTagMappingRequest{GoatID: hsmGoatB, TagID: hsmTagA, TagMAC: hsmTagAMAC}); err != nil {
			t.Fatalf("BindTagMapping: %v", err)
		}
	})
	time.Sleep(5 * time.Millisecond)
	pair.Check(t, ctx, "herdsignals ReplaceTagMapping", hsiTenant, parks, true, func(t *testing.T) {
		if _, err := repo.ReplaceTagMapping(ctx, hsiTenant, hsiActor, domain.ReplaceTagMappingRequest{GoatID: hsmGoatB, NewTagID: hsmTagB, NewTagMAC: hsmTagBMAC}); err != nil {
			t.Fatalf("ReplaceTagMapping: %v", err)
		}
	})
	pair.Check(t, ctx, "herdsignals UnmapTagMapping", hsiTenant, parks, true, func(t *testing.T) {
		if _, err := repo.UnmapTagMapping(ctx, hsiTenant, hsiActor, domain.UnmapTagMappingRequest{TagID: hsmTagB, TagMAC: hsmTagBMAC}); err != nil {
			t.Fatalf("UnmapTagMapping: %v", err)
		}
	})
}
