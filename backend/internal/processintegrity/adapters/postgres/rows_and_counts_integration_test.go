package postgres

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/processintegrity/domain"
)

// TestCanonicalRowsAndCountsMatchTheTwoReadsItReplaces: the combined page+counts statement returns
// exactly the separate LIST page and per-work_state COUNT, across page sizes (keyset boundary,
// extra row), a cursor past the last row (empty page must still carry the counts) and the
// include-completed flag.
func TestCanonicalRowsAndCountsMatchTheTwoReadsItReplaces(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedProcessIntegrityProjection(t, ctx, pool)
	execPI(t, ctx, pool, "drive assignment first partition",
		`INSERT INTO vaccination_drive_assignments (tenant_id, batch_id, planned_date, operator_id, park_id, shed_id, physical_shed, partition_label, animal_count)
		 VALUES ($1, $2, DATE '2026-06-24', $3, $4, $5, 'Process Shed', '1', 1)`,
		piTenant, piBatch, piOperator, piPark, piShed)
	execPI(t, ctx, pool, "drive assignment second partition",
		`INSERT INTO vaccination_drive_assignments (tenant_id, batch_id, planned_date, operator_id, park_id, shed_id, physical_shed, partition_label, animal_count)
		 VALUES ($1, $2, DATE '2026-06-25', $3, $4, $5, 'Process Shed', '2', 1)`,
		piTenant, piBatch, piParkHead, piPark, piShed)
	repo := NewRepository(pool, 5*time.Second)

	nonEmpty := false
	for _, includeCompleted := range []bool{false, true} {
		for _, limit := range []int{1, 2, 10} {
			q := normalizeQuery(domain.Query{
				TenantID:         piTenant,
				AsOf:             time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC),
				DueBefore:        time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
				Limit:            limit,
				IncludeCompleted: includeCompleted,
			})
			args := queryArgs(q)
			wantRows, wantCursor, wantExtra, err := repo.fetchCanonicalRows(ctx, q, args)
			if err != nil {
				t.Fatal(err)
			}
			wantCounts, wantTotal, err := repo.countByWorkStateCanonical(ctx, countQueryArgs(args))
			if err != nil {
				t.Fatal(err)
			}
			gotRows, gotCursor, gotExtra, gotCounts, gotTotal, err := repo.fetchCanonicalRowsAndCounts(ctx, q, args)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(gotRows, wantRows) || !reflect.DeepEqual(gotCursor, wantCursor) || gotExtra != wantExtra {
				t.Fatalf("limit=%d completed=%v page differs:\n got %+v %+v %v\nwant %+v %+v %v", limit, includeCompleted, gotRows, gotCursor, gotExtra, wantRows, wantCursor, wantExtra)
			}
			if !reflect.DeepEqual(gotCounts, wantCounts) || gotTotal != wantTotal {
				t.Fatalf("limit=%d completed=%v counts differ: got %+v/%d want %+v/%d", limit, includeCompleted, gotCounts, gotTotal, wantCounts, wantTotal)
			}
			if len(wantRows) > 0 {
				nonEmpty = true
			}
			if wantCursor == nil {
				continue
			}
			// A cursor past the last row: the page is empty but the counts must survive.
			past := q
			past.Cursor = &domain.Cursor{SortPriority: 1 << 30, DueAt: wantCursor.DueAt, RowID: "~"}
			pastArgs := queryArgs(past)
			emptyRows, _, _, emptyCounts, emptyTotal, err := repo.fetchCanonicalRowsAndCounts(ctx, past, pastArgs)
			if err != nil {
				t.Fatal(err)
			}
			if len(emptyRows) != 0 || !reflect.DeepEqual(emptyCounts, wantCounts) || emptyTotal != wantTotal {
				t.Fatalf("empty page lost counts: rows=%d counts=%+v/%d want %+v/%d", len(emptyRows), emptyCounts, emptyTotal, wantCounts, wantTotal)
			}
		}
	}
	if !nonEmpty {
		t.Fatal("seed produced no rows; the equivalence check proved nothing")
	}
}
