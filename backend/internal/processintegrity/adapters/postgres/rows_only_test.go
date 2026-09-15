package postgres

import (
	"context"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/processintegrity/domain"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestCanonicalMembershipCannotBeReplayedPerShed(t *testing.T) {
	// CPT's 125-shed plan replayed the obligation query 125 times before this
	// optimization fence (229,500 index probes / 11.48s for 11 board rows).
	for name, sql := range map[string]string{"rows": processIntegrityCanonicalRowsSQL, "counts": processIntegrityCanonicalCountsSQL, "adherence": processIntegrityCanonicalAdherenceSummarySQL} {
		if !strings.Contains(sql, "located AS MATERIALIZED (") {
			t.Fatalf("%s must evaluate canonical membership once before dimension joins", name)
		}
	}
}

func TestListRowsOnlyPreservesCanonicalPaginationWithoutPoisoningSummary(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedProcessIntegrityProjection(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)
	q := domain.Query{TenantID: piTenant, AsOf: time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC), DueBefore: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), Limit: 1}
	total := 0
	for page := 0; page < 20; page++ {
		only, err := repo.ListRowsOnly(ctx, q)
		if err != nil {
			t.Fatal(err)
		}
		full, err := repo.ListRows(ctx, q)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(only.Rows, full.Rows) || !reflect.DeepEqual(only.NextCursor, full.NextCursor) {
			t.Fatalf("page %d rows/cursor changed", page)
		}
		if len(only.CountsByWorkState) != 0 || only.TotalCount != 0 {
			t.Fatal("rows-only result incorrectly promises aggregate metadata")
		}
		if len(full.CountsByWorkState) == 0 || full.TotalCount == 0 {
			t.Fatal("rows-only read poisoned full summary cache")
		}
		total += len(only.Rows)
		if only.NextCursor == nil {
			if total < 2 {
				t.Fatalf("expected multiple row pages, got %d", total)
			}
			return
		}
		cursor, err := domain.DecodeCursor(*only.NextCursor)
		if err != nil {
			t.Fatal(err)
		}
		q.Cursor = &cursor
	}
	t.Fatal("cursor walk did not terminate")
}
