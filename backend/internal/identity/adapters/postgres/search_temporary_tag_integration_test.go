package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/identity/ports"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// TestSearchFindsATemporaryTagHoweverItIsTyped (Sales E2E, 2026-09-26): the Herd Register search
// could not find a TEMP- tag. Tags are stored normalized (trimmed, upper-cased); the search compared
// the text as typed, so "temp-cbe-7" or " TEMP-CBE-7 " found nothing. A real round trip on Postgres.
func TestSearchFindsATemporaryTagHoweverItIsTyped(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	repo := NewRepository(pool, 5*time.Second)

	f := seedRelocatePartitionFixture(t, ctx, pool)
	goatID := seedRelocateGoat(t, ctx, pool, f.yashodaShed)
	if _, err := pool.Exec(ctx, `
INSERT INTO goat_identifiers (tenant_id, goat_id, identifier_type, identifier_value, normalized_value, scope_key, is_primary_for_goat, status, valid_from, normalizer_version)
VALUES ($1::uuid, $2::uuid, 'temporary_tag', 'TEMP-CBE-7', 'TEMP-CBE-7', 'global', true, 'active', now(), 'test_v1')`,
		rpTenant, goatID); err != nil {
		t.Fatalf("seed temporary tag: %v", err)
	}
	for _, typed := range []string{"TEMP-CBE-7", "temp-cbe-7", "Temp-Cbe-7"} {
		q := typed
		items, _, err := repo.SearchGoats(ctx, ports.SearchGoatsParams{TenantID: rpTenant, Limit: 10, Query: &q})
		if err != nil {
			t.Fatalf("search %q: %v", typed, err)
		}
		if len(items) != 1 || items[0].GoatID != goatID {
			t.Fatalf("search %q found %d animals, want the one carrying TEMP-CBE-7", typed, len(items))
		}
	}
}
